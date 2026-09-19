package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// --- Card 请求与响应 ---

// createCardRequest 是创建 Card 的请求体.
// topic_id 可为 null; enable_embedding 缺省为 true.
type createCardRequest struct {
	TopicID         *int64 `json:"topic_id"`
	Front           string `json:"front"`
	Back            string `json:"back"`
	EnableEmbedding *bool  `json:"enable_embedding"`
}

// updateCardRequest 是修改 Card 的请求体.
// topic_id 三态: 缺省不改, null 设为无 Topic, 数字指定 Topic.
type updateCardRequest struct {
	ExpectedVersion *int64        `json:"expected_version"`
	TopicID         NullableInt64 `json:"topic_id"`
	Front           *string       `json:"front"`
	Back            *string       `json:"back"`
	EnableEmbedding *bool         `json:"enable_embedding"`
}

// trashCardRequest 是回收 Card 的请求体.
type trashCardRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// batchTrashCardsRequest 是批量回收 Card 的请求体.
type batchTrashCardsRequest struct {
	Items []struct {
		ID              int64 `json:"id"`
		ExpectedVersion int64 `json:"expected_version"`
	} `json:"items"`
}

// cardResponse 是 Card 列表项的响应体, 不含 schedule 与向量.
type cardResponse struct {
	ID              int64     `json:"id"`
	TopicID         *int64    `json:"topic_id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	EnableEmbedding bool      `json:"enable_embedding"`
	EmbeddingStatus string    `json:"embedding_status"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// cardDetailResponse 是 Card 详情响应体, 在列表项基础上增加
// embedding_error 与 schedule. 向量 BLOB 永远不出现在响应中.
type cardDetailResponse struct {
	cardResponse
	EmbeddingError *string           `json:"embedding_error"`
	Schedule       *scheduleResponse `json:"schedule"`
}

// scheduleResponse 是 FSRS 调度快照的响应体.
type scheduleResponse struct {
	Due           time.Time  `json:"due"`
	State         string     `json:"state"`
	Stability     float64    `json:"stability"`
	Difficulty    float64    `json:"difficulty"`
	ScheduledDays int64      `json:"scheduled_days"`
	Reps          int64      `json:"reps"`
	Lapses        int64      `json:"lapses"`
	LastReviewAt  *time.Time `json:"last_review_at"`
	Version       int64      `json:"version"`
}

// trashedCardResponse 是回收站 Card 的响应体.
type trashedCardResponse struct {
	ID              int64     `json:"id"`
	Front           string    `json:"front"`
	Back            string    `json:"back"`
	EnableEmbedding bool      `json:"enable_embedding"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	TrashedAt       time.Time `json:"trashed_at"`
}

// cardToResponse 把 Card 行转为列表项响应体.
func cardToResponse(c *model.Card) cardResponse {
	return cardResponse{
		ID:              c.ID,
		TopicID:         c.TopicID,
		Front:           c.Front,
		Back:            c.Back,
		EnableEmbedding: c.EnableEmbedding,
		EmbeddingStatus: embeddingStatusText(c),
		Version:         c.Version,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       c.UpdatedAt,
	}
}

// cardDetailToResponse 把 Card 详情转为详情响应体.
func cardDetailToResponse(d *appsvc.CardDetail) cardDetailResponse {
	resp := cardDetailResponse{cardResponse: cardToResponse(d.Card)}
	if d.Card.EnableEmbedding {
		resp.EmbeddingError = d.Card.EmbeddingError
	}
	if d.Schedule != nil {
		resp.Schedule = scheduleToResponse(d.Schedule)
	}
	return resp
}

// scheduleToResponse 把调度行转为响应体.
func scheduleToResponse(s *model.CardSchedule) *scheduleResponse {
	return &scheduleResponse{
		Due:           s.Due,
		State:         scheduleStateText(s.State),
		Stability:     s.Stability,
		Difficulty:    s.Difficulty,
		ScheduledDays: s.ScheduledDays,
		Reps:          s.Reps,
		Lapses:        s.Lapses,
		LastReviewAt:  s.LastReviewAt,
		Version:       s.Version,
	}
}

// trashedCardToResponse 把回收站 Card 转为响应体.
func trashedCardToResponse(tc model.TrashedCard) trashedCardResponse {
	return trashedCardResponse{
		ID:              tc.ID,
		Front:           tc.Front,
		Back:            tc.Back,
		EnableEmbedding: tc.EnableEmbedding,
		Version:         tc.Version,
		CreatedAt:       tc.CreatedAt,
		TrashedAt:       tc.TrashedAt,
	}
}

// embeddingStatusText 把任务状态映射为响应字符串.
// enable_embedding=false 时为 disabled; 启用但状态为空是数据异常,
// 按 pending 兜底让响应始终合法.
func embeddingStatusText(c *model.Card) string {
	if !c.EnableEmbedding {
		return "disabled"
	}
	if c.EmbeddingStatus == nil {
		return "pending"
	}
	switch *c.EmbeddingStatus {
	case model.EmbeddingProcessing:
		return "processing"
	case model.EmbeddingReady:
		return "ready"
	case model.EmbeddingFailed:
		return "failed"
	default:
		return "pending"
	}
}

// scheduleStateText 把调度状态映射为响应字符串.
// v1 正常流程只产生 new 与 review, 其余值仅为数据完整性保留.
func scheduleStateText(state int16) string {
	switch state {
	case model.StateNew:
		return "new"
	case model.StateReview:
		return "review"
	case model.StateLearning:
		return "learning"
	case model.StateRelearning:
		return "relearning"
	default:
		return "unknown"
	}
}

// --- Card handlers ---

// parseCardFilter 解析 Card 列表的查询参数.
// 非法取值直接报校验错误而不是静默回退, 让客户端尽早发现拼写问题.
func parseCardFilter(r *http.Request) (repo.CardFilter, error) {
	f := repo.CardFilter{Sort: "created_at", Order: "desc"}
	if v := r.URL.Query().Get("topic_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return f, validationFieldError("topic_id", "必须是非负整数")
		}
		// 0 表示无 Topic 的 Card.
		f.TopicID = &n
	}
	f.Q = r.URL.Query().Get("q")
	if v := r.URL.Query().Get("embedding_status"); v != "" {
		switch v {
		case "pending", "processing", "ready", "failed", "disabled":
			f.EmbeddingStatus = v
		default:
			return f, validationFieldError("embedding_status", "取值不合法")
		}
	}
	if v := r.URL.Query().Get("sort"); v != "" {
		switch v {
		case "created_at", "updated_at":
			f.Sort = v
		default:
			return f, validationFieldError("sort", "取值不合法")
		}
	}
	if v := r.URL.Query().Get("order"); v != "" {
		switch v {
		case "asc", "desc":
			f.Order = v
		default:
			return f, validationFieldError("order", "取值不合法")
		}
	}
	return f, nil
}

// validationFieldError 构造带字段名的校验错误.
func validationFieldError(field, message string) error {
	return apperr.Validation(field + " " + message).
		WithDetails(map[string]any{"field": field})
}

// handleListCards 实现 GET /api/{web,cli}/cards.
func (s *Server) handleListCards(w http.ResponseWriter, r *http.Request) {
	filter, err := parseCardFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	page, pageSize := parsePaging(r)
	rows, total, err := s.Cards.List(r.Context(), filter, page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]cardResponse, 0, len(rows))
	for i := range rows {
		items = append(items, cardToResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, pageResponse[cardResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleCreateCard 实现 POST /api/{web,cli}/cards.
func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var req createCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Cards.Create(r.Context(), cardInputFromRequest(req))
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_created", "card", detail.Card.ID)
	writeJSON(w, http.StatusCreated, cardDetailToResponse(detail))
}

// cardInputFromRequest 把创建请求转为服务输入, enable_embedding 缺省 true.
func cardInputFromRequest(req createCardRequest) appsvc.CardInput {
	return appsvc.CardInput{
		TopicID:         req.TopicID,
		Front:           req.Front,
		Back:            req.Back,
		EnableEmbedding: req.EnableEmbedding == nil || *req.EnableEmbedding,
	}
}

// handleBatchCreateCards 实现 POST /api/{web,cli}/cards/batch-create.
func (s *Server) handleBatchCreateCards(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []createCardRequest `json:"items"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	inputs := make([]appsvc.CardInput, 0, len(req.Items))
	for _, item := range req.Items {
		inputs = append(inputs, cardInputFromRequest(item))
	}
	details, err := s.Cards.BatchCreate(r.Context(), inputs)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]cardDetailResponse, 0, len(details))
	for i := range details {
		items = append(items, cardDetailToResponse(&details[i]))
	}
	s.logEntityEvent(r, "card_batch_created", "card", int64(len(details)))
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

