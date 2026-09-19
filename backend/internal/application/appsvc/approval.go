package appsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// ApprovalService 编排审批请求的创建、查询、批准与拒绝.
//
// 提案创建与批准执行都在单个事务内完成: 领域服务写方法检测到 ctx
// 已携带事务时直接复用, 因此"校验 targets — 执行领域修改 — 落审批状态"
// 是一个原子单元. Web 批准走这里; CLI 提案由 HTTP 层在幂等事务内调用.
type ApprovalService struct {
	db         *gorm.DB
	approvals  *repo.ApprovalRepository
	topics     *repo.TopicRepository
	cards      *repo.CardRepository
	glossaries *repo.GlossaryRepository

	topicSvc    *TopicService
	cardSvc     *CardService
	glossarySvc *GlossaryService

	logger *slog.Logger
	now    func() time.Time
}

// NewApprovalService 构造审批服务.
func NewApprovalService(
	db *gorm.DB,
	approvals *repo.ApprovalRepository,
	topics *repo.TopicRepository,
	cards *repo.CardRepository,
	glossaries *repo.GlossaryRepository,
	topicSvc *TopicService,
	cardSvc *CardService,
	glossarySvc *GlossaryService,
	logger *slog.Logger,
) *ApprovalService {
	return &ApprovalService{
		db:          db,
		approvals:   approvals,
		topics:      topics,
		cards:       cards,
		glossaries:  glossaries,
		topicSvc:    topicSvc,
		cardSvc:     cardSvc,
		glossarySvc: glossarySvc,
		logger:      logger,
		now:         func() time.Time { return persistence.Now() },
	}
}

// ProposalSpec 是一次 CLI 写操作转成审批提案所需的全部信息.
// EntityID 是操作主对象 ID, 创建类为 nil; Payload 是原始请求 JSON.
type ProposalSpec struct {
	Operation  int16
	EntityType int16
	EntityID   *int64
	Payload    []byte
}

// CreateProposals 在一个事务中为每个项目创建独立审批请求.
// 任一项校验或目标解析失败则整批不创建, 错误携带 details.index.
func (s *ApprovalService) CreateProposals(
	ctx context.Context,
	apiKeyID int64,
	specs []ProposalSpec,
) ([]model.ApprovalRequest, error) {
	if len(specs) == 0 {
		return nil, apperr.Validation("items 不能为空").
			WithDetails(map[string]any{"field": "items"})
	}
	if len(specs) > BatchMaxItems {
		return nil, apperr.Newf(apperr.CodeValidationError, "批量项目数超过上限 %d", BatchMaxItems).
			WithDetails(map[string]any{"field": "items"})
	}
	created := make([]model.ApprovalRequest, 0, len(specs))
	err := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		for i, spec := range specs {
			req, err := s.createOne(innerCtx, tx, apiKeyID, spec)
			if err != nil {
				if len(specs) == 1 {
					return err
				}
				return withBatchIndex(err, i)
			}
			created = append(created, *req)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// createOne 在事务中解析目标并写入一条审批请求与它的 targets.
func (s *ApprovalService) createOne(
	ctx context.Context,
	tx *gorm.DB,
	apiKeyID int64,
	spec ProposalSpec,
) (*model.ApprovalRequest, error) {
	targets, err := s.prepare(ctx, tx, spec)
	if err != nil {
		return nil, err
	}
	now := s.now()
	keyID := apiKeyID
	req := &model.ApprovalRequest{
		Operation:           spec.Operation,
		EntityType:          spec.EntityType,
		Status:              model.ApprovalPending,
		RequestedByAPIKeyID: &keyID,
		// 原样保存 CLI 提交的 JSON, 不重编码, 保证"原始 payload"可在
		// 审批界面原封不动地展示与对比.
		OriginalPayload: string(spec.Payload),
		CreatedAt:       now,
	}
	approvals := s.approvals.WithTx(tx)
	if err := approvals.Create(ctx, req); err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i].ApprovalRequestID = req.ID
		targets[i].CreatedAt = now
	}
	if err := approvals.CreateTargets(ctx, targets); err != nil {
		return nil, err
	}
	return req, nil
}

// ApprovalDetail 是审批请求详情: 请求行加全部 targets.
type ApprovalDetail struct {
	Request *model.ApprovalRequest
	Targets []model.ApprovalTarget
}

