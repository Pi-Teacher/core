// Package schedule 定义复习调度的领域接口与调度快照值类型.
//
// 上层 (Card/Review/审批/HTTP/repository) 只依赖这里的 Scheduler 接口
// 和快照结构, 不感知任何 FSRS 参数细节; 固定参数与第三库调用全部封装
// 在 infrastructure 层的 adapter 中. 未来替换调度算法或引入个性化参数
// 时只需替换 adapter, 不改动业务代码.
package schedule

import "time"

// Rating 是复习评分, 取值与持久化模型和 FSRS 库对齐.
type Rating int16

const (
	// Again 表示完全忘记.
	Again Rating = 1
	// Hard 表示回忆困难.
	Hard Rating = 2
	// Good 表示正常回忆.
	Good Rating = 3
	// Easy 表示轻松回忆.
	Easy Rating = 4
)

// ParseRating 把对外的评分字符串解析为 Rating, 非法值返回 false.
// CLI 与 Web 共用同一字符串契约 (again|hard|good|easy).
func ParseRating(name string) (Rating, bool) {
	switch name {
	case "again":
		return Again, true
	case "hard":
		return Hard, true
	case "good":
		return Good, true
	case "easy":
		return Easy, true
	default:
		return 0, false
	}
}

// String 返回评分的对外字符串.
func (r Rating) String() string {
	switch r {
	case Again:
		return "again"
	case Hard:
		return "hard"
	case Good:
		return "good"
	case Easy:
		return "easy"
	default:
		return "unknown"
	}
}

// State 是调度状态, 与 FSRS 的 state 枚举对齐.
type State int16

const (
	// New 表示从未复习的新卡.
	New State = 0
	// Learning 表示处于分钟级学习步骤; v1 固定空步骤, 正常流程不产生.
	Learning State = 1
	// Review 表示已进入天级复习.
	Review State = 2
	// Relearning 表示重学步骤; v1 固定空步骤, 正常流程不产生.
	Relearning State = 3
)

// String 返回状态的对外字符串.
func (s State) String() string {
	switch s {
	case New:
		return "new"
	case Learning:
		return "learning"
	case Review:
		return "review"
	case Relearning:
		return "relearning"
	default:
		return "unknown"
	}
}

// Snapshot 是一次调度运算后的完整状态, 同时用于 Card Schedule 的持久化
// 与响应构造. 时间字段全部为 UTC.
type Snapshot struct {
	Due            time.Time
	Stability      float64
	Difficulty     float64
	ScheduledDays  int64
	Reps           int64
	Lapses         int64
	State          State
	LastReviewAt   *time.Time
	RemainingSteps int64
}

// LogSnapshot 是一次复习要写入 Review Log 的核心快照.
// Due 是本次评分前的到期时间 (前置快照), 其余字段是评分后的结果.
type LogSnapshot struct {
	Rating         Rating
	Due            time.Time
	ScheduledDays  int64
	ReviewedAt     time.Time
	State          State
	Stability      float64
	Difficulty     float64
	RemainingSteps int64
}

// Result 是 Next 的返回值: 评分后的调度状态与本次复习日志快照.
type Result struct {
	Next Snapshot
	Log  LogSnapshot
}

// Scheduler 抽象复习调度能力. 实现必须无状态且并发安全, 固定参数只由
// adapter 在其内部构造, 不通过接口暴露.
type Scheduler interface {
	// NewSchedule 返回新卡 (从未复习) 的初始调度快照, Due 取 now.
	NewSchedule(now time.Time) Snapshot
	// Next 按当前快照与评分计算下一调度快照, 并给出本次复习日志快照.
	Next(current Snapshot, rating Rating, now time.Time) (Result, error)
}
