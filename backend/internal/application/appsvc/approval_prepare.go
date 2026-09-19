package appsvc

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// prepare 在提案事务中校验原始 payload 并解析全部依赖目标.
//
// 校验与解析完全对齐直写路径: 字段规则复用同一批 validate 辅助函数,
// 乐观锁在提案时就与当前版本对齐, 避免生成一提交就注定 stale 的请求.
// 返回的 targets 尚未绑定 approval_request_id, 由 createOne 补齐.
func (s *ApprovalService) prepare(
	ctx context.Context,
	tx *gorm.DB,
	spec ProposalSpec,
) ([]model.ApprovalTarget, error) {
	switch spec.Operation {
	case model.OpTopicCreate:
		var p TopicCreatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if _, err := validateIdentifier("name", p.Name); err != nil {
			return nil, err
		}
		if _, err := validateOptionalText("description", p.Description); err != nil {
			return nil, err
		}
		return nil, nil

	case model.OpTopicUpdate:
		var p TopicUpdatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if p.Name == nil && p.Description == nil {
			return nil, apperr.Validation("至少提供一个要修改的字段")
		}
		if p.Name != nil {
			if _, err := validateIdentifier("name", *p.Name); err != nil {
				return nil, err
			}
		}
		if p.Description != nil {
			if _, err := validateOptionalText("description", *p.Description); err != nil {
				return nil, err
			}
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		topic, err := s.activeTarget(ctx, tx, model.EntityTopic, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		return []model.ApprovalTarget{topic}, nil

	case model.OpTopicTrash:
		var p TopicTrashPayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		target, err := s.activeTarget(ctx, tx, model.EntityTopic, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		targets := []model.ApprovalTarget{target}
		if p.Include() {
			// 连带回收会改变每张关联卡, 全部登记为 affected_card:
			// 任一卡在批准前被改动, 整条请求按文档语义转 stale.
			linked, err := s.cards.WithTx(tx).ListActiveByTopic(ctx, *spec.EntityID)
			if err != nil {
				return nil, err
			}
			for i := range linked {
				targets = append(targets, model.ApprovalTarget{
					EntityType:  model.EntityCard,
					EntityID:    linked[i].ID,
					BaseVersion: linked[i].Version,
					Role:        roleAffectedCard,
				})
			}
		}
		return targets, nil

	case model.OpTopicRestore:
		var p TopicRestorePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		trashed, err := s.topics.WithTx(tx).FindTrashed(ctx, *spec.EntityID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("回收站中不存在该 Topic")
			}
			return nil, err
		}
		if trashed.Version != expectedVersion {
			return nil, versionConflict("回收站 Topic", trashed.Version)
		}
		return []model.ApprovalTarget{{
			EntityType: model.EntityTopic, EntityID: trashed.ID,
			BaseVersion: trashed.Version, Role: roleTarget,
		}}, nil

	case model.OpCardCreate:
		var p CardCreatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if _, err := validateContent("front", p.Front); err != nil {
			return nil, err
		}
		if _, err := validateContent("back", p.Back); err != nil {
			return nil, err
		}
		return s.topicReference(ctx, tx, p.TopicID)

	case model.OpCardUpdate:
		var p CardUpdatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if !p.TopicID.Set && p.Front == nil && p.Back == nil && p.EnableEmbedding == nil {
			return nil, apperr.Validation("至少提供一个要修改的字段")
		}
		if p.Front != nil {
			if _, err := validateContent("front", *p.Front); err != nil {
				return nil, err
			}
		}
		if p.Back != nil {
			if _, err := validateContent("back", *p.Back); err != nil {
				return nil, err
			}
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		target, err := s.activeTarget(ctx, tx, model.EntityCard, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		targets := []model.ApprovalTarget{target}
		if p.TopicID.Set && p.TopicID.Value != nil {
			topicRefs, err := s.topicReference(ctx, tx, p.TopicID.Value)
			if err != nil {
				return nil, err
			}
			targets = append(targets, topicRefs...)
		}
		return targets, nil

	case model.OpCardTrash:
		var p CardTrashPayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		target, err := s.activeTarget(ctx, tx, model.EntityCard, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		return []model.ApprovalTarget{target}, nil

	case model.OpCardRestore:
		var p CardRestorePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		trashed, err := s.cards.WithTx(tx).FindTrashed(ctx, *spec.EntityID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("回收站中不存在该 Card")
			}
			return nil, err
		}
		if trashed.Version != expectedVersion {
			return nil, versionConflict("回收站 Card", trashed.Version)
		}
		targets := []model.ApprovalTarget{{
			EntityType: model.EntityCard, EntityID: trashed.ID,
			BaseVersion: trashed.Version, Role: roleTarget,
		}}
		topicRefs, err := s.topicReference(ctx, tx, p.TopicID)
		if err != nil {
			return nil, err
		}
		return append(targets, topicRefs...), nil

	case model.OpCardMerge:
		var p CardMergePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		// 提案时就把来源卡当前 version 快照为 base_version, 批准时严格校验;
		// 任一张来源卡缺失/进回收站/被改动都会让整条请求 stale.
		ids, err := validateMergeSourceIDs(p.SourceCardIDs)
		if err != nil {
			return nil, err
		}
		first, err := s.cards.WithTx(tx).Find(ctx, ids[0])
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("来源 Card 不存在")
			}
			return nil, err
		}
		second, err := s.cards.WithTx(tx).Find(ctx, ids[1])
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("来源 Card 不存在")
			}
			return nil, err
		}
		// 用与直写相同的规则预解析: Topic 冲突与 embedding 冲突在提案时就
		// 报错, 不让用户提交一条注定无法批准的请求.
		if _, err := s.cardSvc.resolveMerge(ctx, tx, p.ToInput(), first, second); err != nil {
			return nil, err
		}
		targets := []model.ApprovalTarget{
			{EntityType: model.EntityCard, EntityID: first.ID, BaseVersion: first.Version, Role: roleSource1},
			{EntityType: model.EntityCard, EntityID: second.ID, BaseVersion: second.Version, Role: roleSource2},
		}
		// payload 显式引用的 Topic 登记为 role=topic (不校版本).
		topicRefs, err := s.topicReference(ctx, tx, p.TopicID.Value)
		if err != nil {
			return nil, err
		}
		return append(targets, topicRefs...), nil

	case model.OpGlossaryCreate:
		var p GlossaryCreatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if _, err := validateIdentifier("term", p.Term); err != nil {
			return nil, err
		}
		if _, err := validateContent("definition", p.Definition); err != nil {
			return nil, err
		}
		return nil, nil

	case model.OpGlossaryUpdate:
		var p GlossaryUpdatePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		if p.Term == nil && p.Definition == nil {
			return nil, apperr.Validation("至少提供一个要修改的字段")
		}
		if p.Term != nil {
			if _, err := validateIdentifier("term", *p.Term); err != nil {
				return nil, err
			}
		}
		if p.Definition != nil {
			if _, err := validateContent("definition", *p.Definition); err != nil {
				return nil, err
			}
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		target, err := s.activeTarget(ctx, tx, model.EntityGlossary, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		return []model.ApprovalTarget{target}, nil

	case model.OpGlossaryTrash:
		var p GlossaryTrashPayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		target, err := s.activeTarget(ctx, tx, model.EntityGlossary, *spec.EntityID, expectedVersion)
		if err != nil {
			return nil, err
		}
		return []model.ApprovalTarget{target}, nil

	case model.OpGlossaryRestore:
		var p GlossaryRestorePayload
		if err := decodeProposalPayload(string(spec.Payload), &p); err != nil {
			return nil, err
		}
		expectedVersion, err := requirePayloadVersion(p.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		trashed, err := s.glossaries.WithTx(tx).FindTrashed(ctx, *spec.EntityID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("回收站中不存在该 Glossary")
			}
			return nil, err
		}
		if trashed.Version != expectedVersion {
			return nil, versionConflict("回收站 Glossary", trashed.Version)
		}
		return []model.ApprovalTarget{{
			EntityType: model.EntityGlossary, EntityID: trashed.ID,
			BaseVersion: trashed.Version, Role: roleTarget,
		}}, nil

	default:
		return nil, apperr.Newf(apperr.CodeValidationError, "该操作不支持审批提案: %d", spec.Operation)
	}
}

// activeTarget 校验主对象当前正常存在且版本与提案一致, 返回 target.
func (s *ApprovalService) activeTarget(
	ctx context.Context,
	tx *gorm.DB,
	entityType int16,
	id, expectedVersion int64,
) (model.ApprovalTarget, error) {
	switch entityType {
	case model.EntityTopic:
		t, err := s.topics.WithTx(tx).FindActive(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.ApprovalTarget{}, apperr.NotFound("Topic 不存在")
			}
			return model.ApprovalTarget{}, err
		}
		if t.Version != expectedVersion {
			return model.ApprovalTarget{}, versionConflict("Topic", t.Version)
		}
		return model.ApprovalTarget{
			EntityType: model.EntityTopic, EntityID: t.ID,
			BaseVersion: t.Version, Role: roleTarget,
		}, nil
	case model.EntityCard:
		c, err := s.cards.WithTx(tx).Find(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.ApprovalTarget{}, apperr.NotFound("Card 不存在")
			}
			return model.ApprovalTarget{}, err
		}
		if c.Version != expectedVersion {
			return model.ApprovalTarget{}, versionConflict("Card", c.Version)
		}
		return model.ApprovalTarget{
			EntityType: model.EntityCard, EntityID: c.ID,
			BaseVersion: c.Version, Role: roleTarget,
		}, nil
	case model.EntityGlossary:
		g, err := s.glossaries.WithTx(tx).FindActive(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.ApprovalTarget{}, apperr.NotFound("Glossary 不存在")
			}
			return model.ApprovalTarget{}, err
		}
		if g.Version != expectedVersion {
			return model.ApprovalTarget{}, versionConflict("Glossary", g.Version)
		}
		return model.ApprovalTarget{
			EntityType: model.EntityGlossary, EntityID: g.ID,
			BaseVersion: g.Version, Role: roleTarget,
		}, nil
	default:
		return model.ApprovalTarget{}, apperr.Newf(apperr.CodeInternal, "未知对象类型 %d", entityType)
	}
}

// topicReference 在 payload 显式引用 Topic 时把它登记为 role=topic 目标.
// topicID 为 nil 表示无引用, 返回空目标. 版本快照仅作展示参考,
// 批准时对 role=topic 不校验版本 (见数据库设计 4.14 批注).
func (s *ApprovalService) topicReference(
	ctx context.Context,
	tx *gorm.DB,
	topicID *int64,
) ([]model.ApprovalTarget, error) {
	if topicID == nil {
		return nil, nil
	}
	t, err := s.topics.WithTx(tx).FindActive(ctx, *topicID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Topic 不存在")
		}
		return nil, err
	}
	return []model.ApprovalTarget{{
		EntityType: model.EntityTopic, EntityID: t.ID,
		BaseVersion: t.Version, Role: roleTopic,
	}}, nil
}

// requirePayloadVersion 提取 payload 中必填的 expected_version.
func requirePayloadVersion(v *int64) (int64, error) {
	if v == nil {
		return 0, apperr.Validation("缺少 expected_version").
			WithDetails(map[string]any{"field": "expected_version"})
	}
	return *v, nil
}
