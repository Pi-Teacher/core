package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// TopicRepository 持久化 Topic. 正常与回收站对象同表, 以 trashed_at
// 区分; 名称唯一是业务约束, 数据库不建 UNIQUE 索引.
type TopicRepository struct {
	db *gorm.DB
}

// NewTopicRepository 构造 Topic 仓库.
func NewTopicRepository(db *gorm.DB) *TopicRepository {
	return &TopicRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本, 供服务层在单个事务中
// 组合多个仓库的操作; 原仓库不受影响.
func (r *TopicRepository) WithTx(tx *gorm.DB) *TopicRepository {
	return &TopicRepository{db: tx}
}

// TopicRow 是 Topic 列表与详情的投影: 在表字段之外附加计算列 card_count,
// 即当前正常关联的 Card 数量, 由服务端计算, 不接受客户端传入.
type TopicRow struct {
	model.Topic
	CardCount int64
}

// FindActive 按 id 查找正常 Topic, 不存在时返回 gorm.ErrRecordNotFound.
func (r *TopicRepository) FindActive(ctx context.Context, id int64) (*model.Topic, error) {
	var t model.Topic
	err := r.db.WithContext(ctx).Where("id = ? AND trashed_at IS NULL", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FindTrashed 按 id 查找回收站中的 Topic.
func (r *TopicRepository) FindTrashed(ctx context.Context, id int64) (*model.Topic, error) {
	var t model.Topic
	err := r.db.WithContext(ctx).Where("id = ? AND trashed_at IS NOT NULL", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ExistsActiveByName 判断是否存在同名正常 Topic.
// excludeID 用于改名场景排除自身, 传 0 表示不排除.
func (r *TopicRepository) ExistsActiveByName(ctx context.Context, name string, excludeID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Topic{}).
		Where("name = ? AND trashed_at IS NULL AND id <> ?", name, excludeID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteTrashedByName 物理删除同名回收站记录, 创建与改名时用于覆盖,
// 让新对象立即拥有该名称; 不要求旧记录的版本.
// 返回被删除记录 ID: 调用方据此把依赖旧记录的 pending 审批标记 stale.
// 先查 ID 再删, 避免依赖 RETURNING 造成方言差异.
func (r *TopicRepository) DeleteTrashedByName(ctx context.Context, name string) ([]int64, error) {
	ids := make([]int64, 0, 2)
	err := r.db.WithContext(ctx).Model(&model.Topic{}).
		Where("name = ? AND trashed_at IS NOT NULL", name).
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&model.Topic{}).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ListTrashedIDs 返回全部回收站 Topic 的 ID, 供清空回收站前记录
// 需要联动 stale 的对象. 顺序稳定便于测试与日志.
func (r *TopicRepository) ListTrashedIDs(ctx context.Context) ([]int64, error) {
	ids := make([]int64, 0, 16)
	err := r.db.WithContext(ctx).Model(&model.Topic{}).
		Where("trashed_at IS NOT NULL").
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ListActive 返回正常 Topic 分页 (含 card_count 计算列).
// q 为名称子串搜索, ASCII 大小写不敏感; 按最近更新在前.
func (r *TopicRepository) ListActive(ctx context.Context, q string, offset, limit int) ([]TopicRow, int64, error) {
	scope := r.db.WithContext(ctx).Model(&model.Topic{}).Where("trashed_at IS NULL")
	if q != "" {
		scope = scope.Where("LOWER(name) LIKE ? ESCAPE '\\'", likePattern(q))
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]TopicRow, 0, limit)
	// card_count 用相关子查询一次算出, 避免逐行 COUNT 的 N+1 查询.
	err := scope.
		Select("topic.*, (SELECT COUNT(*) FROM card WHERE card.topic_id = topic.id) AS card_count").
		Order("updated_at DESC, id DESC").
		Offset(offset).Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListTrashed 返回回收站 Topic 分页, 按最近回收在前.
// 回收站 Topic 的关联卡在进入回收站时已清空, card_count 恒为 0,
// 因此不需要计算列.
func (r *TopicRepository) ListTrashed(ctx context.Context, offset, limit int) ([]model.Topic, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Topic{}).Where("trashed_at IS NOT NULL")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.Topic, 0, limit)
	if err := q.Order("trashed_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Create 插入一条 Topic.
func (r *TopicRepository) Create(ctx context.Context, topic *model.Topic) error {
	return r.db.WithContext(ctx).Create(topic).Error
}

// Restore 把回收站中的 Topic 恢复为正常对象, 条件更新携带期望 version
// 承载乐观锁. 返回 false 表示记录不存在、不在回收站或版本不匹配.
func (r *TopicRepository) Restore(ctx context.Context, id, expectedVersion int64, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Topic{}).
		Where("id = ? AND version = ? AND trashed_at IS NOT NULL", id, expectedVersion).
		Updates(map[string]any{
			"trashed_at": nil,
			"version":    gorm.Expr("version + 1"),
			"updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// DeleteTrashed 永久删除回收站中的 Topic, 条件携带期望 version.
// 返回 false 表示记录不存在、不在回收站或版本不匹配.
func (r *TopicRepository) DeleteTrashed(ctx context.Context, id, expectedVersion int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND version = ? AND trashed_at IS NOT NULL", id, expectedVersion).
		Delete(&model.Topic{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// DeleteAllTrashed 物理删除全部回收站 Topic, 供清空回收站使用.
func (r *TopicRepository) DeleteAllTrashed(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Where("trashed_at IS NOT NULL").Delete(&model.Topic{})
	return res.RowsAffected, res.Error
}
