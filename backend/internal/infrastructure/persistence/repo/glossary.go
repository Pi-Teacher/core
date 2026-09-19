package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// GlossaryRepository 持久化 Glossary. 与 Topic 同构: trashed_at 区分
// 回收状态, 名称唯一由服务层维护, 数据库不建 UNIQUE 索引.
type GlossaryRepository struct {
	db *gorm.DB
}

// NewGlossaryRepository 构造 Glossary 仓库.
func NewGlossaryRepository(db *gorm.DB) *GlossaryRepository {
	return &GlossaryRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本, 供服务层在单个事务中
// 组合多个仓库的操作; 原仓库不受影响.
func (r *GlossaryRepository) WithTx(tx *gorm.DB) *GlossaryRepository {
	return &GlossaryRepository{db: tx}
}

// FindActive 按 id 查找正常 Glossary, 不存在时返回 gorm.ErrRecordNotFound.
func (r *GlossaryRepository) FindActive(ctx context.Context, id int64) (*model.Glossary, error) {
	var g model.Glossary
	err := r.db.WithContext(ctx).Where("id = ? AND trashed_at IS NULL", id).First(&g).Error
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// FindTrashed 按 id 查找回收站中的 Glossary.
func (r *GlossaryRepository) FindTrashed(ctx context.Context, id int64) (*model.Glossary, error) {
	var g model.Glossary
	err := r.db.WithContext(ctx).Where("id = ? AND trashed_at IS NOT NULL", id).First(&g).Error
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ExistsActiveByTerm 判断是否存在同名正常 Glossary.
// excludeID 用于改名场景排除自身, 传 0 表示不排除.
func (r *GlossaryRepository) ExistsActiveByTerm(ctx context.Context, term string, excludeID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Glossary{}).
		Where("term = ? AND trashed_at IS NULL AND id <> ?", term, excludeID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteTrashedByTerm 物理删除同名回收站记录, 创建与改名时用于覆盖.
// 返回被删除记录 ID, 供调用方把依赖旧记录的 pending 审批标记 stale.
func (r *GlossaryRepository) DeleteTrashedByTerm(ctx context.Context, term string) ([]int64, error) {
	ids := make([]int64, 0, 2)
	err := r.db.WithContext(ctx).Model(&model.Glossary{}).
		Where("term = ? AND trashed_at IS NOT NULL", term).
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&model.Glossary{}).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ListTrashedIDs 返回全部回收站 Glossary 的 ID, 供清空回收站前记录
// 需要联动 stale 的对象.
func (r *GlossaryRepository) ListTrashedIDs(ctx context.Context) ([]int64, error) {
	ids := make([]int64, 0, 16)
	err := r.db.WithContext(ctx).Model(&model.Glossary{}).
		Where("trashed_at IS NOT NULL").
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ListActive 返回正常 Glossary 分页, q 为术语子串搜索,
// ASCII 大小写不敏感, 按最近更新在前.
func (r *GlossaryRepository) ListActive(ctx context.Context, q string, offset, limit int) ([]model.Glossary, int64, error) {
	scope := r.db.WithContext(ctx).Model(&model.Glossary{}).Where("trashed_at IS NULL")
	if q != "" {
		scope = scope.Where("LOWER(term) LIKE ? ESCAPE '\\'", likePattern(q))
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.Glossary, 0, limit)
	if err := scope.Order("updated_at DESC, id DESC").
		Offset(offset).Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListTrashed 返回回收站 Glossary 分页, 按最近回收在前.
func (r *GlossaryRepository) ListTrashed(ctx context.Context, offset, limit int) ([]model.Glossary, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Glossary{}).Where("trashed_at IS NOT NULL")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.Glossary, 0, limit)
	if err := q.Order("trashed_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Create 插入一条 Glossary.
func (r *GlossaryRepository) Create(ctx context.Context, g *model.Glossary) error {
	return r.db.WithContext(ctx).Create(g).Error
}

// Restore 把回收站中的 Glossary 恢复为正常对象, 条件更新携带期望
// version 承载乐观锁. 返回 false 表示记录不存在、不在回收站或版本不匹配.
func (r *GlossaryRepository) Restore(ctx context.Context, id, expectedVersion int64, now time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Glossary{}).
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

// DeleteTrashed 永久删除回收站中的 Glossary, 条件携带期望 version.
// 返回 false 表示记录不存在、不在回收站或版本不匹配.
func (r *GlossaryRepository) DeleteTrashed(ctx context.Context, id, expectedVersion int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND version = ? AND trashed_at IS NOT NULL", id, expectedVersion).
		Delete(&model.Glossary{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// DeleteAllTrashed 物理删除全部回收站 Glossary, 供清空回收站使用.
// 清空本身就是全表语义, 显式开放全局删除避免 GORM 的防误删拦截.
func (r *GlossaryRepository) DeleteAllTrashed(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&model.Glossary{})
	return res.RowsAffected, res.Error
}
