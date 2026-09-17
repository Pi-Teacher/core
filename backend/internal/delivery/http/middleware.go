package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// SessionCookieName 是 WebUI 的 session cookie 名.
const SessionCookieName = "pi_teacher_session"

// CSRFCookieName 是可读的 CSRF 伴随 cookie.
// 校验始终针对 session 绑定的哈希, 这个 cookie 只是让 WebUI 能取回明文.
const CSRFCookieName = "pi_teacher_csrf"

// CookiePath 把 cookie 作用域限制在 Web API 内.
const CookiePath = "/api/web"

// Server 汇集 handler 共享的依赖.
type Server struct {
	Auth   *appsvc.AuthService
	Logger *slog.Logger
	// AllowedOrigins 是 CSRF 来源校验额外接受的来源集合,
	// 为空时只按请求 Host 匹配.
	AllowedOrigins map[string]struct{}
}

// sessionFromContext 返回已认证的 session, 不存在时为 nil.
func sessionFromContext(ctx context.Context) *model.WebSession {
	if v, ok := ctx.Value(ctxKeySession).(*model.WebSession); ok {
		return v
	}
	return nil
}

// apiKeyFromContext 返回已认证的 API Key, 不存在时为 nil.
func apiKeyFromContext(ctx context.Context) *model.APIKey {
	if v, ok := ctx.Value(ctxKeyAPIKey).(*model.APIKey); ok {
		return v
	}
	return nil
}

// requireWebSession 是 /api/web 的认证中间件.
//
// 写请求 (非 GET/HEAD) 额外做两层校验: Origin/Referer 与请求 Host 一致,
// 以及 session 绑定的 CSRF token. 只依赖 SameSite 不足以防御 CSRF,
// 因此两道都做.
func (s *Server) requireWebSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, apperr.Unauthorized("未登录"))
			return
		}
		sess, err := s.Auth.ResolveSession(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.checkOrigin(r) {
				writeError(w, apperr.Forbidden("Origin 校验失败"))
				return
			}
			if !s.Auth.VerifyCSRF(sess, r.Header.Get("X-CSRF-Token")) {
				writeError(w, apperr.Forbidden("CSRF 校验失败"))
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxKeySession, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireCLIAPIKey 是 /api/cli 的认证中间件, 只认 Bearer API Key.
// 即使请求同时携带有效 session 也按未认证处理, 两个命名空间绝不互通.
func (s *Server) requireCLIAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := trimBearer(header)
		if !ok {
			writeError(w, apperr.Unauthorized("缺少 API Key"))
			return
		}
		key, err := s.Auth.AuthenticateAPIKey(r.Context(), token)
		if err != nil {
			writeError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyAPIKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// checkOrigin 校验 Origin (缺省时 Referer) 与请求 Host 一致.
//
// 两个头都没有时放行: 此时 CSRF token 仍然保护写请求, 这种组合只出现在
// 非浏览器客户端, 浏览器同源请求必带其一.
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if len(s.AllowedOrigins) > 0 {
		if _, ok := s.AllowedOrigins[strings.ToLower(u.Scheme+"://"+u.Host)]; ok {
			return true
		}
	}
	return strings.EqualFold(u.Host, r.Host)
}
