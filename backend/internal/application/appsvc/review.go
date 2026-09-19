package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/domain/schedule"
	fsrsadapter "github.com/Pi-Teacher/server/internal/infrastructure/fsrs"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// 复习队列默认与最大返回条数, 与 API 设计 5.10 一致.
const (
	defaultDueLimit = 20
	maxDueLimit     = 100
)

// ReviewService 编排复习队列查询与复习结果提交.
//
// 复习提交在一个事务内完成: 校验 Card 与 Schedule 双版本, 由调度器
// 计算下一状态, 条件更新 Schedule, 插入 Review Log 并按用户时区累计
// Calendar. 复习永远直接生效, 不经过审批.
type ReviewService struct {
	db        *gorm.DB
	cards     *repo.CardRepository
	calendar  *repo.CalendarRepository
	scheduler schedule.Scheduler
	logger    *slog.Logger
	// timezone 返回用户配置的日历时区. 每次调用读当前快照.
	timezone func() *time.Location
	now      func() time.Time
}

// NewReviewService 构造复习服务. timezone 为 nil 时按 UTC 处理;
// scheduler 为 nil 时回退到固定参数调度器.
func NewReviewService(
	db *gorm.DB,
	cards *repo.CardRepository,
	calendar *repo.CalendarRepository,
	scheduler schedule.Scheduler,
	logger *slog.Logger,
	timezone func() *time.Location,
) *ReviewService {
	if timezone == nil {
		timezone = func() *time.Location { return time.UTC }
	}
	if scheduler == nil {
		scheduler = fsrsadapter.NewScheduler()
	}
	return &ReviewService{
		db:        db,
		cards:     cards,
		calendar:  calendar,
		scheduler: scheduler,
		logger:    logger,
		timezone:  timezone,
		now:       func() time.Time { return persistence.Now() },
	}
}

// DueList 是到期复习队列: 条目与总数.
type DueList struct {
	Items []repo.DueCard
	Total int64
}

// Due 返回当前应复习的卡片, 即 due <= now 的正常 Card, 按 due 升序
// (最逾期在前). topicID 为 nil 不过滤, 指向 0 表示无 Topic 的卡.
// limit 为 0 时取默认值, 超过上限时截断.
func (s *ReviewService) Due(ctx context.Context, topicID *int64, limit int) (*DueList, error) {
	limit = normalizeDueLimit(limit)
	now := s.now()
	items, err := s.cards.ListDue(ctx, topicID, now, limit)
	if err != nil {
		return nil, err
	}
	total, err := s.cards.CountDue(ctx, topicID, now)
	if err != nil {
		return nil, err
	}
	return &DueList{Items: items, Total: total}, nil
}

// normalizeDueLimit 归一化复习队列条数: <=0 取默认, 超上限按上限截断.
func normalizeDueLimit(limit int) int {
	if limit <= 0 {
		return defaultDueLimit
	}
	if limit > maxDueLimit {
		return maxDueLimit
	}
	return limit
}

// ReviewResult 是一次复习提交的结果: 新调度状态与写入的日志 ID.
type ReviewResult struct {
	CardID      int64
	Rating      schedule.Rating
	Schedule    *model.CardSchedule
	ReviewLogID int64
}

