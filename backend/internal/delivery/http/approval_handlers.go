package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// approvalSummary 是提案/列表里的精简审批对象.
type approvalSummary struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// approvalTargetResponse 是审批目标响应体.
type approvalTargetResponse struct {
	EntityType  string `json:"entity_type"`
	EntityID    int64  `json:"entity_id"`
	BaseVersion int64  `json:"base_version"`
	Role        string `json:"role"`
}

// approvalResponse 是审批请求响应体. targets 只在详情中返回;
// original_payload / approved_payload 直接内嵌 JSON 对象.
type approvalResponse struct {
	ID                  int64                    `json:"id"`
	Operation           string                   `json:"operation"`
	EntityType          string                   `json:"entity_type"`
	Status              string                   `json:"status"`
	RequestedByAPIKeyID *int64                   `json:"requested_by_api_key_id"`
	OriginalPayload     json.RawMessage          `json:"original_payload"`
	ApprovedPayload     json.RawMessage          `json:"approved_payload"`
	Reason              *string                  `json:"reason"`
	Targets             []approvalTargetResponse `json:"targets,omitempty"`
	CreatedAt           time.Time                `json:"created_at"`
	ProcessedAt         *time.Time               `json:"processed_at"`
}

// approvalToResponse 把请求行与 targets 转为响应体.
// withTargets 为 false 时省略 targets (列表响应).
func approvalToResponse(req *model.ApprovalRequest, targets []model.ApprovalTarget, withTargets bool) approvalResponse {
	resp := approvalResponse{
		ID:                  req.ID,
		Operation:           appsvc.ApprovalOperationName(req.Operation),
		EntityType:          appsvc.ApprovalEntityTypeName(req.EntityType),
		Status:              appsvc.ApprovalStatusName(req.Status),
		RequestedByAPIKeyID: req.RequestedByAPIKeyID,
		OriginalPayload:     rawOrNull(req.OriginalPayload),
		ApprovedPayload:     rawOrNullPtr(req.ApprovedPayload),
		Reason:              req.Reason,
		CreatedAt:           req.CreatedAt,
		ProcessedAt:         req.ProcessedAt,
	}
	if withTargets {
		resp.Targets = make([]approvalTargetResponse, 0, len(targets))
		for _, t := range targets {
			resp.Targets = append(resp.Targets, approvalTargetResponse{
				EntityType:  appsvc.ApprovalEntityTypeName(t.EntityType),
				EntityID:    t.EntityID,
				BaseVersion: t.BaseVersion,
				Role:        t.Role,
			})
		}
	}
	return resp
}

// rawOrNull 把存储的 JSON 字符串还原为内嵌 JSON; 非法或空串返回 null.
func rawOrNull(s string) json.RawMessage {
	if s == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

// rawOrNullPtr 是 rawOrNull 的可空指针版本.
func rawOrNullPtr(s *string) json.RawMessage {
	if s == nil {
		return json.RawMessage("null")
	}
	return rawOrNull(*s)
}

// parseApprovalStatusFilter 解析 status 查询参数, 缺省全部.
func parseApprovalStatusFilter(r *http.Request) (*int16, error) {
	v := r.URL.Query().Get("status")
	if v == "" {
		return nil, nil
	}
	status, ok := appsvc.ParseApprovalStatus(v)
	if !ok {
		return nil, validationFieldError("status", "取值不合法")
	}
	return &status, nil
}

// handleListApprovals 实现 GET /api/web/approvals.
func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	status, err := parseApprovalStatusFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	page, pageSize := parsePaging(r)
	rows, total, err := s.Approvals.List(r.Context(), status, nil, page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]approvalResponse, 0, len(rows))
	for i := range rows {
		items = append(items, approvalToResponse(&rows[i], nil, false))
	}
	writeJSON(w, http.StatusOK, pageResponse[approvalResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleGetApproval 实现 GET /api/web/approvals/{id}.
func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Approvals.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approvalToResponse(detail.Request, detail.Targets, true))
}

