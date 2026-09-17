import React from 'react';
import { ReviewSessionSummary } from '../types';

interface ReviewSummaryProps {
  summary: ReviewSessionSummary;
  onRestart: () => void;
}

export const ReviewSummary: React.FC<ReviewSummaryProps> = ({ summary, onRestart }) => {
  const retentionRate =
    summary.reviewedCount > 0
      ? Math.round(
          ((summary.goodCount + summary.easyCount) / summary.reviewedCount) * 100
        )
      : 100;

  return (
    <div className="w-full max-w-2xl mx-auto bg-surface-container-lowest rounded-2xl border border-outline-variant/40 shadow-card p-8 sm:p-10 text-center animate-fadeIn">
      {/* Trophy / Checkmark Icon */}
      <div className="w-16 h-16 mx-auto rounded-2xl bg-tertiary-fixed flex items-center justify-center text-on-tertiary-fixed mb-5 shadow-sm">
        <span className="material-symbols-outlined text-[36px]">task_alt</span>
      </div>

      <h2 className="font-headline-md text-headline-md text-on-surface font-semibold tracking-tight">
        太棒了！今日复习队列已清空
      </h2>
      <p className="text-on-surface-variant text-body-md mt-1.5 max-w-md mx-auto">
        本次会话的评分已提交至服务端，由 FSRS 重新计算每张卡片的下次到期时间。
      </p>

      {/* Metrics Grid */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 my-8 text-left">
        <div className="p-4 rounded-xl bg-surface-container-low border border-outline-variant/30 flex flex-col">
          <span className="font-mono text-label-sm text-[11px] text-on-surface-variant uppercase">复习总数</span>
          <span className="font-headline-md text-[24px] font-bold text-on-surface mt-1">
            {summary.reviewedCount}
          </span>
          <span className="font-mono text-[11px] text-tertiary mt-0.5">Cards Cleared</span>
        </div>

        <div className="p-4 rounded-xl bg-surface-container-low border border-outline-variant/30 flex flex-col">
          <span className="font-mono text-label-sm text-[11px] text-on-surface-variant uppercase">记忆保留率</span>
          <span className="font-headline-md text-[24px] font-bold text-tertiary mt-1">
            {retentionRate}%
          </span>
          <span className="font-mono text-[11px] text-on-surface-variant mt-0.5">Good & Easy</span>
        </div>

        <div className="p-4 rounded-xl bg-surface-container-low border border-outline-variant/30 flex flex-col">
          <span className="font-mono text-label-sm text-[11px] text-on-surface-variant uppercase">平均用时</span>
          <span className="font-headline-md text-[24px] font-bold text-on-surface mt-1">
            {summary.averageTimeSeconds.toFixed(1)}s
          </span>
          <span className="font-mono text-[11px] text-on-surface-variant mt-0.5">Per Card</span>
        </div>

        <div className="p-4 rounded-xl bg-surface-container-low border border-outline-variant/30 flex flex-col">
          <span className="font-mono text-label-sm text-[11px] text-on-surface-variant uppercase">总投入时间</span>
          <span className="font-headline-md text-[24px] font-bold text-primary mt-1">
            {Math.round(summary.totalTimeSeconds)}s
          </span>
          <span className="font-mono text-[11px] text-on-surface-variant mt-0.5">Total Focus</span>
        </div>      </div>

      {/* Grade Breakdown Pill Bar */}
      <div className="flex items-center justify-between p-3 rounded-xl bg-surface-container-low border border-outline-variant/20 mb-8 font-mono text-[12px]">
        <span className="flex items-center gap-1.5 text-fsrs-again-text font-medium">
          <span className="w-2 h-2 rounded-full bg-fsrs-again" />
          忘记: {summary.againCount}
        </span>
        <span className="flex items-center gap-1.5 text-fsrs-hard-text font-medium">
          <span className="w-2 h-2 rounded-full bg-fsrs-hard" />
          困难: {summary.hardCount}
        </span>
        <span className="flex items-center gap-1.5 text-fsrs-good-text font-medium">
          <span className="w-2 h-2 rounded-full bg-fsrs-good" />
          良好: {summary.goodCount}
        </span>
        <span className="flex items-center gap-1.5 text-fsrs-easy-text font-medium">
          <span className="w-2 h-2 rounded-full bg-fsrs-easy" />
          简单: {summary.easyCount}
        </span>
      </div>

      {/* Actions */}
      <div className="flex items-center justify-center">
        <button
          onClick={onRestart}
          className="px-6 py-2.5 rounded-xl bg-primary hover:bg-primary-container text-on-primary font-semibold text-[14px] transition-colors shadow-sm flex items-center justify-center gap-2 cursor-pointer"
        >
          <span className="material-symbols-outlined text-[18px]">replay</span>
          <span>再来一轮</span>
        </button>
      </div>
    </div>
  );
};
