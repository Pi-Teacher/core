// Package model 定义全部 16 张表的 GORM 持久化模型.
//
// 约定:
//   - 不建数据库外键和触发器, 关联完整性由服务层在事务中维护;
//   - 时间一律由 Go 生成 UTC 后写入, 不依赖数据库默认值;
//   - 可修改对象携带显式 version 供乐观锁使用, 只增不改的表不带;
//   - 二进制字段由各方言迁移文件建表 (SQLite BLOB, MySQL LONGBLOB,
//     PostgreSQL BYTEA), 模型统一用 []byte 承载.
package model

import "time"

// setting_keys.value_type 的取值.
const (
	ValueTypeString  int16 = 1
	ValueTypeBool    int16 = 2
	ValueTypeInt64   int16 = 3
	ValueTypeFloat64 int16 = 4
	ValueTypeJSON    int16 = 5
)

// 调度与复习状态值, 与 go-fsrs/v4 的 fsrs.State 对应.
const (
	StateNew        int16 = 0
	StateLearning   int16 = 1
	StateReview     int16 = 2
	StateRelearning int16 = 3
)

// Card 的 embedding 任务状态.
const (
	EmbeddingPending    int16 = 0
	EmbeddingProcessing int16 = 1
	EmbeddingReady      int16 = 2
	EmbeddingFailed     int16 = 3
)

// 复习评分值.
const (
	RatingAgain int16 = 1
	RatingHard  int16 = 2
	RatingGood  int16 = 3
	RatingEasy  int16 = 4
)

// 审批请求状态.
const (
	ApprovalPending   int16 = 0
	ApprovalApproved  int16 = 1
	ApprovalRejected  int16 = 2
	ApprovalCancelled int16 = 3
	ApprovalStale     int16 = 4
)

// 审批操作类型.
const (
	OpCardCreate      int16 = 1
	OpCardUpdate      int16 = 2
	OpCardTrash       int16 = 3
	OpCardRestore     int16 = 4
	OpCardMerge       int16 = 5
	OpTopicCreate     int16 = 10
	OpTopicUpdate     int16 = 11
	OpTopicTrash      int16 = 12
	OpTopicRestore    int16 = 13
	OpGlossaryCreate  int16 = 20
	OpGlossaryUpdate  int16 = 21
	OpGlossaryTrash   int16 = 22
	OpGlossaryRestore int16 = 23
)

// 审批目标与日志使用的对象类型.
const (
	EntityTopic    int16 = 1
	EntityCard     int16 = 2
	EntityGlossary int16 = 3
)

// 幂等记录状态.
const (
	IdempotencyProcessing int16 = 0
	IdempotencyCompleted  int16 = 1
)

// app_log 日志级别.
const (
	LogDebug int16 = 0
	LogInfo  int16 = 1
	LogWarn  int16 = 2
	LogError int16 = 3
)

// app_log 来源.
const (
	SourceWeb    int16 = 1
	SourceCLI    int16 = 2
	SourceWorker int16 = 3
	SourceAdmin  int16 = 4
	SourceSystem int16 = 5
)

// Account 是单用户账号, 整个实例只有一行.
type Account struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	PasswordHash string    `gorm:"column:password_hash;type:text;not null"`
	Version      int64     `gorm:"column:version;not null;default:1"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (Account) TableName() string { return "account" }

// WebSession 是一条登录 session, 与一个 CSRF token 绑定.
// token 与 CSRF 都只存 SHA-256, 明文只在创建时返回给客户端.
type WebSession struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	AccountID     int64     `gorm:"column:account_id;not null;index:idx_web_session_account_id"`
	TokenHash     string    `gorm:"column:token_hash;type:varchar(64);not null;uniqueIndex:uq_web_session_token_hash"`
	CSRFTokenHash string    `gorm:"column:csrf_token_hash;type:varchar(64);not null"`
	CreatedAt     time.Time `gorm:"column:created_at;not null"`
	ExpiresAt     time.Time `gorm:"column:expires_at;not null;index:idx_web_session_expires_at"`
}

func (WebSession) TableName() string { return "web_session" }

// APIKey 是 CLI 凭据. 按既定决策明文存储, 物理删除该行即吊销.
type APIKey struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	AccountID int64     `gorm:"column:account_id;not null;index:idx_api_key_account_created,priority:1"`
	Name      string    `gorm:"column:name;type:varchar(200);not null"`
	APIKey    string    `gorm:"column:api_key;type:varchar(128);not null;uniqueIndex:uq_api_key_api_key"`
	Version   int64     `gorm:"column:version;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index:idx_api_key_account_created,priority:2"`
}

func (APIKey) TableName() string { return "api_key" }

// SettingKey 是实例级类型化 KV 设置, 数据库为权威来源, 启动后加载进内存快照.
type SettingKey struct {
	SettingKey   string    `gorm:"column:setting_key;type:varchar(200);primaryKey"`
	SettingValue string    `gorm:"column:setting_value;type:text;not null"`
	ValueType    int16     `gorm:"column:value_type;not null"`
	Version      int64     `gorm:"column:version;not null;default:1"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (SettingKey) TableName() string { return "setting_keys" }

