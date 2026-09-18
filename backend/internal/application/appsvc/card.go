package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/open-spaced-repetition/go-fsrs/v4"
	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/domain/card"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// CardService 编排 Card 的创建、修改、回收、恢复与永久删除.
// 建卡事务同时初始化 FSRS 调度并累计 Calendar 制卡数;
// embedding 任务状态直接保存在 Card 上, 本批只负责置 pending,
// worker 在第五批接入.
type CardService struct {
	db       *gorm.DB
	cards    *repo.CardRepository
	topics   *repo.TopicRepository
	calendar *repo.CalendarRepository
	logger   *slog.Logger
	// timezone 返回用户配置的日历时区, 用于把 UTC 时刻映射为自然日.
	// 每次调用读当前快照, 设置变更后立即生效.
	timezone func() *time.Location
	// now 集中提供当前 UTC 时间, 便于测试替换.
	now func() time.Time
}

// NewCardService 构造 Card 服务. timezone 为 nil 时按 UTC 处理.
func NewCardService(
	db *gorm.DB,
	cards *repo.CardRepository,
	topics *repo.TopicRepository,
	calendar *repo.CalendarRepository,
	logger *slog.Logger,
	timezone func() *time.Location,
) *CardService {
	if timezone == nil {
		timezone = func() *time.Location { return time.UTC }
	}
	return &CardService{
		db:       db,
		cards:    cards,
		topics:   topics,
		calendar: calendar,
		logger:   logger,
		timezone: timezone,
		now:      func() time.Time { return persistence.Now() },
	}
}

// CardInput 是创建 Card 的已解码参数.
// TopicID 为 nil 表示无 Topic; EnableEmbedding 已按请求缺省 true 处理.
type CardInput struct {
	TopicID         *int64
	Front           string
	Back            string
	EnableEmbedding bool
}

// CardDetail 是 Card 详情: 正式行加调度行, 调度恒存在,
// 读取失败视为数据损坏按内部错误处理.
type CardDetail struct {
	Card     *model.Card
	Schedule *model.CardSchedule
}

