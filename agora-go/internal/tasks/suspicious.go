package tasks

import (
	"context"
	"fmt"

	"agora/internal/app"
	"agora/internal/modules/login"
	"agora/internal/modules/qag"
)

// DeleteSuspiciousSupports is DeleteSuspiciousSupportsUseCase.execute: when the
// DeleteBannedUsersSupports feature is on, the supports given during the last
// week by banned users on QaGs that are not selected are deleted.
//
// The use case is listed under S1 in parity/LEDGER.md, but its repository is the
// QaG support repository (S2) and qag imports login, so login cannot host it. It
// is ported here, next to its only caller (the daily task). The feature check is
// login.Get(a).IsFeatureEnabled, the delete is qag.Get(a).Supports.
func DeleteSuspiciousSupports(ctx context.Context, a *app.App) error {
	enabled, err := login.Get(a).IsFeatureEnabled(ctx, login.FeatureDeleteBannedUsersSupports)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	a.Log.Info("🏴 Remove supports of banned users")
	removed, err := qag.Get(a).Supports.DeleteBannedUsersLastWeekSupportsOnUnselectedQags(ctx)
	if err != nil {
		return err
	}
	a.Log.Info(fmt.Sprintf("🏴 %d banned users' supports has been removed", removed))
	return nil
}
