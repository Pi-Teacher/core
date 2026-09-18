package appsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// GlossaryService 编排 Glossary 的创建、修改、回收、恢复与永久删除.
// 与 Topic 同构但更简单: 不关联 Card, 无 include_cards 与 preview.
type GlossaryService struct {
	db         *gorm.DB
	glossaries *repo.GlossaryRepository
	logger     *slog.Logger
	// now 集中提供当前 UTC 时间, 便于测试替换.
	now func() time.Time
}

// NewGlossaryService 构造 Glossary 服务.
func NewGlossaryService(
	db *gorm.DB,
	glossaries *repo.GlossaryRepository,
	logger *slog.Logger,
) *GlossaryService {
	return &GlossaryService{
		db:         db,
		glossaries: glossaries,
		logger:     logger,
		now:        func() time.Time { return persistence.Now() },
	}
}

// GlossaryInput 是创建 Glossary 的已解码参数.
type GlossaryInput struct {
	Term       string
	Definition string
}

// Create 创建 Glossary. 同名回收站记录在同一事务中被物理删除.
func (s *GlossaryService) Create(ctx context.Context, input GlossaryInput) (*model.Glossary, error) {
	term, err := validateIdentifier("term", input.Term)
	if err != nil {
		return nil, err
	}
	definition, err := validateContent("definition", input.Definition)
	if err != nil {
		return nil, err
	}
	var created *model.Glossary
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		created, txErr = s.createInTx(ctx, tx, term, definition)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// createInTx 在给定事务中执行创建: 正常同名冲突返回 name_conflict,
// 回收站同名记录物理删除后新建.
func (s *GlossaryService) createInTx(ctx context.Context, tx *gorm.DB, term, definition string) (*model.Glossary, error) {
	glossaries := s.glossaries.WithTx(tx)
	if exists, err := glossaries.ExistsActiveByTerm(ctx, term, 0); err != nil {
		return nil, err
	} else if exists {
		return nil, apperr.Conflict(apperr.CodeNameConflict, "同名 Glossary 已存在")
	}
	if err := glossaries.DeleteTrashedByTerm(ctx, term); err != nil {
		return nil, err
	}
	now := s.now()
	g := &model.Glossary{
		Term:       term,
		Definition: definition,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := glossaries.Create(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// BatchCreate 在单个事务中整批创建 Glossary.
func (s *GlossaryService) BatchCreate(ctx context.Context, inputs []GlossaryInput) ([]model.Glossary, error) {
	type validated struct {
		term       string
		definition string
	}
	items := make([]validated, len(inputs))
	for i, in := range inputs {
		term, err := validateIdentifier("term", in.Term)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		definition, err := validateContent("definition", in.Definition)
		if err != nil {
			return nil, withBatchIndex(err, i)
		}
		items[i] = validated{term: term, definition: definition}
	}
	created := make([]model.Glossary, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item validated) error {
		g, err := s.createInTx(ctx, tx, item.term, item.definition)
		if err != nil {
			return err
		}
		created = append(created, *g)
		return nil
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// Get 返回正常 Glossary 详情.
func (s *GlossaryService) Get(ctx context.Context, id int64) (*model.Glossary, error) {
	g, err := s.glossaries.FindActive(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Glossary 不存在")
		}
		return nil, err
	}
	return g, nil
}

// List 返回正常 Glossary 分页, q 为术语子串搜索.
func (s *GlossaryService) List(ctx context.Context, q string, page, pageSize int) ([]model.Glossary, int64, error) {
	offset := (page - 1) * pageSize
	return s.glossaries.ListActive(ctx, q, offset, pageSize)
}

// GlossaryPatch 描述 PATCH 的可选修改, nil 表示不修改该字段.
type GlossaryPatch struct {
	Term       *string
	Definition *string
}

// Update 修改正常 Glossary, 改名时维护术语唯一.
// 所有字段值都未变化时不递增 version.
func (s *GlossaryService) Update(ctx context.Context, id, expectedVersion int64, patch GlossaryPatch) (*model.Glossary, error) {
	if patch.Term == nil && patch.Definition == nil {
		return nil, apperr.Validation("至少提供一个要修改的字段")
	}
	var term *string
	if patch.Term != nil {
		v, err := validateIdentifier("term", *patch.Term)
		if err != nil {
			return nil, err
		}
		term = &v
	}
	var definition *string
	if patch.Definition != nil {
		v, err := validateContent("definition", *patch.Definition)
		if err != nil {
			return nil, err
		}
		definition = &v
	}
	var updated *model.Glossary
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		glossaries := s.glossaries.WithTx(tx)
		g, err := glossaries.FindActive(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Glossary 不存在")
			}
			return err
		}
		fields := map[string]any{}
		if term != nil && *term != g.Term {
			if exists, err := glossaries.ExistsActiveByTerm(ctx, *term, id); err != nil {
				return err
			} else if exists {
				return apperr.Conflict(apperr.CodeNameConflict, "同名 Glossary 已存在")
			}
			if err := glossaries.DeleteTrashedByTerm(ctx, *term); err != nil {
				return err
			}
			fields["term"] = *term
		}
		if definition != nil && *definition != g.Definition {
			fields["definition"] = *definition
		}
		if len(fields) == 0 {
			updated = g
			return nil
		}
		if err := persistence.UpdateOptimistic(tx, &model.Glossary{}, id, expectedVersion, fields); err != nil {
			if errors.Is(err, persistence.ErrVersionConflict) {
				return versionConflict("Glossary", g.Version)
			}
			return err
		}
		updated, err = glossaries.FindActive(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Trash 把正常 Glossary 放入回收站.
func (s *GlossaryService) Trash(ctx context.Context, id, expectedVersion int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.trashInTx(ctx, tx, id, expectedVersion)
	})
}

// trashInTx 是 Trash 的事务内实现, 供单个与批量回收共用.
func (s *GlossaryService) trashInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) error {
	glossaries := s.glossaries.WithTx(tx)
	g, err := glossaries.FindActive(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("Glossary 不存在")
		}
		return err
	}
	if err := persistence.UpdateOptimistic(tx, &model.Glossary{}, id, expectedVersion, map[string]any{
		"trashed_at": s.now(),
	}); err != nil {
		if errors.Is(err, persistence.ErrVersionConflict) {
			return versionConflict("Glossary", g.Version)
		}
		return err
	}
	return nil
}

// BatchTrash 在单个事务中整批回收 Glossary.
func (s *GlossaryService) BatchTrash(ctx context.Context, items []VersionedItem) (int64, error) {
	var count int64
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		if err := s.trashInTx(ctx, tx, item.ID, item.ExpectedVersion); err != nil {
			return err
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

// ListTrashed 返回回收站 Glossary 分页.
func (s *GlossaryService) ListTrashed(ctx context.Context, page, pageSize int) ([]model.Glossary, int64, error) {
	offset := (page - 1) * pageSize
	return s.glossaries.ListTrashed(ctx, offset, pageSize)
}

// Restore 把回收站 Glossary 恢复为正常对象, 同名冲突返回 name_conflict.
func (s *GlossaryService) Restore(ctx context.Context, id, expectedVersion int64) (*model.Glossary, error) {
	var restored *model.Glossary
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		restored, txErr = s.restoreInTx(ctx, tx, id, expectedVersion)
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return restored, nil
}

// restoreInTx 是 Restore 的事务内实现, 供单个与批量恢复共用.
func (s *GlossaryService) restoreInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) (*model.Glossary, error) {
	glossaries := s.glossaries.WithTx(tx)
	g, err := glossaries.FindTrashed(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("回收站中不存在该 Glossary")
		}
		return nil, err
	}
	if g.Version != expectedVersion {
		return nil, versionConflict("Glossary", g.Version)
	}
	if exists, err := glossaries.ExistsActiveByTerm(ctx, g.Term, 0); err != nil {
		return nil, err
	} else if exists {
		return nil, apperr.Conflict(apperr.CodeNameConflict, "同名 Glossary 已存在")
	}
	ok, err := glossaries.Restore(ctx, id, expectedVersion, s.now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, versionConflict("Glossary", g.Version)
	}
	return glossaries.FindActive(ctx, id)
}

// BatchRestore 在单个事务中整批恢复回收站 Glossary.
func (s *GlossaryService) BatchRestore(ctx context.Context, items []VersionedItem) ([]model.Glossary, error) {
	restored := make([]model.Glossary, 0, len(items))
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		g, err := s.restoreInTx(ctx, tx, item.ID, item.ExpectedVersion)
		if err != nil {
			return err
		}
		restored = append(restored, *g)
		return nil
	}); err != nil {
		return nil, err
	}
	return restored, nil
}

