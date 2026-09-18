package appsvc

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
)

// TrashService 承载跨领域的回收站操作.
// 单类对象的恢复与永久删除在各自领域服务中; 只有清空回收站同时
// 涉及三类表且必须在一个事务中完成, 因此独立成服务.
type TrashService struct {
	db         *gorm.DB
	topics     *repo.TopicRepository
	cards      *repo.CardRepository
	glossaries *repo.GlossaryRepository
	logger     *slog.Logger
}

// NewTrashService 构造回收站服务.
func NewTrashService(
	db *gorm.DB,
	topics *repo.TopicRepository,
	cards *repo.CardRepository,
	glossaries *repo.GlossaryRepository,
	logger *slog.Logger,
) *TrashService {
	return &TrashService{
		db:         db,
		topics:     topics,
		cards:      cards,
		glossaries: glossaries,
		logger:     logger,
	}
}

// EmptyTrashResult 是清空回收站的各类删除数量.
type EmptyTrashResult struct {
	Cards    int64 `json:"cards"`
	Topics   int64 `json:"topics"`
	Glossary int64 `json:"glossary"`
}

// Empty 在一个事务中删除当时的全部回收站对象.
// Card 的调度与复习日志在进入回收站时已清理, Topic 的关联在进入
// 回收站时已清空, 因此这里只需删除三类回收站行.
func (s *TrashService) Empty(ctx context.Context) (*EmptyTrashResult, error) {
	result := &EmptyTrashResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cards, err := s.cards.WithTx(tx).DeleteAllTrashed(ctx)
		if err != nil {
			return err
		}
		topics, err := s.topics.WithTx(tx).DeleteAllTrashed(ctx)
		if err != nil {
			return err
		}
		glossaries, err := s.glossaries.WithTx(tx).DeleteAllTrashed(ctx)
		if err != nil {
			return err
		}
		result.Cards = cards
		result.Topics = topics
		result.Glossary = glossaries
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
