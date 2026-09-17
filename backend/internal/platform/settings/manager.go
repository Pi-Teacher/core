package settings

import (
	"context"
	"sync/atomic"
)

// Provider 加载和持久化设置值, 由基础设施层实现.
// Apply 必须事务化: 全部更新成功才提交, 并返回完整值集.
type Provider interface {
	// LoadAll 以原始字符串值返回全部已持久化的设置行.
	LoadAll(ctx context.Context) (map[string]string, error)
	// Apply 校验并持久化给定的更新, 返回更新后的完整值集.
	Apply(ctx context.Context, updates []Update) (map[string]string, error)
}

// Update 是单个设置变更.
type Update struct {
	Key   string
	Value string
}

// Manager 持有内存设置快照, 每次写成功后原子替换.
// 读路径无锁: 调用方拿到的是不可变快照, 不会被并发写撕裂.
type Manager struct {
	registry *Registry
	provider Provider
	current  atomic.Pointer[Snapshot]
}

// NewManager 构造 manager, 初始快照为全默认值.
// 启动后应立即调用 Refresh 换成数据库里的真实值.
func NewManager(provider Provider) *Manager {
	m := &Manager{registry: Default(), provider: provider}
	m.current.Store(NewSnapshot(nil))
	return m
}

// Registry 返回设置注册表.
func (m *Manager) Registry() *Registry { return m.registry }

// Snapshot 返回当前快照, 永不为 nil, 调用方无需判空.
func (m *Manager) Snapshot() *Snapshot { return m.current.Load() }

// Refresh 从数据库重载全部设置并原子替换快照.
func (m *Manager) Refresh(ctx context.Context) error {
	values, err := m.provider.LoadAll(ctx)
	if err != nil {
		return err
	}
	m.current.Store(NewSnapshot(values))
	return nil
}

// Apply 先整体校验再持久化: 任一 key 未知或值非法就整批拒绝,
// 不出现半批生效. 成功后用返回的完整值集替换快照.
func (m *Manager) Apply(ctx context.Context, updates []Update) error {
	if len(updates) == 0 {
		return nil
	}
	for _, u := range updates {
		if _, ok := m.registry.Lookup(u.Key); !ok {
			return &UnknownKeyError{Key: u.Key}
		}
		if _, _, err := m.registry.MarshalValue(u.Key, u.Value); err != nil {
			return &InvalidValueError{Key: u.Key, Err: err}
		}
	}
	values, err := m.provider.Apply(ctx, updates)
	if err != nil {
		return err
	}
	m.current.Store(NewSnapshot(values))
	return nil
}

// UnknownKeyError 表示未注册的设置 key.
type UnknownKeyError struct{ Key string }

func (e *UnknownKeyError) Error() string { return "未知设置 key: " + e.Key }

// InvalidValueError 表示值未通过校验.
type InvalidValueError struct {
	Key string
	Err error
}

func (e *InvalidValueError) Error() string {
	return "设置 " + e.Key + " 值无效: " + e.Err.Error()
}

func (e *InvalidValueError) Unwrap() error { return e.Err }
