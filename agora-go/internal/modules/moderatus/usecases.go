package moderatus

import (
	"context"
	"errors"
	"slices"
	"time"

	"agora/internal/auth"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// ---------------------------------------------------------------------------
// Domain (ModeratusQag, ModeratusQagLockResult, ModeratusQagModerateResult)
// ---------------------------------------------------------------------------

// ModeratusQag is domain/ModeratusQag.
type ModeratusQag struct {
	QagID       string
	Title       string
	Description string
	Date        time.Time
	Username    string
	UserID      string
}

// QagLockResult is domain/QagLockResult.
type QagLockResult int

// The QagLockResult constants.
const (
	LockSuccess QagLockResult = iota
	LockNotFound
)

// ModeratusQagLockResult is domain/ModeratusQagLockResult.
type ModeratusQagLockResult struct {
	QagID      string
	LockResult QagLockResult
}

// ModeratusQagModerateResult is domain/ModeratusQagModerateResult.
type ModeratusQagModerateResult int

// The ModeratusQagModerateResult constants.
const (
	ModerateSuccess ModeratusQagModerateResult = iota
	ModerateNotFound
	ModerateFailure
)

// ---------------------------------------------------------------------------
// ModeratusLoginUseCase
// ---------------------------------------------------------------------------

type loginTokenDecoder interface {
	Decode(token string) (string, error)
}

type userFinder interface {
	FindUser(ctx context.Context, userID string) (*users.UserInfo, error)
}

// ModeratusLoginResult is the sealed class ModeratusLoginResult: UserID is only meaningful on success.
type ModeratusLoginResult struct {
	Success bool
	UserID  string
}

// ModeratusLoginUseCase decodes the login token (the "password" of Moderatus) and
// requires the MODERATE_QAG authorization.
type ModeratusLoginUseCase struct {
	tokens loginTokenDecoder
	users  userFinder
}

// Login is login(loginToken). A user lookup error is an exception (500).
func (u *ModeratusLoginUseCase) Login(ctx context.Context, loginToken string) (ModeratusLoginResult, error) {
	userID, err := u.tokens.Decode(loginToken)
	if err != nil {
		if errors.Is(err, auth.ErrLoginTokenDecode) {
			return ModeratusLoginResult{}, nil
		}
		return ModeratusLoginResult{}, err
	}
	user, err := u.users.FindUser(ctx, userID)
	if err != nil {
		return ModeratusLoginResult{}, err
	}
	if user == nil || !slices.Contains(user.Authorizations, httpx.AuthModerateQag) {
		return ModeratusLoginResult{}, nil
	}
	return ModeratusLoginResult{Success: true, UserID: user.UserID}, nil
}

// ---------------------------------------------------------------------------
// GetModeratusQagListUseCase (file GetModeratusQagModeratingUseCase.kt) + ModeratusQagMapper
// ---------------------------------------------------------------------------

type qagToModerateReader interface {
	GetQagInfoToModerateList(ctx context.Context) ([]qag.QagInfo, error)
}

// GetModeratusQagListUseCase lists the open QaGs that Moderatus has not locked yet.
type GetModeratusQagListUseCase struct{ qags qagToModerateReader }

// GetQagModeratingList is getQagModeratingList.
func (u *GetModeratusQagListUseCase) GetQagModeratingList(ctx context.Context) ([]ModeratusQag, error) {
	infos, err := u.qags.GetQagInfoToModerateList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModeratusQag, len(infos))
	for i, q := range infos {
		out[i] = ModeratusQag{QagID: q.ID, Title: q.Title, Description: q.Description, Date: q.Date, Username: q.Username, UserID: q.UserID}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// LockModeratusQagListUseCase
// ---------------------------------------------------------------------------

type qagInfoReader interface {
	GetQagInfo(ctx context.Context, qagID string) (*qag.QagInfo, error)
}

// ModeratusQagLockRepository is usecase/moderatus/repository/ModeratusQagLockRepository.
type ModeratusQagLockRepository interface {
	GetLockedQagIDs(ctx context.Context) ([]string, error)
	AddLockedIDs(ctx context.Context, lockedQagIDs []string) error
	RemoveLockedQagID(ctx context.Context, qagID string) error
}

// LockModeratusQagListUseCase answers the acknowledgement of Moderatus.
type LockModeratusQagListUseCase struct {
	qags  qagInfoReader
	locks ModeratusQagLockRepository
}

// LockQagIDs is lockQagIds: for each distinct id (compared as written), SUCCESS when the QaG is open and
// its id (as written, not normalised) is not among the locked ones; the successes are then locked in one write.
func (u *LockModeratusQagListUseCase) LockQagIDs(ctx context.Context, lockedQagIDs []string) ([]ModeratusQagLockResult, error) {
	locked, err := u.locks.GetLockedQagIDs(ctx)
	if err != nil {
		return nil, err
	}
	var distinct []string
	for _, id := range lockedQagIDs {
		if !slices.Contains(distinct, id) {
			distinct = append(distinct, id)
		}
	}
	results := make([]ModeratusQagLockResult, 0, len(distinct))
	for _, id := range distinct {
		info, err := u.qags.GetQagInfo(ctx, id)
		if err != nil {
			return nil, err
		}
		res := LockNotFound
		if info != nil && info.Status == qag.StatusOpen && !slices.Contains(locked, id) {
			res = LockSuccess
		}
		results = append(results, ModeratusQagLockResult{QagID: id, LockResult: res})
	}
	var additional []string
	for _, r := range results {
		if r.LockResult == LockSuccess {
			additional = append(additional, r.QagID)
		}
	}
	if len(additional) > 0 {
		if err := u.locks.AddLockedIDs(ctx, additional); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// ModerateQagOptionsMapper
// ---------------------------------------------------------------------------

// ModerateQagOptions is usecase/moderatus/ModerateQagOptions.
type ModerateQagOptions struct {
	QagID        string
	UserID       string
	IsAccepted   bool
	Reason       *string
	ShouldDelete bool
	MotifID      *string
}

// OptionsResult is ModerateQagOptionsMapper.Result: Error is set on Result.Error.
type OptionsResult struct {
	Options ModerateQagOptions
	Error   *string
}

// ToModerateQagOptions is ModerateQagOptionsMapper.toModerateQagOptions: "OK" accepts, "NOK" rejects, any
// other status is an error; the QaG is deleted only when it is rejected and the flag is 1.
func ToModerateQagOptions(qagID, userID, status string, reason *string, shouldDeleteFlag *int, motifID *string) OptionsResult {
	var isAccepted bool
	switch status {
	case "OK":
		isAccepted = true
	case "NOK":
		isAccepted = false
	default:
		msg := "Status invalide"
		return OptionsResult{Error: &msg}
	}
	return OptionsResult{Options: ModerateQagOptions{
		QagID:        qagID,
		UserID:       userID,
		IsAccepted:   isAccepted,
		Reason:       reason,
		ShouldDelete: !isAccepted && shouldDeleteFlag != nil && *shouldDeleteFlag == 1,
		MotifID:      motifID,
	}}
}

// ---------------------------------------------------------------------------
// ModerateModeratusQagUseCase
// ---------------------------------------------------------------------------

type qagModerationStore interface {
	GetQagInfo(ctx context.Context, qagID string) (*qag.QagInfo, error)
	UpdateQagStatus(ctx context.Context, qagID string, newStatus qag.QagStatus) (qag.QagUpdateResult, error)
	UpdateQagMotifID(ctx context.Context, qagID string, motifID *string) error
}

type qagUpdatesWriter interface {
	InsertQagUpdates(ctx context.Context, u qag.QagInsertingUpdates) error
}

// ModerateModeratusQagUseCase applies the decision of a moderator.
type ModerateModeratusQagUseCase struct {
	qags     qagModerationStore
	notifier QagNotifier
	updates  qagUpdatesWriter
	locks    ModeratusQagLockRepository
	// afterStatusChange runs once the new status is committed: it drops the shared micro-cached
	// QaG lists / count (Kotlin has no such cache, see evict.go).
	afterStatusChange func(ctx context.Context)
}

// ErrCannotModerate is the IllegalStateException of notifyUpdateIfRequired (unreachable: only
// OPEN, MODERATED_ACCEPTED and MODERATED_REJECTED QaGs get there).
var ErrCannotModerate = errors.New("IllegalStateException: Can not moderate qag with status")

// ModerateQag is moderateQag: NOT_FOUND for an unknown QaG or one that is neither open nor moderated.
func (u *ModerateModeratusQagUseCase) ModerateQag(ctx context.Context, o ModerateQagOptions) (ModeratusQagModerateResult, error) {
	info, err := u.qags.GetQagInfo(ctx, o.QagID)
	if err != nil {
		return ModerateFailure, err
	}
	if info == nil {
		return ModerateNotFound, nil
	}
	switch info.Status {
	case qag.StatusOpen, qag.StatusModeratedAccepted, qag.StatusModeratedRejected:
		return u.changeQagStatus(ctx, *info, o)
	}
	return ModerateNotFound, nil
}

func (u *ModerateModeratusQagUseCase) changeQagStatus(ctx context.Context, info qag.QagInfo, o ModerateQagOptions) (ModeratusQagModerateResult, error) {
	newStatus := qag.StatusModeratedRejected
	if o.IsAccepted {
		newStatus = qag.StatusModeratedAccepted
	}

	var update qag.QagUpdateResult
	if info.Status != newStatus {
		var err error
		update, err = u.qags.UpdateQagStatus(ctx, info.ID, newStatus)
		if u.afterStatusChange != nil {
			u.afterStatusChange(ctx)
		}
		if err != nil {
			return ModerateFailure, err
		}
	} else {
		update = qag.QagUpdateResult{Info: &info}
	}
	if !update.Success() {
		return ModerateFailure, nil
	}

	if err := u.notifyUpdateIfRequired(ctx, info, o.IsAccepted); err != nil {
		return ModerateFailure, err
	}
	if err := u.qags.UpdateQagMotifID(ctx, info.ID, o.MotifID); err != nil {
		return ModerateFailure, err
	}
	if err := u.updates.InsertQagUpdates(ctx, qag.QagInsertingUpdates{
		QagID:        info.ID,
		NewQagStatus: newStatus,
		UserID:       o.UserID,
		Reason:       o.Reason,
		ShouldDelete: o.ShouldDelete,
		MotifID:      o.MotifID,
	}); err != nil {
		return ModerateFailure, err
	}
	return ModerateSuccess, nil
}

// notifyUpdateIfRequired looks at the status the QaG had BEFORE the moderation.
func (u *ModerateModeratusQagUseCase) notifyUpdateIfRequired(ctx context.Context, info qag.QagInfo, isAccepted bool) error {
	switch info.Status {
	case qag.StatusOpen:
		var err error
		if isAccepted {
			_, err = u.notifier.SendNotificationQagAccepted(ctx, info.ID)
		} else {
			_, err = u.notifier.SendNotificationQagRejected(ctx, info.ID)
		}
		if err != nil {
			return err
		}
		return u.locks.RemoveLockedQagID(ctx, info.ID)
	case qag.StatusModeratedAccepted:
		if !isAccepted {
			_, err := u.notifier.SendNotificationQagRejected(ctx, info.ID)
			return err
		}
	case qag.StatusModeratedRejected:
		if isAccepted {
			_, err := u.notifier.SendNotificationQagAcceptedAfterReject(ctx, info.ID)
			return err
		}
	default:
		return ErrCannotModerate
	}
	return nil
}

// splitLockedQagIDs is the parsing of ModeratusLockQagController: split on ";" (a plain string),
// blank elements dropped, the others trimmed (Kotlin trim: Char.isWhitespace).
func splitLockedQagIDs(lockedQagIDs string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(lockedQagIDs); i++ {
		if i == len(lockedQagIDs) || lockedQagIDs[i] == ';' {
			part := lockedQagIDs[start:i]
			if !javacompat.KotlinIsBlank(part) {
				out = append(out, javacompat.KotlinTrim(part))
			}
			start = i + 1
		}
	}
	return out
}
