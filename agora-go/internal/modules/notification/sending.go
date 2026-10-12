package notification

import (
	"context"
	"fmt"
	"time"

	"agora/internal/app"
	"agora/internal/config"
	"agora/internal/fcm"
	"agora/internal/javacompat"
)

// fanoutBatchSize is the number of tokens of one FCM multicast: the FCM maximum per
// SendEachForMulticast (C-S8-FANOUT; Kotlin's MAX_SIMULTANEOUS_NOTIFICATIONS was 300).
const fanoutBatchSize = 500

// notificationTypeKey is NOTIFICATION_TYPE_KEY, the FCM data key of the notification type.
const notificationTypeKey = "type"

// tokenChunks collects the FCM tokens like createMultiMessage:
// `fcmTokenList.filter { it.isNotBlank() }.distinct().chunked(n)`, in chunks of fanoutBatchSize
// (C-S8-FANOUT). It is fed one token at a time so
// that a storm over millions of users never builds the intermediate lists; the first occurrence of a
// token wins and the order is the order of the users.
type tokenChunks struct {
	seen   map[string]struct{}
	chunks [][]string
}

func newTokenChunks() *tokenChunks { return &tokenChunks{seen: map[string]struct{}{}} }

func (t *tokenChunks) add(token string) {
	if javacompat.KotlinIsBlank(token) {
		return
	}
	if _, dup := t.seen[token]; dup {
		return
	}
	t.seen[token] = struct{}{}
	n := len(t.chunks)
	if n == 0 || len(t.chunks[n-1]) == fanoutBatchSize {
		t.chunks = append(t.chunks, make([]string, 0, fanoutBatchSize))
		n++
	}
	t.chunks[n-1] = append(t.chunks[n-1], token)
}

// release drops the dedupe table (the chunks keep the tokens).
func (t *tokenChunks) release() { t.seen = nil }

// Sender is NotificationSendingRepositoryImpl: it cuts the tokens in batches and schedules batch i at
// now + i × interval on a single-threaded scheduler (see scheduler). The HTTP request does not wait
// for the sends. Kotlin sends 300 tokens a minute (about 56 h for a million users); the Go side
// sends AGORA_NOTIFICATION_FANOUT_PER_MINUTE tokens a minute, by default a million in about an hour
// (C-S8-FANOUT, validated on 12 Oct.).
type Sender struct {
	a     *app.App
	sched *scheduler
	// interval separates the due times of two consecutive batches.
	interval time.Duration
	// send performs the FCM call of one batch (FirebaseMessaging.sendEachForMulticast); replaced in tests.
	send func(ctx context.Context, m fcm.Message) (fcm.BatchResponse, error)
}

func newSender(a *app.App) *Sender {
	perMinute := config.DefaultNotificationFanoutPerMinute
	if a.Cfg != nil && a.Cfg.NotificationFanoutPerMinute > 0 {
		perMinute = a.Cfg.NotificationFanoutPerMinute
	}
	s := &Sender{a: a, interval: batchInterval(perMinute)}
	s.send = func(ctx context.Context, m fcm.Message) (fcm.BatchResponse, error) {
		return a.FCM.SendEachForMulticast(ctx, m)
	}
	ctx := a.Background
	if ctx == nil {
		ctx = context.Background()
	}
	s.sched = newScheduler(ctx, a.Now, func(r any) {
		a.Log.Error(fmt.Sprintf("⚠️ Unexpected error occurred in scheduled task: %v", r))
	})
	return s
}

// SendGenericMultiNotification is sendGenericMultiNotification: the tokens may be blank or repeated.
// The result is Success unless building or scheduling the batches fails (never in practice).
func (s *Sender) SendGenericMultiNotification(title, description string, fcmTokens []string, typ TypeNotification, pageArgument *string) Result {
	chunks := newTokenChunks()
	for _, t := range fcmTokens {
		chunks.add(t)
	}
	return s.sendChunks(title, description, chunks, typ, pageArgument)
}

func (s *Sender) sendChunks(title, description string, chunks *tokenChunks, typ TypeNotification, pageArgument *string) (res Result) {
	s.a.Log.Info("📩 Sending multi-notification: " + title)
	defer func() {
		if r := recover(); r != nil {
			s.a.Log.Error(fmt.Sprintf("⚠️ Send réponse support notification error: %v", r))
			res = Failure
		}
	}()
	messages := make([]fcm.Message, len(chunks.chunks))
	for i, tokens := range chunks.chunks {
		data := map[string]string{notificationTypeKey: typ.Name()}
		if pageArgument != nil {
			data["pageArgument"] = *pageArgument
		}
		messages[i] = fcm.Message{Tokens: tokens, Title: title, Body: description, Data: data}
	}
	s.scheduleBatches(messages)
	return Success
}

// batchInterval is the time between two batches of fanoutBatchSize tokens at perMinute tokens a minute.
func batchInterval(perMinute int) time.Duration {
	return time.Minute * fanoutBatchSize / time.Duration(perMinute)
}

// scheduleBatches is sendMultiNotifications: batch i is due at now + i × interval (Kotlin:
// LocalDateTime.now(clock).plusMinutes(i), C-S8-FANOUT).
func (s *Sender) scheduleBatches(messages []fcm.Message) {
	now := s.a.Now()
	for i, m := range messages {
		index, count := i, len(messages)
		msg := m
		s.sched.schedule(now.Add(time.Duration(index)*s.interval), func() { s.runBatch(msg, index, count) })
	}
}

// runBatch is SendNotificationTask.run.
func (s *Sender) runBatch(m fcm.Message, index, count int) {
	ctx := s.sched.ctx
	resp, err := s.send(ctx, m)
	if err != nil {
		s.a.Log.Error(fmt.Sprintf("⚠️ Send multi-notification error on batch n°%d/%d %v", index+1, count, err))
		return
	}
	s.a.Log.Info(fmt.Sprintf("📩 Send multi-notification n°%d/%d results:\n- %d total responses\n- %d successes\n- %d failures",
		index+1, count, resp.SuccessCount+resp.FailureCount, resp.SuccessCount, resp.FailureCount))
}
