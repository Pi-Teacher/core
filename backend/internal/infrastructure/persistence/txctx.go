package persistence

import (
	"context"

	"gorm.io/gorm"
)

// txKey 是 context 中携带事务句柄的私有键.
// 用空结构体避免与其他包的键冲突, 也不导出类型.
type txKey struct{}

// WithTx 把事务句柄放进 context, 让服务层写方法复用调用方事务.
// CLI 写请求的幂等包装用它把幂等记录、审批提案与领域修改收进同一事务.
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFrom 返回 context 中的事务句柄, 不存在时为 nil.
func TxFrom(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(txKey{}).(*gorm.DB)
	return tx
}

// RunInTx 在 ctx 已携带事务时直接复用该事务执行 fn, 否则新开事务执行.
//
// fn 同时拿到 tx 与一个把该 tx 注入后的 ctx: 嵌套调用下层服务方法时
// 必须传这个 ctx, 下层才能复用同一事务而不另开新事务 (另开会死锁).
// 服务层写方法统一走这里: 普通 HTTP 请求各自开事务, 而 CLI 写请求由
// 幂等包装预先开启事务放进 ctx, 使响应捕获与业务修改原子提交.
func RunInTx(ctx context.Context, db *gorm.DB, fn func(ctx context.Context, tx *gorm.DB) error) error {
	if tx := TxFrom(ctx); tx != nil {
		return fn(ctx, tx)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithTx(ctx, tx), tx)
	})
}
