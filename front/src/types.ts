// 本文件的类型严格对应 backend/API设计.md 的响应契约,
// 不添加任何后端未返回的字段.

export type FSRSRating = 'again' | 'hard' | 'good' | 'easy';

/** 对应 4.1 Topic */
export interface Topic {
  id: number;
  name: string;
  description: string;
  card_count: number;
  version: number;
  created_at: string;
  updated_at: string;
}

/**
 * 对应 5.10 GET /api/web/review/due 的 items 元素.
 * 注意: 复习队列不返回 stability / difficulty, 也没有任何间隔预览字段.
 * state 在 v1 正常流程中只会是 new 或 review.
 */
export interface DueReviewItem {
  card_id: number;
  front: string;
  back: string;
  /** 0 表示无 Topic (见技术约定 12) */
  topic_id: number;
  card_version: number;
  /** RFC3339 */
  due: string;
  state: 'new' | 'review';
  schedule_version: number;
  reps: number;
  lapses: number;
}

/** 对应 5.10 的完整响应 */
export interface DueReviewResponse {
  items: DueReviewItem[];
  total: number;
}

/** 复习会话本地统计, 全部由前端根据用户点击累积, 不来自 API */
export interface ReviewSessionSummary {
  reviewedCount: number;
  againCount: number;
  hardCount: number;
  goodCount: number;
  easyCount: number;
  averageTimeSeconds: number;
  totalTimeSeconds: number;
}

/**
 * 评分档位的展示顺序：按难度升序 (简单 → 良好 → 困难 → 忘记).
 * 快捷键数字由此数组下标 + 1 得出 (简单=1, 良好=2, 困难=3, 忘记=4).
 * 这是「展示顺序 ↔ 快捷键」的唯一数据源, RatingDock 与键盘监听都从这里派生,
 * 避免两处不一致. 注意: 与后端 rating 字符串本身的语义无关.
 */
export const FSRS_RATING_ORDER: FSRSRating[] = ['easy', 'good', 'hard', 'again'];

/** 由展示顺序派生的快捷键映射: '1' -> easy, '2' -> good, '3' -> hard, '4' -> again */
export const RATING_SHORTCUT_MAP: Record<string, FSRSRating> = FSRS_RATING_ORDER.reduce(
  (acc, rating, index) => {
    acc[String(index + 1)] = rating;
    return acc;
  },
  {} as Record<string, FSRSRating>
);

/** 取某个评分对应的快捷键数字字符串 */
export const getRatingShortcut = (rating: FSRSRating): string =>
  String(FSRS_RATING_ORDER.indexOf(rating) + 1);

export const FSRS_RATING_META: Record<
  FSRSRating,
  {
    label: string;
    english: string;
    bgClass: string;
    textClass: string;
    borderClass: string;
    hoverClass: string;
  }
> = {
  again: {
    label: '忘记',
    english: 'Again',
    bgClass: 'bg-fsrs-again-bg',
    textClass: 'text-fsrs-again-text',
    borderClass: 'border-fsrs-again-border',
    hoverClass: 'hover:border-fsrs-again'
  },
  hard: {
    label: '困难',
    english: 'Hard',
    bgClass: 'bg-fsrs-hard-bg',
    textClass: 'text-fsrs-hard-text',
    borderClass: 'border-fsrs-hard-border',
    hoverClass: 'hover:border-fsrs-hard'
  },
  good: {
    label: '良好',
    english: 'Good',
    bgClass: 'bg-fsrs-good-bg',
    textClass: 'text-fsrs-good-text',
    borderClass: 'border-fsrs-good-border',
    hoverClass: 'hover:border-fsrs-good'
  },
  easy: {
    label: '简单',
    english: 'Easy',
    bgClass: 'bg-fsrs-easy-bg',
    textClass: 'text-fsrs-easy-text',
    borderClass: 'border-fsrs-easy-border',
    hoverClass: 'hover:border-fsrs-easy'
  }
};
