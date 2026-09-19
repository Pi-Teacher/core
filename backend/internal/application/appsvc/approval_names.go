package appsvc

import "github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"

// 审批目标的角色常量. target 是操作主对象; topic 是建卡/改卡 payload
// 显式引用的 Topic; affected_card 是 Topic 连带回收涉及的在册 Card;
// source_1/source_2 是 Card 合并提案的两张来源卡.
const (
	roleTarget       = "target"
	roleTopic        = "topic"
	roleAffectedCard = "affected_card"
	// roleSource1 / roleSource2 是 Card 合并提案的两张来源卡.
	roleSource1 = "source_1"
	roleSource2 = "source_2"
)

// ApprovalOperationName 返回操作的对外字符串名.
func ApprovalOperationName(op int16) string {
	switch op {
	case model.OpCardCreate:
		return "card_create"
	case model.OpCardUpdate:
		return "card_update"
	case model.OpCardTrash:
		return "card_trash"
	case model.OpCardRestore:
		return "card_restore"
	case model.OpCardMerge:
		return "card_merge"
	case model.OpTopicCreate:
		return "topic_create"
	case model.OpTopicUpdate:
		return "topic_update"
	case model.OpTopicTrash:
		return "topic_trash"
	case model.OpTopicRestore:
		return "topic_restore"
	case model.OpGlossaryCreate:
		return "glossary_create"
	case model.OpGlossaryUpdate:
		return "glossary_update"
	case model.OpGlossaryTrash:
		return "glossary_trash"
	case model.OpGlossaryRestore:
		return "glossary_restore"
	default:
		return "unknown"
	}
}

// ApprovalEntityTypeName 返回对象类型的对外字符串名.
func ApprovalEntityTypeName(entityType int16) string {
	switch entityType {
	case model.EntityTopic:
		return "topic"
	case model.EntityCard:
		return "card"
	case model.EntityGlossary:
		return "glossary"
	default:
		return "unknown"
	}
}

// ApprovalStatusName 返回审批状态的对外字符串名.
func ApprovalStatusName(status int16) string {
	switch status {
	case model.ApprovalPending:
		return "pending"
	case model.ApprovalApproved:
		return "approved"
	case model.ApprovalRejected:
		return "rejected"
	case model.ApprovalCancelled:
		return "cancelled"
	case model.ApprovalStale:
		return "stale"
	default:
		return "unknown"
	}
}

// ParseApprovalStatus 解析状态查询参数, 非法值返回 false.
func ParseApprovalStatus(name string) (int16, bool) {
	switch name {
	case "pending":
		return model.ApprovalPending, true
	case "approved":
		return model.ApprovalApproved, true
	case "rejected":
		return model.ApprovalRejected, true
	case "cancelled":
		return model.ApprovalCancelled, true
	case "stale":
		return model.ApprovalStale, true
	default:
		return 0, false
	}
}

// ApprovalSwitchKey 返回操作对应的审批开关设置 key.
func ApprovalSwitchKey(op int16) string {
	switch op {
	case model.OpCardCreate:
		return "enable_cli_card_create_approval"
	case model.OpCardUpdate:
		return "enable_cli_card_update_approval"
	case model.OpCardTrash:
		return "enable_cli_card_trash_approval"
	case model.OpCardRestore:
		return "enable_cli_card_restore_approval"
	case model.OpCardMerge:
		return "enable_cli_card_merge_approval"
	case model.OpTopicCreate:
		return "enable_cli_topic_create_approval"
	case model.OpTopicUpdate:
		return "enable_cli_topic_update_approval"
	case model.OpTopicTrash:
		return "enable_cli_topic_trash_approval"
	case model.OpTopicRestore:
		return "enable_cli_topic_restore_approval"
	case model.OpGlossaryCreate:
		return "enable_cli_glossary_create_approval"
	case model.OpGlossaryUpdate:
		return "enable_cli_glossary_update_approval"
	case model.OpGlossaryTrash:
		return "enable_cli_glossary_trash_approval"
	case model.OpGlossaryRestore:
		return "enable_cli_glossary_restore_approval"
	default:
		return ""
	}
}
