package httpapi

import (
	"net/http"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/logging"
)

// --- 回收站请求与响应 ---

// restoreCardRequest 是恢复回收站 Card 的请求体, topic_id 可选,
// 缺省恢复为无 Topic.
type restoreCardRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
	TopicID         *int64 `json:"topic_id"`
}

// versionedItemRequest 是批量恢复/删除的单项.
type versionedItemRequest struct {
	ID              int64 `json:"id"`
	ExpectedVersion int64 `json:"expected_version"`
}

// restoreCardItemRequest 是批量恢复回收站 Card 的单项, topic_id 可选.
type restoreCardItemRequest struct {
	ID              int64  `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	TopicID         *int64 `json:"topic_id"`
}

// restoreCardResultResponse 是恢复 Card 的响应体.
type restoreCardResultResponse struct {
	TrashCardID int64 `json:"trash_card_id"`
	NewCardID   int64 `json:"new_card_id"`
}

// --- 回收站列表 ---

// handleListTrashedCards 实现 GET /api/{web,cli}/trash/cards.
func (s *Server) handleListTrashedCards(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Cards.ListTrashed(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]trashedCardResponse, 0, len(rows))
	for i := range rows {
		items = append(items, trashedCardToResponse(rows[i]))
	}
	writeJSON(w, http.StatusOK, pageResponse[trashedCardResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleListTrashedTopics 实现 GET /api/{web,cli}/trash/topics.
func (s *Server) handleListTrashedTopics(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Topics.ListTrashed(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]trashedTopicResponse, 0, len(rows))
	for i := range rows {
		items = append(items, trashedTopicToResponse(rows[i]))
	}
	writeJSON(w, http.StatusOK, pageResponse[trashedTopicResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleListTrashedGlossary 实现 GET /api/{web,cli}/trash/glossary.
func (s *Server) handleListTrashedGlossary(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Glossaries.ListTrashed(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]trashedGlossaryResponse, 0, len(rows))
	for i := range rows {
		items = append(items, trashedGlossaryToResponse(rows[i]))
	}
	writeJSON(w, http.StatusOK, pageResponse[trashedGlossaryResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// --- 恢复 ---

// handleRestoreCard 实现 POST /api/{web,cli}/trash/cards/{id}/restore.
func (s *Server) handleRestoreCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req restoreCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	result, _, err := s.Cards.Restore(r.Context(), id, expectedVersion, req.TopicID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_restored", "card", result.NewCardID)
	writeJSON(w, http.StatusOK, restoreCardResultResponse{
		TrashCardID: result.TrashCardID,
		NewCardID:   result.NewCardID,
	})
}

// handleBatchRestoreCards 实现 POST /api/web/trash/cards/batch-restore.
func (s *Server) handleBatchRestoreCards(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []restoreCardItemRequest `json:"items"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	items := make([]appsvc.RestoreItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, appsvc.RestoreItem{
			ID:              item.ID,
			ExpectedVersion: item.ExpectedVersion,
			TopicID:         item.TopicID,
		})
	}
	results, _, err := s.Cards.BatchRestore(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	restored := make([]restoreCardResultResponse, 0, len(results))
	for _, result := range results {
		restored = append(restored, restoreCardResultResponse{
			TrashCardID: result.TrashCardID,
			NewCardID:   result.NewCardID,
		})
	}
	s.logEntityEvent(r, "card_batch_restored", "card", int64(len(restored)))
	writeJSON(w, http.StatusOK, map[string]any{"restored": restored})
}

