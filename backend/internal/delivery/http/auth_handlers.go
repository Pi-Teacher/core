package httpapi

import (
	"net/http"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/platform/logging"
)

// loginRequest 是 POST /api/web/auth/login 的请求体.
type loginRequest struct {
	Password string `json:"password"`
}

// sessionResponse 是登录与 session 查询的响应体.
type sessionResponse struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleLogin 实现 POST /api/web/auth/login.
// 登录入口尚未建立 session, 因此在这里单独做来源校验.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if !s.checkOrigin(r) {
		writeError(w, apperr.Forbidden("Origin 校验失败"))
		return
	}
	result, err := s.Auth.Login(r.Context(), req.Password)
	if err != nil {
		s.Logger.WarnContext(logging.WithEvent(r.Context(), "auth_login_failed"),
			"登录失败",
			logging.AttrRequestID, RequestIDFromContext(r.Context()),
			logging.AttrError, err.Error(),
		)
		writeError(w, err)
		return
	}
	s.setSessionCookies(w, result.SessionToken, result.CSRFToken, result.ExpiresAt)
	s.Logger.InfoContext(logging.WithEvent(r.Context(), "auth_login_succeeded"),
		"登录成功",
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
		logging.AttrSource, "web",
	)
	writeJSON(w, http.StatusOK, sessionResponse{
		CSRFToken: result.CSRFToken,
		ExpiresAt: result.ExpiresAt,
	})
}

// handleLogout 实现 POST /api/web/auth/logout, CSRF 由会话中间件校验.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, apperr.Unauthorized("未登录"))
		return
	}
	if err := s.Auth.Logout(r.Context(), cookie.Value); err != nil {
		writeError(w, err)
		return
	}
	s.clearSessionCookies(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSession 实现 GET /api/web/auth/session.
//
// 数据库只存 CSRF token 的 SHA-256, 无法还原明文; 明文在登录时写入了
// 可读的伴随 cookie, 这里从它取值返回, 让刷新页面的 WebUI 也能恢复 token.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	if sess == nil {
		writeError(w, apperr.Unauthorized("未登录"))
		return
	}
	csrf := ""
	if c, err := r.Cookie(CSRFCookieName); err == nil {
		csrf = c.Value
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		CSRFToken: csrf,
		ExpiresAt: sess.ExpiresAt,
	})
}

// passwordRequest 是 PATCH /api/web/auth/password 的请求体.
type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword 实现 PATCH /api/web/auth/password.
// 成功后吊销全部 session, 客户端需要重新登录.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		writeError(w, err)
		return
	}
	s.clearSessionCookies(w)
	s.Logger.InfoContext(logging.WithEvent(r.Context(), "auth_password_changed"),
		"密码已修改, 全部 session 已吊销",
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
	)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setSessionCookies 写入 session 与 CSRF cookie.
//
// session cookie 设 HttpOnly 阻止 JS 读取; CSRF cookie 故意可读,
// WebUI 需要它回填 X-CSRF-Token 头, 校验仍以 session 绑定哈希为准.
// Secure 固定 false: HTTP 局域网与 NAS 部署是必须支持的场景,
// 跨不可信网络时应由反向代理提供 HTTPS.
func (s *Server) setSessionCookies(w http.ResponseWriter, sessionToken, csrfToken string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionToken,
		Path:     CookiePath,
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    csrfToken,
		Path:     CookiePath,
		Expires:  expires,
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
	})
}

// clearSessionCookies 让两个 cookie 立即失效.
func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     CookiePath,
			MaxAge:   -1,
			HttpOnly: name == SessionCookieName,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

// --- API Key ---

// createAPIKeyRequest 是 POST /api/web/api-keys 的请求体.
type createAPIKeyRequest struct {
	Name string `json:"name"`
}

// apiKeyResponse 是 API Key 的完整响应体, 按既定决策返回明文不做脱敏.
type apiKeyResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	APIKey    string    `json:"api_key"`
	CreatedAt time.Time `json:"created_at"`
}

// pageResponse 是统一的分页响应结构.
type pageResponse[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// handleListAPIKeys 实现 GET /api/web/api-keys.
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Auth.ListAPIKeys(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]apiKeyResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, apiKeyResponse{
			ID: row.ID, Name: row.Name, APIKey: row.APIKey, CreatedAt: row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, pageResponse[apiKeyResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleCreateAPIKey 实现 POST /api/web/api-keys.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	row, err := s.Auth.CreateAPIKey(r.Context(), req.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	s.Logger.InfoContext(logging.WithEvent(r.Context(), "api_key_created"),
		"API Key 已创建",
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
		logging.AttrEntityType, "api_key",
		logging.AttrEntityID, row.ID,
	)
	writeJSON(w, http.StatusCreated, apiKeyResponse{
		ID: row.ID, Name: row.Name, APIKey: row.APIKey, CreatedAt: row.CreatedAt,
	})
}

// handleDeleteAPIKey 实现 DELETE /api/web/api-keys/{id}.
func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.Auth.DeleteAPIKey(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	s.Logger.InfoContext(logging.WithEvent(r.Context(), "api_key_revoked"),
		"API Key 已吊销",
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
		logging.AttrEntityType, "api_key",
		logging.AttrEntityID, id,
	)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- 系统 ---

// systemInfoResponse 是 GET /api/cli/system/info 的响应体.
type systemInfoResponse struct {
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	DBDriver      string `json:"db_driver"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// handleHealth 实现 GET /api/health, 无认证.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
