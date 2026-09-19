package fsrs_test

import (
	"testing"
	"time"

	"github.com/Pi-Teacher/server/internal/domain/schedule"
	fsrsadapter "github.com/Pi-Teacher/server/internal/infrastructure/fsrs"
)

// TestNewScheduleIsNewCard 验证新卡调度初值来自库的新卡约定:
// state=New, due=now, 无上次复习时间, 各计数为 0.
func TestNewScheduleIsNewCard(t *testing.T) {
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	s := fsrsadapter.NewScheduler().NewSchedule(now)

	if !s.Due.Equal(now) {
		t.Fatalf("new card due = %v, want %v", s.Due, now)
	}
	if s.State != schedule.New {
		t.Fatalf("new card state = %v, want New", s.State)
	}
	if s.Reps != 0 || s.Lapses != 0 || s.RemainingSteps != 0 {
		t.Fatalf("new card counters = reps:%d lapses:%d remaining:%d, want all 0",
			s.Reps, s.Lapses, s.RemainingSteps)
	}
	if s.LastReviewAt != nil {
		t.Fatalf("new card LastReviewAt = %v, want nil", s.LastReviewAt)
	}
}

// TestEmptyStepsGoStraightToReview 验证空学习步骤的关键行为:
// 新卡任意评分都直接进入 Review, 不产生 Learning/Relearning.
func TestEmptyStepsGoStraightToReview(t *testing.T) {
	scheduler := fsrsadapter.NewScheduler()
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	ratings := []schedule.Rating{schedule.Again, schedule.Hard, schedule.Good, schedule.Easy}

	for _, r := range ratings {
		fresh := scheduler.NewSchedule(now)
		res, err := scheduler.Next(fresh, r, now)
		if err != nil {
			t.Fatalf("Next(%v) error: %v", r, err)
		}
		if res.Next.State != schedule.Review {
			t.Fatalf("rating %v: state = %v, want Review", r, res.Next.State)
		}
		if res.Next.RemainingSteps != 0 {
			t.Fatalf("rating %v: remaining_steps = %d, want 0", r, res.Next.RemainingSteps)
		}
		if res.Log.Rating != r {
			t.Fatalf("log rating = %v, want %v", res.Log.Rating, r)
		}
		if res.Log.ReviewedAt != now {
			t.Fatalf("log reviewed_at = %v, want %v", res.Log.ReviewedAt, now)
		}
	}
}

// TestReviewAgainStaysReview 验证已进入 Review 的卡评 Again 后仍为 Review,
// 不回落 Learning/Relearning (空重学步骤的直接结果).
func TestReviewAgainStaysReview(t *testing.T) {
	scheduler := fsrsadapter.NewScheduler()
	now := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)

	first, err := scheduler.Next(scheduler.NewSchedule(now), schedule.Good, now)
	if err != nil {
		t.Fatalf("first Next error: %v", err)
	}
	later := now.Add(72 * time.Hour)
	second, err := scheduler.Next(first.Next, schedule.Again, later)
	if err != nil {
		t.Fatalf("second Next error: %v", err)
	}
	if second.Next.State != schedule.Review {
		t.Fatalf("state after Again = %v, want Review", second.Next.State)
	}
	if second.Next.RemainingSteps != 0 {
		t.Fatalf("remaining_steps after Again = %d, want 0", second.Next.RemainingSteps)
	}
}

// TestRatingParseString 验证评分字符串解析与序列化互为逆运算.
func TestRatingParseString(t *testing.T) {
	for _, name := range []string{"again", "hard", "good", "easy"} {
		r, ok := schedule.ParseRating(name)
		if !ok {
			t.Fatalf("ParseRating(%q) failed", name)
		}
		if r.String() != name {
			t.Fatalf("roundtrip %q -> %v -> %q", name, r, r.String())
		}
	}
	if _, ok := schedule.ParseRating("manual"); ok {
		t.Fatal("ParseRating(manual) should fail")
	}
}
