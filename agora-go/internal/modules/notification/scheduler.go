package notification

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

// scheduler is the ThreadPoolTaskScheduler of the Kotlin application: Spring Boot auto-configures
// it with ONE thread (spring.task.scheduling.pool.size = 1), so the scheduled FCM batches run one
// at a time, in due-time order, and a long batch delays the following ones. Tasks live in memory
// only: the ones not run yet are lost when the process stops (shutdownNow), like in Kotlin.
type scheduler struct {
	mu      sync.Mutex
	queue   taskHeap
	seq     uint64
	wake    chan struct{}
	started bool
	// ctx stops the worker (process lifetime).
	ctx context.Context
	now func() time.Time
	// onPanic is called when a task panics (the worker survives, like the executor's error handler).
	onPanic func(any)
}

type scheduledTask struct {
	at  time.Time
	seq uint64
	run func()
}

type taskHeap []*scheduledTask

func (h taskHeap) Len() int { return len(h) }
func (h taskHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}
func (h taskHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *taskHeap) Push(x any)   { *h = append(*h, x.(*scheduledTask)) }
func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	t := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return t
}

func newScheduler(ctx context.Context, now func() time.Time, onPanic func(any)) *scheduler {
	return &scheduler{ctx: ctx, now: now, wake: make(chan struct{}, 1), onPanic: onPanic}
}

// schedule runs f at the given instant (immediately when it is already past).
func (s *scheduler) schedule(at time.Time, f func()) {
	s.mu.Lock()
	s.seq++
	heap.Push(&s.queue, &scheduledTask{at: at, seq: s.seq, run: f})
	if !s.started {
		s.started = true
		go s.loop()
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// pending is the number of tasks not run yet (tests).
func (s *scheduler) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *scheduler) loop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		s.mu.Lock()
		var next *scheduledTask
		if len(s.queue) > 0 {
			next = s.queue[0]
		}
		var wait time.Duration
		if next != nil {
			wait = next.at.Sub(s.now())
			if wait <= 0 {
				heap.Pop(&s.queue)
			}
		}
		s.mu.Unlock()
		switch {
		case next == nil:
			select {
			case <-s.wake:
			case <-s.ctx.Done():
				return
			}
		case wait <= 0:
			s.runTask(next)
		default:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
			select {
			case <-timer.C:
			case <-s.wake:
			case <-s.ctx.Done():
				return
			}
		}
		if s.ctx.Err() != nil {
			return
		}
	}
}

func (s *scheduler) runTask(t *scheduledTask) {
	defer func() {
		if r := recover(); r != nil && s.onPanic != nil {
			s.onPanic(r)
		}
	}()
	t.run()
}
