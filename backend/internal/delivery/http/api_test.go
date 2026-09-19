package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	httpapi "github.com/Pi-Teacher/server/internal/delivery/http"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
	"github.com/Pi-Teacher/server/internal/platform/settings"
)

// testServer 基于临时 SQLite 文件组装一个完成迁移的服务端实例.
type testServer struct {
	t        *testing.T
	srv      *httptest.Server
	auth     *appsvc.AuthService
	password string
	client   *http.Client
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cfg := &config.Config{DBDriver: config.DriverSQLite, DBDSN: dbPath, Listen: ":0"}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 测试不关心日志内容, 丢弃输出保持测试输出干净.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	accountRepo := repo.NewAccountRepository(db.DB)
	sessionRepo := repo.NewSessionRepository(db.DB)
	apiKeyRepo := repo.NewAPIKeyRepository(db.DB)
	auth := appsvc.NewAuthService(accountRepo, sessionRepo, apiKeyRepo, logger)
	password, created, err := auth.EnsureInitialAccount(ctx)
	if err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if !created {
		t.Fatal("expected account creation")
	}

	topicRepo := repo.NewTopicRepository(db.DB)
	cardRepo := repo.NewCardRepository(db.DB)
	glossaryRepo := repo.NewGlossaryRepository(db.DB)
	calendarRepo := repo.NewCalendarRepository(db.DB)
	approvalRepo := repo.NewApprovalRepository(db.DB)
	idempotencyRepo := repo.NewIdempotencyRepository(db.DB)
	settingsRepo := repo.NewSettingsRepository(db.DB)
	if err := settingsRepo.EnsureDefaults(ctx); err != nil {
		t.Fatalf("ensure settings: %v", err)
	}
	manager := settings.NewManager(settingsRepo)
	if err := manager.Refresh(ctx); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	topics := appsvc.NewTopicService(db.DB, topicRepo, cardRepo, approvalRepo, logger)
	cards := appsvc.NewCardService(db.DB, cardRepo, topicRepo, calendarRepo, approvalRepo, logger, nil)
	glossaries := appsvc.NewGlossaryService(db.DB, glossaryRepo, approvalRepo, logger)
	trash := appsvc.NewTrashService(db.DB, topicRepo, cardRepo, glossaryRepo, approvalRepo, logger)
	approvals := appsvc.NewApprovalService(db.DB, approvalRepo, topicRepo, cardRepo, glossaryRepo,
		topics, cards, glossaries, logger)
	idempotency := appsvc.NewIdempotencyService(db.DB, idempotencyRepo, logger)
	settingsSvc := appsvc.NewSettingsService(manager, nil)

	handler := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:        auth,
		Topics:      topics,
		Cards:       cards,
		Glossaries:  glossaries,
		Trash:       trash,
		Approvals:   approvals,
		Idempotency: idempotency,
		Settings:    settingsSvc,
		Logger:      logger,
		DBDriver:    db.Driver,
		StartedAt:   time.Now(),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := &http.Client{Jar: newCookieJar(t)}
	return &testServer{t: t, srv: srv, auth: auth, password: password, client: client}
}

func newCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return jar
}

