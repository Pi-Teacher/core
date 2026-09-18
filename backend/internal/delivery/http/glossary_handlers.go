package httpapi

import (
	"net/http"
	"time"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// --- Glossary 请求与响应 ---

// createGlossaryRequest 是创建 Glossary 的请求体.
type createGlossaryRequest struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

// updateGlossaryRequest 是修改 Glossary 的请求体, 字段可选, 至少提供一个.
type updateGlossaryRequest struct {
	ExpectedVersion *int64  `json:"expected_version"`
	Term            *string `json:"term"`
	Definition      *string `json:"definition"`
}

// trashGlossaryRequest 是回收 Glossary 的请求体.
type trashGlossaryRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// glossaryResponse 是 Glossary 的响应体.
type glossaryResponse struct {
	ID         int64     `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// trashedGlossaryResponse 是回收站 Glossary 的响应体, 加 trashed_at.
type trashedGlossaryResponse struct {
	ID         int64     `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	Version    int64     `json:"version"`
	TrashedAt  time.Time `json:"trashed_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// glossaryToResponse 把 Glossary 行转为响应体.
func glossaryToResponse(g *model.Glossary) glossaryResponse {
	return glossaryResponse{
		ID:         g.ID,
		Term:       g.Term,
		Definition: g.Definition,
		Version:    g.Version,
		CreatedAt:  g.CreatedAt,
		UpdatedAt:  g.UpdatedAt,
	}
}

// trashedGlossaryToResponse 把回收站 Glossary 转为响应体.
func trashedGlossaryToResponse(g model.Glossary) trashedGlossaryResponse {
	return trashedGlossaryResponse{
		ID:         g.ID,
		Term:       g.Term,
		Definition: g.Definition,
		Version:    g.Version,
		TrashedAt:  derefTime(g.TrashedAt),
		CreatedAt:  g.CreatedAt,
		UpdatedAt:  g.UpdatedAt,
	}
}

// --- Glossary handlers ---

// handleListGlossary 实现 GET /api/{web,cli}/glossary.
func (s *Server) handleListGlossary(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePaging(r)
	rows, total, err := s.Glossaries.List(r.Context(), r.URL.Query().Get("q"), page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]glossaryResponse, 0, len(rows))
	for i := range rows {
		items = append(items, glossaryToResponse(&rows[i]))
	}
	writeJSON(w, http.StatusOK, pageResponse[glossaryResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleCreateGlossary 实现 POST /api/{web,cli}/glossary.
func (s *Server) handleCreateGlossary(w http.ResponseWriter, r *http.Request) {
	var req createGlossaryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	g, err := s.Glossaries.Create(r.Context(), appsvc.GlossaryInput{
		Term:       req.Term,
		Definition: req.Definition,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_created", "glossary", g.ID)
	writeJSON(w, http.StatusCreated, glossaryToResponse(g))
}

// handleBatchCreateGlossary 实现 POST /api/{web,cli}/glossary/batch-create.
func (s *Server) handleBatchCreateGlossary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []createGlossaryRequest `json:"items"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	inputs := make([]appsvc.GlossaryInput, 0, len(req.Items))
	for _, item := range req.Items {
		inputs = append(inputs, appsvc.GlossaryInput{
			Term:       item.Term,
			Definition: item.Definition,
		})
	}
	created, err := s.Glossaries.BatchCreate(r.Context(), inputs)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]glossaryResponse, 0, len(created))
	for i := range created {
		items = append(items, glossaryToResponse(&created[i]))
	}
	s.logEntityEvent(r, "glossary_batch_created", "glossary", int64(len(created)))
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

// handleGetGlossary 实现 GET /api/{web,cli}/glossary/{id}.
func (s *Server) handleGetGlossary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := s.Glossaries.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, glossaryToResponse(g))
}

// handleUpdateGlossary 实现 PATCH /api/{web,cli}/glossary/{id}.
func (s *Server) handleUpdateGlossary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req updateGlossaryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := s.Glossaries.Update(r.Context(), id, expectedVersion, appsvc.GlossaryPatch{
		Term:       req.Term,
		Definition: req.Definition,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_updated", "glossary", id)
	writeJSON(w, http.StatusOK, glossaryToResponse(g))
}

// handleTrashGlossary 实现 POST /api/{web,cli}/glossary/{id}/trash.
func (s *Server) handleTrashGlossary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req trashGlossaryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	expectedVersion, err := requireExpectedVersion(req.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.Glossaries.Trash(r.Context(), id, expectedVersion); err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_trashed", "glossary", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBatchTrashGlossary 实现 POST /api/{web,cli}/glossary/batch-trash.
func (s *Server) handleBatchTrashGlossary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			ID              int64 `json:"id"`
			ExpectedVersion int64 `json:"expected_version"`
		} `json:"items"`
	}
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
	count, err := s.Glossaries.BatchTrash(r.Context(), items)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "glossary_batch_trashed", "glossary", count)
	writeJSON(w, http.StatusOK, map[string]any{"trashed_glossary": count})
}