// handleRestoreTopic 实现 POST /api/{web,cli}/trash/topics/{id}/restore.
func (s *Server) handleRestoreTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := restoreVersion(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	topic, err := s.Topics.Restore(r.Context(), id, expectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_restored", "topic", id)
	writeJSON(w, http.StatusOK, topicToResponse(repo.TopicRow{Topic: *topic}))
}

// handleBatchRestoreTopics 实现 POST /api/web/trash/topics/batch-restore.
func (s *Server) handleBatchRestoreTopics(w http.ResponseWriter, r *http.Request) {
	items, err := decodeVersionedItems(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	restored, err := s.Topics.BatchRestore(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]topicResponse, 0, len(restored))
	for i := range restored {
		out = append(out, topicToResponse(repo.TopicRow{Topic: restored[i]}))
	}
	s.logEntityEvent(r, "topic_batch_restored", "topic", int64(len(restored)))
	writeJSON(w, http.StatusOK, map[string]any{"restored": out})
}

// handleRestoreGlossary 实现 POST /api/{web,cli}/trash/glossary/{id}/restore.
func (s *Server) handleRestoreGlossary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := restoreVersion(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := s.Glossaries.Restore(r.Context(), id, expectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_restored", "glossary", id)
	writeJSON(w, http.StatusOK, glossaryToResponse(g))
}

// handleBatchRestoreGlossary 实现 POST /api/web/trash/glossary/batch-restore.
func (s *Server) handleBatchRestoreGlossary(w http.ResponseWriter, r *http.Request) {
	items, err := decodeVersionedItems(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	restored, err := s.Glossaries.BatchRestore(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]glossaryResponse, 0, len(restored))
	for i := range restored {
		out = append(out, glossaryToResponse(&restored[i]))
	}
	s.logEntityEvent(r, "glossary_batch_restored", "glossary", int64(len(restored)))
	writeJSON(w, http.StatusOK, map[string]any{"restored": out})
}

// restoreVersion 解析只含 expected_version 的请求体,
// 供恢复与永久删除类端点共用.
func restoreVersion(w http.ResponseWriter, r *http.Request) (int64, error) {
	var req struct {
		ExpectedVersion *int64 `json:"expected_version"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return 0, err
	}
	return requireExpectedVersion(req.ExpectedVersion)
}

// decodeVersionedItems 解析 { items: [{id, expected_version}] } 请求体.
func decodeVersionedItems(w http.ResponseWriter, r *http.Request) ([]appsvc.VersionedItem, error) {
	var req struct {
		Items []versionedItemRequest `json:"items"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return nil, err
	}
	items := make([]appsvc.VersionedItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, appsvc.VersionedItem{
			ID:              item.ID,
			ExpectedVersion: item.ExpectedVersion,
		})
	}
	return items, nil
}

// --- 永久删除 (仅 Web) ---

// handleDeleteTrashedCard 实现 POST /api/web/trash/cards/{id}/delete.
func (s *Server) handleDeleteTrashedCard(w http.ResponseWriter, r *http.Request) {
	id, expectedVersion, err := pathIDAndVersion(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.Cards.DeleteForever(r.Context(), id, expectedVersion); err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_deleted_forever", "card", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBatchDeleteTrashedCards 实现 POST /api/web/trash/cards/batch-delete.
func (s *Server) handleBatchDeleteTrashedCards(w http.ResponseWriter, r *http.Request) {
	items, err := decodeVersionedItems(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	deleted, err := s.Cards.BatchDeleteForever(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "card_batch_deleted_forever", "card", deleted)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// handleDeleteTrashedTopic 实现 POST /api/web/trash/topics/{id}/delete.
func (s *Server) handleDeleteTrashedTopic(w http.ResponseWriter, r *http.Request) {
	id, expectedVersion, err := pathIDAndVersion(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.Topics.DeleteForever(r.Context(), id, expectedVersion); err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_deleted_forever", "topic", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBatchDeleteTrashedTopics 实现 POST /api/web/trash/topics/batch-delete.
func (s *Server) handleBatchDeleteTrashedTopics(w http.ResponseWriter, r *http.Request) {
	items, err := decodeVersionedItems(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	deleted, err := s.Topics.BatchDeleteForever(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "topic_batch_deleted_forever", "topic", deleted)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// handleDeleteTrashedGlossary 实现 POST /api/web/trash/glossary/{id}/delete.
func (s *Server) handleDeleteTrashedGlossary(w http.ResponseWriter, r *http.Request) {
	id, expectedVersion, err := pathIDAndVersion(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.Glossaries.DeleteForever(r.Context(), id, expectedVersion); err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_deleted_forever", "glossary", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBatchDeleteTrashedGlossary 实现 POST /api/web/trash/glossary/batch-delete.
func (s *Server) handleBatchDeleteTrashedGlossary(w http.ResponseWriter, r *http.Request) {
	items, err := decodeVersionedItems(w, r)
	if err != nil {
		writeError(w, err)
		return
	}
	deleted, err := s.Glossaries.BatchDeleteForever(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_batch_deleted_forever", "glossary", deleted)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// handleEmptyTrash 实现 POST /api/web/trash/empty.
func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	result, err := s.Trash.Empty(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	s.Logger.InfoContext(logging.WithEvent(r.Context(), "trash_emptied"), "回收站已清空",
		logging.AttrRequestID, RequestIDFromContext(r.Context()),
		"cards", result.Cards,
		"topics", result.Topics,
		"glossary", result.Glossary,
	)
	writeJSON(w, http.StatusOK, result)
}

// pathIDAndVersion 解析路径 id 与请求体中的 expected_version,
// 供永久删除类端点共用.
func pathIDAndVersion(w http.ResponseWriter, r *http.Request) (int64, int64, error) {
	id, err := pathInt64(r, "id")
	if err != nil {
		return 0, 0, err
	}
	expectedVersion, err := restoreVersion(w, r)
	if err != nil {
		return 0, 0, err
	}
	return id, expectedVersion, nil
}
