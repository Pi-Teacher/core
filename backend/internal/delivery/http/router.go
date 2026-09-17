package httpapi

import (
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/platform/version"
)

// RouterConfig 汇集路由构造所需的全部依赖.
type RouterConfig struct {
	Auth         *appsvc.AuthService
	Logger       *slog.Logger
	DBDriver     string
	StartedAt    time.Time
	AllowOrigins []string
}

// NewRouter 构造顶层 HTTP handler.
//
// /api/web 与 /api/cli 是两个隔离的认证命名空间, 各自挂认证中间件;
// 未知路由返回统一错误信封. 内嵌 WebUI 在后续批次接入.
func NewRouter(cfg RouterConfig) http.Handler {
	s := &Server{
		Auth:   cfg.Auth,
		Logger: cfg.Logger,
	}
	if len(cfg.AllowOrigins) > 0 {
		s.AllowedOrigins = make(map[string]struct{}, len(cfg.AllowOrigins))
		for _, o := range cfg.AllowOrigins {
			s.AllowedOrigins[strings.ToLower(strings.TrimSpace(o))] = struct{}{}
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)

	// login 不挂会话中间件: 它负责建立 session, 自行做来源校验.
	// 其余认证端点统一走 requireWebSession, 写请求自动附带 CSRF 校验.
	mux.HandleFunc("POST /api/web/auth/login", s.handleLogin)
	mux.Handle("POST /api/web/auth/logout", s.requireWebSession(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/web/auth/session", s.requireWebSession(http.HandlerFunc(s.handleSession)))
	mux.Handle("PATCH /api/web/auth/password", s.requireWebSession(http.HandlerFunc(s.handleChangePassword)))

	mux.Handle("GET /api/web/api-keys", s.requireWebSession(http.HandlerFunc(s.handleListAPIKeys)))
	mux.Handle("POST /api/web/api-keys", s.requireWebSession(http.HandlerFunc(s.handleCreateAPIKey)))
	mux.Handle("DELETE /api/web/api-keys/{id}", s.requireWebSession(http.HandlerFunc(s.handleDeleteAPIKey)))

	mux.Handle("GET /api/cli/system/info", s.requireCLIAPIKey(http.HandlerFunc(
		s.handleSystemInfo(cfg.DBDriver, cfg.StartedAt),
	)))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, apperr.NotFound("路由不存在"))
	}))

	var handler http.Handler = mux
	handler = withRecovery(cfg.Logger, handler)
	handler = withRequestID(handler)
	handler = withAccessLog(cfg.Logger, handler)
	return handler
}

// handleSystemInfo 返回版本, 驱动与运行时长等诊断信息.
func (s *Server) handleSystemInfo(dbDriver string, startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, systemInfoResponse{
			Version:       version.Version,
			GoVersion:     runtime.Version(),
			DBDriver:      dbDriver,
			UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		})
	}
}
