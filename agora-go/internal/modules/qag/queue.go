package qag

import "sync"

// agoraQueue is infrastructure/utils/AgoraQueue: a per-task-type in-memory
// lock. A task equal to one that is already running is rejected (the caller
// answers 400) instead of being executed.
//
// Kotlin checked `canAddTask` and added the task in two separate steps and
// never removed it when the action threw (the user stayed locked until the
// instance restarted: class B-AGORAQUEUE); here the check-and-add is atomic and
// the lock is released by defer.
type agoraQueue[T comparable] struct {
	mu     sync.Mutex
	queued []T
}

func (q *agoraQueue[T]) add(task T) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, t := range q.queued {
		if t == task {
			return false
		}
	}
	q.queued = append(q.queued, task)
	return true
}

func (q *agoraQueue[T]) remove(task T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, t := range q.queued {
		if t == task {
			q.queued = append(q.queued[:i], q.queued[i+1:]...)
			return
		}
	}
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

// Task types of the three queues (InsertQagQueue, SupportQagQueue,
// FeedbackQagQueue are separate beans: the same user may run one task of each
// queue at the same time).
type (
	insertQagTask struct{ userID string }
	supportTask   struct {
		add    bool // AddSupport / RemoveSupport
		userID string
	}
	feedbackTask struct{ userID string }
)

// queues are the three AgoraQueue beans.
type queues struct {
	insert   agoraQueue[insertQagTask]
	support  agoraQueue[supportTask]
	feedback agoraQueue[feedbackTask]
}
