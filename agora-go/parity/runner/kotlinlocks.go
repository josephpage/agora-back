package runner

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Kotlin's AgoraQueue never releases a user's slot when the queued action
// throws (B-AGORAQUEUE): until the reference JVM restarts, every later call of
// that user on the same queue is rejected (400), across scenarios and runs,
// because a reseed does not reset the JVM. The runner records each (queue,
// user) for which the reference answered 500 on a queued route, in a file that
// survives runs (run.sh deletes it when it stops the reference), and annotates
// a later 400-vs-other status diff on the same pair. It is only a hint: a 500
// raised before the queue (authentication, binding) does not lock anything.

var queuedRoutes = []struct {
	method string
	re     *regexp.Regexp
	queue  string
}{
	{"POST", regexp.MustCompile(`^/qags/?$`), "InsertQag"},
	{"POST", regexp.MustCompile(`^/qags/[^/]+/support/?$`), "AddSupport"},
	{"DELETE", regexp.MustCompile(`^/qags/[^/]+/support/?$`), "RemoveSupport"},
	{"POST", regexp.MustCompile(`^/qags/[^/]+/feedback/?$`), "AddFeedbackQag"},
	{"POST", regexp.MustCompile(`^/consultations/[^/]+/updates/[^/]+/feedback/?$`), "AddFeedbackConsultationUpdate"},
	{"POST", regexp.MustCompile(`^/consultations/[^/]+/responses/?$`), "InsertResponse"},
}

// queueKey returns "<queue>:<user>" for a step on a queued route.
func queueKey(method, path, user string) string {
	if user == "" {
		return ""
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for _, q := range queuedRoutes {
		if q.method == method && q.re.MatchString(path) {
			return q.queue + ":" + user
		}
	}
	return ""
}

type kotlinLocks struct {
	mu   sync.Mutex
	file string
	set  map[string]string // key → step that probably locked it
}

func loadKotlinLocks(file string) *kotlinLocks {
	l := &kotlinLocks{file: file, set: map[string]string{}}
	if file == "" {
		return l
	}
	f, err := os.Open(file)
	if err != nil {
		return l
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, origin, ok := strings.Cut(sc.Text(), "\t"); ok {
			l.set[k] = origin
		}
	}
	return l
}

func (l *kotlinLocks) add(key, origin string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.set[key]; ok {
		return
	}
	l.set[key] = origin
	if l.file != "" {
		if f, err := os.OpenFile(l.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(key + "\t" + origin + "\n")
			_ = f.Close()
		}
	}
}

func (l *kotlinLocks) origin(key string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	o, ok := l.set[key]
	return o, ok
}
