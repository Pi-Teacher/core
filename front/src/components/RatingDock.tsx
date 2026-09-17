import React from 'react';
import {
  FSRSRating,
  FSRS_RATING_META,
  FSRS_RATING_ORDER,
  getRatingShortcut
} from '../types';

interface RatingDockProps {
  onRate: (rating: FSRSRating) => void;
  disabled?: boolean;
}

/**
 * FSRS 四档评分栏.
 * 展示顺序为难度升序 (简单 → 良好 → 困难 → 忘记), 快捷键数字跟随展示顺序:
 * 简单=1, 良好=2, 困难=3, 忘记=4.
 * 说明: API (5.10 POST /api/web/review/{card_id}/submit) 只接受 rating 字符串,
 * 响应才返回新的 schedule. 复习队列接口不提供任何"下次间隔预览", 故不展示预估间隔.
 */
export const RatingDock: React.FC<RatingDockProps> = ({ onRate, disabled = false }) => {
  return (
    <div className="w-full max-w-2xl mx-auto pt-4">
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {FSRS_RATING_ORDER.map((type) => {
          const meta = FSRS_RATING_META[type];
          return (
            <button
              key={type}
              disabled={disabled}
              onClick={() => onRate(type)}
              className={`relative flex flex-col items-center justify-center py-3.5 px-3 rounded-xl border ${meta.bgClass} ${meta.borderClass} ${meta.textClass} ${meta.hoverClass} hover:shadow-sm active:scale-[0.99] transition-all duration-150 cursor-pointer group disabled:opacity-50 disabled:cursor-not-allowed`}
            >
              {/* 快捷键键帽 (label-sm / JetBrains Mono) */}
              <span className="absolute top-2 right-2 w-5 h-5 rounded flex items-center justify-center font-mono text-[11px] font-semibold bg-white/70 border border-outline-variant/40">
                {getRatingShortcut(type)}
              </span>

              <span className="font-headline-sm text-[16px] font-semibold tracking-tight">
                {meta.label}
              </span>

              <span className="font-mono text-[11px] uppercase tracking-wider opacity-70 mt-0.5">
                {meta.english}
              </span>
            </button>
          );
        })}
      </div>

      <p className="text-center font-mono text-[11px] text-on-surface-variant mt-3">
        按{' '}
        {FSRS_RATING_ORDER.map((type, index) => (
          <React.Fragment key={type}>
            {index > 0 && <span className="mx-1 text-outline-variant">·</span>}
            <kbd className="mx-0.5 px-1.5 py-0.5 rounded bg-surface-container-high text-on-surface font-semibold border border-outline-variant/40">
              {getRatingShortcut(type)}
            </kbd>
            {FSRS_RATING_META[type].label}
          </React.Fragment>
        ))}
      </p>
    </div>
  );
};
