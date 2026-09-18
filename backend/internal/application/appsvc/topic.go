package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// VersionedItem 是批量操作的单项输入: 对象 ID 与客户端持有的期望版本.
type VersionedItem struct {
	ID              int64
	ExpectedVersion int64
}

// TopicService 编排 Topic 的创建、修改、回收、恢复与永久删除.
// 名称唯一是业务约束: 正常对象之间区分大小写唯一, 回收站对象不占名,
// 创建或改名时同名回收站记录被物理删除.
type TopicService struct {
	db     *gorm.DB
	topics *repo.TopicRepository
	cards  *repo.CardRepository
	logger *slog.Logger
	// now 集中提供当前 UTC 时间, 便于测试替换.
	now func() time.Time
}

// NewTopicService 构造 Topic 服务.
func NewTopicService(
	db *gorm.DB,
	topics *repo.TopicRepository,
	cards *repo.CardRepository,
	logger *slog.Logger,
) *TopicService {
	return &TopicService{
		db:     db,
		topics: topics,
		cards:  cards,
		logger: logger,
		now:    func() time.Time { return persistence.Now() },
	}
}

// TopicInput 是创建 Topic 的已解码参数.
type TopicInput struct {
	Name        string
	Description string
}

// Create 创建 Topic. 同名回收站记录在同一事务中被物理删除,
// 让新对象立即拥有该名称.
func (s *TopicService) Create(ctx context.Context, input TopicInput) (*model.Topic, error) {
	name, err := validateIdentifier("name", input.Name)
	if err != nil {
		return nil, err
	}
	description, err := validateOptionalText("description", input.Description)
	if err != nil {
		return nil, err
	}
	var created *model.Topic
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		created, txErr = s.createInTx(ctx, tx, name, description)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// createInTx 在给定事务中执行创建: 正常同名冲突返回 name_conflict,
// 回收站同名记录物理删除后新建.
func (s *TopicService) createInTx(ctx context.Context, tx *gorm.DB, name, description string) (*model.Topic, error) {
	topics := s.topics.WithTx(tx)
	if exists, err := topics.ExistsActiveByName(ctx, name, 0); err != nil {
		return nil, err
	} else if exists {
		return nil, apperr.Conflict(apperr.CodeNameConflict, "同名 Topic 已存在")
	}
	if err := topics.DeleteTrashedByName(ctx, name); err != nil {
		return nil, err
	}
	now := s.now()
	topic := &model.Topic{
		Name:        name,
		Description: description,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := topics.Create(ctx, topic); err != nil {
		return nil, err
	}
	return topic, nil
}

// BatchCreate 在单个事务中整批创建 Topic, 任一项失败整批回滚,
// 错误携带 details.index 标识失败下标.
func (s *TopicService) BatchCreate(ctx context.Context, inputs []TopicInput) ([]model.Topic, error) {
	// 格式校验放在事务外: 失败早退, 不消耗事务.
	type validated struct {
		name        string
		description string
	}
	items := make([]validated, len(inputs))
	for i, in := range inputs {
		name, err := validateIdentifier("name", in.Name)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		description, err := validateOptionalText("description", in.Description)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		items[i] = validated{name: name, description: description}
	}
	created := make([]model.Topic, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item validated) error {
		topic, err := s.createInTx(ctx, tx, item.name, item.description)
		if err != nil {
			return err
		}
		created = append(created, *topic)
		return nil
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// Get 返回正常 Topic 详情, 含计算列 card_count.
func (s *TopicService) Get(ctx context.Context, id int64) (*repo.TopicRow, error) {
	topic, err := s.topics.FindActive(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Topic 不存在")
		}
		return nil, err
	}
	count, err := s.cards.CountActiveByTopic(ctx, id)
	if err != nil {
		return nil, err
	}
	return &repo.TopicRow{Topic: *topic, CardCount: count}, nil
}

// List 返回正常 Topic 分页, q 为名称子串搜索.
func (s *TopicService) List(ctx context.Context, q string, page, pageSize int) ([]repo.TopicRow, int64, error) {
	offset := (page - 1) * pageSize
	return s.topics.ListActive(ctx, q, offset, pageSize)
}

// TopicPatch 描述 PATCH 的可选修改, nil 表示不修改该字段.
type TopicPatch struct {
	Name        *string
	Description *string
}

// Update 修改正常 Topic. 改名时维护名称唯一: 正常同名冲突返回
// name_conflict, 回收站同名记录被物理删除. 所有字段值都未变化时
// 不递增 version, 直接返回当前对象.
func (s *TopicService) Update(ctx context.Context, id, expectedVersion int64, patch TopicPatch) (*repo.TopicRow, error) {
	if patch.Name == nil && patch.Description == nil {
		return nil, apperr.Validation("至少提供一个要修改的字段")
	}
	var name *string
	if patch.Name != nil {
		v, err := validateIdentifier("name", *patch.Name)
		if err != nil {
			return nil, err
		}
		name = &v
	}
	var description *string
	if patch.Description != nil {
		v, err := validateOptionalText("description", *patch.Description)
		if err != nil {
			return nil, err
		}
		description = &v
	}
	var updated *repo.TopicRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		topics := s.topics.WithTx(tx)
		topic, err := topics.FindActive(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Topic 不存在")
			}
			return err
		}
		fields := map[string]any{}
		if name != nil && *name != topic.Name {
			if exists, err := topics.ExistsActiveByName(ctx, *name, id); err != nil {
				return err
			} else if exists {
				return apperr.Conflict(apperr.CodeNameConflict, "同名 Topic 已存在")
			}
			if err := topics.DeleteTrashedByName(ctx, *name); err != nil {
				return err
			}
			fields["name"] = *name
		}
		if description != nil && *description != topic.Description {
			fields["description"] = *description
		}
		if len(fields) == 0 {
			count, err := s.cards.CountActiveByTopic(ctx, id)
			if err != nil {
				return err
			}
			updated = &repo.TopicRow{Topic: *topic, CardCount: count}
			return nil
		}
		if err := persistence.UpdateOptimistic(tx, &model.Topic{}, id, expectedVersion, fields); err != nil {
			if errors.Is(err, persistence.ErrVersionConflict) {
				return versionConflict("Topic", topic.Version)
			}
			return err
		}
		row, err := s.readRow(ctx, tx, id)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// readRow 在事务内重读 Topic 并附加 card_count, 供更新后的响应构造.
func (s *TopicService) readRow(ctx context.Context, tx *gorm.DB, id int64) (*repo.TopicRow, error) {
	topic, err := s.topics.WithTx(tx).FindActive(ctx, id)
	if err != nil {
		return nil, err
	}
	count, err := s.cards.WithTx(tx).CountActiveByTopic(ctx, id)
	if err != nil {
		return nil, err
	}
	return &repo.TopicRow{Topic: *topic, CardCount: count}, nil
}

// Trash 把正常 Topic 放入回收站. includeCards 为 true 时关联卡在同一
// 事务中连带回收, 否则仅解除关联 (topic_id 置空, version 加一);
// 两种情况下关联卡 version 都恰好增加一次.
func (s *TopicService) Trash(ctx context.Context, id, expectedVersion int64, includeCards bool) (trashedID, affectedCards int64, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		affectedCards, txErr = s.trashInTx(ctx, tx, id, expectedVersion, includeCards)
		return txErr
	})
	if err != nil {
		return 0, 0, err
	}
	return id, affectedCards, nil
}

// BatchTrash 在单个事务中整批回收 Topic, includeCards 语义同 Trash.
func (s *TopicService) BatchTrash(ctx context.Context, items []VersionedItem, includeCards bool) (trashedCount, affectedCards int64, err error) {
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		affected, err := s.trashInTx(ctx, tx, item.ID, item.ExpectedVersion, includeCards)
		if err != nil {
			return err
		}
		trashedCount++
		affectedCards += affected
		return nil
	}); err != nil {
		return 0, 0, err
	}
	return trashedCount, affectedCards, nil
}

