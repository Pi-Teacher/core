package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// 本文件为每个 CLI 写端点提供提案构建函数: 从缓存的原始请求体里
// 解码出 payload, 取出路径/请求体中的目标 ID. 校验与目标解析交由
// 应用层在事务内完成, 这里只负责协议层拆包.

// specOf 组装单个提案 spec.
func specOf(op, entityType int16, id *int64, body []byte) appsvc.ProposalSpec {
	return appsvc.ProposalSpec{Operation: op, EntityType: entityType, EntityID: id, Payload: body}
}

// pathIDPtr 取出路径参数 id 的指针, 供提案 ID 使用.
func pathIDPtr(r *http.Request, name string) (*int64, error) {
	id, err := pathInt64(r, name)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// --- 单个端点构建器 ---

func (s *Server) buildTopicCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	return []appsvc.ProposalSpec{specOf(model.OpTopicCreate, model.EntityTopic, nil, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildTopicUpdate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpTopicUpdate, model.EntityTopic, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildTopicTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpTopicTrash, model.EntityTopic, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildTopicRestore(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpTopicRestore, model.EntityTopic, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildCardCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	return []appsvc.ProposalSpec{specOf(model.OpCardCreate, model.EntityCard, nil, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildCardUpdate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpCardUpdate, model.EntityCard, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildCardTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpCardTrash, model.EntityCard, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildCardRestore(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpCardRestore, model.EntityCard, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildGlossaryCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	return []appsvc.ProposalSpec{specOf(model.OpGlossaryCreate, model.EntityGlossary, nil, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildGlossaryUpdate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpGlossaryUpdate, model.EntityGlossary, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildGlossaryTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpGlossaryTrash, model.EntityGlossary, id, rawBodyFromContext(r.Context()))}, nil
}

func (s *Server) buildGlossaryRestore(r *http.Request) ([]appsvc.ProposalSpec, error) {
	id, err := pathIDPtr(r, "id")
	if err != nil {
		return nil, err
	}
	return []appsvc.ProposalSpec{specOf(model.OpGlossaryRestore, model.EntityGlossary, id, rawBodyFromContext(r.Context()))}, nil
}

// --- 批量端点构建器 ---

// topicBatchItem 是批量创建 Topic 的单项.
type topicBatchItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// batchTopicCreateRequest 是批量创建 Topic 的请求体.
type batchTopicCreateRequest struct {
	Items []topicBatchItem `json:"items"`
}

func (s *Server) buildTopicBatchCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchTopicCreateRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		item, err := json.Marshal(req.Items[i])
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
		}
		specs = append(specs, specOf(model.OpTopicCreate, model.EntityTopic, nil, item))
	}
	return specs, nil
}

// topicTrashItem 是批量回收 Topic 的单项.
type topicTrashItem struct {
	ID              int64 `json:"id"`
	ExpectedVersion int64 `json:"expected_version"`
}

// batchTopicTrashRequest 是批量回收 Topic 的请求体.
type batchTopicTrashRequest struct {
	Items        []topicTrashItem `json:"items"`
	IncludeCards *bool            `json:"include_cards"`
}

func (s *Server) buildTopicBatchTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchTopicTrashRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		// 每项独立提案, payload 内即包含该项的 id 与 expected_version;
		// include_cards 是整批共享参数, 无法从单项 payload 表达,
		// 故这里把共享参数并入每项, 让每一份 payload 自包含.
		payload, err := mergedTopicTrashPayload(req.Items[i], req.IncludeCards)
		if err != nil {
			return nil, err
		}
		id := req.Items[i].ID
		specs = append(specs, specOf(model.OpTopicTrash, model.EntityTopic, &id, payload))
	}
	return specs, nil
}

// mergedTopicTrashPayload 把单项与共享 include_cards 合成自包含 payload.
func mergedTopicTrashPayload(item topicTrashItem, includeCards *bool) ([]byte, error) {
	m := map[string]any{
		"expected_version": item.ExpectedVersion,
	}
	if includeCards != nil {
		m["include_cards"] = *includeCards
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
	}
	return b, nil
}

// batchCardCreateRequest 是批量创建 Card 的请求体.
type batchCardCreateRequest struct {
	Items []appsvc.CardCreatePayload `json:"items"`
}

func (s *Server) buildCardBatchCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchCardCreateRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		payload, err := json.Marshal(req.Items[i])
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
		}
		specs = append(specs, specOf(model.OpCardCreate, model.EntityCard, nil, payload))
	}
	return specs, nil
}

// batchCardTrashRequest 是批量回收 Card 的请求体.
type batchCardTrashRequest struct {
	Items []topicTrashItem `json:"items"`
}

func (s *Server) buildCardBatchTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchCardTrashRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		id := req.Items[i].ID
		payload, err := json.Marshal(map[string]any{"expected_version": req.Items[i].ExpectedVersion})
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
		}
		specs = append(specs, specOf(model.OpCardTrash, model.EntityCard, &id, payload))
	}
	return specs, nil
}

// batchGlossaryCreateRequest 是批量创建 Glossary 的请求体.
type batchGlossaryCreateRequest struct {
	Items []appsvc.GlossaryCreatePayload `json:"items"`
}

func (s *Server) buildGlossaryBatchCreate(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchGlossaryCreateRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		payload, err := json.Marshal(req.Items[i])
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
		}
		specs = append(specs, specOf(model.OpGlossaryCreate, model.EntityGlossary, nil, payload))
	}
	return specs, nil
}

// batchGlossaryTrashRequest 是批量回收 Glossary 的请求体.
type batchGlossaryTrashRequest struct {
	Items []topicTrashItem `json:"items"`
}

func (s *Server) buildGlossaryBatchTrash(r *http.Request) ([]appsvc.ProposalSpec, error) {
	var req batchGlossaryTrashRequest
	if err := decodeRaw(rawBodyFromContext(r.Context()), &req); err != nil {
		return nil, err
	}
	specs := make([]appsvc.ProposalSpec, 0, len(req.Items))
	for i := range req.Items {
		id := req.Items[i].ID
		payload, err := json.Marshal(map[string]any{"expected_version": req.Items[i].ExpectedVersion})
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "编码提案 payload 失败", err)
		}
		specs = append(specs, specOf(model.OpGlossaryTrash, model.EntityGlossary, &id, payload))
	}
	return specs, nil
}

// decodeRaw 按 DisallowUnknownFields 解码原始 JSON, 保持与 HTTP 解码一致.
func decodeRaw(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Validation("请求体不是合法 JSON: " + err.Error())
	}
	return nil
}