// newSchedule 用 fsrs.NewCard 的初始值构造新卡调度.
// v1 固定空学习步骤, 新卡 state=New, due=now;
// LastReview 为零值表示未复习, 落库为 NULL.
func newSchedule(cardID int64, now time.Time) *model.CardSchedule {
	c := fsrs.NewCard(now)
	return &model.CardSchedule{
		CardID:         cardID,
		Due:            c.Due,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  int64(c.ScheduledDays),
		Reps:           int64(c.Reps),
		Lapses:         int64(c.Lapses),
		State:          int16(c.State),
		LastReviewAt:   nil,
		RemainingSteps: int64(c.RemainingSteps),
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// calendarDay 把 UTC 时刻映射为用户时区下的自然日, 以该日的 UTC 零点
// 表示. DATE 列写入时三种数据库都取年月日, UTC 零点表示保证
// 同一自然日在任何方言下写入相同日期.
func calendarDay(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// Create 创建正式 Card: 校验 Topic, 计算 fingerprint, 插入 Card 与
// 新卡调度, 并按用户时区自然日累计 Calendar 制卡数, 全部在同一事务.
func (s *CardService) Create(ctx context.Context, input CardInput) (*CardDetail, error) {
	front, err := validateContent("front", input.Front)
	if err != nil {
		return nil, err
	}
	back, err := validateContent("back", input.Back)
	if err != nil {
		return nil, err
	}
	var detail *CardDetail
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		detail, txErr = s.createInTx(ctx, tx, CardInput{
			TopicID:         input.TopicID,
			Front:           front,
			Back:            back,
			EnableEmbedding: input.EnableEmbedding,
		}, s.now())
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// createInTx 是 Create 的事务内实现, 供单个与批量创建共用.
func (s *CardService) createInTx(ctx context.Context, tx *gorm.DB, input CardInput, now time.Time) (*CardDetail, error) {
	cards := s.cards.WithTx(tx)
	var topicRef *int64
	if input.TopicID != nil {
		if _, err := s.topics.WithTx(tx).FindActive(ctx, *input.TopicID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("Topic 不存在")
			}
			return nil, err
		}
		topicRef = input.TopicID
	}
	c := &model.Card{
		TopicID:          topicRef,
		Front:            input.Front,
		Back:             input.Back,
		EnableEmbedding:  input.EnableEmbedding,
		FrontFingerprint: card.FrontFingerprint(input.Front),
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if input.EnableEmbedding {
		// 启用 embedding 的新卡进入 pending, 等待第五批的 worker 消费.
		status := model.EmbeddingPending
		c.EmbeddingStatus = &status
	}
	if err := cards.Create(ctx, c); err != nil {
		return nil, err
	}
	schedule := newSchedule(c.ID, now)
	if err := cards.CreateSchedule(ctx, schedule); err != nil {
		return nil, err
	}
	day := calendarDay(now, s.timezone())
	if err := s.calendar.WithTx(tx).AddCreatedCards(ctx, day, 1, now); err != nil {
		return nil, err
	}
	return &CardDetail{Card: c, Schedule: schedule}, nil
}

// BatchCreate 在单个事务中整批创建 Card.
func (s *CardService) BatchCreate(ctx context.Context, inputs []CardInput) ([]CardDetail, error) {
	// 格式校验放在事务外, 错误同样携带 index.
	type validated struct {
		input CardInput
	}
	items := make([]validated, len(inputs))
	for i, in := range inputs {
		front, err := validateContent("front", in.Front)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		back, err := validateContent("back", in.Back)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		in.Front = front
		in.Back = back
		items[i] = validated{input: in}
	}
	now := s.now()
	created := make([]CardDetail, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item validated) error {
		detail, err := s.createInTx(ctx, tx, item.input, now)
		if err != nil {
			return err
		}
		created = append(created, *detail)
		return nil
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// Get 返回正常 Card 详情, 含调度与 embedding 任务错误.
func (s *CardService) Get(ctx context.Context, id int64) (*CardDetail, error) {
	c, err := s.cards.Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Card 不存在")
		}
		return nil, err
	}
	schedule, err := s.cards.FindSchedule(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.Wrap(apperr.CodeInternal, "Card 缺少调度行", err)
		}
		return nil, err
	}
	return &CardDetail{Card: c, Schedule: schedule}, nil
}

// List 按筛选条件返回正常 Card 分页.
func (s *CardService) List(ctx context.Context, filter repo.CardFilter, page, pageSize int) ([]model.Card, int64, error) {
	offset := (page - 1) * pageSize
	return s.cards.List(ctx, filter, offset, pageSize)
}

// CardPatch 描述 PATCH 的可选修改. SetXxx 为 false 表示不修改该字段;
// SetTopicID 为 true 时 TopicID 取 nil 表示设为无 Topic.
type CardPatch struct {
	SetTopicID         bool
	TopicID            *int64
	SetFront           bool
	Front              string
	SetBack            bool
	Back               string
	SetEnableEmbedding bool
	EnableEmbedding    bool
}

// Update 修改正常 Card. front 变化时重算 fingerprint, 并按 embedding
// 规则迁移任务字段; 所有字段值都未变化时不递增 version.
func (s *CardService) Update(ctx context.Context, id, expectedVersion int64, patch CardPatch) (*CardDetail, error) {
	if !patch.SetTopicID && !patch.SetFront && !patch.SetBack && !patch.SetEnableEmbedding {
		return nil, apperr.Validation("至少提供一个要修改的字段")
	}
	if patch.SetFront {
		front, err := validateContent("front", patch.Front)
		if err != nil {
			return nil, err
		}
		patch.Front = front
	}
	if patch.SetBack {
		back, err := validateContent("back", patch.Back)
		if err != nil {
			return nil, err
		}
		patch.Back = back
	}
	var detail *CardDetail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		detail, txErr = s.updateInTx(ctx, tx, id, expectedVersion, patch)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// updateInTx 是 Update 的事务内实现.
func (s *CardService) updateInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64, patch CardPatch) (*CardDetail, error) {
	cards := s.cards.WithTx(tx)
	c, err := cards.Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Card 不存在")
		}
		return nil, err
	}
	if c.Version != expectedVersion {
		return nil, versionConflict("Card", c.Version)
	}

	fields := map[string]any{}
	if patch.SetTopicID {
		if patch.TopicID != nil {
			if _, err := s.topics.WithTx(tx).FindActive(ctx, *patch.TopicID); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, apperr.NotFound("Topic 不存在")
				}
				return nil, err
			}
		}
		if !sameInt64Ptr(patch.TopicID, c.TopicID) {
			fields["topic_id"] = patch.TopicID
		}
	}
	frontChanged := false
	if patch.SetFront && patch.Front != c.Front {
		frontChanged = true
		fields["front"] = patch.Front
		fields["front_fingerprint"] = card.FrontFingerprint(patch.Front)
	}
	if patch.SetBack && patch.Back != c.Back {
		fields["back"] = patch.Back
	}

	// embedding 任务字段迁移: front 变化或开关切换时才触碰,
	// 只改 back 或 topic 不影响 embedding 与 fingerprint.
	newEnable := c.EnableEmbedding
	if patch.SetEnableEmbedding {
		newEnable = patch.EnableEmbedding
	}
	if frontChanged && newEnable {
		fields["embedding"] = nil
		fields["embedding_status"] = model.EmbeddingPending
		fields["embedding_error"] = nil
	}
	if patch.SetEnableEmbedding && patch.EnableEmbedding != c.EnableEmbedding {
		fields["enable_embedding"] = patch.EnableEmbedding
		if patch.EnableEmbedding {
			// false -> true: 进入 pending, 旧向量本就为空.
			fields["embedding_status"] = model.EmbeddingPending
			fields["embedding_error"] = nil
		} else {
			// true -> false: 清空向量与全部任务字段.
			fields["embedding"] = nil
			fields["embedding_status"] = nil
			fields["embedding_error"] = nil
		}
	}

	if len(fields) == 0 {
		// 无实际变化: 返回当前对象, 不递增 version.
		return s.readDetail(ctx, tx, id)
	}
	if err := persistence.UpdateOptimistic(tx, &model.Card{}, id, expectedVersion, fields); err != nil {
		if errors.Is(err, persistence.ErrVersionConflict) {
			return nil, versionConflict("Card", c.Version)
		}
		return nil, err
	}
	return s.readDetail(ctx, tx, id)
}

