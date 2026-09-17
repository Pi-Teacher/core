package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/domain/auth"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// AccountRepository 持久化单用户账号.
type AccountRepository struct {
	db *gorm.DB
}

// NewAccountRepository 构造账号仓库.
func NewAccountRepository(db *gorm.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// ErrNoAccount 表示账号不存在, 复用 gorm 的记录不存在错误便于 errors.Is 判断.
var ErrNoAccount = gorm.ErrRecordNotFound

// Get 返回唯一账号行, 按主键升序取第一条以容忍历史遗留的多行.
func (r *AccountRepository) Get(ctx context.Context) (*model.Account, error) {
	var acc model.Account
	err := r.db.WithContext(ctx).Order("id ASC").First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// CreateInitial 写入首条账号, 固定 id=1.
func (r *AccountRepository) CreateInitial(ctx context.Context, passwordHash string) (*model.Account, error) {
	now := persistence.Now()
	acc := model.Account{
		ID:           1,
		PasswordHash: passwordHash,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := r.db.WithContext(ctx).Create(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// ChangePassword 校验版本后更新密码哈希, 并在同一事务删除全部 session,
// 防止旧会话在改密后继续有效.
func (r *AccountRepository) ChangePassword(ctx context.Context, id, expectedVersion int64, newHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := persistence.UpdateOptimistic(tx, &model.Account{}, id, expectedVersion, map[string]any{
			"password_hash": newHash,
		}); err != nil {
			return err
		}
		return tx.Where("account_id = ?", id).Delete(&model.WebSession{}).Error
	})
}

// ForceResetPassword 不校验期望版本, 供本机 admin 重置命令使用,
// 同样在同一事务吊销全部 session.
func (r *AccountRepository) ForceResetPassword(ctx context.Context, id int64, newHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Account{}).Where("id = ?", id).Updates(map[string]any{
			"password_hash": newHash,
			"version":       gorm.Expr("version + 1"),
			"updated_at":    persistence.Now(),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return persistence.ErrVersionConflict
		}
		return tx.Where("account_id = ?", id).Delete(&model.WebSession{}).Error
	})
}

// SessionRepository 持久化 Web 登录 session.
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 构造 session 仓库.
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create 写入新 session, 过期时间固定为创建时间加 30 天, 不滑动续期.
func (r *SessionRepository) Create(ctx context.Context, accountID int64, tokenHash, csrfHash string, now time.Time) (*model.WebSession, error) {
	s := model.WebSession{
		AccountID:     accountID,
		TokenHash:     tokenHash,
		CSRFTokenHash: csrfHash,
		CreatedAt:     now,
		ExpiresAt:     now.Add(auth.SessionTTL),
	}
	if err := r.db.WithContext(ctx).Create(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// FindByTokenHash 按 token 哈希查找未过期 session.
func (r *SessionRepository) FindByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*model.WebSession, error) {
	var s model.WebSession
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", tokenHash, now).
		First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteByTokenHash 删除 session, 用于注销.
func (r *SessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&model.WebSession{}).Error
}

// DeleteExpired 删除已过期 session, 供后台维护任务调用.
func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&model.WebSession{})
	return res.RowsAffected, res.Error
}

// APIKeyRepository 持久化 API Key.
type APIKeyRepository struct {
	db *gorm.DB
}

// NewAPIKeyRepository 构造 API Key 仓库.
func NewAPIKeyRepository(db *gorm.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

// Create 写入一条 API Key.
func (r *APIKeyRepository) Create(ctx context.Context, accountID int64, name, key string, now time.Time) (*model.APIKey, error) {
	row := model.APIKey{
		AccountID: accountID,
		Name:      name,
		APIKey:    key,
		Version:   1,
		CreatedAt: now,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 返回账号下的 API Key 分页, 按创建时间倒序.
func (r *APIKeyRepository) List(ctx context.Context, accountID int64, offset, limit int) ([]model.APIKey, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.APIKey{}).Where("account_id = ?", accountID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.APIKey
	if err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// FindByKey 按明文 key 精确匹配. key 明文存储是既定决策,
// 因此查询不需要哈希索引之外的任何变换.
func (r *APIKeyRepository) FindByKey(ctx context.Context, key string) (*model.APIKey, error) {
	var row model.APIKey
	if err := r.db.WithContext(ctx).Where("api_key = ?", key).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Delete 物理删除即吊销. 审批与幂等历史只保留当时的 API Key ID, 不级联.
func (r *APIKeyRepository) Delete(ctx context.Context, accountID, id int64) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND account_id = ?", id, accountID).
		Delete(&model.APIKey{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
