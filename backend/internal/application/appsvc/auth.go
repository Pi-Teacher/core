// Package appsvc 承载应用服务: 编排领域规则与仓库,
// 被 Web 和 CLI 两个交付层共享, 保证两套路由复用同一套业务规则.
package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/domain/auth"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// AuthService 负责账号, session 与 API Key 行为.
type AuthService struct {
	accounts *repo.AccountRepository
	sessions *repo.SessionRepository
	apiKeys  *repo.APIKeyRepository
	attempts *auth.LoginAttempts
	logger   *slog.Logger
	// now 集中提供当前 UTC 时间, 便于测试替换.
	now func() time.Time
}

// NewAuthService 构造认证服务.
func NewAuthService(
	accounts *repo.AccountRepository,
	sessions *repo.SessionRepository,
	apiKeys *repo.APIKeyRepository,
	logger *slog.Logger,
) *AuthService {
	return &AuthService{
		accounts: accounts,
		sessions: sessions,
		apiKeys:  apiKeys,
		attempts: auth.NewLoginAttempts(),
		logger:   logger,
		now:      func() time.Time { return persistence.Now() },
	}
}

// EnsureInitialAccount 在首次启动时创建唯一账号并返回明文初始密码.
//
// 明文密码只在这里出现一次, 调用方必须直接写 stderr, 不经过任何可能
// 落库的 slog handler. 第二个返回值为 false 表示账号已存在, 此时不再
// 生成密码. 并发创建撞上唯一约束时按已初始化处理, 不把错误抛给用户.
func (s *AuthService) EnsureInitialAccount(ctx context.Context) (string, bool, error) {
	_, err := s.accounts.Get(ctx)
	if err == nil {
		return "", false, nil
	}
	if !errors.Is(err, repo.ErrNoAccount) {
		return "", false, err
	}
	password, err := auth.GeneratePassword()
	if err != nil {
		return "", false, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", false, err
	}
	if _, err := s.accounts.CreateInitial(ctx, hash); err != nil {
		if _, getErr := s.accounts.Get(ctx); getErr == nil {
			return "", false, nil
		}
		return "", false, err
	}
	return password, true, nil
}

// ResetPassword 生成新的随机密码并吊销全部 session, 仅供本机 admin 命令使用.
func (s *AuthService) ResetPassword(ctx context.Context) (string, error) {
	acc, err := s.accounts.Get(ctx)
	if err != nil {
		return "", err
	}
	password, err := auth.GeneratePassword()
	if err != nil {
		return "", err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	if err := s.accounts.ForceResetPassword(ctx, acc.ID, hash); err != nil {
		return "", err
	}
	return password, nil
}

// LoginResult 是登录成功后返回给客户端的凭据.
type LoginResult struct {
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

// Login 校验密码并创建 session.
//
// 限流检查放在密码校验之前, 让延迟先生效; 连续失败 5 次后开始渐进延迟,
// 成功后立即清零. 密码错误统一返回 unauthorized, 不区分账号是否存在,
// 避免给探测方提供信息.
func (s *AuthService) Login(ctx context.Context, password string) (*LoginResult, error) {
	now := s.now()
	if d := s.attempts.RetryAfter(now); d > 0 {
		return nil, apperr.New(apperr.CodeRateLimited, "登录失败过多, 请稍后重试").
			WithDetails(map[string]any{"retry_after_seconds": int(d.Seconds() + 0.999)})
	}

	acc, err := s.accounts.Get(ctx)
	if err != nil {
		if errors.Is(err, repo.ErrNoAccount) {
			s.attempts.Fail(now)
			return nil, apperr.Unauthorized("密码错误")
		}
		return nil, apperr.From(err)
	}
	if err := auth.VerifyPassword(acc.PasswordHash, password); err != nil {
		s.attempts.Fail(now)
		if errors.Is(err, auth.ErrMismatch) {
			return nil, apperr.Unauthorized("密码错误")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "校验密码失败", err)
	}
	s.attempts.Reset()

	sessionToken, err := auth.GenerateToken(auth.SessionTokenBytes)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "生成 session 失败", err)
	}
	csrfToken, err := auth.GenerateToken(auth.SessionTokenBytes)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "生成 CSRF token 失败", err)
	}
	sess, err := s.sessions.Create(ctx, acc.ID, auth.HashToken(sessionToken), auth.HashToken(csrfToken), now)
	if err != nil {
		return nil, apperr.From(err)
	}
	return &LoginResult{SessionToken: sessionToken, CSRFToken: csrfToken, ExpiresAt: sess.ExpiresAt}, nil
}