// readDetail 在事务内重读 Card 与调度, 供更新后的响应构造.
func (s *CardService) readDetail(ctx context.Context, tx *gorm.DB, id int64) (*CardDetail, error) {
	cards := s.cards.WithTx(tx)
	c, err := cards.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	schedule, err := cards.FindSchedule(ctx, id)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Card 缺少调度行", err)
	}
	return &CardDetail{Card: c, Schedule: schedule}, nil
}

// sameInt64Ptr 比较两个可空 int64 是否相等, nil 与 nil 相等.
func sameInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Trash 把正常 Card 放入回收站: 复制正反面到 trashed_card, 删除调度与
// 全部复习日志, 删除正式行. Calendar 历史计数不回退.
func (s *CardService) Trash(ctx context.Context, id, expectedVersion int64) (int64, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.trashInTx(ctx, tx, id, expectedVersion)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// trashInTx 是 Trash 的事务内实现, 供单个与批量回收共用.
func (s *CardService) trashInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) error {
	cards := s.cards.WithTx(tx)
	c, err := cards.Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("Card 不存在")
		}
		return err
	}
	if c.Version != expectedVersion {
		return versionConflict("Card", c.Version)
	}
	return trashCardTx(ctx, tx, cards, c, s.now())
}

// trashCardTx 在调用方事务中把一张正常 Card 放入回收站:
// 复制正反面与元数据到 trashed_card (version 取原 version + 1),
// 删除调度与全部复习日志, 最后按读取时的 version 条件删除正式行.
// 条件删除未命中说明读取后被并发修改, 事务回滚由调用方统一处理.
func trashCardTx(ctx context.Context, tx *gorm.DB, cards *repo.CardRepository, c *model.Card, now time.Time) error {
	trashed := model.TrashedCard{
		ID:              c.ID,
		Front:           c.Front,
		Back:            c.Back,
		EnableEmbedding: c.EnableEmbedding,
		Version:         c.Version + 1,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       now,
		TrashedAt:       now,
	}
	if err := cards.CreateTrashed(ctx, &trashed); err != nil {
		return err
	}
	if err := cards.DeleteSchedule(ctx, c.ID); err != nil {
		return err
	}
	if err := cards.DeleteReviewLogs(ctx, c.ID); err != nil {
		return err
	}
	ok, err := cards.DeleteConditional(ctx, c.ID, c.Version)
	if err != nil {
		return err
	}
	if !ok {
		return persistence.ErrVersionConflict
	}
	return nil
}

// BatchTrash 在单个事务中整批回收 Card.
func (s *CardService) BatchTrash(ctx context.Context, items []VersionedItem) (int64, error) {
	var count int64
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		if err := s.trashInTx(ctx, tx, item.ID, item.ExpectedVersion); err != nil {
			return err
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

// RestoreCardResult 是恢复 Card 的响应数据: 回收站行 ID 与新 Card ID.
type RestoreCardResult struct {
	TrashCardID int64
	NewCardID   int64
}

// Restore 把回收站 Card 恢复为全新正式 Card: 生成新 ID, 重算
// fingerprint, 初始化新调度; 指定 topicID 时校验其正常存在.
// 恢复不增加 Calendar 制卡数.
func (s *CardService) Restore(ctx context.Context, trashedID, expectedVersion int64, topicID *int64) (*RestoreCardResult, *CardDetail, error) {
	var result *RestoreCardResult
	var detail *CardDetail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		result, detail, txErr = s.restoreInTx(ctx, tx, trashedID, expectedVersion, topicID)
		return txErr
	})
	if err != nil {
		return nil, nil, err
	}
	return result, detail, nil
}