// handleGetCard 实现 GET /api/{web,cli}/cards/{id}.
func (s *Server) handleGetCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Cards.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardDetailToResponse(detail))
}

// handleUpdateCard 实现 PATCH /api/{web,cli}/cards/{id}.
func (s *Server) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req updateCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	patch := appsvc.CardPatch{
		SetTopicID: req.TopicID.Set,
		TopicID:    req.TopicID.Value,
	}
	if req.Front != nil {
		patch.SetFront, patch.Front = true, *req.Front
	}
	if req.Back != nil {
		patch.SetBack, patch.Back = true, *req.Back
	}
	if req.EnableEmbedding != nil {
		patch.SetEnableEmbedding, patch.EnableEmbedding = true, *req.EnableEmbedding
	}
	detail, err := s.Cards.Update(r.Context(), id, expectedVersion, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_updated", "card", id)
	writeJSON(w, http.StatusOK, cardDetailToResponse(detail))
}

// handleTrashCard 实现 POST /api/{web,cli}/cards/{id}/trash.
func (s *Server) handleTrashCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req trashCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	trashedID, err := s.Cards.Trash(r.Context(), id, expectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_trashed", "card", id)
	writeJSON(w, http.StatusOK, map[string]any{"trashed_card_id": trashedID})
}

