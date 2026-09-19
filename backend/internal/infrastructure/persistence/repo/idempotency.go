package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// IdempotencyRepository 持久化 CLI 写请求的幂等记录.
// 记录只保存 2xx 响应, 业务失败回滚后不落行, 同 Key 可重试.
type IdempotencyRepository struct {
	db *gorm.DB
}

// NewIdempotencyRepository 构造幂等仓库.
func NewIdempotencyRepository(db *gorm.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本.
func (r *IdempotencyRepository) WithTx(tx *gorm.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: tx}
}

// Find 按 (api_key_id, idempotency_key) 查找记录, 不存在时返回
// (nil, nil): 未命中是正常路径而不是错误.
func (r *IdempotencyRepository) Find(ctx context.Context, apiKeyID int64, key string) (*model.IdempotencyRecord, error) {
	var rec model.IdempotencyRecord
	err := r.db.WithContext(ctx).
		Where("api_key_id = ? AND idempotency_key = ?", apiKeyID, key).
		First(&rec).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// Claim 以 processing 状态抢先占位幂等键. 唯一索引冲突时什么都不做,
// 由返回的 false 告知调用方键已被占用 (含并发同 Key 请求).
//
// 用 ON CONFLICT DO NOTHING 而不是先查后插, 避免两个并发请求都查不到
// 记录后同时插入; 该写法由 GORM 翻译为三种数据库各自的等价语法.
func (r *IdempotencyRepository) Claim(ctx context.Context, rec *model.IdempotencyRecord) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rec)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Complete 把记录改写为已完成并保存可重放的响应.
func (r *IdempotencyRepository) Complete(ctx context.Context, id int64, status int64, body string) error {
	return r.db.WithContext(ctx).Model(&model.IdempotencyRecord{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"state":           model.IdempotencyCompleted,
			"response_status": status,
			"response_body":   body,
		}).Error
}

// DeleteExpired 删除全部已过期记录, 返回删除行数.
func (r *IdempotencyRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("expires_at <= ?", now).
		Delete(&model.IdempotencyRecord{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// DeleteByID 按 id 删除单条记录, 供命中过期记录时就地清理.
func (r *IdempotencyRepository) DeleteByID(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).
		Where("id = ?", id).
		Delete(&model.IdempotencyRecord{}).Error
}
