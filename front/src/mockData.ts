import { DueReviewItem, Topic } from './types';

/**
 * Mock 数据严格按 backend/API设计.md 4.1 / 5.10 的字段构造.
 * 不包含 stability / difficulty / 间隔预览 / topic 名称等后端未返回的字段.
 */

export const MOCK_TOPICS: Topic[] = [
  {
    id: 1,
    name: 'Rust 核心并发与所有权',
    description: 'Rust 中的所有权、借用检查与跨线程共享状态。',
    card_count: 8,
    version: 2,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-09-10T00:00:00Z'
  },
  {
    id: 2,
    name: 'FSRS 间隔重复算法',
    description: 'FSRS 的记忆模型、稳定性与难度参数。',
    card_count: 6,
    version: 1,
    created_at: '2026-08-05T00:00:00Z',
    updated_at: '2026-09-08T00:00:00Z'
  },
  {
    id: 3,
    name: 'LLM 架构与嵌入检索',
    description: 'RAG、稠密与稀疏检索、重排序。',
    card_count: 9,
    version: 3,
    created_at: '2026-07-20T00:00:00Z',
    updated_at: '2026-09-12T00:00:00Z'
  },
  {
    id: 4,
    name: '分布式系统与数据库',
    description: '共识算法、一致性与分布式存储。',
    card_count: 5,
    version: 1,
    created_at: '2026-06-11T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z'
  }
];

export const MOCK_DUE_ITEMS: DueReviewItem[] = [
  {
    card_id: 1,
    topic_id: 1,
    card_version: 3,
    schedule_version: 5,
    due: '2026-09-16T00:00:00Z',
    state: 'review',
    reps: 5,
    lapses: 1,
    front: `## 在 Rust 并发编程中，为什么 \`Arc<T>\` 本身无法提供数据可变性？

若需要跨线程共享**可变**状态，通常应该与哪种结构结合使用？`,
    back: `### 核心原理解析

1. \`Arc<T>\` (Atomically Reference Counted) 仅提供**原子引用计数**的多所有权共享，其解引用操作遵循共享引用规则：只能获取 \`&T\`（不可变借用）。
2. 这是为了在编译期防止数据竞争（Data Race）。

### 解决方案

若要在多线程间修改共享数据，必须引入**内部可变性 (Interior Mutability)**：
- 结合使用 \`Arc<Mutex<T>>\`（提供互斥锁保护）
- 或 \`Arc<RwLock<T>>\`（读写锁，读多写少场景）

\`\`\`rust
use std::sync::{Arc, Mutex};
use std::thread;

let counter = Arc::new(Mutex::new(0));
let mut handles = vec![];

for _ in 0..10 {
    let counter_clone = Arc::clone(&counter);
    let handle = thread::spawn(move || {
        let mut num = counter_clone.lock().unwrap();
        *num += 1;
    });
    handles.push(handle);
}
\`\`\``
  },
  {
    card_id: 2,
    topic_id: 2,
    card_version: 4,
    schedule_version: 7,
    due: '2026-09-15T00:00:00Z',
    state: 'review',
    reps: 7,
    lapses: 1,
    front: `## 简述 FSRS 中 stability (稳定性) 的数学含义与业务定义。`,
    back: `### 数学与业务定义

- **稳定性 (Stability, S)**：定义为从上次成功回忆起，卡片回忆概率从 **100% 衰减至 90%** 所经历的时间跨度（通常以天为单位）。
- 直观表达：当经过天数 $t = S$ 时，遗忘曲线上的目标留存概率正好为 **$R = 0.9$**。

> **核心优势**：相较于传统 SM-2 的难度因子乘积，FSRS 基于可解释的记忆认知动力学方程，参数可基于用户真实复习历史数据进行训练拟合。`
  },
  {
    card_id: 3,
    topic_id: 3,
    card_version: 1,
    schedule_version: 2,
    due: '2026-09-14T00:00:00Z',
    state: 'review',
    reps: 2,
    lapses: 0,
    front: `## 在 RAG 管道中，**稠密向量检索 (Dense Retrieval)** 与 **BM25 稀疏检索 (Sparse Retrieval)** 相比有何核心优缺点？`,
    back: `### 优劣势对比分析

| 维度 | Dense Embedding 向量检索 | BM25 稀疏关键字检索 |
| :--- | :--- | :--- |
| **语义泛化** | 极强（同义词、语义近义跨越） | 较弱（依赖字面词项重合） |
| **专有名词/代码** | 易产生漂移（OOD 词汇表现差） | 精确命中（型号、错误代码、缩写） |
| **冷启动与资源** | 需模型推理计算、向量索引库维护 | 零模型开销、索引构建极快 |

### 生产级最佳实践
现代工程架构中通常采用 **RRF (Reciprocal Rank Fusion)** 或 **Cross-Encoder Reranker** 将两者做混合检索（Hybrid Search）。`
  },
  {
    card_id: 4,
    topic_id: 4,
    card_version: 2,
    schedule_version: 3,
    due: '2026-09-13T00:00:00Z',
    state: 'new',
    reps: 0,
    lapses: 0,
    front: `## 为什么分布式数据库中通常优先选择 **Raft** 而非原始 **Multi-Paxos** 进行工程实现？`,
    back: `### 关键原因

1. **可理解性 (Understandability)**：Paxos 将领导选举与日志复制解耦得过于抽象；Raft 通过强 Leader 假定，将状态机分解为 Leader 选举、日志复制、安全性三个独立子问题。
2. **强一致的领导者限制**：Raft 规定日志只能从 Leader 单向流向 Follower，极大精简了冲突解决分支逻辑。
3. **成员变更设计完备**：内置单节点变更与联合共识（Joint Consensus）平滑动态扩缩容。`
  }
];
