package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// CalendarRepository 持久化按自然日累计的鼓励统计.
// Calendar 是非核心统计, 不用乐观锁, 增量走原子 UPSERT.
type CalendarRepository struct {
	db *gorm.DB
}

// NewCalendarRepository 构造 Calendar 仓库.
func NewCalendarRepository(db *gorm.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本, 供服务层在单个事务中
// 组合多个仓库的操作; 原仓库不受影响.
func (r *CalendarRepository) WithTx(tx *gorm.DB) *CalendarRepository {
	return &CalendarRepository{db: tx}
}

// AddCreatedCards 原子递增某自然日的制卡计数, 行不存在时插入.
//
// 三方言兼容性: GORM 把 clause.OnConflict 翻译为 SQLite/PostgreSQL 的
// ON CONFLICT DO UPDATE 和 MySQL 的 ON DUPLICATE KEY UPDATE;
// 赋值右侧用裸列名引用已存在行, 三种数据库语义一致.
func (r *CalendarRepository) AddCreatedCards(ctx context.Context, day time.Time, delta int64, now time.Time) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "activity_date"}},
		DoUpdates: clause.Assignments(map[string]any{
			"created_cards": gorm.Expr("created_cards + ?", delta),
			"updated_at":    now,
		}),
	}).Create(&model.Calendar{
		ActivityDate: day,
		CreatedCards: delta,
		ReviewEvents: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error
}