// trashInTx 是 Trash 的事务内实现, 供单个与批量回收共用.
// 返回受影响关联卡数量.
func (s *TopicService) trashInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64, includeCards bool) (int64, error) {
	topics := s.topics.WithTx(tx)
	cards := s.cards.WithTx(tx)
	topic, err := topics.FindActive(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, apperr.NotFound("Topic 不存在")
		}
		return 0, err
	}
	linked, err := cards.ListActiveByTopic(ctx, id)
	if err != nil {
		return 0, err
	}
	affected := int64(len(linked))
	now := s.now()
	if includeCards {
		for i := range linked {
			if err := trashCardTx(ctx, tx, cards, &linked[i], now); err != nil {
				return 0, err
			}
		}
	} else if len(linked) > 0 {
		if _, err := cards.DetachFromTopic(ctx, id, now); err != nil {
			return 0, err
		}
	}
	if err := persistence.UpdateOptimistic(tx, &model.Topic{}, id, expectedVersion, map[string]any{
		"trashed_at": now,
	}); err != nil {
		if errors.Is(err, persistence.ErrVersionConflict) {
			return 0, versionConflict("Topic", topic.Version)
		}
		return 0, err
	}
	return affected, nil
}

// TrashPreview 统计回收影响, 不执行任何修改, 供 WebUI 二次确认.
// ids 去重后计数: 正常 Topic 数、其关联卡总数、已在回收站的数量.
func (s *TopicService) TrashPreview(ctx context.Context, ids []int64) (topics, affectedCards, alreadyTrashed int64, err error) {
	if len(ids) == 0 {
		return 0, 0, 0, apperr.Validation("ids 不能为空").WithDetails(map[string]any{"field": "ids"})
	}
	if len(ids) > BatchMaxItems {
		return 0, 0, 0, apperr.Newf(apperr.CodeValidationError, "批量项目数超过上限 %d", BatchMaxItems).
			WithDetails(map[string]any{"field": "ids"})
	}
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	var activeIDs []int64
	for _, id := range unique {
		if _, err := s.topics.FindActive(ctx, id); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if _, terr := s.topics.FindTrashed(ctx, id); terr == nil {
					alreadyTrashed++
				}
				continue
			}
			return 0, 0, 0, err
		}
		activeIDs = append(activeIDs, id)
	}
	topics = int64(len(activeIDs))
	if len(activeIDs) > 0 {
		counts, err := s.cards.CountActiveByTopics(ctx, activeIDs)
		if err != nil {
			return 0, 0, 0, err
		}
		for _, n := range counts {
			affectedCards += n
		}
	}
	return topics, affectedCards, alreadyTrashed, nil
}

