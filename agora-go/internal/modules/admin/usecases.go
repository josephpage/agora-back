package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agora/internal/app"
	"agora/internal/javacompat"
	"agora/internal/modules/consultationresponse"
	"agora/internal/modules/login"
	"agora/internal/modules/notification"
	"agora/internal/modules/profile"
	"agora/internal/modules/qag"
	"agora/internal/modules/users"
)

// clearAllCaches is ClearCacheUseCase.clearAllCaches: Redis FLUSHDB. It removes the
// Kotlin keys too (the same command), and FlushDB publishes the flush to every Go
// instance, which drops its L1 entries (the epoch sentinel does the same for the
// instances that miss the message). The Kotlin connection is closed after the
// flush: there is no per-request connection on the Go side.
func clearAllCaches(ctx context.Context, flush func(ctx context.Context) error) error {
	return flush(context.WithoutCancel(ctx))
}

// deletionStep is one repository call of DeleteUsersUseCase.
type deletionStep struct {
	name string
	run  func(ctx context.Context, userIDs []string) error
}

// deleteUsersInOrder runs the steps in order. Every step commits on its own (each
// Kotlin repository method is its own @Transactional, the use case is not), so a
// failing step leaves the earlier ones committed, as in Kotlin.
func deleteUsersInOrder(ctx context.Context, steps []deletionStep, userIDs []string) error {
	for _, s := range steps {
		if err := s.run(ctx, userIDs); err != nil {
			return err
		}
	}
	return nil
}

// deleteUserSteps are the deletions of DeleteUsersUseCase.deleteUsers, in its order.
func deleteUserSteps(a *app.App) []deletionStep {
	return []deletionStep{
		{"notifications", notification.Get(a).DeleteUsersNotifications},
		{"consultation responses", withUUIDsOnly(consultationresponse.Get(a).DeleteUserConsultationResponses)},
		{"supports", func(ctx context.Context, ids []string) error {
			return qag.Get(a).Supports.DeleteUsersSupportQag(ctx, ids)
		}},
		{"qags", func(ctx context.Context, ids []string) error { return qag.Get(a).Info.DeleteUsersQag(ctx, ids) }},
		{"feedbacks", func(ctx context.Context, ids []string) error {
			return qag.Get(a).Feedbacks.DeleteUsersFeedbackQag(ctx, ids)
		}},
		{"profiles", profile.Get(a).DeleteUsersProfile},
		{"users data", login.Get(a).DeleteUsersData},
		// users.Service.DeleteUsers also evicts the cached principals of the users
		{"users", users.Get(a).DeleteUsers},
	}
}

// DeleteUsers is DeleteUsersUseCase.deleteUsers. The ids are the raw strings of
// the body: each step keeps the UUIDs only (toUuidOrNull), so a malformed id is
// simply not deleted anywhere. An empty list changes nothing.
func DeleteUsers(ctx context.Context, a *app.App, userIDs []string) error {
	ctx = context.WithoutCancel(ctx)
	a.Log.Info(fmt.Sprintf("💁❌ Delete %d user(s) with ID:\n%s", len(userIDs), strings.Join(userIDs, "\n")))
	if len(userIDs) == 0 {
		return nil
	}
	return deleteUsersInOrder(ctx, deleteUserSteps(a), userIDs)
}

// changeUserAuthorizationLevel is UpgradeUsersToPublisherUseCase.execute and
// DowngradeUsersUseCase.execute: the number of rows updated by the UPDATE.
func changeUserAuthorizationLevel(ctx context.Context, a *app.App, userIDs []string, level int) (int, error) {
	return users.Get(a).ChangeAuthorizationLevel(context.WithoutCancel(ctx), userIDs, level)
}

// withUUIDsOnly skips the step when no id is a UUID. The reference runs the empty `IN` of
// the consultation responses delete without error (DELETE /admin/delete_users with
// ["not-a-uuid"] answers 200 and changes nothing), while consultationresponse raises a
// syntax error for an empty list (S6): with nothing to delete, the call is skipped.
func withUUIDsOnly(step func(ctx context.Context, userIDs []string) error) func(ctx context.Context, userIDs []string) error {
	return func(ctx context.Context, userIDs []string) error {
		if !hasUserUUID(userIDs) {
			return nil
		}
		return step(ctx, userIDs)
	}
}

// hasUserUUID tells whether one of the ids is a UUID (toUuidOrNull is not null).
func hasUserUUID(ids []string) bool {
	for _, id := range ids {
		if _, ok := javacompat.ToUUIDOrNull(id); ok {
			return true
		}
	}
	return false
}

// errNullUserID is the NullPointerException of `String.toUuidOrNull()` (UuidUtils)
// applied to a null element of a request list: the exception is raised by the
// first repository call, before any write, so it is a 500 with no change.
var errNullUserID = errors.New("NullPointerException: Parameter specified as non-null is null: method UuidUtils.toUuidOrNull, parameter <this>")

// nonNullUserIDs returns the ids of a request list, or the Kotlin NullPointerException.
func nonNullUserIDs(ids []*string) ([]string, error) {
	out := make([]string, len(ids))
	for i, id := range ids {
		if id == nil {
			return nil, errNullUserID
		}
		out[i] = *id
	}
	return out, nil
}
