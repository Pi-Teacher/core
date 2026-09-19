// Package fsrs 是 schedule.Scheduler 的 go-fsrs/v4 适配实现.
//
// 本包是本项目唯一出现 FSRS 参数与第三库类型的地方: 固定 Parameters
// 在包内构造, 对上只暴露 domain/schedule 的接口和值类型. 上层 (Card
// 服务, 复习服务, 审批, HTTP, repository) 不得直接 import go-fsrs.
package fsrs

import (
	"fmt"
	"time"

	gofsrs "github.com/open-spaced-repetition/go-fsrs/v4"

	"github.com/Pi-Teacher/server/internal/domain/schedule"
)

// Scheduler 用固定的 go-fsrs/v4 参数实现调度.
//
// 参数按程序描述第 11 节固定: 库默认 21 个权重与 retention/最大间隔,
// 学习步骤与重学步骤显式置为空数组, 因此新卡任意评分都直接进入 Review,
// Review 卡评分 Again 也保持 Review, 不产生分钟级重复; v1 不使用模糊,
// 保证调度结果确定可测. 参数不可变, 结构体零值即可安全并发使用.
type Scheduler struct{}

// NewScheduler 构造固定参数的调度器.
func NewScheduler() *Scheduler { return &Scheduler{} }

// fixedParameters 构造固定 FSRS 参数. 与 DefaultParam 的差异只有空步骤:
// 其余 (默认权重, retention 0.9, 最大间隔 36500, short-term 开, fuzz 关)
// 直接沿用库默认值.
func fixedParameters() gofsrs.Parameters {
	params := gofsrs.DefaultParam()
	params.LearningSteps = []float64{}
	params.RelearningSteps = []float64{}
	return params
}

// toCard 把领域快照转成 go-fsrs 卡. LastReviewAt 为空时用零值, 库据此
// 判断卡尚未复习 (New 状态).
func toCard(s schedule.Snapshot) gofsrs.Card {
	c := gofsrs.Card{
		Due:            s.Due,
		Stability:      s.Stability,
		Difficulty:     s.Difficulty,
		ScheduledDays:  uint64(s.ScheduledDays),
		Reps:           uint64(s.Reps),
		Lapses:         uint64(s.Lapses),
		State:          gofsrs.State(s.State),
		RemainingSteps: int(s.RemainingSteps),
	}
	if s.LastReviewAt != nil {
		c.LastReview = *s.LastReviewAt
	}
	return c
}

// snapshotFromCard 把 go-fsrs 卡转回领域快照.
func snapshotFromCard(c gofsrs.Card) schedule.Snapshot {
	s := schedule.Snapshot{
		Due:            c.Due,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  int64(c.ScheduledDays),
		Reps:           int64(c.Reps),
		Lapses:         int64(c.Lapses),
		State:          schedule.State(c.State),
		RemainingSteps: int64(c.RemainingSteps),
	}
	if !c.LastReview.IsZero() {
		last := c.LastReview
		s.LastReviewAt = &last
	}
	return s
}

// NewSchedule 用库的新卡初始值构造初始调度快照, Due 取 now.
// 直接复用 go-fsrs 的 NewCard, 保证与库后续计算的前提一致.
func (Scheduler) NewSchedule(now time.Time) schedule.Snapshot {
	return snapshotFromCard(gofsrs.NewCard(now))
}

// Next 按当前快照与评分计算下一调度状态, 并返回本次复习的日志快照.
func (Scheduler) Next(current schedule.Snapshot, rating schedule.Rating, now time.Time) (schedule.Result, error) {
	fsrs := gofsrs.NewFSRS(fixedParameters())
	info, err := fsrs.Next(toCard(current), now, gofsrs.Rating(rating))
	if err != nil {
		return schedule.Result{}, fmt.Errorf("fsrs 调度失败: %w", err)
	}
	return schedule.Result{
		Next: snapshotFromCard(info.Card),
		Log: schedule.LogSnapshot{
			Rating:         schedule.Rating(info.ReviewLog.Rating),
			Due:            info.ReviewLog.Due,
			ScheduledDays:  int64(info.ReviewLog.ScheduledDays),
			ReviewedAt:     info.ReviewLog.Review,
			State:          schedule.State(info.ReviewLog.State),
			Stability:      info.ReviewLog.Stability,
			Difficulty:     info.ReviewLog.Difficulty,
			RemainingSteps: int64(info.ReviewLog.RemainingSteps),
		},
	}, nil
}