// GetForAPIKey 返回当前 API Key 自己提交的请求详情, 非本人发起时
// 返回 not_found, 不泄漏其他 Key 的提案是否存在.
func (s *ApprovalService) GetForAPIKey(ctx context.Context, id, apiKeyID int64) (*ApprovalDetail, error) {
	detail, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if detail.Request.RequestedByAPIKeyID == nil || *detail.Request.RequestedByAPIKeyID != apiKeyID {
		return nil, apperr.NotFound("审批请求不存在")
	}
	return detail, nil
}

// Get 返回审批请求详情 (含 targets).
func (s *ApprovalService) Get(ctx context.Context, id int64) (*ApprovalDetail, error) {
	req, err := s.approvals.Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("审批请求不存在")
		}
		return nil, err
	}
	targets, err := s.approvals.ListTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ApprovalDetail{Request: req, Targets: targets}, nil
}

// List 返回审批请求分页. status 为 nil 表示全部; apiKeyID 为 nil
// 表示不限来源 (Web 端), 非 nil 时只返回该 Key 提交的 (CLI 端).
func (s *ApprovalService) List(
	ctx context.Context,
	status *int16,
	apiKeyID *int64,
	page, pageSize int,
) ([]model.ApprovalRequest, int64, error) {
	return s.approvals.List(ctx, repo.ApprovalListOptions{
		Status:   status,
		APIKeyID: apiKeyID,
		Offset:   (page - 1) * pageSize,
		Limit:    pageSize,
	})
}

// --- 批准 / 拒绝 ---