// DeleteForever 永久删除回收站 Glossary. 只开放给 Web 会话.
func (s *GlossaryService) DeleteForever(ctx context.Context, id, expectedVersion int64) error {
	return s.deleteForeverInTx(ctx, s.db, id, expectedVersion)
}

// BatchDeleteForever 在单个事务中整批永久删除回收站 Glossary.
func (s *GlossaryService) BatchDeleteForever(ctx context.Context, items []VersionedItem) (int64, error) {
	var deleted int64
	if err := runBatch(ctx, s.db, items, func(tx *gorm.DB, item VersionedItem) error {
		if err := s.deleteForeverInTx(ctx, tx, item.ID, item.ExpectedVersion); err != nil {
			return err
		}
		deleted++
		return nil
	}); err != nil {
		return 0, err
	}
	return deleted, nil
}

// deleteForeverInTx 按期望 version 条件删除回收站 Glossary,
// 未命中时区分不存在与版本不匹配.
func (s *GlossaryService) deleteForeverInTx(ctx context.Context, tx *gorm.DB, id, expectedVersion int64) error {
	glossaries := s.glossaries.WithTx(tx)
	g, err := glossaries.FindTrashed(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("回收站中不存在该 Glossary")
		}
		return err
	}
	if g.Version != expectedVersion {
		return versionConflict("Glossary", g.Version)
	}
	ok, err := glossaries.DeleteTrashed(ctx, id, expectedVersion)
	if err != nil {
		return err
	}
	if !ok {
		return versionConflict("Glossary", g.Version)
	}
	return nil
}