// approveRequest 是批准请求体: payload 可选, 缺省表示按原 payload 批准.
type approveRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// handleApprove 实现 POST /api/web/approvals/{id}/approve.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req approveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	// payload 必须是非空 JSON 对象; 空请求体或 null 表示按原 payload.
	var override *json.RawMessage
	if len(req.Payload) > 0 && string(req.Payload) != "null" {
		if !json.Valid(req.Payload) {
			writeError(w, validationFieldError("payload", "不是合法 JSON"))
			return
		}
		override = &req.Payload
	}
	detail, err := s.Approvals.Approve(r.Context(), id, override)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "approval_processed", "approval", id)
	writeJSON(w, http.StatusOK, approvalToResponse(detail.Request, detail.Targets, true))
}

// rejectRequest 是拒绝请求体.
type rejectRequest struct {
	Reason string `json:"reason"`
}

// handleReject 实现 POST /api/web/approvals/{id}/reject.
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	var req rejectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Approvals.Reject(r.Context(), id, trimSpace(req.Reason))
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "approval_processed", "approval", id)
	writeJSON(w, http.StatusOK, approvalToResponse(detail.Request, detail.Targets, true))
}

// batchApproveRequest 是批量批准请求体.
type batchApproveRequest struct {
	IDs []int64 `json:"ids"`
}

// batchRejectRequest 是批量拒绝请求体.
type batchRejectRequest struct {
	IDs    []int64 `json:"ids"`
	Reason string  `json:"reason"`
}

// batchResultResponse 是批量批准/拒绝的单条结果.
type batchResultResponse struct {
	ID      int64              `json:"id"`
	Success bool               `json:"success"`
	Error   *batchResultDetail `json:"error,omitempty"`
}

// batchResultDetail 是批量结果的错误详情.
type batchResultDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// handleBatchApprove 实现 POST /api/web/approvals/batch-approve.
func (s *Server) handleBatchApprove(w http.ResponseWriter, r *http.Request) {
	var req batchApproveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	results, err := s.Approvals.BatchApprove(r.Context(), req.IDs)
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "approval_batch_processed", "approval", int64(len(results)))
	writeJSON(w, http.StatusOK, map[string]any{"results": batchResultsToResponse(results)})
}

// handleBatchReject 实现 POST /api/web/approvals/batch-reject.
func (s *Server) handleBatchReject(w http.ResponseWriter, r *http.Request) {
	var req batchRejectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	results, err := s.Approvals.BatchReject(r.Context(), req.IDs, trimSpace(req.Reason))
	if err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "approval_batch_processed", "approval", int64(len(results)))
	writeJSON(w, http.StatusOK, map[string]any{"results": batchResultsToResponse(results)})
}

// batchResultsToResponse 转换批量结果.
func batchResultsToResponse(results []appsvc.ApprovalBatchResult) []batchResultResponse {
	out := make([]batchResultResponse, 0, len(results))
	for _, res := range results {
		item := batchResultResponse{ID: res.RequestID, Success: res.Success}
		if !res.Success {
			item.Error = &batchResultDetail{Code: res.ErrorCode, Message: res.ErrorMsg}
		}
		out = append(out, item)
	}
	return out
}

// --- CLI 审批查询 (仅自己的) ---

// handleListCLIApprovals 实现 GET /api/cli/approvals.
func (s *Server) handleListCLIApprovals(w http.ResponseWriter, r *http.Request) {
	apiKey := apiKeyFromContext(r.Context())
	if apiKey == nil {
		writeError(w, apperr.Unauthorized("缺少 API Key"))
		return
	}
	status, err := parseApprovalStatusFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	page, pageSize := parsePaging(r)
	rows, total, err := s.Approvals.List(r.Context(), status, &apiKey.ID, page, pageSize)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]approvalResponse, 0, len(rows))
	for i := range rows {
		items = append(items, approvalToResponse(&rows[i], nil, false))
	}
	writeJSON(w, http.StatusOK, pageResponse[approvalResponse]{
		Items: items, Total: total, Page: page, PageSize: pageSize,
	})
}

// handleGetCLIApproval 实现 GET /api/cli/approvals/{id}, 非本人发起返回 404.
func (s *Server) handleGetCLIApproval(w http.ResponseWriter, r *http.Request) {
	apiKey := apiKeyFromContext(r.Context())
	if apiKey == nil {
		writeError(w, apperr.Unauthorized("缺少 API Key"))
		return
	}
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	detail, err := s.Approvals.GetForAPIKey(r.Context(), id, apiKey.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approvalToResponse(detail.Request, detail.Targets, true))
}
