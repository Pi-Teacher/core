// Package logging 提供统一 slog handler: 标准输出加可选的 app_log 异步落库.
//
// 落库是尽力而为: 队列容量固定 1024, 满时直接丢弃记录 (包括 error),
// 绝不让日志写库阻塞业务请求.
package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// 数据库 sink 赋予特殊含义的属性键: 命中这些键的属性写入专门列,
// 其余属性序列化进 details.
const (
	AttrEvent      = "event"
	AttrRequestID  = "request_id"
	AttrSource     = "source"
	AttrEntityType = "entity_type"
	AttrEntityID   = "entity_id"
	AttrError      = "error"
)

// queueCapacity 是 app_log 队列的固定容量.
const queueCapacity = 1024

// messageLimit 和 detailsLimit 分别限制 message 与 details 列的 8 KiB 上限.
const (
	messageLimit = 8 * 1024
	detailsLimit = 8 * 1024
)

// DBWriter 持久化 app_log 行, 由基础设施层实现.
type DBWriter interface {
	InsertAppLogs(ctx context.Context, rows []model.AppLog) error
}

// RuntimeConfig 是可原子替换的运行期日志配置.
// 设置变更后整体换新, 读写双方都不加锁.
type RuntimeConfig struct {
	StdoutLevel slog.Level
	DBEnabled   bool
	DBLevel     slog.Level
}

// LevelMapping 把级别名转为 slog.Level, 未知名称按 info 处理.
func LevelMapping(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Handler 把日志扇出到 stdout 和可选的数据库 sink, 两个出口各自独立过滤级别.
type Handler struct {
	stdout slog.Handler
	sink   *dbSink
	cfg    *atomic.Pointer[RuntimeConfig]
	// attrs 是 WithAttrs 附加的属性, 落库时需要重新遍历,
	// 因为 slog 只把最终 Record 交给 Handle.
	attrs []slog.Attr
}

// New 构造 handler. writer 为 nil 时不启用数据库落库.
//
// stdout 的 TextHandler 固定为 debug 级, 实际过滤在 Handle 里做:
// 这样一个 handler 能同时服务两个不同阈值的出口.
func New(w io.Writer, cfg *atomic.Pointer[RuntimeConfig], writer DBWriter) *Handler {
	h := &Handler{
		stdout: slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
		cfg: cfg,
	}
	if writer != nil {
		h.sink = newDBSink(writer)
	}
	return h
}

// Enabled 返回任一出口愿意接收该级别.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	cfg := h.cfg.Load()
	if level >= cfg.StdoutLevel {
		return true
	}
	if h.sink != nil && cfg.DBEnabled && level >= cfg.DBLevel {
		return true
	}
	return false
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	cfg := h.cfg.Load()
	if r.Level >= cfg.StdoutLevel {
		if err := h.stdout.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	if h.sink != nil && cfg.DBEnabled && r.Level >= cfg.DBLevel {
		h.sink.enqueue(h.recordToAppLog(ctx, r))
	}
	return nil
}

// WithAttrs 返回附加属性后的新 handler. slog 要求派生方法不可变,
// 中间件链上每层都会生成新实例而不是修改原实例.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.stdout = h.stdout.WithAttrs(attrs)
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

func (h *Handler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.stdout = h.stdout.WithGroup(name)
	return &clone
}

// recordToAppLog 把 slog 记录和已附加属性转成 app_log 行.
// 已知属性键映射到专门列, 其余属性进 details JSON;
// 超长内容截断到列上限, 而不是丢弃整条日志.
func (h *Handler) recordToAppLog(ctx context.Context, r slog.Record) model.AppLog {
	row := model.AppLog{
		LoggedAt: r.Time.UTC(),
		Level:    levelToInt(r.Level),
		Message:  truncate(r.Message, messageLimit),
	}
	if row.LoggedAt.IsZero() {
		row.LoggedAt = time.Now().UTC()
	}
	if ev, ok := ctx.Value(ctxKeyEvent).(string); ok && ev != "" {
		row.Event = ev
	}

	details := map[string]any{}
	addAttr := func(a slog.Attr) {
		switch a.Key {
		case AttrEvent:
			row.Event = a.Value.String()
		case AttrRequestID:
			s := a.Value.String()
			row.RequestID = &s
		case AttrSource:
			row.Source = sourceFromString(a.Value.String())
		case AttrEntityType:
			row.EntityType = entityTypeFromString(a.Value.String())
		case AttrEntityID:
			id := a.Value.Int64()
			row.EntityID = &id
		case AttrError:
			details[a.Key] = a.Value.String()
		default:
			details[a.Key] = attrToAny(a)
		}
	}
	for _, a := range h.attrs {
		addAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(a)
		return true
	})

	if row.Event == "" {
		// event 列非空且用于查询, 无事件名的记录统一标记而不是存空串.
		row.Event = "unknown"
	}
	row.Event = truncate(row.Event, 100)
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			s := truncate(string(b), detailsLimit)
			row.Details = &s
		}
	}
	return row
}

// attrToAny 把属性值转为可 JSON 序列化的形式, 组属性递归展开.
func attrToAny(a slog.Attr) any {
	if a.Value.Kind() == slog.KindGroup {
		m := map[string]any{}
		for _, ga := range a.Value.Group() {
			m[ga.Key] = attrToAny(ga)
		}
		return m
	}
	return a.Value.Any()
}

func levelToInt(l slog.Level) int16 {
	switch {
	case l < slog.LevelInfo:
		return model.LogDebug
	case l < slog.LevelWarn:
		return model.LogInfo
	case l < slog.LevelError:
		return model.LogWarn
	default:
		return model.LogError
	}
}

func sourceFromString(s string) *int16 {
	var v int16
	switch strings.ToLower(s) {
	case "web":
		v = model.SourceWeb
	case "cli":
		v = model.SourceCLI
	case "worker":
		v = model.SourceWorker
	case "admin":
		v = model.SourceAdmin
	case "system":
		v = model.SourceSystem
	default:
		return nil
	}
	return &v
}

func entityTypeFromString(s string) *int16 {
	var v int16
	switch strings.ToLower(s) {
	case "topic":
		v = model.EntityTopic
	case "card":
		v = model.EntityCard
	case "glossary":
		v = model.EntityGlossary
	default:
		return nil
	}
	return &v
}

// truncate 按字节截断, 列上限以字节计.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// contextKey 是事件名的 context 键类型.
type contextKey int

const ctxKeyEvent contextKey = iota

// WithEvent 把稳定事件名挂到 context 上, 供 handler 提取.
// 事件名走 context 而不是属性, 避免它被序列化进 details 重复一份.
func WithEvent(ctx context.Context, event string) context.Context {
	return context.WithValue(ctx, ctxKeyEvent, event)
}
