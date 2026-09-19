package httpapi

import (
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/version"
)

// RouterConfig 汇集路由构造所需的全部依赖.
type RouterConfig struct {
	Auth         *appsvc.AuthService
	Topics       *appsvc.TopicService
	Cards        *appsvc.CardService
	Glossaries   *appsvc.GlossaryService
	Trash        *appsvc.TrashService
	Reviews      *appsvc.ReviewService
	Calendar     *repo.CalendarRepository
	Approvals    *appsvc.ApprovalService
	Idempotency  *appsvc.IdempotencyService
	Settings     *appsvc.SettingsService
	Logger       *slog.Logger
	DBDriver     string
	StartedAt    time.Time
	AllowOrigins []string
}

// NewRouter 构造顶层 HTTP handler.
//
// /api/web 与 /api/cli 是两个隔离的认证命名空间, 各自挂认证中间件;
// 两套路由复用同一组 handler. 路由层只负责认证来源与 CLI 写请求的
// 幂等包装与审批分流 (202 提案). 未知路由返回统一错误信封.
func NewRouter(cfg RouterConfig) http.Handler {
	s := &Server{
		Auth:        cfg.Auth,
		Topics:      cfg.Topics,
		Cards:       cfg.Cards,
		Glossaries:  cfg.Glossaries,
		Trash:       cfg.Trash,
		Reviews:     cfg.Reviews,
		Calendar:    cfg.Calendar,
		Approvals:   cfg.Approvals,
		Idempotency: cfg.Idempotency,
		Settings:    cfg.Settings,
		Logger:      cfg.Logger,
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

	// --- 设置 (仅 Web) ---
	mux.Handle("GET /api/web/settings", s.requireWebSession(http.HandlerFunc(s.handleGetSettings)))
	mux.Handle("PATCH /api/web/settings", s.requireWebSession(http.HandlerFunc(s.handlePatchSettings)))

	// --- 审批 (Web 处理; CLI 仅查看自己提交的) ---
	mux.Handle("GET /api/web/approvals", s.requireWebSession(http.HandlerFunc(s.handleListApprovals)))
	mux.Handle("GET /api/web/approvals/{id}", s.requireWebSession(http.HandlerFunc(s.handleGetApproval)))
	mux.Handle("POST /api/web/approvals/{id}/approve", s.requireWebSession(http.HandlerFunc(s.handleApprove)))
	mux.Handle("POST /api/web/approvals/{id}/reject", s.requireWebSession(http.HandlerFunc(s.handleReject)))
	mux.Handle("POST /api/web/approvals/batch-approve", s.requireWebSession(http.HandlerFunc(s.handleBatchApprove)))
	mux.Handle("POST /api/web/approvals/batch-reject", s.requireWebSession(http.HandlerFunc(s.handleBatchReject)))
	mux.Handle("GET /api/cli/approvals", s.requireCLIAPIKey(http.HandlerFunc(s.handleListCLIApprovals)))
	mux.Handle("GET /api/cli/approvals/{id}", s.requireCLIAPIKey(http.HandlerFunc(s.handleGetCLIApproval)))

	// --- 复习与日历 ---

	// 复习写端点永远直接生效, 不挂审批分流; CLI 侧仍走幂等中间件.
	s.registerCRUD(mux, "review/due", "GET", "", s.handleReviewDue, cliWriteSpec{})
	s.registerCRUD(mux, "review", "POST", "/{card_id}/submit", s.handleReviewSubmit, cliWriteSpec{})
	// 日历仅 Web, 不提供 CLI 同构.
	mux.Handle("GET /api/web/calendar", s.requireWebSession(http.HandlerFunc(s.handleCalendar)))

	// --- 领域 CRUD: Web 与 CLI 复用同一组 handler ---

	// Topic. trash-preview 是 WebUI 二次确认专用, 不开放给 CLI.
	s.registerCRUD(mux, "topics", "GET", "", s.handleListTopics, cliWriteSpec{})
	s.registerCRUD(mux, "topics", "POST", "", s.handleCreateTopic, s.writeSpec(model.OpTopicCreate, s.buildTopicCreate, false))
	s.registerCRUD(mux, "topics", "POST", "/batch-create", s.handleBatchCreateTopics, s.writeSpec(model.OpTopicCreate, s.buildTopicBatchCreate, true))
	s.registerCRUD(mux, "topics", "POST", "/batch-trash", s.handleBatchTrashTopics, s.writeSpec(model.OpTopicTrash, s.buildTopicBatchTrash, true))
	s.registerCRUD(mux, "topics", "GET", "/{id}", s.handleGetTopic, cliWriteSpec{})
	s.registerCRUD(mux, "topics", "PATCH", "/{id}", s.handleUpdateTopic, s.writeSpec(model.OpTopicUpdate, s.buildTopicUpdate, false))
	s.registerCRUD(mux, "topics", "POST", "/{id}/trash", s.handleTrashTopic, s.writeSpec(model.OpTopicTrash, s.buildTopicTrash, false))
	mux.Handle("POST /api/web/topics/trash-preview", s.requireWebSession(http.HandlerFunc(s.handleTrashPreviewTopics)))

	// Card.
	s.registerCRUD(mux, "cards", "GET", "", s.handleListCards, cliWriteSpec{})
	s.registerCRUD(mux, "cards", "POST", "", s.handleCreateCard, s.writeSpec(model.OpCardCreate, s.buildCardCreate, false))
	s.registerCRUD(mux, "cards", "POST", "/batch-create", s.handleBatchCreateCards, s.writeSpec(model.OpCardCreate, s.buildCardBatchCreate, true))
	s.registerCRUD(mux, "cards", "POST", "/batch-trash", s.handleBatchTrashCards, s.writeSpec(model.OpCardTrash, s.buildCardBatchTrash, true))
	s.registerCRUD(mux, "cards", "GET", "/{id}", s.handleGetCard, cliWriteSpec{})
	s.registerCRUD(mux, "cards", "PATCH", "/{id}", s.handleUpdateCard, s.writeSpec(model.OpCardUpdate, s.buildCardUpdate, false))
	s.registerCRUD(mux, "cards", "POST", "/{id}/trash", s.handleTrashCard, s.writeSpec(model.OpCardTrash, s.buildCardTrash, false))
	s.registerCRUD(mux, "cards", "POST", "/merge", s.handleMergeCard, s.writeSpec(model.OpCardMerge, s.buildCardMerge, false))
	// 复习历史仅 Web, 不提供 CLI 同构.
	mux.Handle("GET /api/web/cards/{id}/reviews", s.requireWebSession(http.HandlerFunc(s.handleCardReviews)))

	// Glossary.
	s.registerCRUD(mux, "glossary", "GET", "", s.handleListGlossary, cliWriteSpec{})
	s.registerCRUD(mux, "glossary", "POST", "", s.handleCreateGlossary, s.writeSpec(model.OpGlossaryCreate, s.buildGlossaryCreate, false))
	s.registerCRUD(mux, "glossary", "POST", "/batch-create", s.handleBatchCreateGlossary, s.writeSpec(model.OpGlossaryCreate, s.buildGlossaryBatchCreate, true))
	s.registerCRUD(mux, "glossary", "POST", "/batch-trash", s.handleBatchTrashGlossary, s.writeSpec(model.OpGlossaryTrash, s.buildGlossaryBatchTrash, true))
	s.registerCRUD(mux, "glossary", "GET", "/{id}", s.handleGetGlossary, cliWriteSpec{})
	s.registerCRUD(mux, "glossary", "PATCH", "/{id}", s.handleUpdateGlossary, s.writeSpec(model.OpGlossaryUpdate, s.buildGlossaryUpdate, false))
	s.registerCRUD(mux, "glossary", "POST", "/{id}/trash", s.handleTrashGlossary, s.writeSpec(model.OpGlossaryTrash, s.buildGlossaryTrash, false))

	// --- 回收站 ---

	// 列表: Web 与 CLI 同构.
	s.registerCRUD(mux, "trash/cards", "GET", "", s.handleListTrashedCards, cliWriteSpec{})
	s.registerCRUD(mux, "trash/topics", "GET", "", s.handleListTrashedTopics, cliWriteSpec{})
	s.registerCRUD(mux, "trash/glossary", "GET", "", s.handleListTrashedGlossary, cliWriteSpec{})

	// 恢复: Web 直接生效; CLI 按恢复审批开关分流. 批量恢复仅 Web 提供.
	s.registerCRUD(mux, "trash/cards", "POST", "/{id}/restore", s.handleRestoreCard, s.writeSpec(model.OpCardRestore, s.buildCardRestore, false))
	s.registerCRUD(mux, "trash/topics", "POST", "/{id}/restore", s.handleRestoreTopic, s.writeSpec(model.OpTopicRestore, s.buildTopicRestore, false))
	s.registerCRUD(mux, "trash/glossary", "POST", "/{id}/restore", s.handleRestoreGlossary, s.writeSpec(model.OpGlossaryRestore, s.buildGlossaryRestore, false))
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

// cliWriteSpec 描述一个 CLI 写端点的审批分流: 空值表示该资源不参与
// 审批 (本批仅读端点与 Web 专用端点是空值).
type cliWriteSpec struct {
	gate *cliProposalGate
}

// topicWriteSpec 组装一个写端点的审批分流描述.
// build 在开关开启时把请求拆成提案; batch 标记批量响应形状.
func (s *Server) writeSpec(
	op int16,
	build func(*http.Request) ([]appsvc.ProposalSpec, error),
	batch bool,
) cliWriteSpec {
	return cliWriteSpec{gate: &cliProposalGate{operation: op, batch: batch, build: build}}
}

// registerCRUD 把同一个 handler 同时挂到 Web 与 CLI 命名空间,
// 是两套路由复用同一应用服务的落点: Web 挂 session 认证,
// CLI 挂 API Key 认证; CLI 写请求包一层幂等中间件, 再按需包审批分流.
// method 为 HTTP 方法, pattern 是相对资源根的子路径 (如 "/{id}/trash").
func (s *Server) registerCRUD(
	mux *http.ServeMux,
	resource, method, pattern string,
	h http.HandlerFunc,
	spec cliWriteSpec,
) {
	mux.Handle(method+" /api/web/"+resource+pattern, s.requireWebSession(h))
	cliHandler := http.Handler(h)
	if method != http.MethodGet {
		// 先包审批分流, 再包幂等中间件: 幂等包装在外层开事务,
		// 于是提案创建与幂等记录落在同一事务.
		if spec.gate != nil {
			cliHandler = s.withProposalGate(*spec.gate, cliHandler)
		}
		cliHandler = s.withIdempotency(cliHandler)
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
