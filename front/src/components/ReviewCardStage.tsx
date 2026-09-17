import React from 'react';
import { DueReviewItem, FSRSRating } from '../types';
import { MarkdownRenderer } from './MarkdownRenderer';
import { RatingDock } from './RatingDock';

interface ReviewCardStageProps {
  card: DueReviewItem;
  /** 由 topics 列表 join 得到; topic_id=0 时为 undefined */
  topicName?: string;
  isRevealed: boolean;
  onReveal: () => void;
  onRate: (rating: FSRSRating) => void;
}

const formatDue = (iso: string): string => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
};

export const ReviewCardStage: React.FC<ReviewCardStageProps> = ({
  card,
  topicName,
  isRevealed,
  onReveal,
  onRate
}) => {
  return (
    <div className="w-full max-w-3xl mx-auto flex flex-col items-center">
      <div className="w-full bg-surface-container-lowest rounded-2xl border border-outline-variant/40 shadow-card p-6 sm:p-10 transition-all duration-300">
        {/* Card Header: 仅展示 API 实际返回的字段 */}
        <div className="flex items-center justify-between border-b border-outline-variant/30 pb-4 mb-6">
          <div className="flex items-center gap-2 min-w-0">
            <span className="w-2.5 h-2.5 rounded-full bg-primary flex-shrink-0" />
            <span className="font-mono text-[12px] font-semibold text-on-surface truncate">
              {topicName ?? `Topic #${card.topic_id}`}
            </span>
            <span className="font-mono text-[11px] px-1.5 py-0.5 rounded bg-surface-container text-on-surface-variant border border-outline-variant/30">
              #{card.card_id}
            </span>
          </div>

          <div className="flex items-center gap-2 flex-shrink-0">
            <span
              title="复习次数 reps"
              className="font-mono text-[11px] px-2 py-0.5 rounded-md bg-surface-container-low text-on-surface-variant border border-outline-variant/30"
            >
              reps <span className="font-semibold text-on-surface">{card.reps}</span>
            </span>
            <span
              title="遗忘次数 lapses"
              className="font-mono text-[11px] px-2 py-0.5 rounded-md bg-surface-container-low text-on-surface-variant border border-outline-variant/30"
            >
              lapses <span className="font-semibold text-on-surface">{card.lapses}</span>
            </span>
            <span
              title="到期时间 due"
              className="hidden lg:inline font-mono text-[11px] px-2 py-0.5 rounded-md bg-surface-container-low text-on-surface-variant border border-outline-variant/30"
            >
              due <span className="font-semibold text-on-surface">{formatDue(card.due)}</span>
            </span>
          </div>
        </div>

        {/* 阶段一: 问题 front (Markdown) */}
        <div className="min-h-[140px] flex flex-col justify-center">
          <div className="text-on-surface text-[17px] sm:text-[19px] leading-relaxed font-medium">
            <MarkdownRenderer content={card.front} />
          </div>
        </div>

        {/* 阶段二: 揭示答案 back (Markdown) */}
        {isRevealed ? (
          <div className="mt-8 pt-8 border-t-2 border-dashed border-outline-variant/40 animate-fadeIn">
            <div className="flex items-center gap-2 mb-3 text-primary font-mono text-[12px] font-semibold tracking-wider uppercase">
              <span className="material-symbols-outlined text-[16px]">verified</span>
              <span>Answer</span>
            </div>
            <div className="text-on-surface text-[15px] sm:text-[16px] leading-relaxed">
              <MarkdownRenderer content={card.back} />
            </div>
          </div>
        ) : (
          <div className="mt-10 pt-4 flex flex-col items-center justify-center">
            <button
              onClick={onReveal}
              className="w-full sm:w-auto min-w-[240px] px-8 py-3.5 rounded-xl bg-primary hover:bg-primary-container text-on-primary font-semibold text-[15px] shadow-sm hover:shadow transition-all duration-150 flex items-center justify-center gap-2.5 cursor-pointer active:scale-[0.99]"
            >
              <span className="material-symbols-outlined text-[20px]">visibility</span>
              <span>显示答案</span>
              <kbd className="ml-2 font-mono text-[11px] px-2 py-0.5 rounded bg-white/20 text-white font-semibold">
                Space
              </kbd>
            </button>
            <span className="mt-2 text-on-surface-variant text-[12px] font-mono">
              点击按钮或按空格键展开答案
            </span>
          </div>
        )}
      </div>

      {/* 阶段三: FSRS 评分 */}
      {isRevealed && (
        <div className="w-full mt-6 animate-slideUp">
          <RatingDock onRate={onRate} />
        </div>
      )}
    </div>
  );
};
