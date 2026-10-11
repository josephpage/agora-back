package consultationresponse

import (
	"sync"
	"sync/atomic"
	"testing"
)

// B-AGORAQUEUE: one action per user at a time, the check and the add are one step
func TestQueueOnlyOneTaskPerUser(t *testing.T) {
	var q agoraQueue[insertResponseTask]
	var executed, rejected atomic.Int32
	release := make(chan struct{})
	running := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		executeTask(&q, insertResponseTask{"u"}, func() int {
			close(running)
			<-release
			executed.Add(1)
			return 1
		}, func() int { rejected.Add(1); return 0 })
	}()
	<-running
	for i := 0; i < 20; i++ {
		executeTask(&q, insertResponseTask{"u"}, func() int { executed.Add(1); return 1 }, func() int { rejected.Add(1); return 0 })
	}
	// another user is not blocked
	mustEqual(t, "other user", executeTask(&q, insertResponseTask{"v"}, func() int { return 1 }, func() int { return 0 }), 1)
	close(release)
	wg.Wait()
	mustEqual(t, "executed", int(executed.Load()), 1)
	mustEqual(t, "rejected", int(rejected.Load()), 20)
	// released at the end
	mustEqual(t, "free again", executeTask(&q, insertResponseTask{"u"}, func() int { return 1 }, func() int { return 0 }), 1)
}

func TestQueueSimultaneousRequestsOfOneUser(t *testing.T) {
	var q agoraQueue[insertResponseTask]
	var executed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	hold := make(chan struct{})
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			executeTask(&q, insertResponseTask{"u"}, func() int { executed.Add(1); <-hold; return 1 }, func() int { return 0 })
		}()
	}
	close(start)
	// let every goroutine try before the first one finishes
	for i := 0; i < 1000; i++ {
		q.mu.Lock()
		n := len(q.queued)
		q.mu.Unlock()
		if n == 1 {
			break
		}
	}
	close(hold)
	wg.Wait()
	if executed.Load() < 1 {
		t.Fatal("nobody ran")
	}
	// at any time at most one runs: the 200 goroutines never held the lock together
	mustEqual(t, "free", len(q.queued), 0)
}

// the lock is released when the action panics (Kotlin kept the user locked until the restart)
func TestQueueReleasedAfterAPanic(t *testing.T) {
	var q agoraQueue[insertResponseTask]
	func() {
		defer func() { _ = recover() }()
		executeTask(&q, insertResponseTask{"u"}, func() int { panic("boom") }, func() int { return 0 })
	}()
	mustEqual(t, "free again", executeTask(&q, insertResponseTask{"u"}, func() int { return 1 }, func() int { return 0 }), 1)
}