// Approve 批准一条 pending 请求. override 非空时用用户修改后的 payload
// 替换原始 payload, 并写入 approved_payload 作为最终生效内容.
//
// 校验不过的 target 会把请求转为 stale 并返回 (200 语义);
// 领域事务失败 (如同名占用) 会向上返回错误, 请求保持 pending.
func (s *ApprovalService) Approve(ctx context.Context, id int64, override *json.RawMessage) (*ApprovalDetail, error) {
	var detail *ApprovalDetail
	err := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		req, err := s.loadPending(innerCtx, tx, id)
		if err != nil {
			return err
		}
		targets, err := s.approvals.WithTx(tx).ListTargets(innerCtx, id)
		if err != nil {
			return err
		}
		reason, err := s.validateTargets(innerCtx, tx, req.Operation, targets)
		if err != nil {
			return err
		}
		if reason != "" {
			detail, err = s.markResult(innerCtx, tx, id, model.ApprovalStale, reason, nil)
			return err
		}
		payload := req.OriginalPayload
		if override != nil {
			payload = string(*override)
		}
		// 先把请求置为 approved 终态, 再执行领域修改: 回收/删除类操作会
		// 联动把依赖该对象的 pending 请求标 stale, 若不先落终态会误伤
		// 正在批准的自己. 领域修改失败时整个事务回滚, 状态恢复 pending.
		if err := s.markProcessed(innerCtx, tx, id, model.ApprovalApproved, "", &payload); err != nil {
			return err
		}
		// 用 innerCtx 执行领域修改, 让下层服务复用本事务.
		if err := s.execute(innerCtx, req.Operation, targets, payload); err != nil {
			return err
		}
		detail, err = s.readDetail(innerCtx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Reject 拒绝一条 pending 请求, reason 可为空.
func (s *ApprovalService) Reject(ctx context.Context, id int64, reason string) (*ApprovalDetail, error) {
	var detail *ApprovalDetail
	err := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		if _, err := s.loadPending(innerCtx, tx, id); err != nil {
			return err
		}
		var err error
		detail, err = s.markResult(innerCtx, tx, id, model.ApprovalRejected, reason, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// ApprovalBatchResult 是批量批准/拒绝中单条的结果.
// Success 表示业务是否已生效: approve 时 stale 记 false 且 code=stale.
type ApprovalBatchResult struct {
	RequestID int64
	Success   bool
	ErrorCode string
	ErrorMsg  string
}

// BatchApprove 逐条独立批准, 每条请求在自己的事务中执行,
// 一条失败不影响其他条目.
func (s *ApprovalService) BatchApprove(ctx context.Context, ids []int64) ([]ApprovalBatchResult, error) {
	if err := validateBatchIDs(ids); err != nil {
		return nil, err
	}
	results := make([]ApprovalBatchResult, 0, len(ids))
	for _, id := range ids {
		detail, err := s.Approve(ctx, id, nil)
		results = append(results, batchResult(id, detail, err))
	}
	return results, nil
}

// BatchReject 逐条独立拒绝.
func (s *ApprovalService) BatchReject(ctx context.Context, ids []int64, reason string) ([]ApprovalBatchResult, error) {
	if err := validateBatchIDs(ids); err != nil {
		return nil, err
	}
	results := make([]ApprovalBatchResult, 0, len(ids))
	for _, id := range ids {
		_, err := s.Reject(ctx, id, reason)
		results = append(results, batchResult(id, nil, err))
	}
	return results, nil
}

// batchResult 把一个单条结果折叠为批量结果.
// approve 走到 stale 终态在单条接口里算成功; 批量结果按 API 设计
// 示例记为 success=false + code=stale, 让前端能突出"需重新检查".
func batchResult(id int64, detail *ApprovalDetail, err error) ApprovalBatchResult {
	if err == nil {
		if detail != nil && detail.Request.Status == model.ApprovalStale {
			return ApprovalBatchResult{
				RequestID: id,
				Success:   false,
				ErrorCode: "stale",
				ErrorMsg:  derefString(detail.Request.Reason),
			}
		}
		return ApprovalBatchResult{RequestID: id, Success: true}
	}
	appErr := apperr.From(err)
	return ApprovalBatchResult{
		RequestID: id,
		Success:   false,
		ErrorCode: string(appErr.Code),
		ErrorMsg:  appErr.Message,
	}
}

// validateBatchIDs 校验批量 id 列表非空且不超上限.
func validateBatchIDs(ids []int64) error {
	if len(ids) == 0 {
		return apperr.Validation("ids 不能为空").WithDetails(map[string]any{"field": "ids"})
	}
	if len(ids) > BatchMaxItems {
		return apperr.Newf(apperr.CodeValidationError, "批量项目数超过上限 %d", BatchMaxItems).
			WithDetails(map[string]any{"field": "ids"})
	}
	return nil
}

// loadPending 读取请求并要求其仍为 pending, 否则返回 approval_not_pending.
func (s *ApprovalService) loadPending(ctx context.Context, tx *gorm.DB, id int64) (*model.ApprovalRequest, error) {
	req, err := s.approvals.WithTx(tx).Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("审批请求不存在")
		}
		return nil, err
	}
	if req.Status != model.ApprovalPending {
		return nil, apperr.New(apperr.CodeApprovalNotPending, "审批请求已处理").
			WithDetails(map[string]any{"status": ApprovalStatusName(req.Status)})
	}
	return req, nil
}

// markResult 条件更新请求为终态并返回最新详情.
// 条件更新未命中说明并发下已被处理, 按 approval_not_pending 返回.
func (s *ApprovalService) markResult(
	ctx context.Context,
	tx *gorm.DB,
	id int64,
	status int16,
	reason string,
	approvedPayload *string,
) (*ApprovalDetail, error) {
	if err := s.markProcessed(ctx, tx, id, status, reason, approvedPayload); err != nil {
		return nil, err
	}
	return s.readDetail(ctx, tx, id)
}

// markProcessed 条件更新请求为终态, 未命中时返回 approval_not_pending.
func (s *ApprovalService) markProcessed(
	ctx context.Context,
	tx *gorm.DB,
	id int64,
	status int16,
	reason string,
	approvedPayload *string,
) error {
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	ok, err := s.approvals.WithTx(tx).MarkProcessed(ctx, id, status, reasonPtr, approvedPayload, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return apperr.New(apperr.CodeApprovalNotPending, "审批请求已处理")
	}
	return nil
}

// readDetail 读取请求与 targets 组装详情.
func (s *ApprovalService) readDetail(ctx context.Context, tx *gorm.DB, id int64) (*ApprovalDetail, error) {
	req, err := s.approvals.WithTx(tx).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	targets, err := s.approvals.WithTx(tx).ListTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ApprovalDetail{Request: req, Targets: targets}, nil
}

// --- 目标校验 ---

// validateTargets 校验全部 target 当前存在、状态符合操作要求且版本一致.
// 返回非空原因是需要把请求标记 stale 的文案; 返回错误是底层查询失败.
func (s *ApprovalService) validateTargets(
	ctx context.Context,
	tx *gorm.DB,
	op int16,
	targets []model.ApprovalTarget,
) (string, error) {
	restore := isRestoreOp(op)
	for _, t := range targets {
		switch t.Role {
		case roleTopic:
			state, _, err := s.entityState(ctx, tx, model.EntityTopic, t.EntityID)
			if err != nil {
				return "", err
			}
			if state != stateActive {
				return missingOrTrashedReason(state), nil
			}
		case roleAffectedCard:
			state, ver, err := s.entityState(ctx, tx, model.EntityCard, t.EntityID)
			if err != nil {
				return "", err
			}
			if state == stateMissing {
				return staleReasonDeleted, nil
			}
			if state != stateActive {
				return staleReasonTrashed, nil
			}
			if ver != t.BaseVersion {
				return staleReasonVersionChanged, nil
			}
		case roleTarget:
			state, ver, err := s.entityState(ctx, tx, t.EntityType, t.EntityID)
			if err != nil {
				return "", err
			}
			if restore {
				if state == stateMissing {
					return staleReasonDeleted, nil
				}
				if state == stateActive {
					return staleReasonNotTrashed, nil
				}
			} else {
				if state == stateMissing {
					return staleReasonDeleted, nil
				}
				if state == stateTrashed {
					return staleReasonTrashed, nil
				}
			}
			if ver != t.BaseVersion {
				return staleReasonVersionChanged, nil
			}
		case roleSource1, roleSource2:
			// 合并来源卡: 必须当前正常存在且版本未变, 与 role=target 的非恢复类一致.
			state, ver, err := s.entityState(ctx, tx, model.EntityCard, t.EntityID)
			if err != nil {
				return "", err
			}
			if state == stateMissing {
				return staleReasonDeleted, nil
			}
			if state == stateTrashed {
				return staleReasonTrashed, nil
			}
			if ver != t.BaseVersion {
				return staleReasonVersionChanged, nil
			}
		}
	}
	return "", nil
}

// entityState 描述一个对象当前的落位.
type entityState int

const (
	stateMissing entityState = iota
	stateActive
	stateTrashed
)

// entityState 查询任一领域对象的当前状态与版本.
func (s *ApprovalService) entityState(
	ctx context.Context,
	tx *gorm.DB,
	entityType int16,
	id int64,
) (entityState, int64, error) {
	switch entityType {
	case model.EntityTopic:
		if t, err := s.topics.WithTx(tx).FindActive(ctx, id); err == nil {
			return stateActive, t.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
		if t, err := s.topics.WithTx(tx).FindTrashed(ctx, id); err == nil {
			return stateTrashed, t.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
	case model.EntityCard:
		if c, err := s.cards.WithTx(tx).Find(ctx, id); err == nil {
			return stateActive, c.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
		if tc, err := s.cards.WithTx(tx).FindTrashed(ctx, id); err == nil {
			return stateTrashed, tc.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
	case model.EntityGlossary:
		if g, err := s.glossaries.WithTx(tx).FindActive(ctx, id); err == nil {
			return stateActive, g.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
		if g, err := s.glossaries.WithTx(tx).FindTrashed(ctx, id); err == nil {
			return stateTrashed, g.Version, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return stateMissing, 0, err
		}
	}
	return stateMissing, 0, nil
}

// missingOrTrashedReason 把对象状态映射为 stale 原因.
func missingOrTrashedReason(state entityState) string {
	if state == stateTrashed {
		return staleReasonTrashed
	}
	return staleReasonDeleted
}

// isRestoreOp 判断操作是否为恢复类.
func isRestoreOp(op int16) bool {
	switch op {
	case model.OpCardRestore, model.OpTopicRestore, model.OpGlossaryRestore:
		return true
	default:
		return false
	}
}

// --- 执行 ---

// execute 在已开启的事务中按操作类型执行对应领域修改.
// 更新/回收/恢复类用 target 上的 base_version 作为期望版本, 忽略
// payload 内的 expected_version: 提案时的版本快照才是权威依据.
func (s *ApprovalService) execute(ctx context.Context, op int16, targets []model.ApprovalTarget, payload string) error {
	primaryID, primaryVersion, err := primaryTarget(targets)
	if err != nil && requiresPrimaryTarget(op) {
		return err
	}
	switch op {
	case model.OpTopicCreate:
		var p TopicCreatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.topicSvc.Create(ctx, p.ToInput())
		return err
	case model.OpTopicUpdate:
		var p TopicUpdatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.topicSvc.Update(ctx, primaryID, primaryVersion, p.ToPatch())
		return err
	case model.OpTopicTrash:
		var p TopicTrashPayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, _, err := s.topicSvc.Trash(ctx, primaryID, primaryVersion, p.Include())
		return err
	case model.OpTopicRestore:
		var p TopicRestorePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.topicSvc.Restore(ctx, primaryID, primaryVersion)
		return err
	case model.OpCardCreate:
		var p CardCreatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.cardSvc.Create(ctx, p.ToInput())
		return err
	case model.OpCardUpdate:
		var p CardUpdatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.cardSvc.Update(ctx, primaryID, primaryVersion, p.ToPatch())
		return err
	case model.OpCardTrash:
		var p CardTrashPayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.cardSvc.Trash(ctx, primaryID, primaryVersion)
		return err
	case model.OpCardRestore:
		var p CardRestorePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, _, err := s.cardSvc.Restore(ctx, primaryID, primaryVersion, p.TopicID)
		return err
	case model.OpGlossaryCreate:
		var p GlossaryCreatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.glossarySvc.Create(ctx, p.ToInput())
		return err
	case model.OpGlossaryUpdate:
		var p GlossaryUpdatePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.glossarySvc.Update(ctx, primaryID, primaryVersion, p.ToPatch())
		return err
	case model.OpGlossaryTrash:
		var p GlossaryTrashPayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		return s.glossarySvc.Trash(ctx, primaryID, primaryVersion)
	case model.OpGlossaryRestore:
		var p GlossaryRestorePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		_, err := s.glossarySvc.Restore(ctx, primaryID, primaryVersion)
		return err
	case model.OpCardMerge:
		var p CardMergePayload
		if err := decodeProposalPayload(payload, &p); err != nil {
			return err
		}
		// 来源卡以校验过的 targets 为权威依据 (与更新类忽略 payload
		// expected_version 同一原则), 防止用户改 payload 指向未经校验的卡.
		sourceIDs, err := mergeSourceIDsFromTargets(targets)
		if err != nil {
			return err
		}
		p.SourceCardIDs = sourceIDs[:]
		_, err = s.cardSvc.Merge(ctx, p.ToInput())
		return err
	default:
		return apperr.Newf(apperr.CodeInternal, "未知审批操作 %d", op)
	}
}

// primaryTarget 取出 role=target 的主对象 ID 与版本.
func primaryTarget(targets []model.ApprovalTarget) (int64, int64, error) {
	for _, t := range targets {
		if t.Role == roleTarget {
			return t.EntityID, t.BaseVersion, nil
		}
	}
	return 0, 0, apperr.New(apperr.CodeInternal, "审批请求缺少主目标")
}

// mergeSourceIDsFromTargets 按 role=source_1/source_2 取出合并来源卡 ID,
// 顺序固定. 缺失任一角色视为审批数据损坏按内部错误处理.
func mergeSourceIDsFromTargets(targets []model.ApprovalTarget) ([2]int64, error) {
	var ids [2]int64
	var found [2]bool
	for _, t := range targets {
		switch t.Role {
		case roleSource1:
			ids[0], found[0] = t.EntityID, true
		case roleSource2:
			ids[1], found[1] = t.EntityID, true
		}
	}
	if !found[0] || !found[1] {
		return ids, apperr.New(apperr.CodeInternal, "合并审批缺少来源卡目标")
	}
	return ids, nil
}

// requiresPrimaryTarget 判断某操作是否依赖 role=target 主目标.
// 新增类无主目标 (主对象尚未创建); Card 合并依赖两张 role=source_*
// 来源卡而非单一主目标, 也不取 role=target.
func requiresPrimaryTarget(op int16) bool {
	switch op {
	case model.OpCardCreate, model.OpTopicCreate, model.OpGlossaryCreate, model.OpCardMerge:
		return false
	default:
		return true
	}
}

// derefString 解引用可空字符串, nil 返回空串.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// decodeProposalPayload 把审批 payload 解码为给定 schema, 禁止未知字段.
func decodeProposalPayload(payload string, dst any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Validation("payload 不是合法 JSON: " + err.Error())
	}
	return nil
}
