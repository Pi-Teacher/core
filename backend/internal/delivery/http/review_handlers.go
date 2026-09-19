package httpapi

import (
	"net/http"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/domain/schedule"
)

// --- 复习 ---

// dueItemResponse 是复习队列单项: 到期卡与其调度摘要.
type dueItemResponse struct {
	CardID          int64     `json:"card_id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	TopicID         *int64    `json:"topic_id"`
	CardVersion     int64     `json:"card_version"`
	Due             time.Time `json:"due"`
	State           string    `json:"state"`
	ScheduleVersion int64     `json:"schedule_version"`
	Reps            int64     `json:"reps"`
	Lapses          int64     `json:"lapses"`
}

// dueListResponse 是复习队列响应.
type dueListResponse struct {
	Items []dueItemResponse `json:"items"`
	Total int64             `json:"total"`
}

// handleReviewDue 实现 GET /api/{web,cli}/review/due.
func (s *Server) handleReviewDue(w http.ResponseWriter, r *http.Request) {
	topicID, err := parseOptionalTopicID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	limit, err := parseDueLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := s.Reviews.Due(r.Context(), topicID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]dueItemResponse, 0, len(list.Items))
	for i := range list.Items {
		it := list.Items[i]
		items = append(items, dueItemResponse{
			CardID:          it.CardID,
			Front:           it.Front,
			Back:            it.Back,
			TopicID:         it.TopicID,
			CardVersion:     it.CardVersion,
			Due:             it.Due,
			State:           schedule.State(it.State).String(),
			ScheduleVersion: it.ScheduleVersion,
			Reps:            it.Reps,
			Lapses:          it.Lapses,
		})
	}
	writeJSON(w, http.StatusOK, dueListResponse{Items: items, Total: list.Total})
}

// submitReviewRequest 是复习提交请求体.
type submitReviewRequest struct {
	Rating                  string `json:"rating"`
	ExpectedCardVersion     *int64 `json:"expected_card_version"`
	ExpectedScheduleVersion *int64 `json:"expected_schedule_version"`
}

// submitReviewResponse 是复习提交响应体.
type submitReviewResponse struct {
	CardID      int64             `json:"card_id"`
	Rating      string            `json:"rating"`
	Schedule    *scheduleResponse `json:"schedule"`
	ReviewLogID int64             `json:"review_log_id"`
}

// handleReviewSubmit 实现 POST /api/{web,cli}/review/{card_id}/submit.
func (s *Server) handleReviewSubmit(w http.ResponseWriter, r *http.Request) {
	cardID, err := pathInt64(r, "card_id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req submitReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	rating, ok := schedule.ParseRating(req.Rating)
	if !ok {
		writeError(w, validationFieldError("rating", "取值必须是 again/hard/good/easy"))
		return
	}
	cardVersion, err := requireExpectedVersion(req.ExpectedCardVersion)
	if err != nil {
		writeError(w, validationFieldError("expected_card_version", "必填"))
		return
	}
	scheduleVersion, err := requireExpectedVersion(req.ExpectedScheduleVersion)
	if err != nil {
		writeError(w, validationFieldError("expected_schedule_version", "必填"))
		return
	}
	result, err := s.Reviews.Submit(r.Context(), cardID, rating, cardVersion, scheduleVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "review_submitted", "card", cardID)
	writeJSON(w, http.StatusOK, submitReviewResponse{
		CardID:      result.CardID,
		Rating:      result.Rating.String(),
		Schedule:    scheduleToResponse(result.Schedule),
		ReviewLogID: result.ReviewLogID,
	})
}

// reviewLogResponse 是复习历史单项响应体, 与 API 设计 4.6 ReviewLog 对齐.
type reviewLogResponse struct {
	ID            int64     `json:"id"`
	CardID        int64     `json:"card_id"`
	Rating        string    `json:"rating"`
	Due           time.Time `json:"due"`
	ScheduledDays int64     `json:"scheduled_days"`
	ReviewedAt    time.Time `json:"reviewed_at"`
	State         string    `json:"state"`
	Stability     float64   `json:"stability"`
	Difficulty    float64   `json:"difficulty"`
}

// handleCardReviews 实现 GET /api/web/cards/{id}/reviews.
func (s *Server) handleCardReviews(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	page, pageSize := parsePaging(r)
	rows, total, err := s.Reviews.Reviews(r.Context(), id, page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]reviewLogResponse, 0, len(rows))
	for i := range rows {
		row := rows[i]
		items = append(items, reviewLogResponse{
			ID:            row.ID,
			CardID:        row.CardID,
			Rating:        schedule.Rating(row.Rating).String(),
			Due:           row.Due,
			ScheduledDays: row.ScheduledDays,
			ReviewedAt:    row.ReviewedAt,
			State:         schedule.State(row.State).String(),
			Stability:     row.Stability,
			Difficulty:    row.Difficulty,
		})
	}
	writeJSON(w, http.StatusOK, pageResponse[reviewLogResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// --- 日历 ---

// calendarDayResponse 是日历单日响应体.
type calendarDayResponse struct {
	Date         string `json:"date"`
	CreatedCards int64  `json:"created_cards"`
	ReviewEvents int64  `json:"review_events"`
}

// calendarResponse 是日历响应体.
type calendarResponse struct {
	Days []calendarDayResponse `json:"days"`
}

// handleCalendar 实现 GET /api/web/calendar.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseCalendarRange(r)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.Calendar.ListRange(r.Context(), from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	days := make([]calendarDayResponse, 0, len(rows))
	for i := range rows {
		days = append(days, calendarDayResponse{
			Date:         rows[i].ActivityDate.Format("2006-01-02"),
			CreatedCards: rows[i].CreatedCards,
			ReviewEvents: rows[i].ReviewEvents,
		})
	}
	writeJSON(w, http.StatusOK, calendarResponse{Days: days})
}

// parseCalendarRange 解析 from/to (必填, YYYY-MM-DD), 校验顺序与跨度.
// 返回的两个时间都以 UTC 零点表示自然日, 与 Calendar 落库口径一致.
func parseCalendarRange(r *http.Request) (time.Time, time.Time, error) {
	fromRaw := r.URL.Query().Get("from")
	toRaw := r.URL.Query().Get("to")
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, apperr.Validation("from 与 to 必填").
			WithDetails(map[string]any{"field": "from"})
	}
	from, err := time.Parse("2006-01-02", fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, validationFieldError("from", "必须是 YYYY-MM-DD")
	}
	to, err := time.Parse("2006-01-02", toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, validationFieldError("to", "必须是 YYYY-MM-DD")
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, validationFieldError("from", "不能晚于 to")
	}
	// 闭区间天数 = 差值 + 1, 上限 366 天.
	if to.Sub(from) > 365*24*time.Hour {
		return time.Time{}, time.Time{}, apperr.Validation("日历跨度最多 366 天").
			WithDetails(map[string]any{"field": "from"})
	}
	return from, to, nil
}
