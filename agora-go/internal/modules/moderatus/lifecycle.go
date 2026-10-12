package moderatus

import (
	"context"
	"math/rand/v2"
	"time"

	"agora/internal/modules/qag"
)

// This file holds the periodic QaG lifecycle use cases the tasks of the slice S10 run:
// ArchiveOldQagUseCase, AnonymizeOldQagUseCase and SelectMostPopularQagUseCase (with
// RandomQagSelector), and the QaG part of WeeklyTasksHandler.

// FeatureChecker is FeatureFlagsRepository.isFeatureEnabled for one flag.
type FeatureChecker func(ctx context.Context) (bool, error)

// ArchiveQagListResult is ArchiveQagListResult.
type ArchiveQagListResult int

// The ArchiveQagListResult constants.
const (
	ArchiveSuccess ArchiveQagListResult = iota
	ArchiveFailure
)

// AnonymizeQagListResult is AnonymizeQagListResult.
type AnonymizeQagListResult int

// The AnonymizeQagListResult constants.
const (
	AnonymizeSuccess AnonymizeQagListResult = iota
	AnonymizeFailure
)

type qagArchiver interface {
	ArchiveOldQags(ctx context.Context, resetDate time.Time) error
	AnonymizeOldQags(ctx context.Context, date time.Time) error
}

// ArchiveOldQagUseCase archives the accepted QaGs of the weeks before this one.
type ArchiveOldQagUseCase struct {
	qagSelect FeatureChecker // AgoraFeature.QagSelect
	qags      qagArchiver
	now       func() time.Time
	afterSave func(ctx context.Context)
	logf      func(format string, args ...any)
}

// ArchiveOldQag is archiveOldQag: FAILURE when the QagSelect flag is off, otherwise archives (and
// anonymizes the rejected ones of) the QaGs moderated before the Monday of this week at 10:00
// (`LocalDateTime.now(clock).with(MONDAY).withHour(10).withMinute(0).withSecond(0)`: the
// nanoseconds of now are kept, the Date has millisecond precision).
func (u *ArchiveOldQagUseCase) ArchiveOldQag(ctx context.Context) (ArchiveQagListResult, error) {
	enabled, err := u.qagSelect(ctx)
	if err != nil {
		return ArchiveFailure, err
	}
	if !enabled {
		return ArchiveFailure, nil
	}
	u.logf("📜️ Archiving old QaGs...")
	if err := u.qags.ArchiveOldQags(ctx, mondayThisWeekAt10(u.now())); err != nil {
		u.afterSave(ctx)
		return ArchiveFailure, err
	}
	u.afterSave(ctx)
	u.logf("📜️ Archiving old QaGs finished !")
	return ArchiveSuccess, nil
}

// mondayThisWeekAt10 is now.with(DayOfWeek.MONDAY).withHour(10).withMinute(0).withSecond(0).
func mondayThisWeekAt10(now time.Time) time.Time {
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	return time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 10, 0, 0, now.Nanosecond(), now.Location())
}

// AnonymizeOldQagUseCase blanks the username of the archived QaGs moderated more than three weeks ago.
type AnonymizeOldQagUseCase struct {
	anonymization FeatureChecker // AgoraFeature.QagAnonymization
	qags          qagArchiver
	now           func() time.Time
	afterSave     func(ctx context.Context)
	logf          func(format string, args ...any)
}