// handleBatchTrashCards 实现 POST /api/{web,cli}/cards/batch-trash.
func (s *Server) handleBatchTrashCards(w http.ResponseWriter, r *http.Request) {
	var req batchTrashCardsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	items := make([]appsvc.VersionedItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, appsvc.VersionedItem{
			ID:              item.ID,
			ExpectedVersion: item.ExpectedVersion,
		})
	}
	count, err := s.Cards.BatchTrash(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_batch_trashed", "card", count)
	writeJSON(w, http.StatusOK, map[string]any{"trashed_cards": count})
}

// mergeCardRequest 是合并 Card 的请求体.
// topic_id 三态: 缺省继承, null 无 Topic, 数字指定;
// enable_embedding 缺省继承来源卡, front/back 缺省按 Q1/Q2 拼接.
type mergeCardRequest struct {
	SourceCardIDs   []int64       `json:"source_card_ids"`
	Front           *string       `json:"front"`
	Back            *string       `json:"back"`
	TopicID         NullableInt64 `json:"topic_id"`
	EnableEmbedding *bool         `json:"enable_embedding"`
}

// handleMergeCard 实现 POST /api/{web,cli}/cards/merge: 合并两张来源卡为
// 一张新卡, 来源卡进回收站. Web 直写; CLI 按 merge 审批开关分流.
func (s *Server) handleMergeCard(w http.ResponseWriter, r *http.Request) {
	var req mergeCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Cards.Merge(r.Context(), appsvc.CardMergeInput{
		SourceIDs:       req.SourceCardIDs,
		Front:           req.Front,
		Back:            req.Back,
		TopicID:         req.TopicID,
		EnableEmbedding: req.EnableEmbedding,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_merged", "card", detail.Card.ID)
	writeJSON(w, http.StatusCreated, cardDetailToResponse(detail))
}
