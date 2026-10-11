// Package moderatus ports the moderation of the QaGs and their lifecycle:
//
//   - the Moderatus integration (GET /moderatus/new_qags, /moderatus/ack_qags,
//     /moderatus/result_qag: XML answers, the "password" is a login token of a user
//     with the MODERATE_QAG authorization) with its lock table (moderatus_locked_qags);
//   - the admin route that sets the status of a QaG (PUT /admin/qags/{qagId}/status,
//     AdminQagController; the use case itself is qag.Service.AdminUpdateQagStatus);
//   - the periodic use cases of the QaGs that the tasks of the slice S10 call:
//     ArchiveOldQagUseCase, AnonymizeOldQagUseCase, SelectMostPopularQagUseCase
//     (+ RandomQagSelector) and the QaG part of WeeklyTasksHandler.
//
// Other slices use the exported Service (see Get):
//
//	s := moderatus.Get(a)
//	s.Archive.ArchiveOldQag(ctx)                   // ArchiveOldQagUseCase
//	s.Anonymize.AnonymizeOldQag(ctx)               // AnonymizeOldQagUseCase
//	s.Select.PutMostPopularQagInSelectedStatus(ctx) // SelectMostPopularQagUseCase
//	s.WeeklyQagTasks(ctx, args, inTransition)      // WeeklyTasksHandler.handleTask
//
// See parity/ledger/S7.md.
package moderatus

import (
	"context"
	"fmt"

	"agora/internal/app"
	"agora/internal/modules/login"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// Cache names of the micro-cached shared QaG lists of the slice S3 (qaglist: pages of the
// top / latest tabs and the count of accepted QaGs). Kotlin has no such cache; a status change
// drops them on every instance so a moderated QaG appears in / disappears from the lists at once
// instead of within AGORA_MICROCACHE_TTL.
const (
	qagListPageCache  = "qagListPage"
	qagListCountCache = "qagListCount"
)

// Service gathers the use cases of the slice.
type Service struct {
	a *app.App

	Login     *ModeratusLoginUseCase
	List      *GetModeratusQagListUseCase
	Lock      *LockModeratusQagListUseCase
	Moderate  *ModerateModeratusQagUseCase
	Archive   *ArchiveOldQagUseCase
	Anonymize *AnonymizeOldQagUseCase
	Select    *SelectMostPopularQagUseCase
	Locks     *LockRepository

	qags *qag.Service
}

// Get returns the App-wide service.
func Get(a *app.App) *Service {
	return app.Singleton(a, "moderatus", func() *Service { return build(a) })
}

func build(a *app.App) *Service {
	q := qag.Get(a)
	lg := login.Get(a)
	u := users.Get(a)
	locks := &LockRepository{a: a}
	s := &Service{a: a, Locks: locks, qags: q}

	logf := func(format string, args ...any) { logInfo(a, format, args...) }
	flag := func(f login.Feature) FeatureChecker {
		return func(ctx context.Context) (bool, error) { return lg.IsFeatureEnabled(ctx, f) }
	}
	evict := func(ctx context.Context) { EvictQagLists(ctx, a) }

	s.Login = &ModeratusLoginUseCase{tokens: a.LoginTokens, users: u}
	s.List = &GetModeratusQagListUseCase{qags: q.Info}
	s.Lock = &LockModeratusQagListUseCase{qags: q.Info, locks: locks}
	s.Moderate = &ModerateModeratusQagUseCase{
		qags:              q.Info,
		notifier:          newQagNotifier(a, q.Info, u),
		updates:           q.Updates,
		locks:             locks,
		afterStatusChange: evict,
	}
	s.Archive = &ArchiveOldQagUseCase{qagSelect: flag(login.FeatureQagSelect), qags: q.Info, now: a.Now, afterSave: evict, logf: logf}
	s.Anonymize = &AnonymizeOldQagUseCase{anonymization: flag(login.FeatureQagAnonymization), qags: q.Info, now: a.Now, afterSave: evict, logf: logf}
	s.Select = &SelectMostPopularQagUseCase{qagSelect: flag(login.FeatureQagSelect), qags: q.Info, random: DefaultRandomQagSelector{}, afterSave: evict, logf: logf}
	return s
}

// SetNotifier replaces the notifier of the moderation (see QagNotifier): the integration of the
// notifications slice plugs its SendQagNotificationUseCase in with it.
func (s *Service) SetNotifier(n QagNotifier) { s.Moderate.notifier = n }

// SetRandomQagSelector replaces the RandomQagSelector (tests).
func (s *Service) SetRandomQagSelector(r RandomQagSelector) { s.Select.random = r }

// EvictQagLists drops the micro-cached shared QaG lists and count of the slice S3 on every
// instance (a status change moves a QaG in or out of the accepted ones).
func EvictQagLists(ctx context.Context, a *app.App) {
	a.Cache.InvalidateAll(ctx, qagListPageCache)
	a.Cache.InvalidateAll(ctx, qagListCountCache)
}

func logInfo(a *app.App, format string, args ...any) {
	if a.Log == nil {
		return
	}
	a.Log.Info(fmt.Sprintf(format, args...))
}
