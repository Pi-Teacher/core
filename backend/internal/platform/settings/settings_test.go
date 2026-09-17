package settings

import (
	"context"
	"testing"
)

// TestRegistryDefaultsAndTypes 验证注册表内容:
// 13 个审批开关全部默认开启, 敏感 key 有标记.
func TestRegistryDefaultsAndTypes(t *testing.T) {
	r := Default()
	approvals := r.Group(GroupApproval)
	if len(approvals) != 13 {
		t.Fatalf("approval switch count = %d, want 13", len(approvals))
	}
	for _, s := range approvals {
		if s.Type != TypeBool {
			t.Errorf("%s: type = %v, want bool", s.Key, s.Type)
		}
		if s.Default != "true" {
			t.Errorf("%s: default = %q, want true", s.Key, s.Default)
		}
	}

	embKey, ok := r.Lookup("embedding_api_key")
	if !ok || !embKey.Sensitive {
		t.Fatal("embedding_api_key must be registered and sensitive")
	}
	if _, ok := r.Lookup("embedding_worker_batch_size"); !ok {
		t.Fatal("embedding_worker_batch_size must be registered")
	}
	if _, ok := r.Lookup("does_not_exist"); ok {
		t.Fatal("unknown key must not resolve")
	}
}

// TestSnapshotFallbackToDefaults 验证快照回退:
// 缺失 key 使用默认值, 未注册 key 被忽略.
func TestSnapshotFallbackToDefaults(t *testing.T) {
	snap := NewSnapshot(map[string]string{"stdout_log_level": "debug"})
	if snap.String("stdout_log_level") != "debug" {
		t.Fatal("explicit value not honored")
	}
	if !snap.Bool("enable_cli_card_create_approval") {
		t.Fatal("missing bool should fall back to default true")
	}
	if snap.Int64("database_max_rows") != 10000 {
		t.Fatal("missing int should fall back to default 10000")
	}
	if snap.Timezone() == nil {
		t.Fatal("timezone must never be nil")
	}
}

// TestMarshalValueValidation 验证各类型的校验器拒绝非法值.
func TestMarshalValueValidation(t *testing.T) {
	r := Default()
	if _, _, err := r.MarshalValue("enable_cli_card_create_approval", "not-bool"); err == nil {
		t.Fatal("expected bool validation error")
	}
	if _, _, err := r.MarshalValue("embedding_dimensions", "-1"); err == nil {
		t.Fatal("expected positive-int validation error")
	}
	if _, _, err := r.MarshalValue("embedding_similarity_min_ready_percent", "150"); err == nil {
		t.Fatal("expected percent validation error")
	}
	if _, _, err := r.MarshalValue("calendar_timezone", "Not/AZone"); err == nil {
		t.Fatal("expected timezone validation error")
	}
	if _, _, err := r.MarshalValue("unknown_key", "x"); err == nil {
		t.Fatal("expected unknown key error")
	}
}

// fakeProvider 在内存中记录已应用的更新, 模拟持久化层.
type fakeProvider struct {
	values map[string]string
}

func (f *fakeProvider) LoadAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, nil
}

func (f *fakeProvider) Apply(_ context.Context, updates []Update) (map[string]string, error) {
	if f.values == nil {
		f.values = map[string]string{}
	}
	for _, u := range updates {
		encoded, _, err := Default().MarshalValue(u.Key, u.Value)
		if err != nil {
			return nil, err
		}
		f.values[u.Key] = encoded
	}
	return f.LoadAll(context.Background())
}

// TestManagerAppliesAndSwaps 验证写成功后快照被原子替换,
// 未知 key 在触达持久化层之前就被拒绝.
func TestManagerAppliesAndSwaps(t *testing.T) {
	provider := &fakeProvider{}
	m := NewManager(provider)
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background(), []Update{{Key: "database_log_enabled", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	if !m.Snapshot().Bool("database_log_enabled") {
		t.Fatal("snapshot was not atomically replaced")
	}
	if err := m.Apply(context.Background(), []Update{{Key: "nope", Value: "1"}}); err == nil {
		t.Fatal("expected unknown key error")
	}
}
