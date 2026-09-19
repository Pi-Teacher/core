package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// maxIdempotencyBody 是幂等响应体上限, 与数据库设计 4.15 一致.
const maxIdempotencyBody = 1 << 20 // 1 MiB

// ctxKeyRawBody 携带本请求已读入内存的原始请求体, 供提案构建多次解码.
const ctxKeyRawBody ctxKey = 100

// rawBodyFromContext 返回缓存的原始请求体, 不存在时为 nil.
func rawBodyFromContext(ctx context.Context) []byte {
	b, _ := ctx.Value(ctxKeyRawBody).([]byte)
	return b
}

// requestHash 计算幂等请求摘要: 方法 + 路径 + 原始请求体的 SHA-256.
// 路径包含资源 id, 请求体决定内容, 两者一起锁定"同一请求"的语义.
func requestHash(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// withIdempotency 是 CLI 写请求的幂等中间件: 要求 Idempotency-Key 头,
// 命中已完成记录时重放, 否则在事务内执行下游 handler 并记录 2xx 响应.
//
// 响应先写入缓冲, 只有事务提交成功才回写客户端, 保证"看到响应"与
// "修改已落库"同时成立.
func (s *Server) withIdempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			writeError(w, apperr.Validation("缺少 Idempotency-Key 请求头").
				WithDetails(map[string]any{"field": "Idempotency-Key"}))
			return
		}
		apiKey := apiKeyFromContext(r.Context())
		if apiKey == nil {
			writeError(w, apperr.Unauthorized("缺少 API Key"))
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
		if err != nil {
			writeError(w, apperr.Wrap(apperr.CodeInternal, "读取请求体失败", err))
			return
		}
		// 还原请求体供下游解码, 并把原始体放进 context 供提案构建复用.
		r.Body = io.NopCloser(bytes.NewReader(raw))
		ctx := context.WithValue(r.Context(), ctxKeyRawBody, raw)
		r = r.WithContext(ctx)

		path := r.URL.Path
		hash := requestHash(r.Method, path, raw)

		replay, err := s.Idempotency.Lookup(r.Context(), apiKey.ID, key, r.Method, path, hash)
		if err != nil {
			writeError(w, err)
			return
		}
		if replay != nil {
			writeReplay(w, replay)
			return
		}

		buf := &bufferedResponse{header: make(http.Header)}
		status, body, err := s.Idempotency.Execute(
			r.Context(), apiKey.ID, key, r.Method, path, hash,
			func(execCtx context.Context) (int, []byte) {
				buf.reset()
				next.ServeHTTP(buf, r.WithContext(execCtx))
				return buf.statusOrOK(), buf.body.Bytes()
			},
		)
		if err != nil {
			writeError(w, err)
			return
		}
		writeBuffered(w, status, body, buf.header)
	})
}

// writeReplay 回放已完成的响应.
func writeReplay(w http.ResponseWriter, replay *appsvc.IdempotencyReplay) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(replay.Status)
	if len(replay.Body) > 0 {
		_, _ = w.Write(replay.Body)
	}
}

// writeBuffered 把捕获的响应写回真实 ResponseWriter.
func writeBuffered(w http.ResponseWriter, status int, body []byte, header http.Header) {
	for k, vs := range header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

// bufferedResponse 是一个只在内存中收集状态, 头与体的 ResponseWriter,
// 供幂等事务判断响应是否可记录, 提交后才回写客户端.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// statusOrOK 返回已记录状态, handler 未显式写状态时按 200 处理.
func (b *bufferedResponse) statusOrOK() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (b *bufferedResponse) reset() {
	b.status = 0
	b.body.Reset()
	b.header = make(http.Header)
}

// --- 审批分流 ---

// cliProposalGate 在 CLI 写链路上实现审批分流: 开关开启时把请求转成
// 审批提案并返回 202, 关闭时透传给直写 handler. Web 请求不经这里.
type cliProposalGate struct {
	operation int16
	// batch 标记批量端点: 开关开启时为每个项目创建独立提案,
	// 202 响应体为 approvals 数组; 否则为单个 approval.
	batch bool
	// build 用缓存的原始请求体构建提案. 返回错误表示 payload 不合法
	// 或引用的对象不存在, 此时不创建任何提案.
	build func(*http.Request) ([]appsvc.ProposalSpec, error)
}

// withProposalGate 包装 CLI 写 handler.
func (s *Server) withProposalGate(gate cliProposalGate, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := apiKeyFromContext(r.Context())
		if apiKey == nil {
			writeError(w, apperr.Unauthorized("缺少 API Key"))
			return
		}
		key := appsvc.ApprovalSwitchKey(gate.operation)
		if s.Settings == nil || !s.Settings.ApprovalEnabled(key) {
			next.ServeHTTP(w, r)
			return
		}
		specs, err := gate.build(r)
		if err != nil {
			writeError(w, err)
			return
		}
		created, err := s.Approvals.CreateProposals(r.Context(), apiKey.ID, specs)
		if err != nil {
			writeError(w, err)
			return
		}
		s.logProposalCreated(r, created)
		writeJSON(w, http.StatusAccepted, proposalResponse(created, gate.batch))
	})
}

// proposalResponse 构造 202 响应体: 单个为 {"approval": {...}},
// 批量为 {"approvals": [...]}, 顺序与请求 items 一致.
func proposalResponse(created []model.ApprovalRequest, batch bool) any {
	if batch {
		approvals := make([]approvalSummary, 0, len(created))
		for i := range created {
			approvals = append(approvals, approvalSummary{
				ID: created[i].ID, Status: appsvc.ApprovalStatusName(created[i].Status),
			})
		}
		return map[string]any{"approvals": approvals}
	}
	return map[string]any{"approval": approvalSummary{
		ID: created[0].ID, Status: appsvc.ApprovalStatusName(created[0].Status),
	}}
}

// logProposalCreated 记录提案创建事件.
func (s *Server) logProposalCreated(r *http.Request, created []model.ApprovalRequest) {
	for i := range created {
		s.logEntityEvent(r, "approval_created", "approval", created[i].ID)
	}
}