// AnonymizeOldQag is anonymizeOldQag: FAILURE when the QagAnonymization flag is off, otherwise
// anonymizes the QaGs moderated before `now.minusWeeks(3)` (same wall-clock time).
func (u *AnonymizeOldQagUseCase) AnonymizeOldQag(ctx context.Context) (AnonymizeQagListResult, error) {
	enabled, err := u.anonymization(ctx)
	if err != nil {
		return AnonymizeFailure, err
	}
	if !enabled {
		return AnonymizeFailure, nil
	}
	u.logf("🕵️ Anonymizing old QaGs...")
	now := u.now()
	threeWeeksAgo := time.Date(now.Year(), now.Month(), now.Day()-21, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	err = u.qags.AnonymizeOldQags(ctx, threeWeeksAgo)
	u.afterSave(ctx)
	if err != nil {
		return AnonymizeFailure, err
	}
	u.logf("🕵️ Anonymizing old QaGs finished !")
	return AnonymizeSuccess, nil
}

// RandomQagSelector is RandomQagSelector.chooseRandom (List.random()).
type RandomQagSelector interface {
	ChooseRandom(qags []qag.QagInfoWithSupportCount) qag.QagInfoWithSupportCount
}

// DefaultRandomQagSelector picks one QaG uniformly at random.
type DefaultRandomQagSelector struct{}

// ChooseRandom implements RandomQagSelector.
func (DefaultRandomQagSelector) ChooseRandom(qags []qag.QagInfoWithSupportCount) qag.QagInfoWithSupportCount {
	return qags[rand.IntN(len(qags))]
}

type qagSelector interface {
	GetMostPopularQags(ctx context.Context) ([]qag.QagInfoWithSupportCount, error)
	SelectQagForResponse(ctx context.Context, qagID string) (qag.QagUpdateResult, error)
}

// SelectMostPopularQagUseCase puts the most supported accepted QaG in the SELECTED_FOR_RESPONSE status.
type SelectMostPopularQagUseCase struct {
	qagSelect FeatureChecker // AgoraFeature.QagSelect
	qags      qagSelector
	random    RandomQagSelector
	afterSave func(ctx context.Context)
	logf      func(format string, args ...any)
}

// PutMostPopularQagInSelectedStatus is putMostPopularQagInSelectedStatus: nothing when the flag is off or
// when no accepted QaG exists; the QaG is picked at random among the ties of the highest support count
// (more than one candidate), then selected. A failed selection is only logged.
func (u *SelectMostPopularQagUseCase) PutMostPopularQagInSelectedStatus(ctx context.Context) error {
	enabled, err := u.qagSelect(ctx)
	if err != nil || !enabled {
		return err
	}
	u.logf("🗳️ Selecting the most popular QaG...")
	mostPopular, err := u.qags.GetMostPopularQags(ctx)
	if err != nil {
		return err
	}
	var selected qag.QagInfoWithSupportCount
	switch len(mostPopular) {
	case 0:
		u.logf("⚠️ Select popular QaG error: no QaGs to select")
		return nil
	case 1:
		selected = mostPopular[0]
	default:
		selected = u.random.ChooseRandom(mostPopular)
	}
	u.logf("🗳️ Most popular QaG, with %d supports is : %s", selected.SupportCount, selected.Title)
	result, err := u.qags.SelectQagForResponse(ctx, selected.ID)
	u.afterSave(ctx)
	if err != nil {
		return err
	}
	if !result.Success() {
		u.logf("⚠️️ Select popular QaG error")
	}
	u.logf("🗳️ Selecting the most popular QaG finished !")
	return nil
}

// WeeklyQagTasks is the QaG part of WeeklyTasksHandler.handleTask:
//
//	val ignoreTransition = arguments?.get("force_question_selection") == "true"
//	if (ignoreTransition || isThemeHebdoTransitionUseCase.isInTransition()) { select; archive }
//	anonymize
//
// inTransition is IsThemeHebdoTransitionUseCase.isInTransition (not called when the selection is forced).
func (s *Service) WeeklyQagTasks(ctx context.Context, args map[string]string, inTransition func(ctx context.Context) (bool, error)) error {
	run := args["force_question_selection"] == "true"
	if !run {
		var err error
		if run, err = inTransition(ctx); err != nil {
			return err
		}
	}
	if run {
		if err := s.Select.PutMostPopularQagInSelectedStatus(ctx); err != nil {
			return err
		}
		if _, err := s.Archive.ArchiveOldQag(ctx); err != nil {
			return err
		}
	}
	_, err := s.Anonymize.AnonymizeOldQag(ctx)
	return err
}
