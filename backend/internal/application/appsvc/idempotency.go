package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// 幂等记录默认保留与响应体上限, 与数据库设计 4.15 一致.
const (
	idempotencyTTL        = 24 * time.Hour
	maxIdempotencyBodyLen = 1 << 20 // 1 MiB
)

// IdempotencyReplay 是一次可重放的已完成响应.
type IdempotencyReplay struct {
	Status int
	Body   []byte
}

// IdempotencyService 实现 CLI 写请求的幂等语义:
// 同 Key 同请求返回首次结果, 同 Key 不同请求返回冲突.
//
// 记录只保存 2xx 响应: 业务失败回滚后不落行, 同 Key 修正请求体后可重试.
// 记录写入与业务修改在同一事务, 由 Execute 统一控制.
type IdempotencyService struct {
	db     *gorm.DB
	repo   *repo.IdempotencyRepository
	logger *slog.Logger
	now    func() time.Time
}

// NewIdempotencyService 构造幂等服务.
func NewIdempotencyService(db *gorm.DB, r *repo.IdempotencyRepository, logger *slog.Logger) *IdempotencyService {
	return &IdempotencyService{
		db:     db,
		repo:   r,
		logger: logger,
		now:    func() time.Time { return persistence.Now() },
	}
}

// Lookup 返回已完成的同 Key 响应. 未命中或已过期返回 nil.
// 同 Key 但请求摘要不同返回 idempotency_conflict.
func (s *IdempotencyService) Lookup(
	ctx context.Context,
	apiKeyID int64,
	key, method, path, requestHash string,
) (*IdempotencyReplay, error) {
	rec, err := s.repo.Find(ctx, apiKeyID, key)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	if !rec.ExpiresAt.After(s.now()) {
		// 过期但尚未被清理: 删除后按新请求处理.
		if err := s.repo.DeleteByID(ctx, rec.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if rec.RequestHash != requestHash {
		return nil, apperr.New(apperr.CodeIdempotencyConflict,
			"同一 Idempotency-Key 已用于不同的请求")
	}
	if rec.State != model.IdempotencyCompleted || rec.ResponseStatus == nil {
		// 已提交的记录恒为 completed; 出现 processing 说明并发请求正在处理.
		return nil, apperr.New(apperr.CodeIdempotencyConflict,
			"同一 Idempotency-Key 的请求正在处理中")
	}
	body := ""
	if rec.ResponseBody != nil {
		body = *rec.ResponseBody
	}
	return &IdempotencyReplay{Status: int(*rec.ResponseStatus), Body: []byte(body)}, nil
}

// Execute 在幂等事务中执行 fn 并记录 2xx 响应.
//
// fn 拿到注入了事务句柄的 ctx, 领域写方法据此复用同一事务. 返回的状态
// 与体是最终要写给客户端的响应: 2xx 且不超上限时记录并提交, 否则回滚,
// 业务修改与幂等记录一起不生效.
func (s *IdempotencyService) Execute(
	ctx context.Context,
	apiKeyID int64,
	key, method, path, requestHash string,
	fn func(ctx context.Context) (int, []byte),
) (int, []byte, error) {
	var status int
	var body []byte
	claimErr := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		now := s.now()
		rec := &model.IdempotencyRecord{
			APIKeyID:       apiKeyID,
			IdempotencyKey: key,
			RequestMethod:  method,
			RequestPath:    path,
			RequestHash:    requestHash,
			State:          model.IdempotencyProcessing,
			CreatedAt:      now,
			ExpiresAt:      now.Add(idempotencyTTL),
		}
		ok, err := s.repo.WithTx(tx).Claim(innerCtx, rec)
		if err != nil {
			return err
		}
		if !ok {
			return errIdempotencyKeyTaken
		}
		status, body = fn(innerCtx)
		if status < 200 || status >= 300 {
			// 失败响应不落幂等记录, 保持同 Key 可重试.
			return errIdempotencyNotRecorded
		}
		if len(body) > maxIdempotencyBodyLen {
			return apperr.New(apperr.CodeInternal, "响应体超过幂等记录上限").
				WithDetails(map[string]any{"limit_bytes": maxIdempotencyBodyLen})
		}
		return s.repo.WithTx(tx).Complete(innerCtx, rec.ID, int64(status), string(body))
	})

	switch {
	case errors.Is(claimErr, errIdempotencyNotRecorded):
		// 事务已回滚, 把 fn 产生的失败响应原样交给客户端.
		return status, body, nil
	case errors.Is(claimErr, errIdempotencyKeyTaken):
		// 并发同 Key: 抢占失败. 回读已提交记录, 按重放或冲突处理.
		replay, err := s.Lookup(ctx, apiKeyID, key, method, path, requestHash)
		if err != nil {
			return 0, nil, err
		}
		if replay == nil {
			return 0, nil, apperr.New(apperr.CodeIdempotencyConflict,
				"同一 Idempotency-Key 的请求正在处理中")
		}
		return replay.Status, replay.Body, nil
	case claimErr != nil:
		return 0, nil, claimErr
	default:
		return status, body, nil
	}
}

// CleanupExpired 删除已过期记录, 返回删除行数. 服务启动与定时任务调用.
func (s *IdempotencyService) CleanupExpired(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpired(ctx, s.now())
}

// 内部哨兵: 用 errors.Is 在 Execute 中区分事务回滚原因.
var (
	errIdempotencyKeyTaken    = errors.New("idempotency key already taken")
	errIdempotencyNotRecorded = errors.New("idempotency response not recorded")
)
