// Package repo 实现 GORM 仓库, 属于基础设施层, 不感知 HTTP.
package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/platform/settings"
)

// SettingsRepository 基于 GORM 实现 settings.Provider.
type SettingsRepository struct {
	db *gorm.DB
}

// NewSettingsRepository 构造设置仓库.
func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

var _ settings.Provider = (*SettingsRepository)(nil)

// LoadAll 读取全部设置行, 返回原始字符串值.
func (r *SettingsRepository) LoadAll(ctx context.Context) (map[string]string, error) {
	var rows []model.SettingKey
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.SettingKey] = row.SettingValue
	}
	return out, nil
}

// Apply 在单个事务内逐项 upsert, 全部成功才提交, 返回更新后的完整值集.
// 冲突分支递增 version 并刷新 updated_at, 保持设置的乐观锁语义连续.
func (r *SettingsRepository) Apply(ctx context.Context, updates []settings.Update) (map[string]string, error) {
	registry := settings.Default()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := persistence.Now()
		for _, u := range updates {
			encoded, valueType, err := registry.MarshalValue(u.Key, u.Value)
			if err != nil {
				return err
			}
			row := model.SettingKey{
				SettingKey:   u.Key,
				SettingValue: encoded,
				ValueType:    int16(valueType),
				Version:      1,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "setting_key"}},
				DoUpdates: clause.Assignments(map[string]any{
					"setting_value": encoded,
					"value_type":    int16(valueType),
					"version":       gorm.Expr("version + 1"),
					"updated_at":    now,
				}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.LoadAll(ctx)
}

// EnsureDefaults 为注册表中尚无数据库行的设置补写默认值, 已存在的值不动.
// 只在启动初始化时调用, 防止每次重启把用户在 WebUI 里的修改冲掉.
func (r *SettingsRepository) EnsureDefaults(ctx context.Context) error {
	now := persistence.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, s := range settings.Default().All() {
			row := model.SettingKey{
				SettingKey:   s.Key,
				SettingValue: s.Default,
				ValueType:    int16(s.Type),
				Version:      1,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "setting_key"}},
				DoNothing: true,
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AppLogWriter 把 app_log 批量写入数据库, 是日志 sink 的落库端.
type AppLogWriter struct {
	db *gorm.DB
}

// NewAppLogWriter 构造日志写入器.
func NewAppLogWriter(db *gorm.DB) *AppLogWriter { return &AppLogWriter{db: db} }

// InsertAppLogs 批量插入日志行, 补齐缺失时间戳防止零值入库.
func (w *AppLogWriter) InsertAppLogs(ctx context.Context, rows []model.AppLog) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		if rows[i].LoggedAt.IsZero() {
			rows[i].LoggedAt = time.Now().UTC()
		}
	}
	return w.db.WithContext(ctx).Create(&rows).Error
}

// PruneAppLogs 先删除超过保留天数的行, 再从旧到新裁剪超出 maxRows 的部分,
// 两个上限不分等级一起生效.
//
// 裁剪用 "按 id 倒序偏移定位边界行, 再删除不大于该 id 的行" 实现:
// DELETE ... LIMIT 在 PostgreSQL 上不可用, 子查询方案三个数据库都支持.
func (w *AppLogWriter) PruneAppLogs(ctx context.Context, retentionDays, maxRows int64) error {
	cutoff := persistence.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	if err := w.db.WithContext(ctx).
		Where("logged_at < ?", cutoff).
		Delete(&model.AppLog{}).Error; err != nil {
		return err
	}
	if maxRows <= 0 {
		return nil
	}
	var boundaryID int64
	err := w.db.WithContext(ctx).
		Model(&model.AppLog{}).
		Select("id").
		Order("id DESC").
		Offset(int(maxRows)).
		Limit(1).
		Scan(&boundaryID).Error
	if err != nil {
		return err
	}
	if boundaryID == 0 {
		// 行数未超上限, 偏移查询没有命中行.
		return nil
	}
	return w.db.WithContext(ctx).
		Where("id <= ?", boundaryID).
		Delete(&model.AppLog{}).Error
}