// restoreInTx 是 Restore 的事务内实现, 供单个与批量恢复共用.
func (s *CardService) restoreInTx(ctx context.Context, tx *gorm.DB, trashedID, expectedVersion int64, topicID *int64) (*RestoreCardResult, *CardDetail, error) {
	cards := s.cards.WithTx(tx)
	tc, err := cards.FindTrashed(ctx, trashedID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, apperr.NotFound("回收站中不存在该 Card")
		}
		return nil, nil, err
	}
	if tc.Version != expectedVersion {
		return nil, nil, versionConflict("回收站 Card", tc.Version)
	}
	var topicRef *int64
	if topicID != nil {
		if _, err := s.topics.WithTx(tx).FindActive(ctx, *topicID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, apperr.NotFound("Topic 不存在")
			}
			return nil, nil, err
		}
		topicRef = topicID
	}
	// 条件删除回收站行承载乐观锁, 未命中说明并发修改.
	ok, err := cards.DeleteTrashedConditional(ctx, trashedID, expectedVersion)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, versionConflict("回收站 Card", tc.Version)
	}
	now := s.now()
	c := &model.Card{
		TopicID:          topicRef,
		Front:            tc.Front,
		Back:             tc.Back,
		EnableEmbedding:  tc.EnableEmbedding,
		FrontFingerprint: card.FrontFingerprint(tc.Front),
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if tc.EnableEmbedding {
		status := model.EmbeddingPending
		c.EmbeddingStatus = &status
	}
	if err := cards.Create(ctx, c); err != nil {
		return nil, nil, err
	}
	schedule := newSchedule(c.ID, now)
	if err := cards.CreateSchedule(ctx, schedule); err != nil {
		return nil, nil, err
	}
	return &RestoreCardResult{TrashCardID: trashedID, NewCardID: c.ID},
		&CardDetail{Card: c, Schedule: schedule}, nil
}

// RestoreItem 是批量恢复 Card 的单项输入, topicID 可选.
type RestoreItem struct {
	ID              int64
	ExpectedVersion int64
	TopicID         *int64
}

// BatchRestore 在单个事务中整批恢复回收站 Card.
func (s *CardService) BatchRestore(ctx context.Context, items []RestoreItem) ([]RestoreCardResult, []CardDetail, error) {
	results := make([]RestoreCardResult, 0, len(items))
	details := make([]CardDetail, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item RestoreItem) error {
		result, detail, err := s.restoreInTx(ctx, tx, item.ID, item.ExpectedVersion, item.TopicID)
		if err != nil {
			return err
		}
		results = append(results, *result)
		details = append(details, *detail)
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return results, details, nil
}

// ListTrashed 返回回收站 Card 分页.
func (s *CardService) ListTrashed(ctx context.Context, page, pageSize int) ([]model.TrashedCard, int64, error) {
	offset := (page - 1) * pageSize
	return s.cards.ListTrashed(ctx, offset, pageSize)
}

// DeleteForever 永久删除回收站 Card. 只开放给 Web 会话.
func (s *CardService) DeleteForever(ctx context.Context, id, expectedVersion int64) error {
	return s.deleteForeverInTx(ctx, s.db, id, expectedVersion)
}

// BatchDeleteForever 在单个事务中整批永久删除回收站 Card.
func (s *CardService) BatchDeleteForever(ctx context.Context, items []VersionedItem) (int64, error) {
	var deleted int64
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		if err := s.deleteForeverInTx(ctx, tx, item.ID, item.ExpectedVersion); err != nil {
			return err
		}
		deleted++
		return nil
	}); err != nil {
		return 0, err
	}
	return deleted, nil
}

// deleteForeverInTx 按期望 version 条件删除回收站 Card,
// 未命中时区分不存在与版本不匹配.
func (s *CardService) deleteForeverInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) error {
	cards := s.cards.WithTx(tx)
	tc, err := cards.FindTrashed(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("回收站中不存在该 Card")
		}
		return err
	}
	if tc.Version != expectedVersion {
		return versionConflict("回收站 Card", tc.Version)
	}
	ok, err := cards.DeleteTrashedConditional(ctx, id, expectedVersion)
	if err != nil {
		return err
	}
	if !ok {
		return versionConflict("回收站 Card", tc.Version)
	}
	return nil
}
