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
	Topics       *appsvc.TopicService
	Cards        *appsvc.CardService
	Glossaries   *appsvc.GlossaryService
	Trash        *appsvc.TrashService
	Logger       *slog.Logger
	DBDriver     string
	StartedAt    time.Time
	AllowOrigins []string
}

// NewRouter 构造顶层 HTTP handler.
//
// /api/web 与 /api/cli 是两个隔离的认证命名空间, 各自挂认证中间件;
// 两套路由复用同一组 handler, 路由层只负责认证来源与 CLI 写请求的
// 幂等头强制. 审批分流 (202 提案) 在第三批接入. 未知路由返回统一错误信封.
func NewRouter(cfg RouterConfig) http.Handler {
	s := &Server{
		Auth:       cfg.Auth,
		Topics:     cfg.Topics,
		Cards:      cfg.Cards,
		Glossaries: cfg.Glossaries,
		Trash:      cfg.Trash,
		Logger:     cfg.Logger,
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

	// --- 领域 CRUD: Web 与 CLI 复用同一组 handler ---

	// Topic. trash-preview 是 WebUI 二次确认专用, 不开放给 CLI.
	s.registerCRUD(mux, "topics", "GET", "", s.handleListTopics)
	s.registerCRUD(mux, "topics", "POST", "", s.handleCreateTopic)
	s.registerCRUD(mux, "topics", "POST", "/batch-create", s.handleBatchCreateTopics)
	s.registerCRUD(mux, "topics", "POST", "/batch-trash", s.handleBatchTrashTopics)
	s.registerCRUD(mux, "topics", "GET", "/{id}", s.handleGetTopic)
	s.registerCRUD(mux, "topics", "PATCH", "/{id}", s.handleUpdateTopic)
	s.registerCRUD(mux, "topics", "POST", "/{id}/trash", s.handleTrashTopic)
	mux.Handle("POST /api/web/topics/trash-preview", s.requireWebSession(http.HandlerFunc(s.handleTrashPreviewTopics)))

	// Card.
	s.registerCRUD(mux, "cards", "GET", "", s.handleListCards)
	s.registerCRUD(mux, "cards", "POST", "", s.handleCreateCard)
	s.registerCRUD(mux, "cards", "POST", "/batch-create", s.handleBatchCreateCards)
	s.registerCRUD(mux, "cards", "POST", "/batch-trash", s.handleBatchTrashCards)
	s.registerCRUD(mux, "cards", "GET", "/{id}", s.handleGetCard)
	s.registerCRUD(mux, "cards", "PATCH", "/{id}", s.handleUpdateCard)
	s.registerCRUD(mux, "cards", "POST", "/{id}/trash", s.handleTrashCard)

	// Glossary.
	s.registerCRUD(mux, "glossary", "GET", "", s.handleListGlossary)
	s.registerCRUD(mux, "glossary", "POST", "", s.handleCreateGlossary)
	s.registerCRUD(mux, "glossary", "POST", "/batch-create", s.handleBatchCreateGlossary)
	s.registerCRUD(mux, "glossary", "POST", "/batch-trash", s.handleBatchTrashGlossary)
	s.registerCRUD(mux, "glossary", "GET", "/{id}", s.handleGetGlossary)
	s.registerCRUD(mux, "glossary", "PATCH", "/{id}", s.handleUpdateGlossary)
	s.registerCRUD(mux, "glossary", "POST", "/{id}/trash", s.handleTrashGlossary)

	// --- 回收站 ---

	// 列表: Web 与 CLI 同构.
	s.registerCRUD(mux, "trash/cards", "GET", "", s.handleListTrashedCards)
	s.registerCRUD(mux, "trash/topics", "GET", "", s.handleListTrashedTopics)
	s.registerCRUD(mux, "trash/glossary", "GET", "", s.handleListTrashedGlossary)

	// 恢复: Web 直接生效; CLI 本批直写, 第三批接入审批开关分流.
	// 批量恢复仅 Web 提供.
	s.registerCRUD(mux, "trash/cards", "POST", "/{id}/restore", s.handleRestoreCard)
	s.registerCRUD(mux, "trash/topics", "POST", "/{id}/restore", s.handleRestoreTopic)
	s.registerCRUD(mux, "trash/glossary", "POST", "/{id}/restore", s.handleRestoreGlossary)
	mux.Handle("POST /api/web/trash/cards/batch-restore", s.requireWebSession(http.HandlerFunc(s.handleBatchRestoreCards)))
	mux.Handle("POST /api/web/trash/topics/batch-restore", s.requireWebSession(http.HandlerFunc(s.handleBatchRestoreTopics)))
	mux.Handle("POST /api/web/trash/glossary/batch-restore", s.requireWebSession(http.HandlerFunc(s.handleBatchRestoreGlossary)))

	// 永久删除与清空: 只开放给 Web 会话, CLI 永远不可永久删除.
	mux.Handle("POST /api/web/trash/cards/{id}/delete", s.requireWebSession(http.HandlerFunc(s.handleDeleteTrashedCard)))
	mux.Handle("POST /api/web/trash/topics/{id}/delete", s.requireWebSession(http.HandlerFunc(s.handleDeleteTrashedTopic)))
	mux.Handle("POST /api/web/trash/glossary/{id}/delete", s.requireWebSession(http.HandlerFunc(s.handleDeleteTrashedGlossary)))
	mux.Handle("POST /api/web/trash/cards/batch-delete", s.requireWebSession(http.HandlerFunc(s.handleBatchDeleteTrashedCards)))
	mux.Handle("POST /api/web/trash/topics/batch-delete", s.requireWebSession(http.HandlerFunc(s.handleBatchDeleteTrashedTopics)))
	mux.Handle("POST /api/web/trash/glossary/batch-delete", s.requireWebSession(http.HandlerFunc(s.handleBatchDeleteTrashedGlossary)))
	mux.Handle("POST /api/web/trash/empty", s.requireWebSession(http.HandlerFunc(s.handleEmptyTrash)))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, apperr.NotFound("路由不存在"))
	}))

	var handler http.Handler = mux
	handler = withRecovery(cfg.Logger, handler)
	handler = withRequestID(handler)
	handler = withAccessLog(cfg.Logger, handler)
	return handler
}

// registerCRUD 把同一个 handler 同时挂到 Web 与 CLI 命名空间,
// 是两套路由复用同一应用服务的落点: Web 挂 session 认证,
// CLI 挂 API Key 认证, CLI 写请求额外强制 Idempotency-Key 头.
// method 为 HTTP 方法, pattern 是相对资源根的子路径 (如 "/{id}/trash").
func (s *Server) registerCRUD(mux *http.ServeMux, resource, method, pattern string, h http.HandlerFunc) {
	mux.Handle(method+" /api/web/"+resource+pattern, s.requireWebSession(h))
	cliHandler := http.Handler(h)
	if method != http.MethodGet {
		cliHandler = requireIdempotencyKey(cliHandler)
	}
	mux.Handle(method+" /api/cli/"+resource+pattern, s.requireCLIAPIKey(cliHandler))
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
