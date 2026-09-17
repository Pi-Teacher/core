import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Sidebar } from '../components/Sidebar';
import { ReviewCardStage } from '../components/ReviewCardStage';
import { ReviewSummary } from '../components/ReviewSummary';
import { EmptyReview } from '../components/EmptyReview';
import { MOCK_DUE_ITEMS, MOCK_TOPICS } from '../mockData';
import { DueReviewItem, FSRSRating, ReviewSessionSummary, RATING_SHORTCUT_MAP } from '../types';

const createEmptySummary = (): ReviewSessionSummary => ({
  reviewedCount: 0,
  againCount: 0,
  hardCount: 0,
  goodCount: 0,
  easyCount: 0,
  averageTimeSeconds: 0,
  totalTimeSeconds: 0
});

export const ReviewPage: React.FC = () => {
  // '' 表示全部; 数字字符串表示 topic_id; '0' 表示无 Topic (技术约定 12)
  const [selectedTopicId, setSelectedTopicId] = useState<string>('');
  const [queue, setQueue] = useState<DueReviewItem[]>(MOCK_DUE_ITEMS);
  const [currentIndex, setCurrentIndex] = useState(0);
  const [isRevealed, setIsRevealed] = useState(false);
  const [isFinished, setIsFinished] = useState(false);
  const [summary, setSummary] = useState<ReviewSessionSummary>(createEmptySummary);
  const [cardStartTime, setCardStartTime] = useState(() => Date.now());

  // topic_id -> name 映射. 复习队列只返回 topic_id, 名称需从 topics 列表 join.
  const topicNameMap = useMemo(() => {
    const map = new Map<number, string>();
    MOCK_TOPICS.forEach((t) => map.set(t.id, t.name));
    return map;
  }, []);

  const handleTopicChange = (value: string) => {
    setSelectedTopicId(value);
    const filtered =
      value === '' ? MOCK_DUE_ITEMS : MOCK_DUE_ITEMS.filter((c) => c.topic_id === Number(value));
    setQueue(filtered);
    setCurrentIndex(0);
    setIsRevealed(false);
    setIsFinished(filtered.length === 0);
    setSummary(createEmptySummary());
    setCardStartTime(Date.now());
  };

  const handleReveal = useCallback(() => setIsRevealed(true), []);

  const handleRate = useCallback(
    (rating: FSRSRating) => {
      const elapsedSec = (Date.now() - cardStartTime) / 1000;

      setSummary((prev) => {
        const nextCount = prev.reviewedCount + 1;
        const nextTotal = prev.totalTimeSeconds + elapsedSec;
        return {
          reviewedCount: nextCount,
          againCount: prev.againCount + (rating === 'again' ? 1 : 0),
          hardCount: prev.hardCount + (rating === 'hard' ? 1 : 0),
          goodCount: prev.goodCount + (rating === 'good' ? 1 : 0),
          easyCount: prev.easyCount + (rating === 'easy' ? 1 : 0),
          totalTimeSeconds: nextTotal,
          averageTimeSeconds: nextTotal / nextCount
        };
      });

      // 真实实现: 此处调用 POST /api/web/review/{card_id}/submit
      // 并处理 409 version_conflict. 原型阶段仅推进本地队列.
      if (currentIndex + 1 < queue.length) {
        setCurrentIndex((i) => i + 1);
        setIsRevealed(false);
        setCardStartTime(Date.now());
      } else {
        setIsFinished(true);
      }
    },
    [cardStartTime, currentIndex, queue.length]
  );

  const handleRestart = useCallback(() => {
    setCurrentIndex(0);
    setIsRevealed(false);
    setIsFinished(false);
    setSummary(createEmptySummary());
    setCardStartTime(Date.now());
  }, []);

  // 键盘: Space 揭示, 1-4 评分, Esc 退出(回到首张)
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

      if (e.key === 'Escape') {
        handleRestart();
        return;
      }

      if (isFinished || queue.length === 0) return;

      if (e.code === 'Space' || e.key === ' ') {
        e.preventDefault();
        if (!isRevealed) handleReveal();
        return;
      }

      if (isRevealed) {
        const rating = RATING_SHORTCUT_MAP[e.key];
        if (rating) {
          e.preventDefault();
          handleRate(rating);
        }
      }
    };

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isRevealed, isFinished, queue.length, handleReveal, handleRate, handleRestart]);

  const currentCard = queue[currentIndex];
  const progressPercent =
    queue.length > 0 ? Math.round(((currentIndex + (isFinished ? 1 : 0)) / queue.length) * 100) : 0;

  return (
    <div className="flex min-h-screen bg-surface">
      <Sidebar currentPath="review" />

      <div className="pl-72 flex-1 flex flex-col min-w-0">
        {/* 顶部工具栏 */}
        <header className="h-16 px-gutter flex items-center justify-between border-b border-outline-variant/30 bg-surface-container-lowest/80 backdrop-blur-md sticky top-0 z-40">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="material-symbols-outlined text-[20px] text-primary">filter_alt</span>
              <span className="font-mono text-[12px] uppercase tracking-wider text-on-surface-variant">
                复习范围
              </span>
            </div>
            <select
              value={selectedTopicId}
              onChange={(e) => handleTopicChange(e.target.value)}
              className="px-3 py-1.5 rounded-lg bg-surface-container-low border border-outline-variant/40 text-on-surface text-[13px] font-medium focus:outline-none focus:ring-2 focus:ring-primary/20 focus:border-primary transition-all cursor-pointer"
            >
              <option value="">全部 Topic</option>
              {MOCK_TOPICS.map((t) => (
                <option key={t.id} value={String(t.id)}>
                  {t.name} ({t.card_count})
                </option>
              ))}
              <option value="0">无 Topic</option>
            </select>
          </div>

          {/* 队列进度 (total 来自 API, 非本地长度) */}
          {!isFinished && queue.length > 0 && (
            <div className="hidden md:flex flex-col items-center">
              <div className="flex items-center gap-2 font-mono text-[12px] text-on-surface">
                <span>进度</span>
                <span className="font-bold text-primary">{currentIndex + 1}</span>
                <span className="text-on-surface-variant">/ {queue.length}</span>
              </div>
              <div className="w-36 h-1.5 bg-surface-container-high rounded-full overflow-hidden mt-1">
                <div
                  className="h-full bg-primary rounded-full transition-all duration-300 ease-out"
                  style={{ width: `${progressPercent}%` }}
                />
              </div>
            </div>
          )}

          <div className="flex items-center gap-3">
            <div className="hidden lg:flex items-center gap-2 px-2.5 py-1 rounded-full bg-surface-container-high/60 border border-outline-variant/30 text-on-surface-variant font-mono text-[11px]">
              <span className="w-1.5 h-1.5 rounded-full bg-tertiary" />
              <span>Space 显示答案 · 1~4 评分 · Esc 重置</span>
            </div>
            <button
              onClick={handleRestart}
              title="重置当前队列"
              className="p-2 rounded-lg text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high transition-colors"
            >
              <span className="material-symbols-outlined text-[20px]">refresh</span>
            </button>
          </div>
        </header>

        {/* 顶部细进度条 */}
        {!isFinished && queue.length > 0 && (
          <div className="w-full h-1 bg-surface-container-high overflow-hidden">
            <div
              className="h-full bg-primary-container transition-all duration-300 ease-out"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        )}

        <main className="flex-1 flex flex-col justify-center px-gutter py-8">
          {isFinished ? (
            <ReviewSummary summary={summary} onRestart={handleRestart} />
          ) : queue.length === 0 ? (
            <EmptyReview onGoCards={() => {}} onGoBack={handleRestart} />
          ) : (
            <ReviewCardStage
              card={currentCard}
              topicName={topicNameMap.get(currentCard.topic_id)}
              isRevealed={isRevealed}
              onReveal={handleReveal}
              onRate={handleRate}
            />
          )}
        </main>

        <footer className="h-10 px-gutter flex items-center justify-between border-t border-outline-variant/20 bg-surface-container-lowest/50 text-on-surface-variant font-mono text-[11px]">
          <div className="flex items-center gap-4">
            <span>FSRS v6 · go-fsrs/v4</span>
            <span className="text-outline-variant">•</span>
            <span>desired_retention 0.9</span>
          </div>
          <span>Precision Cognition</span>
        </footer>
      </div>
    </div>
  );
};