// ResolveSession 把 cookie 中的原始 token 解析为有效 session,
// 过期或不存在都按未登录处理, 不区分原因.
func (s *AuthService) ResolveSession(ctx context.Context, token string) (*model.WebSession, error) {
	if token == "" {
		return nil, apperr.Unauthorized("未登录")
	}
	sess, err := s.sessions.FindByTokenHash(ctx, auth.HashToken(token), s.now())
	if err != nil {
		return nil, apperr.Unauthorized("session 无效或已过期")
	}
	return sess, nil
}

// VerifyCSRF 校验请求头携带的 CSRF token 是否与 session 绑定的哈希一致.
func (s *AuthService) VerifyCSRF(sess *model.WebSession, token string) bool {
	if sess == nil || token == "" {
		return false
	}
	return auth.HashToken(token) == sess.CSRFTokenHash
}

// Logout 删除原始 token 对应的 session.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteByTokenHash(ctx, auth.HashToken(token))
}

// ChangePassword 校验当前密码后设置新密码, 并吊销全部 session (含当前).
// 当前密码错误按表单校验错误返回, 让前端定位到输入框.
func (s *AuthService) ChangePassword(ctx context.Context, current, next string) error {
	if strings.TrimSpace(next) == "" {
		return apperr.Validation("新密码不能为空")
	}
	acc, err := s.accounts.Get(ctx)
	if err != nil {
		return apperr.From(err)
	}
	if err := auth.VerifyPassword(acc.PasswordHash, current); err != nil {
		if errors.Is(err, auth.ErrMismatch) {
			return apperr.Validation("当前密码错误").
				WithDetails(map[string]any{"field": "current_password"})
		}
		return apperr.Wrap(apperr.CodeInternal, "校验密码失败", err)
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "生成密码哈希失败", err)
	}
	if err := s.accounts.ChangePassword(ctx, acc.ID, acc.Version, hash); err != nil {
		if errors.Is(err, persistence.ErrVersionConflict) {
			return apperr.Conflict(apperr.CodeVersionConflict, "账号已被修改, 请重试").
				WithDetails(map[string]any{"current_version": acc.Version})
		}
		return apperr.From(err)
	}
	return nil
}

// CreateAPIKey 生成明文 ptk_ 前缀 API Key 并落库.
func (s *AuthService) CreateAPIKey(ctx context.Context, name string) (*model.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.Validation("name 不能为空").WithDetails(map[string]any{"field": "name"})
	}
	if len([]rune(name)) > 200 {
		return nil, apperr.Validation("name 最多 200 字符").WithDetails(map[string]any{"field": "name"})
	}
	acc, err := s.accounts.Get(ctx)
	if err != nil {
		return nil, apperr.From(err)
	}
	key, err := auth.GenerateAPIKey()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "生成 API Key 失败", err)
	}
	row, err := s.apiKeys.Create(ctx, acc.ID, name, key, s.now())
	if err != nil {
		return nil, apperr.From(err)
	}
	return row, nil
}

// ListAPIKeys 返回一页 API Key.
func (s *AuthService) ListAPIKeys(ctx context.Context, page, pageSize int) ([]model.APIKey, int64, error) {
	acc, err := s.accounts.Get(ctx)
	if err != nil {
		return nil, 0, apperr.From(err)
	}
	offset := (page - 1) * pageSize
	rows, total, err := s.apiKeys.List(ctx, acc.ID, offset, pageSize)
	if err != nil {
		return nil, 0, apperr.From(err)
	}
	return rows, total, nil
}

// DeleteAPIKey 物理删除一条 API Key.
func (s *AuthService) DeleteAPIKey(ctx context.Context, id int64) error {
	acc, err := s.accounts.Get(ctx)
	if err != nil {
		return apperr.From(err)
	}
	ok, err := s.apiKeys.Delete(ctx, acc.ID, id)
	if err != nil {
		return apperr.From(err)
	}
	if !ok {
		return apperr.NotFound("API Key 不存在")
	}
	return nil
}

// AuthenticateAPIKey 把 Bearer token 解析为 API Key 行,
// 无效或已删除都按未认证处理, 不区分原因.
func (s *AuthService) AuthenticateAPIKey(ctx context.Context, token string) (*model.APIKey, error) {
	if !auth.IsAPIKey(token) {
		return nil, apperr.Unauthorized("API Key 无效")
	}
	row, err := s.apiKeys.FindByKey(ctx, token)
	if err != nil {
		return nil, apperr.Unauthorized("API Key 无效或已删除")
	}
	return row, nil
}