// Topic 是 Card 的分类. trashed_at 为空表示正常对象, 非空表示在回收站.
type Topic struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Name        string     `gorm:"column:name;type:varchar(200);not null;index:idx_topic_name"`
	Description string     `gorm:"column:description;type:text;not null"`
	Version     int64      `gorm:"column:version;not null;default:1"`
	TrashedAt   *time.Time `gorm:"column:trashed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

func (Topic) TableName() string { return "topic" }

// Card 只保存当前有效卡片, 回收站内容在 trashed_card 表.
// embedding 任务状态直接存在本表, 不建独立任务表.
type Card struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TopicID          *int64    `gorm:"column:topic_id;index:idx_card_topic_updated,priority:1"`
	Front            string    `gorm:"column:front;type:text;not null"`
	Back             string    `gorm:"column:back;type:text;not null"`
	EnableEmbedding  bool      `gorm:"column:enable_embedding;not null"`
	FrontFingerprint []byte    `gorm:"column:front_fingerprint;not null;index:idx_card_fingerprint_enabled,priority:1"`
	Version          int64     `gorm:"column:version;not null;default:1"`
	Embedding        []byte    `gorm:"column:embedding"`
	EmbeddingStatus  *int16    `gorm:"column:embedding_status;index:idx_card_embedding_status,priority:1"`
	EmbeddingError   *string   `gorm:"column:embedding_error;type:text"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;index:idx_card_created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at;not null;index:idx_card_topic_updated,priority:2"`
}

func (Card) TableName() string { return "card" }

// TrashedCard 是回收站卡片, 只保留恢复所需的正反面内容与原元数据,
// 不带 Topic, 调度, 复习日志和向量; 恢复时生成全新 Card ID.
type TrashedCard struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Front           string    `gorm:"column:front;type:text;not null"`
	Back            string    `gorm:"column:back;type:text;not null"`
	EnableEmbedding bool      `gorm:"column:enable_embedding;not null"`
	Version         int64     `gorm:"column:version;not null;default:1"`
	CreatedAt       time.Time `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null"`
	TrashedAt       time.Time `gorm:"column:trashed_at;not null;index:idx_trashed_card_trashed_id,priority:1"`
}

func (TrashedCard) TableName() string { return "trashed_card" }

// CardSchedule 是每张正常卡片唯一的 FSRS 调度快照.
// 复习事务同时校验 Card 与 Schedule 两个版本.
type CardSchedule struct {
	CardID         int64      `gorm:"column:card_id;primaryKey"`
	Due            time.Time  `gorm:"column:due;not null;index:idx_card_schedule_due"`
	Stability      float64    `gorm:"column:stability;not null"`
	Difficulty     float64    `gorm:"column:difficulty;not null"`
	ScheduledDays  int64      `gorm:"column:scheduled_days;not null"`
	Reps           int64      `gorm:"column:reps;not null"`
	Lapses         int64      `gorm:"column:lapses;not null"`
	State          int16      `gorm:"column:state;not null;index:idx_card_schedule_state_due,priority:1"`
	LastReviewAt   *time.Time `gorm:"column:last_review_at"`
	RemainingSteps int64      `gorm:"column:remaining_steps;not null"`
	Version        int64      `gorm:"column:version;not null;default:1"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null;index:idx_card_schedule_state_due,priority:2"`
}

func (CardSchedule) TableName() string { return "card_schedule" }

// ReviewLog 是只增不改的复习事件历史, 保存 FSRS 需要的核心快照.
type ReviewLog struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CardID         int64     `gorm:"column:card_id;not null;index:idx_review_log_card_reviewed,priority:1"`
	Rating         int16     `gorm:"column:rating;not null"`
	Due            time.Time `gorm:"column:due;not null"`
	ScheduledDays  int64     `gorm:"column:scheduled_days;not null"`
	ReviewedAt     time.Time `gorm:"column:reviewed_at;not null;index:idx_review_log_card_reviewed,priority:2;index:idx_review_log_reviewed_at"`
	State          int16     `gorm:"column:state;not null"`
	Stability      float64   `gorm:"column:stability;not null"`
	Difficulty     float64   `gorm:"column:difficulty;not null"`
	RemainingSteps int64     `gorm:"column:remaining_steps;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (ReviewLog) TableName() string { return "review_log" }

// Calendar 是按自然日累计的鼓励统计, 独立历史计数, 不随对象删除回退.
// 非核心统计, 不用乐观锁, 增量走原子 UPSERT.
type Calendar struct {
	ActivityDate time.Time `gorm:"column:activity_date;type:date;primaryKey"`
	CreatedCards int64     `gorm:"column:created_cards;not null;default:0"`
	ReviewEvents int64     `gorm:"column:review_events;not null;default:0"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (Calendar) TableName() string { return "calendar" }

