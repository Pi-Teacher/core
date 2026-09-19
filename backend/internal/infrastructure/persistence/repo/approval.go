package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// ApprovalRepository 持久化审批请求与审批目标.
// 审批数据不进正式领域表; 列表不展开 targets, 详情按请求单独读取.
type ApprovalRepository struct {
	db *gorm.DB
}

// NewApprovalRepository 构造审批仓库.
func NewApprovalRepository(db *gorm.DB) *ApprovalRepository {
	return &ApprovalRepository{db: db}
}

// WithTx 返回绑定到给定事务句柄的仓库副本, 供服务层在单个事务中
// 组合多个仓库的操作; 原仓库不受影响.
func (r *ApprovalRepository) WithTx(tx *gorm.DB) *ApprovalRepository {
	return &ApprovalRepository{db: tx}
}

// ApprovalListOptions 是审批列表的过滤与分页参数.
// Status 与 APIKeyID 为 nil 表示不过滤.
type ApprovalListOptions struct {
	Status   *int16
	APIKeyID *int64
	Offset   int
	Limit    int
}

// Create 插入一条审批请求, 回填自增 ID.
func (r *ApprovalRepository) Create(ctx context.Context, req *model.ApprovalRequest) error {
	return r.db.WithContext(ctx).Create(req).Error
}

// CreateTargets 批量插入审批目标.
func (r *ApprovalRepository) CreateTargets(ctx context.Context, targets []model.ApprovalTarget) error {
	if len(targets) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&targets).Error
}

// Find 按 id 查找审批请求.
func (r *ApprovalRepository) Find(ctx context.Context, id int64) (*model.ApprovalRequest, error) {
	var req model.ApprovalRequest
	if err := r.db.WithContext(ctx).First(&req, id).Error; err != nil {
		return nil, err
	}
	return &req, nil
}

// ListTargets 返回某审批请求的全部目标, 按插入顺序排列.
func (r *ApprovalRepository) ListTargets(ctx context.Context, requestID int64) ([]model.ApprovalTarget, error) {
	rows := make([]model.ApprovalTarget, 0, 4)
	err := r.db.WithContext(ctx).
		Where("approval_request_id = ?", requestID).
		Order("id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// List 返回审批请求分页, 最近创建在前.
func (r *ApprovalRepository) List(ctx context.Context, opts ApprovalListOptions) ([]model.ApprovalRequest, int64, error) {
	scope := r.db.WithContext(ctx).Model(&model.ApprovalRequest{})
	if opts.Status != nil {
		scope = scope.Where("status = ?", *opts.Status)
	}
	if opts.APIKeyID != nil {
		scope = scope.Where("requested_by_api_key_id = ?", *opts.APIKeyID)
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]model.ApprovalRequest, 0, opts.Limit)
	if err := scope.Order("created_at DESC, id DESC").
		Offset(opts.Offset).Limit(opts.Limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// MarkProcessed 把 pending 请求条件更新为终态. 条件带 status=pending
// 承载幂等: 重复批准/拒绝与并发处理都不会命中.
func (r *ApprovalRepository) MarkProcessed(
	ctx context.Context,
	id int64,
	toStatus int16,
	reason *string,
	approvedPayload *string,
	now time.Time,
) (bool, error) {
	fields := map[string]any{
		"status":       toStatus,
		"processed_at": now,
	}
	if reason != nil {
		fields["reason"] = *reason
	}
	if approvedPayload != nil {
		fields["approved_payload"] = *approvedPayload
	}
	res := r.db.WithContext(ctx).Model(&model.ApprovalRequest{}).
		Where("id = ? AND status = ?", id, model.ApprovalPending).
		Updates(fields)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// MarkStaleByTargets 把所有依赖给定对象的 pending 请求标记为 stale.
// 只影响 pending, 已结束的请求不会被改写; 同一请求重复命中是幂等的.
func (r *ApprovalRepository) MarkStaleByTargets(
	ctx context.Context,
	entityType int16,
	entityIDs []int64,
	reason string,
	now time.Time,
) (int64, error) {
	if len(entityIDs) == 0 {
		return 0, nil
	}
	sub := r.db.Model(&model.ApprovalTarget{}).
		Select("approval_request_id").
		Where("entity_type = ? AND entity_id IN ?", entityType, entityIDs)
	res := r.db.WithContext(ctx).Model(&model.ApprovalRequest{}).
		Where("status = ? AND id IN (?)", model.ApprovalPending, sub).
		Updates(map[string]any{
			"status":       model.ApprovalStale,
			"reason":       reason,
			"processed_at": now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
