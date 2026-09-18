package httpapi

import (
	"net/http"
	"time"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/logging"
)

// --- Topic 请求与响应 ---

// createTopicRequest 是创建 Topic 的请求体.
type createTopicRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// updateTopicRequest 是修改 Topic 的请求体, 字段可选, 至少提供一个.
type updateTopicRequest struct {
	ExpectedVersion *int64  `json:"expected_version"`
	Name            *string `json:"name"`
	Description     *string `json:"description"`
}

// trashTopicRequest 是回收 Topic 的请求体, include_cards 缺省为 false.
type trashTopicRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
	IncludeCards    *bool  `json:"include_cards"`
}

// trashTopicItemRequest 是批量回收的单项.
type trashTopicItemRequest struct {
	ID              int64 `json:"id"`
	ExpectedVersion int64 `json:"expected_version"`
}

// batchTrashTopicsRequest 是批量回收 Topic 的请求体.
type batchTrashTopicsRequest struct {
	Items        []trashTopicItemRequest `json:"items"`
	IncludeCards *bool                   `json:"include_cards"`
}

// trashPreviewTopicsRequest 是回收预览的请求体.
type trashPreviewTopicsRequest struct {
	IDs []int64 `json:"ids"`
}

// topicResponse 是 Topic 的响应体, card_count 由服务端计算.
type topicResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CardCount   int64     `json:"card_count"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// trashedTopicResponse 是回收站 Topic 的响应体: Topic 结构加 trashed_at.
// 回收站 Topic 的关联卡在回收时已清空, card_count 恒为 0,
// 保留该字段让前端复用同一类型.
type trashedTopicResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CardCount   int64     `json:"card_count"`
	Version     int64     `json:"version"`
	TrashedAt   time.Time `json:"trashed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// topicToResponse 把带 card_count 的投影转为响应体.
func topicToResponse(row repo.TopicRow) topicResponse {
	return topicResponse{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		CardCount:   row.CardCount,
		Version:     row.Version,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

// topicToResponse 把新建的 Topic 转为响应体, 新对象 card_count 恒为 0.
func newTopicToResponse(t *model.Topic) topicResponse {
	return topicResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		CardCount:   0,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// trashedTopicToResponse 把回收站 Topic 转为响应体.
func trashedTopicToResponse(t model.Topic) trashedTopicResponse {
	return trashedTopicResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		CardCount:   0,
		Version:     t.Version,
		TrashedAt:   derefTime(t.TrashedAt),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// derefTime 解引用可空时间, 回收站列表只包含已回收对象, nil 视为零值.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// --- Topic handlers ---

// handleListTopics 实现 GET /api/{web,cli}/topics.
func (s *Server) handleListTopics(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Topics.List(r.Context(), r.URL.Query().Get("q"), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]topicResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, topicToResponse(row))
	}
	writeJSON(w, http.StatusOK, pageResponse[topicResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleCreateTopic 实现 POST /api/{web,cli}/topics.
func (s *Server) handleCreateTopic(w http.ResponseWriter, r *http.Request) {
	var req createTopicRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	topic, err := s.Topics.Create(r.Context(), appsvc.TopicInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_created", "topic", topic.ID)
	writeJSON(w, http.StatusCreated, newTopicToResponse(topic))
}

// handleBatchCreateTopics 实现 POST /api/{web,cli}/topics/batch-create.
func (s *Server) handleBatchCreateTopics(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []createTopicRequest `json:"items"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	inputs := make([]appsvc.TopicInput, 0, len(req.Items))
	for _, item := range req.Items {
		inputs = append(inputs, appsvc.TopicInput{
			Name:        item.Name,
			Description: item.Description,
		})
	}
	topics, err := s.Topics.BatchCreate(r.Context(), inputs)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]topicResponse, 0, len(topics))
	for i := range topics {
		items = append(items, newTopicToResponse(&topics[i]))
	}
	s.logEntityEvent(r, "topic_batch_created", "topic", int64(len(topics)))
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

// handleGetTopic 实现 GET /api/{web,cli}/topics/{id}.
func (s *Server) handleGetTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := s.Topics.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, topicToResponse(*row))
}

// handleUpdateTopic 实现 PATCH /api/{web,cli}/topics/{id}.
func (s *Server) handleUpdateTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req updateTopicRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := s.Topics.Update(r.Context(), id, expectedVersion, appsvc.TopicPatch{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_updated", "topic", id)
	writeJSON(w, http.StatusOK, topicToResponse(*row))
}

// handleTrashTopic 实现 POST /api/{web,cli}/topics/{id}/trash.
func (s *Server) handleTrashTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req trashTopicRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	trashedID, affected, err := s.Topics.Trash(r.Context(), id, expectedVersion, boolValue(req.IncludeCards))
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_trashed", "topic", id)
	writeJSON(w, http.StatusOK, map[string]any{
		"trashed_topic_id": trashedID,
		"affected_cards":   affected,
	})
}

// handleBatchTrashTopics 实现 POST /api/{web,cli}/topics/batch-trash.
func (s *Server) handleBatchTrashTopics(w http.ResponseWriter, r *http.Request) {
	var req batchTrashTopicsRequest
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
	trashed, affected, err := s.Topics.BatchTrash(r.Context(), items, boolValue(req.IncludeCards))
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_batch_trashed", "topic", trashed)
	writeJSON(w, http.StatusOK, map[string]any{
		"trashed_topics": trashed,
		"affected_cards": affected,
	})
}

// handleTrashPreviewTopics 实现 POST /api/web/topics/trash-preview.
// 只读统计, 不执行任何修改, 供 WebUI 二次确认.
func (s *Server) handleTrashPreviewTopics(w http.ResponseWriter, r *http.Request) {
	var req trashPreviewTopicsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	topics, affected, alreadyTrashed, err := s.Topics.TrashPreview(r.Context(), req.IDs)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"topics":          topics,
		"affected_cards":  affected,
		"already_trashed": alreadyTrashed,
	})
}

// boolValue 解析可选布尔, 缺省为 false. include_cards 等安全默认值
// 由这里统一提供.
func boolValue(v *bool) bool {
	return v != nil && *v
}

// logEntityEvent 记录实体操作事件, 统一附加请求 ID 与对象标识.
func (s *Server) logEntityEvent(r *http.Request, event, entityType string, entityID int64) {
	s.Logger.InfoContext(logging.WithEvent(r.Context(), event), event,
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
		logging.AttrEntityType, entityType,
		logging.AttrEntityID, entityID,
	)
}
