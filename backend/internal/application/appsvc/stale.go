package appsvc

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// pending 审批被联动标记 stale 时的固定原因文案.
// 按触发场景区分, 便于 WebUI 直接展示与用户理解提案失效原因.
const (
	staleReasonTrashed        = "target_trashed"
	staleReasonDeleted        = "target_permanently_deleted"
	staleReasonOverwritten    = "target_overwritten"
	staleReasonVersionChanged = "target_version_changed"
	staleReasonNotTrashed     = "target_not_trashed"
)

// staleMarker 把依赖被回收、被永久删除或被同名覆盖对象的 pending 审批
// 请求联动标记为 stale. 审批仓库为空时是空操作, 便于不关心审批的场景
// 复用同一批领域服务 (如单元测试).
//
// 所有调用都发生在对象变更的同一事务内, 保证"对象状态"与"提案失效"
// 一起提交或一起回滚.
type staleMarker struct {
	approvals *repo.ApprovalRepository
}

// mark 在给定事务中把依赖 (entityType, ids) 的 pending 审批标记 stale.
func (m staleMarker) mark(
	ctx context.Context,
	tx *gorm.DB,
	entityType int16,
	ids []int64,
	reason string,
	now time.Time,
) error {
	if m.approvals == nil || len(ids) == 0 {
		return nil
	}
	_, err := m.approvals.WithTx(tx).
		MarkStaleByTargets(ctx, entityType, ids, reason, now)
	return err
}

// markOne 是 mark 的单对象便捷形式.
func (m staleMarker) markOne(
	ctx context.Context,
	tx *gorm.DB,
	entityType int16,
	id int64,
	reason string,
	now time.Time,
) error {
	return m.mark(ctx, tx, entityType, []int64{id}, reason, now)
}