// Glossary 是用户已理解术语, 不关联 Topic, 不生成 embedding.
type Glossary struct {
	ID         int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Term       string     `gorm:"column:term;type:varchar(200);not null;index:idx_glossary_term"`
	Definition string     `gorm:"column:definition;type:text;not null"`
	Version    int64      `gorm:"column:version;not null;default:1"`
	TrashedAt  *time.Time `gorm:"column:trashed_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null"`
}

func (Glossary) TableName() string { return "glossary" }

// ApprovalRequest 是 CLI 发起的待审批变更. 待审批数据不进正式领域表,
// 原始与最终 payload 永久保留.
type ApprovalRequest struct {
	ID                  int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Operation           int16      `gorm:"column:operation;not null"`
	EntityType          int16      `gorm:"column:entity_type;not null"`
	Status              int16      `gorm:"column:status;not null;index:idx_approval_request_status_created,priority:1"`
	RequestedByAPIKeyID *int64     `gorm:"column:requested_by_api_key_id;index:idx_approval_request_api_key_created,priority:1"`
	OriginalPayload     string     `gorm:"column:original_payload;type:text;not null"`
	ApprovedPayload     *string    `gorm:"column:approved_payload;type:text"`
	Reason              *string    `gorm:"column:reason;type:text"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null;index:idx_approval_request_status_created,priority:2"`
	ProcessedAt         *time.Time `gorm:"column:processed_at"`
}

func (ApprovalRequest) TableName() string { return "approval_request" }

// ApprovalTarget 记录审批请求依赖的对象及提案时的版本快照.
// 目标对象可能已被永久删除, 记录仍保留其类型与 ID.
type ApprovalTarget struct {
	ID                int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ApprovalRequestID int64     `gorm:"column:approval_request_id;not null;index:idx_approval_target_request,priority:1"`
	EntityType        int16     `gorm:"column:entity_type;not null"`
	EntityID          int64     `gorm:"column:entity_id;not null"`
	BaseVersion       int64     `gorm:"column:base_version;not null"`
	Role              string    `gorm:"column:role;type:varchar(50);not null"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	// (approval_request_id, entity_type, entity_id, role) 的唯一性由迁移中的
	// 唯一索引保证, 模型只需要查询用的普通索引.
}

func (ApprovalTarget) TableName() string { return "approval_target" }

// IdempotencyRecord 保存可重放的 CLI 写请求结果.
// 同 Key 同请求返回首次结果, 同 Key 不同请求返回冲突.
type IdempotencyRecord struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	APIKeyID       int64     `gorm:"column:api_key_id;not null"`
	IdempotencyKey string    `gorm:"column:idempotency_key;type:varchar(200);not null"`
	RequestMethod  string    `gorm:"column:request_method;type:varchar(10);not null"`
	RequestPath    string    `gorm:"column:request_path;type:varchar(500);not null"`
	RequestHash    string    `gorm:"column:request_hash;type:varchar(64);not null"`
	ResponseStatus *int64    `gorm:"column:response_status"`
	ResponseBody   *string   `gorm:"column:response_body;type:text"`
	State          int16     `gorm:"column:state;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
	ExpiresAt      time.Time `gorm:"column:expires_at;not null;index:idx_idempotency_expires_at"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_record" }

// AppLog 是可选的轻量数据库日志, 只在开启数据库日志时写入关键事件,
// 不是完整审计账本, 允许按保留策略清理.
type AppLog struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	LoggedAt   time.Time `gorm:"column:logged_at;not null;index:idx_app_log_logged_id,priority:1"`
	Level      int16     `gorm:"column:level;not null;index:idx_app_log_level_logged,priority:1"`
	Event      string    `gorm:"column:event;type:varchar(100);not null;index:idx_app_log_event_logged,priority:1"`
	Message    string    `gorm:"column:message;type:text;not null"`
	RequestID  *string   `gorm:"column:request_id;type:varchar(100);index:idx_app_log_request_id"`
	Source     *int16    `gorm:"column:source"`
	EntityType *int16    `gorm:"column:entity_type"`
	EntityID   *int64    `gorm:"column:entity_id"`
	Details    *string   `gorm:"column:details;type:text"`
}

func (AppLog) TableName() string { return "app_log" }

// Models 返回全部模型, 供测试和工具遍历.
func Models() []any {
	return []any{
		&Account{}, &WebSession{}, &APIKey{}, &SettingKey{},
		&Topic{}, &Card{}, &TrashedCard{}, &CardSchedule{}, &ReviewLog{},
		&Calendar{}, &Glossary{},
		&ApprovalRequest{}, &ApprovalTarget{}, &IdempotencyRecord{}, &AppLog{},
	}
}