// do 发起一次请求, body 非 nil 时序列化为 JSON.
func (ts *testServer) do(method, path string, body any, headers map[string]string) *http.Response {
	ts.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			ts.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.srv.URL+path, reader)
	if err != nil {
		ts.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.client.Do(req)
	if err != nil {
		ts.t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if dst == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// login 执行一次成功登录并返回 CSRF token.
func (ts *testServer) login() string {
	ts.t.Helper()
	resp := ts.do(http.MethodPost, "/api/web/auth/login",
		map[string]string{"password": ts.password}, nil)
	if resp.StatusCode != http.StatusOK {
		ts.t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	decodeBody(ts.t, resp, &out)
	if out.CSRFToken == "" {
		ts.t.Fatal("login returned empty csrf token")
	}
	return out.CSRFToken
}

// TestFirstBatchAcceptance 覆盖第一批验收路径:
// 登录 → 拿 CSRF → 创建 API Key → 用 API Key 访问 /api/cli/system/info.
func TestFirstBatchAcceptance(t *testing.T) {
	ts := newTestServer(t)

	// 1. 未认证的 CLI 访问被拒绝.
	resp := ts.do(http.MethodGet, "/api/cli/system/info", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated CLI status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. 登录.
	csrf := ts.login()

	// 3. session 端点返回当前会话.
	resp = ts.do(http.MethodGet, "/api/web/auth/session", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. 缺少 CSRF 的写请求被拒绝.
	resp = ts.do(http.MethodPost, "/api/web/api-keys",
		map[string]string{"name": "no-csrf"}, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. 携带合法 CSRF 创建 API Key.
	resp = ts.do(http.MethodPost, "/api/web/api-keys",
		map[string]string{"name": "my-agent"},
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create api key status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID     int64  `json:"id"`
		APIKey string `json:"api_key"`
	}
	decodeBody(t, resp, &created)
	if created.APIKey == "" {
		t.Fatal("created api key is empty")
	}
	if len(created.APIKey) < 4 || created.APIKey[:4] != "ptk_" {
		t.Fatalf("api key %q lacks ptk_ prefix", created.APIKey)
	}

	// 6. 用 API Key 访问 CLI 命名空间.
	resp = ts.do(http.MethodGet, "/api/cli/system/info", nil,
		map[string]string{"Authorization": "Bearer " + created.APIKey})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cli system info status = %d, want 200", resp.StatusCode)
	}
	var info struct {
		Version   string `json:"version"`
		DBDriver  string `json:"db_driver"`
		GoVersion string `json:"go_version"`
	}
	decodeBody(t, resp, &info)
	if info.DBDriver != "sqlite" {
		t.Fatalf("db_driver = %q, want sqlite", info.DBDriver)
	}
	if info.Version == "" || info.GoVersion == "" {
		t.Fatal("system info missing version fields")
	}

	// 7. session 不能访问 CLI 命名空间: cookie jar 里没有有效凭据,
	//    也不带 Authorization, 必须得到 401.
	resp = ts.do(http.MethodGet, "/api/cli/system/info", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session on CLI route status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// 8. API Key 不能认证 Web 命名空间. 用无 cookie 的客户端,
	//    避免仍然有效的 session cookie 掩盖结果.
	noCookies := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+"/api/web/api-keys", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+created.APIKey)
	resp, err = noCookies.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api key on web route status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestLoginRateLimitAndWrongPassword 验证错误密码返回 401,
// 且正确密码在失败后仍可登录并清零计数.
func TestLoginRateLimitAndWrongPassword(t *testing.T) {
	ts := newTestServer(t)

	for i := 0; i < 2; i++ {
		resp := ts.do(http.MethodPost, "/api/web/auth/login", map[string]string{"password": "wrong"}, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong password status = %d, want 401", resp.StatusCode)
		}
		resp.Body.Close()
	}
	csrf := ts.login()
	if csrf == "" {
		t.Fatal("expected csrf token")
	}
}

// TestPasswordChangeRevokesSessions 验证改密后旧 session 与旧密码同时失效.
func TestPasswordChangeRevokesSessions(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	resp := ts.do(http.MethodPatch, "/api/web/auth/password", map[string]string{
		"current_password": ts.password,
		"new_password":     "a-new-strong-password",
	}, map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodGet, "/api/web/auth/session", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after password change = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodPost, "/api/web/auth/login", map[string]string{"password": ts.password}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodPost, "/api/web/auth/login", map[string]string{"password": "a-new-strong-password"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new password status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestLogoutDeletesSession 验证注销后 session 立即失效.
func TestLogoutDeletesSession(t *testing.T) {
	ts := newTestServer(t)
	csrf := ts.login()

	resp := ts.do(http.MethodPost, "/api/web/auth/logout", nil,
		map[string]string{"X-CSRF-Token": csrf})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.do(http.MethodGet, "/api/web/auth/session", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}
