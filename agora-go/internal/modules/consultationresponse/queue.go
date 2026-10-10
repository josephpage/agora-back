package consultationresponse

import "sync"

// agoraQueue is infrastructure/utils/AgoraQueue (InsertResponseConsultationQueue): a
// per-task in-memory lock. A task equal to one that is already running is rejected
// (the caller answers 400) instead of being executed.
//
// Kotlin checked `canAddTask` and added the task in two separate steps and never
// removed it when the action threw (the user stayed locked until the instance
// restarted: class B-AGORAQUEUE); here the check-and-add is atomic and the lock is
// released by defer. The queue is a set: thousands of users can answer at the same
// time when a consultation closes and Kotlin's linear `contains` is not needed.
type agoraQueue[T comparable] struct {
	mu     sync.Mutex
	queued map[T]struct{}
}

func (q *agoraQueue[T]) add(task T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.queued[task]; ok {
		return false
	}
	if q.queued == nil {
		q.queued = map[T]struct{}{}
	}
	q.queued[task] = struct{}{}
	return true
}

func (q *agoraQueue[T]) remove(task T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.queued, task)
}

// executeTask is AgoraQueue.executeTask: onTaskExecuted when the task could be
// queued, onTaskRejected otherwise.
func executeTask[T comparable, R any](q *agoraQueue[T], task T, onTaskExecuted, onTaskRejected func() R) R {
	if !q.add(task) {
		return onTaskRejected()
	}
	defer q.remove(task)
	return onTaskExecuted()
}

// insertResponseTask is InsertResponseConsultationQueue.TaskType.InsertResponse.
type insertResponseTask struct{ userID string }