// ListTrashed 返回回收站 Topic 分页.
func (s *TopicService) ListTrashed(ctx context.Context, page, pageSize int) ([]model.Topic, int64, error) {
	offset := (page - 1) * pageSize
	return s.topics.ListTrashed(ctx, offset, pageSize)
}

// Restore 把回收站 Topic 恢复为正常对象. 同名正常 Topic 已存在时
// 返回 name_conflict, 由用户决定如何处理.
func (s *TopicService) Restore(ctx context.Context, id, expectedVersion int64) (*model.Topic, error) {
	var restored *model.Topic
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		topics := s.topics.WithTx(tx)
		topic, err := topics.FindTrashed(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("回收站中不存在该 Topic")
			}
			return err
		}
		if topic.Version != expectedVersion {
			return versionConflict("Topic", topic.Version)
		}
		if exists, err := topics.ExistsActiveByName(ctx, topic.Name, 0); err != nil {
			return err
		} else if exists {
			return apperr.Conflict(apperr.CodeNameConflict, "同名 Topic 已存在")
		}
		ok, err := topics.Restore(ctx, id, expectedVersion, s.now())
		if err != nil {
			return err
		}
		if !ok {
			// 读取后被并发修改, 按版本冲突处理.
			return versionConflict("Topic", topic.Version)
		}
		restored, err = topics.FindActive(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return restored, nil
}

// BatchRestore 在单个事务中整批恢复回收站 Topic.
func (s *TopicService) BatchRestore(ctx context.Context, items []VersionedItem) ([]model.Topic, error) {
	restored := make([]model.Topic, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		topic, err := s.restoreInTx(ctx, tx, item.ID, item.ExpectedVersion)
		if err != nil {
			return err
		}
		restored = append(restored, *topic)
		return nil
	}); err != nil {
		return nil, err
	}
	return restored, nil
}

// restoreInTx 是 Restore 的事务内实现, 供单个与批量恢复共用.
func (s *TopicService) restoreInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) (*model.Topic, error) {
	topics := s.topics.WithTx(tx)
	topic, err := topics.FindTrashed(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("回收站中不存在该 Topic")
		}
		return nil, err
	}
	if topic.Version != expectedVersion {
		return nil, versionConflict("Topic", topic.Version)
	}
	if exists, err := topics.ExistsActiveByName(ctx, topic.Name, 0); err != nil {
		return nil, err
	} else if exists {
		return nil, apperr.Conflict(apperr.CodeNameConflict, "同名 Topic 已存在")
	}
	ok, err := topics.Restore(ctx, id, expectedVersion, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, versionConflict("Topic", topic.Version)
	}
	return topics.FindActive(ctx, id)
}

// DeleteForever 永久删除回收站 Topic. 只开放给 Web 会话.
func (s *TopicService) DeleteForever(ctx context.Context, id, expectedVersion int64) error {
	return s.deleteForeverInTx(ctx, s.db, id, expectedVersion)
}

// BatchDeleteForever 在单个事务中整批永久删除回收站 Topic.
func (s *TopicService) BatchDeleteForever(ctx context.Context, items []VersionedItem) (int64, error) {
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

// deleteForeverInTx 按期望 version 条件删除回收站 Topic,
// 未命中时区分不存在与版本不匹配.
func (s *TopicService) deleteForeverInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) error {
	topics := s.topics.WithTx(tx)
	topic, err := topics.FindTrashed(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("回收站中不存在该 Topic")
		}
		return err
	}
	if topic.Version != expectedVersion {
		return versionConflict("Topic", topic.Version)
	}
	ok, err := topics.DeleteTrashed(ctx, id, expectedVersion)
	if err != nil {
		return err
	}
	if !ok {
		return versionConflict("Topic", topic.Version)
	}
	return nil
}

// versionConflict 构造带当前版本的乐观锁冲突错误, 让调用方重读重试.
func versionConflict(entity string, currentVersion int64) *apperr.Error {
	return apperr.Conflict(apperr.CodeVersionConflict, entity+" 已被修改, 请重读后重试").
		WithDetails(map[string]any{"current_version": currentVersion})
}
