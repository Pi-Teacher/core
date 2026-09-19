package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// CardFilter 是 Card 列表的筛选与排序条件, 值由 HTTP 层校验后传入.
type CardFilter struct {
	// TopicID 为 nil 表示全部; 指向 0 表示无 Topic 的 Card.
	TopicID *int64
	// Q 是 front 与 back 的子串搜索, ASCII 大小写不敏感.
	Q string
	// EmbeddingStatus 为空表示全部; 取值 pending|processing|ready|failed
	// 时筛选启用 embedding 且处于该状态的卡, disabled 筛选未启用的卡.
	EmbeddingStatus string
	// Sort 取值 created_at|updated_at, 空值按 created_at 处理.
	Sort string
	// Order 取值 asc|desc, 空值按 desc 处理.
	Order string
}

// CardRepository 持久化 Card 及其配套的 card_schedule, review_log,
// trashed_card 行. card 表只保存当前有效卡, 回收站内容在独立表.
type CardRepository struct {
	db *gorm.DB
}

// NewCardRepository 构造 Card 仓库.
func NewCardRepository(db *gorm.DB) *CardRepository {
	return &CardRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本, 供服务层在单个事务中
// 组合多个仓库的操作; 原仓库不受影响.
func (r *CardRepository) WithTx(tx *gorm.DB) *CardRepository {
	return &CardRepository{db: tx}
}

// Find 按 id 查找正常 Card. card 表只存有效卡, 命中即正常.
func (r *CardRepository) Find(ctx context.Context, id int64) (*model.Card, error) {
	var c model.Card
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindSchedule 查找 Card 的调度行, 每张正常卡恰好一条.
func (r *CardRepository) FindSchedule(ctx context.Context, cardID int64) (*model.CardSchedule, error) {
	var s model.CardSchedule
	err := r.db.WithContext(ctx).Where("card_id = ?", cardID).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List 按筛选条件返回正常 Card 分页. 排序列白名单外的值由调用方
// 保证合法, 这里只做防御性回退; id 作为并列时的次序键保证分页稳定.
func (r *CardRepository) List(ctx context.Context, f CardFilter, offset, limit int) ([]model.Card, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Card{})
	switch {
	case f.TopicID == nil:
		// 不过滤
	case *f.TopicID == 0:
		q = q.Where("topic_id IS NULL")
	default:
		q = q.Where("topic_id = ?", *f.TopicID)
	}
	if f.Q != "" {
		pattern := likePattern(f.Q)
		q = q.Where("(LOWER(front) LIKE ? ESCAPE '\\' OR LOWER(back) LIKE ? ESCAPE '\\')", pattern, pattern)
	}
	switch f.EmbeddingStatus {
	case "":
		// 不过滤
	case "disabled":
		q = q.Where("enable_embedding = ?", false)
	default:
		q = q.Where("enable_embedding = ? AND embedding_status = ?", true, embeddingStatusValue(f.EmbeddingStatus))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sortCol := "created_at"
	if f.Sort == "updated_at" {
		sortCol = "updated_at"
	}
	order := "DESC"
	if f.Order == "asc" {
		order = "ASC"
	}
	rows := make([]model.Card, 0, limit)
	err := q.Order(sortCol + " " + order + ", id " + order).
		Offset(offset).Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// embeddingStatusValue 把筛选字符串映射为数据库枚举值,
// 调用方已保证取值合法, 未知值按 pending 处理仅作防御.
func embeddingStatusValue(s string) int16 {
	switch s {
	case "processing":
		return model.EmbeddingProcessing
	case "ready":
		return model.EmbeddingReady
	case "failed":
		return model.EmbeddingFailed
	default:
		return model.EmbeddingPending
	}
}

// Create 插入一条正常 Card.
func (r *CardRepository) Create(ctx context.Context, card *model.Card) error {
	return r.db.WithContext(ctx).Create(card).Error
}

// CreateSchedule 插入 Card 的调度行.
func (r *CardRepository) CreateSchedule(ctx context.Context, s *model.CardSchedule) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// DeleteSchedule 删除 Card 的调度行, 回收时使用.
func (r *CardRepository) DeleteSchedule(ctx context.Context, cardID int64) error {
	return r.db.WithContext(ctx).Where("card_id = ?", cardID).Delete(&model.CardSchedule{}).Error
}

// DeleteReviewLogs 物理删除 Card 的全部复习日志, 回收时使用.
func (r *CardRepository) DeleteReviewLogs(ctx context.Context, cardID int64) error {
	return r.db.WithContext(ctx).Where("card_id = ?", cardID).Delete(&model.ReviewLog{}).Error
}

// ListActiveByTopic 返回引用指定 Topic 的全部正常 Card,
// Topic 回收时用于计算受影响卡集合.
func (r *CardRepository) ListActiveByTopic(ctx context.Context, topicID int64) ([]model.Card, error) {
	var rows []model.Card
	err := r.db.WithContext(ctx).Where("topic_id = ?", topicID).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// CountActiveByTopic 统计引用指定 Topic 的正常 Card 数量.
func (r *CardRepository) CountActiveByTopic(ctx context.Context, topicID int64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Card{}).Where("topic_id = ?", topicID).Count(&n).Error
	return n, err
}

// CountActiveByTopics 批量统计各 Topic 的正常关联卡数量, 供回收预览
// 一次查询算出全部计数.
func (r *CardRepository) CountActiveByTopics(ctx context.Context, topicIDs []int64) (map[int64]int64, error) {
	var rows []struct {
		TopicID int64
		N       int64
	}
	err := r.db.WithContext(ctx).Model(&model.Card{}).
		Select("topic_id, COUNT(*) AS n").
		Where("topic_id IN ?", topicIDs).
		Group("topic_id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.TopicID] = row.N
	}
	return counts, nil
}

// DetachFromTopic 把引用指定 Topic 的全部正常卡置为无 Topic,
// 每张卡 version 加一并刷新 updated_at, 返回受影响行数.
func (r *CardRepository) DetachFromTopic(ctx context.Context, topicID int64, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Card{}).
		Where("topic_id = ?", topicID).
		Updates(map[string]any{
			"topic_id":   nil,
			"version":    gorm.Expr("version + 1"),
			"updated_at": now,
		})
	return res.RowsAffected, res.Error
}

// DeleteConditional 按期望 version 条件删除正常 Card, 承载回收时的
// 乐观锁. 返回 false 表示卡不存在或版本不匹配.
func (r *CardRepository) DeleteConditional(ctx context.Context, id, expectedVersion int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND version = ?", id, expectedVersion).
		Delete(&model.Card{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// --- 回收站 Card ---

// FindTrashed 按 id 查找回收站 Card.
func (r *CardRepository) FindTrashed(ctx context.Context, id int64) (*model.TrashedCard, error) {
	var tc model.TrashedCard
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&tc).Error
	if err != nil {
		return nil, err
	}
	return &tc, nil
}

// ListTrashed 返回回收站 Card 分页, 按最近回收在前.
func (r *CardRepository) ListTrashed(ctx context.Context, offset, limit int) ([]model.TrashedCard, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.TrashedCard{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.TrashedCard, 0, limit)
	if err := q.Order("trashed_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// CreateTrashed 插入回收站 Card. id 沿用进入回收站前的 Card ID,
// 恢复时数据库会生成全新 Card ID, 因此该 ID 不会与正常表冲突.
func (r *CardRepository) CreateTrashed(ctx context.Context, tc *model.TrashedCard) error {
	return r.db.WithContext(ctx).Create(tc).Error
}

// DeleteTrashedConditional 按期望 version 条件删除回收站 Card,
// 供恢复与永久删除承载乐观锁. 返回 false 表示记录不存在或版本不匹配.
func (r *CardRepository) DeleteTrashedConditional(ctx context.Context, id, expectedVersion int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND version = ?", id, expectedVersion).
		Delete(&model.TrashedCard{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// DeleteAllTrashed 物理删除全部回收站 Card, 供清空回收站使用.
// 清空本身就是全表语义, 显式开放全局删除避免 GORM 的防误删拦截.
func (r *CardRepository) DeleteAllTrashed(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&model.TrashedCard{})
	return res.RowsAffected, res.Error
}

// ListTrashedIDs 返回全部回收站 Card 的 ID, 供清空回收站前记录
// 需要联动 stale 的对象. 回收站 Card 的行 ID 即原 Card ID.
func (r *CardRepository) ListTrashedIDs(ctx context.Context) ([]int64, error) {
	ids := make([]int64, 0, 16)
	err := r.db.WithContext(ctx).Model(&model.TrashedCard{}).
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}
