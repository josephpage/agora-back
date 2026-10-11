package moderatus

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"agora/internal/fcm"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// callLog records the calls the fakes receive, in the format of the oracle (S7Functions.java).
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

func (l *callLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func jn(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

// fakeQags is QagInfoRepository (the methods the slice uses).
type fakeQags struct {
	log         *callLog
	byID        map[string]*qag.QagInfo
	updateFails bool
	err         error
	// order of the other recorded calls
	toModerate []qag.QagInfo
}

func (f *fakeQags) GetQagInfo(_ context.Context, id string) (*qag.QagInfo, error) {
	f.log.add("getQagInfo(%s)", id)
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func (f *fakeQags) UpdateQagStatus(_ context.Context, id string, st qag.QagStatus) (qag.QagUpdateResult, error) {
	f.log.add("updateQagStatus(%s,%s)", id, st)
	if f.updateFails {
		return qag.QagUpdateResult{}, nil
	}
	info := *f.byID[id]
	return qag.QagUpdateResult{Info: &info}, nil
}

func (f *fakeQags) UpdateQagMotifID(_ context.Context, id string, motif *string) error {
	f.log.add("updateQagMotifId(%s,%s)", id, jn(motif))
	return nil
}

func (f *fakeQags) GetQagInfoToModerateList(context.Context) ([]qag.QagInfo, error) {
	f.log.add("getQagInfoToModerateList")
	return f.toModerate, f.err
}

type fakeUpdates struct{ log *callLog }

func (f *fakeUpdates) InsertQagUpdates(_ context.Context, u qag.QagInsertingUpdates) error {
	f.log.add("insertQagUpdates(%s,%s,%s,%s,%t,%s)", u.QagID, u.NewQagStatus, u.UserID, jn(u.Reason), u.ShouldDelete, jn(u.MotifID))
	return nil
}

// fakeLocks is ModeratusQagLockRepository.
type fakeLocks struct {
	log    *callLog
	locked []string
	err    error
}

func (f *fakeLocks) GetLockedQagIDs(context.Context) ([]string, error) {
	f.log.add("getLockedQagIds")
	return f.locked, f.err
}

func (f *fakeLocks) AddLockedIDs(_ context.Context, ids []string) error {
	f.log.add("addLockedIds%v", ids)
	return nil
}

func (f *fakeLocks) RemoveLockedQagID(_ context.Context, id string) error {
	f.log.add("removeLockedQagId(%s)", id)
	return nil
}

// fakeNotifier is a QagNotifier that records the three calls.
type fakeNotifier struct {
	log *callLog
	err error
}

func (f *fakeNotifier) SendNotificationQagRejected(_ context.Context, id string) (NotificationResult, error) {
	f.log.add("sendNotificationQagRejected(%s)", id)
	return NotificationSuccess, f.err
}

func (f *fakeNotifier) SendNotificationQagAccepted(_ context.Context, id string) (NotificationResult, error) {
	f.log.add("sendNotificationQagAccepted(%s)", id)
	return NotificationSuccess, f.err
}

func (f *fakeNotifier) SendNotificationQagAcceptedAfterReject(_ context.Context, id string) (NotificationResult, error) {
	f.log.add("sendNotificationQagAcceptedAfterReject(%s)", id)
	return NotificationSuccess, f.err
}

// ---- the real notifier on fakes (calls recorded like the oracle: S7Functions "moderateFlow") ----

func acceptedMarker(s string) string {
	for _, m := range QagAcceptedNotifications {
		if s == m.Title || s == m.Message {
			return "<accepted>"
		}
	}
	return s
}

type fakeUsers struct {
	log   *callLog
	token *string // nil: user not found
	err   error
}

func (f *fakeUsers) FindUser(_ context.Context, id string) (*users.UserInfo, error) {
	f.log.add("getUserById(%s)", id)
	if f.err != nil {
		return nil, f.err
	}
	if f.token == nil {
		return nil, nil
	}
	return &users.UserInfo{UserID: id, FCMToken: *f.token}, nil
}

type fakeSender struct {
	log  *callLog
	resp fcm.BatchResponse
	err  error
}

func (f *fakeSender) SendEachForMulticast(_ context.Context, m fcm.Message) (fcm.BatchResponse, error) {
	f.log.add("sendGenericMultiNotification(%s,%s,[%s],%s,%s)", acceptedMarker(m.Title), acceptedMarker(m.Body), strings.Join(m.Tokens, ", "), m.Data["type"], m.Data["pageArgument"])
	return f.resp, f.err
}

type fakeNotifs struct {
	log *callLog
	err error
}

func (f *fakeNotifs) InsertNotification(_ context.Context, title, description, typeOrdinal, userID string) error {
	typeName := map[string]string{typeDetailsQagOrdinal: typeDetailsQagName}[typeOrdinal]
	f.log.add("insertNotifications(%s,%s,%s,[%s])", acceptedMarker(title), acceptedMarker(description), typeName, userID)
	return f.err
}

// newTestNotifier builds the real qagNotifier on fakes; the FCM batch runs inline.
func newTestNotifier(log *callLog, qags notifQagReader, token *string, pick int) *qagNotifier {
	return &qagNotifier{
		qags:    qags,
		users:   &fakeUsers{log: log, token: token},
		sender:  func() fcmSender { return &fakeSender{log: log} },
		notifs:  &fakeNotifs{log: log},
		pick:    func(int) int { return pick },
		runSend: func(f func()) { f() },
		logf:    func(string, ...any) {},
	}
}

// collapseGetQagInfo drops consecutive identical getQagInfo calls: Kotlin's SendQagNotificationUseCase reads the
// QaG twice in a row (existence check, then the author), Go reads it once.
func collapseGetQagInfo(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "getQagInfo(") && len(out) > 0 && out[len(out)-1] == c {
			continue
		}
		out = append(out, c)
	}
	return out
}

func equalStrings(a, b []string) bool { return slices.Equal(a, b) }

func sp(s string) *string { return &s }