// Submit 提交一次复习评分. expectedCardVersion 与 expectedScheduleVersion
// 由客户端持有; 任一不匹配返回 409. 事务内: 校验双版本 → 调度计算 →
// 条件更新 Schedule → 插入 Review Log → 累计 Calendar.
func (s *ReviewService) Submit(
	ctx context.Context,
	cardID int64,
	rating schedule.Rating,
	expectedCardVersion, expectedScheduleVersion int64,
) (*ReviewResult, error) {
	var result *ReviewResult
	err := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		var txErr error
		result, txErr = s.submitInTx(innerCtx, tx, cardID, rating, expectedCardVersion, expectedScheduleVersion)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// submitInTx 是 Submit 的事务内实现.
func (s *ReviewService) submitInTx(
	ctx context.Context,
	tx *gorm.DB,
	cardID int64,
	rating schedule.Rating,
	expectedCardVersion, expectedScheduleVersion int64,
) (*ReviewResult, error) {
	cards := s.cards.WithTx(tx)
	c, err := cards.Find(ctx, cardID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Card 不存在")
		}
		return nil, err
	}
	sched, err := cards.FindSchedule(ctx, cardID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.Wrap(apperr.CodeInternal, "Card 缺少调度行", err)
		}
		return nil, err
	}
	// 双版本一次性校验, details 同时回传两个当前值, 便于前端重读重试.
	if c.Version != expectedCardVersion || sched.Version != expectedScheduleVersion {
		return nil, reviewVersionConflict(c.Version, sched.Version)
	}
	now := s.now()
	outcome, err := s.scheduler.Next(scheduleFromModel(sched), rating, now)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "复习调度计算失败", err)
	}
	// 条件更新带上期望 version: 读取后若被并发写入, 未命中并整事务回滚.
	ok, err := cards.UpdateScheduleConditional(ctx, cardID, expectedScheduleVersion, map[string]any{
		"due":             outcome.Next.Due,
		"stability":       outcome.Next.Stability,
		"difficulty":      outcome.Next.Difficulty,
		"scheduled_days":  outcome.Next.ScheduledDays,
		"reps":            outcome.Next.Reps,
		"lapses":          outcome.Next.Lapses,
		"state":           int16(outcome.Next.State),
		"last_review_at":  outcome.Next.LastReviewAt,
		"remaining_steps": outcome.Next.RemainingSteps,
		"updated_at":      now,
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, reviewVersionConflict(c.Version, sched.Version)
	}
	reviewLog := &model.ReviewLog{
		CardID:         cardID,
		Rating:         int16(outcome.Log.Rating),
		Due:            outcome.Log.Due,
		ScheduledDays:  outcome.Log.ScheduledDays,
		ReviewedAt:     outcome.Log.ReviewedAt,
		State:          int16(outcome.Log.State),
		Stability:      outcome.Log.Stability,
		Difficulty:     outcome.Log.Difficulty,
		RemainingSteps: outcome.Log.RemainingSteps,
		CreatedAt:      now,
	}
	if err := cards.CreateReviewLog(ctx, reviewLog); err != nil {
		return nil, err
	}
	day := calendarDay(now, s.timezone())
	if err := s.calendar.WithTx(tx).AddReviewEvents(ctx, day, 1, now); err != nil {
		return nil, err
	}
	updated, err := cards.FindSchedule(ctx, cardID)
	if err != nil {
		return nil, err
	}
	return &ReviewResult{
		CardID:      cardID,
		Rating:      rating,
		Schedule:    updated,
		ReviewLogID: reviewLog.ID,
	}, nil
}

// Reviews 返回某正常 Card 的复习历史分页, reviewed_at 降序.
// 卡不存在或已回收时返回 404, 保证与卡片详情一致.
func (s *ReviewService) Reviews(ctx context.Context, cardID int64, page, pageSize int) ([]model.ReviewLog, int64, error) {
	if _, err := s.cards.Find(ctx, cardID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, apperr.NotFound("Card 不存在")
		}
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.cards.ListReviewLogs(ctx, cardID, offset, pageSize)
}

// scheduleFromModel 把持久化调度行转为领域快照.
func scheduleFromModel(m *model.CardSchedule) schedule.Snapshot {
	return schedule.Snapshot{
		Due:            m.Due,
		Stability:      m.Stability,
		Difficulty:     m.Difficulty,
		ScheduledDays:  m.ScheduledDays,
		Reps:           m.Reps,
		Lapses:         m.Lapses,
		State:          schedule.State(m.State),
		LastReviewAt:   m.LastReviewAt,
		RemainingSteps: m.RemainingSteps,
	}
}

// reviewVersionConflict 构造复习双版本冲突错误, details 同时携带
// 当前 Card 与 Schedule 版本.
func reviewVersionConflict(currentCardVersion, currentScheduleVersion int64) *apperr.Error {
	return apperr.New(apperr.CodeVersionConflict, "Card 或调度已被并发修改").
		WithDetails(map[string]any{
			"current_card_version":     currentCardVersion,
			"current_schedule_version": currentScheduleVersion,
		})
}
