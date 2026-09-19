package appsvc

import "encoding/json"

// 审批 payload schema.
//
// 这些结构同时用于两处: CLI 提案时校验并规范化原始 JSON, 批准时把
// 最终 payload 解码回领域输入. HTTP 请求结构体直接复用它们, 避免
// 两套 schema 漂移. 所有结构都用 DisallowUnknownFields 解码.

// TopicCreatePayload 对应 topic_create.
type TopicCreatePayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToInput 转为创建输入.
func (p TopicCreatePayload) ToInput() TopicInput {
	return TopicInput{Name: p.Name, Description: p.Description}
}

// TopicUpdatePayload 对应 topic_update, 字段可选至少一个.
type TopicUpdatePayload struct {
	ExpectedVersion *int64  `json:"expected_version"`
	Name            *string `json:"name"`
	Description     *string `json:"description"`
}

// ToPatch 转为修改补丁.
func (p TopicUpdatePayload) ToPatch() TopicPatch {
	return TopicPatch{Name: p.Name, Description: p.Description}
}

// TopicTrashPayload 对应 topic_trash, include_cards 缺省 false.
type TopicTrashPayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
	IncludeCards    *bool  `json:"include_cards"`
}

// Include 返回 include_cards 的布尔值, 缺省 false.
func (p TopicTrashPayload) Include() bool { return p.IncludeCards != nil && *p.IncludeCards }

// TopicRestorePayload 对应 topic_restore.
type TopicRestorePayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// CardCreatePayload 对应 card_create, enable_embedding 缺省 true.
type CardCreatePayload struct {
	TopicID         *int64 `json:"topic_id"`
	Front           string `json:"front"`
	Back            string `json:"back"`
	EnableEmbedding *bool  `json:"enable_embedding"`
}

// ToInput 转为创建输入, enable_embedding 缺省 true.
func (p CardCreatePayload) ToInput() CardInput {
	return CardInput{
		TopicID:         p.TopicID,
		Front:           p.Front,
		Back:            p.Back,
		EnableEmbedding: p.EnableEmbedding == nil || *p.EnableEmbedding,
	}
}

// CardUpdatePayload 对应 card_update. topic_id 三态: 缺省不改,
// null 设为无 Topic, 数字指定 Topic.
type CardUpdatePayload struct {
	ExpectedVersion *int64        `json:"expected_version"`
	TopicID         NullableInt64 `json:"topic_id"`
	Front           *string       `json:"front"`
	Back            *string       `json:"back"`
	EnableEmbedding *bool         `json:"enable_embedding"`
}

// ToPatch 转为修改补丁.
func (p CardUpdatePayload) ToPatch() CardPatch {
	patch := CardPatch{
		SetTopicID: p.TopicID.Set,
		TopicID:    p.TopicID.Value,
	}
	if p.Front != nil {
		patch.SetFront, patch.Front = true, *p.Front
	}
	if p.Back != nil {
		patch.SetBack, patch.Back = true, *p.Back
	}
	if p.EnableEmbedding != nil {
		patch.SetEnableEmbedding, patch.EnableEmbedding = true, *p.EnableEmbedding
	}
	return patch
}

// MarshalJSON 重新编码 payload, 保留 topic_id 的三态语义:
// 字段缺省时省略, 显式 null 时写 null, 数字时写数字.
// 提案入库时用它把客户端 JSON 规范化为稳定形态.
func (p CardUpdatePayload) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 5)
	if p.ExpectedVersion != nil {
		m["expected_version"] = *p.ExpectedVersion
	}
	if p.TopicID.Set {
		// Value 为 nil 时 json.Marshal 输出 null.
		m["topic_id"] = p.TopicID.Value
	}
	if p.Front != nil {
		m["front"] = *p.Front
	}
	if p.Back != nil {
		m["back"] = *p.Back
	}
	if p.EnableEmbedding != nil {
		m["enable_embedding"] = *p.EnableEmbedding
	}
	return json.Marshal(m)
}

// CardTrashPayload 对应 card_trash.
type CardTrashPayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// CardRestorePayload 对应 card_restore, topic_id 缺省恢复为无 Topic.
type CardRestorePayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
	TopicID         *int64 `json:"topic_id"`
}

// GlossaryCreatePayload 对应 glossary_create.
type GlossaryCreatePayload struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

// ToInput 转为创建输入.
func (p GlossaryCreatePayload) ToInput() GlossaryInput {
	return GlossaryInput{Term: p.Term, Definition: p.Definition}
}

// GlossaryUpdatePayload 对应 glossary_update, 字段可选至少一个.
type GlossaryUpdatePayload struct {
	ExpectedVersion *int64  `json:"expected_version"`
	Term            *string `json:"term"`
	Definition      *string `json:"definition"`
}

// ToPatch 转为修改补丁.
func (p GlossaryUpdatePayload) ToPatch() GlossaryPatch {
	return GlossaryPatch{Term: p.Term, Definition: p.Definition}
}

// GlossaryTrashPayload 对应 glossary_trash.
type GlossaryTrashPayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

// GlossaryRestorePayload 对应 glossary_restore.
type GlossaryRestorePayload struct {
	ExpectedVersion *int64 `json:"expected_version"`
}
