package logging

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
)

// dbSink 缓冲 app_log 行并批量落库.
// enqueue 永不阻塞: 队列满时丢弃该行, stdout 已经输出过它.
type dbSink struct {
	writer DBWriter
	queue  chan model.AppLog

	closeOnce sync.Once
	done      chan struct{}
}

func newDBSink(writer DBWriter) *dbSink {
	s := &dbSink{
		writer: writer,
		queue:  make(chan model.AppLog, queueCapacity),
		done:   make(chan struct{}),
	}
	go s.run()
	return s
}

// enqueue 非阻塞入队, 满时丢弃.
func (s *dbSink) enqueue(row model.AppLog) {
	select {
	case s.queue <- row:
	default:
	}
}

// run 是唯一的落库 goroutine: 攒批加定时刷新.
// 单写者避免并发插入竞争, 批量写入减少事务开销.
func (s *dbSink) run() {
	defer close(s.done)
	const (
		flushInterval = time.Second
		maxBatch      = 256
	)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]model.AppLog, 0, maxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.writer.InsertAppLogs(ctx, batch)
		cancel()
		if err != nil {
			// 写库失败只回退 stderr, 不递归产生新的数据库日志,
			// 否则故障时会形成自我放大的日志循环.
			fmt.Fprintf(os.Stderr, "pi-teacher: database log write failed: %v\n", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case row, ok := <-s.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, row)
			if len(batch) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Close 排空队列并停止 goroutine, 保证进程退出前已入队的日志尽量落库.
func (s *dbSink) Close() {
	s.closeOnce.Do(func() {
		close(s.queue)
		<-s.done
	})
}

// Close 排空并停止 handler 的数据库 sink, 未启用时是空操作.
func (h *Handler) Close() {
	if h.sink != nil {
		h.sink.Close()
	}
}
