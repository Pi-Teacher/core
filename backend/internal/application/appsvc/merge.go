package appsvc

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/domain/card"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// CardMergeInput 是一次合并请求的已解码参数.
//
// Front/Back 为 nil 表示未显式传入, 按 Q1/Q2 规则拼接生成;
// TopicID 三态 (缺省继承 / 显式 null 无 Topic / 数字指定);
// EnableEmbedding 为 nil 表示未显式传入, 缺省按来源卡继承.
type CardMergeInput struct {
	SourceIDs       []int64
	Front           *string
	Back            *string
	TopicID         NullableInt64
	EnableEmbedding *bool
}

// Merge 合并两张来源卡为一张新卡. Web 直写与 CLI 直写走这里;
// 审批批准走 mergeInTx 复用调用方事务.
func (s *CardService) Merge(ctx context.Context, input CardMergeInput) (*CardDetail, error) {
	var detail *CardDetail
	err := persistence.RunInTx(ctx, s.db, func(innerCtx context.Context, tx *gorm.DB) error {
		var txErr error
		detail, txErr = s.mergeInTx(innerCtx, tx, input)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// resolvedMerge 是合并请求解析后的新卡属性, 供直写与提案校验共用.
type resolvedMerge struct {
	topicRef        *int64
	enableEmbedding bool
	front           string
	back            string
}

// resolveMerge 按合并规则解析新卡属性: Topic (继承/指定/冲突),
// enable_embedding (继承/指定/冲突) 与 front/back 内容 (缺省拼接 + 校验).
// 直写路径与提案校验都调用它, 保证两条路径的规则完全一致.
func (s *CardService) resolveMerge(
	ctx context.Context,
	tx *gorm.DB,
	input CardMergeInput,
	first, second *model.Card,
) (*resolvedMerge, error) {
	topicRef, err := s.mergeTopic(ctx, tx, input, first, second)
	if err != nil {
		return nil, err
	}
	enableEmbedding, err := mergeEnableEmbedding(input, first, second)
	if err != nil {
		return nil, err
	}
	front, back, err := mergeContent(input, first, second)
	if err != nil {
		return nil, err
	}
	return &resolvedMerge{
		topicRef:        topicRef,
		enableEmbedding: enableEmbedding,
		front:           front,
		back:            back,
	}, nil
}

// mergeInTx 在给定事务中执行合并: 校验两张来源卡, 计算新卡内容与属性,
// 建新卡与新调度, 回收两张来源卡, 并按自然日累计制卡数. 全程在一个事务.
func (s *CardService) mergeInTx(ctx context.Context, tx *gorm.DB, input CardMergeInput) (*CardDetail, error) {
	ids, err := validateMergeSourceIDs(input.SourceIDs)
	if err != nil {
		return nil, err
	}
	cards := s.cards.WithTx(tx)
	// 事务内读出两张来源卡当前状态与 version; 直写路径据此条件回收,
	// 读到即用, 读后被并发改动则条件删除未命中并整事务回滚.
	first, err := s.loadActiveCard(ctx, tx, ids[0])
	if err != nil {
		return nil, err
	}
	second, err := s.loadActiveCard(ctx, tx, ids[1])
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolveMerge(ctx, tx, input, first, second)
	if err != nil {
		return nil, err
	}

	now := s.now()
	merged := &model.Card{
		TopicID:          resolved.topicRef,
		Front:            resolved.front,
		Back:             resolved.back,
		EnableEmbedding:  resolved.enableEmbedding,
		FrontFingerprint: card.FrontFingerprint(resolved.front),
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if resolved.enableEmbedding {
		status := model.EmbeddingPending
		merged.EmbeddingStatus = &status
	}
	if err := cards.Create(ctx, merged); err != nil {
		return nil, err
	}
	newSched := s.newSchedule(merged.ID, now)
	if err := cards.CreateSchedule(ctx, newSched); err != nil {
		return nil, err
	}
	// 两张来源卡按统一回收规则放入回收站, 顺序固定为 source_1 后 source_2.
	if err := trashCardTx(ctx, tx, cards, s.stale, first, now); err != nil {
		return nil, err
	}
	if err := trashCardTx(ctx, tx, cards, s.stale, second, now); err != nil {
		return nil, err
	}
	day := calendarDay(now, s.timezone())
	if err := s.calendar.WithTx(tx).AddCreatedCards(ctx, day, 1, now); err != nil {
		return nil, err
	}
	return &CardDetail{Card: merged, Schedule: newSched}, nil
}

// validateMergeSourceIDs 校验来源卡 ID: 恰好两个, 正数且互不相同.
func validateMergeSourceIDs(ids []int64) ([2]int64, error) {
	if len(ids) != 2 {
		return [2]int64{}, apperr.Validation("source_card_ids 必须恰好包含两个 ID").
			WithDetails(map[string]any{"field": "source_card_ids"})
	}
	if ids[0] <= 0 || ids[1] <= 0 {
		return [2]int64{}, apperr.Validation("source_card_ids 必须是正整数").
			WithDetails(map[string]any{"field": "source_card_ids"})
	}
	if ids[0] == ids[1] {
		return [2]int64{}, apperr.Validation("source_card_ids 不能重复").
			WithDetails(map[string]any{"field": "source_card_ids"})
	}
	return [2]int64{ids[0], ids[1]}, nil
}

// loadActiveCard 读取一张正常来源卡, 不存在按 404 返回.
func (s *CardService) loadActiveCard(ctx context.Context, tx *gorm.DB, id int64) (*model.Card, error) {
	c, err := s.cards.WithTx(tx).Find(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("来源 Card 不存在")
		}
		return nil, err
	}
	return c, nil
}

// mergeTopic 决定新卡的 Topic. 显式传入以显式值为准 (null 表示无 Topic,
// 数字需校验存在); 缺省时两来源 Topic 相同则继承, 不同则报 409.
func (s *CardService) mergeTopic(
	ctx context.Context,
	tx *gorm.DB,
	input CardMergeInput,
	first, second *model.Card,
) (*int64, error) {
	if input.TopicID.Set {
		if input.TopicID.Value == nil {
			return nil, nil
		}
		if _, err := s.topics.WithTx(tx).FindActive(ctx, *input.TopicID.Value); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("Topic 不存在")
			}
			return nil, err
		}
		return input.TopicID.Value, nil
	}
	if sameInt64Ptr(first.TopicID, second.TopicID) {
		return first.TopicID, nil
	}
	return nil, apperr.New(apperr.CodeMergeTopicRequired,
		"两张来源卡的 Topic 不同, 必须显式指定 topic_id 或传 null")
}

// mergeEnableEmbedding 决定新卡是否启用 embedding. 显式传入以显式值为准;
// 缺省时两来源相同则继承, 不同则报 409.
func mergeEnableEmbedding(input CardMergeInput, first, second *model.Card) (bool, error) {
	if input.EnableEmbedding != nil {
		return *input.EnableEmbedding, nil
	}
	if first.EnableEmbedding == second.EnableEmbedding {
		return first.EnableEmbedding, nil
	}
	return false, apperr.New(apperr.CodeMergeEmbeddingReqd,
		"两张来源卡的 enable_embedding 不同, 必须显式指定")
}

// mergeContent 计算新卡的 front 与 back. 未显式传入的字段按
// Q1/Q2 规则由两张来源卡拼接, 生成结果同样经过内容校验.
func mergeContent(input CardMergeInput, first, second *model.Card) (string, string, error) {
	front := derefOr(input.Front, func() string { return mergeField(first.Front, second.Front) })
	back := derefOr(input.Back, func() string { return mergeField(first.Back, second.Back) })
	vv, err := validateContent("front", front)
	if err != nil {
		return "", "", err
	}
	bv, err := validateContent("back", back)
	if err != nil {
		return "", "", err
	}
	return vv, bv, nil
}

// mergeField 按 Q1/Q2 规则拼接来源卡的单个字段.
func mergeField(left, right string) string {
	var b strings.Builder
	b.WriteString("Q1: ")
	b.WriteString(left)
	b.WriteString("\nQ2: ")
	b.WriteString(right)
	return b.String()
}

// derefOr 在指针非空时返回其值, 否则返回 fallback 的结果.
func derefOr(p *string, fallback func() string) string {
	if p != nil {
		return *p
	}
	return fallback()
}
