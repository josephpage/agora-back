package admin

// DeleteUsersJSON is DeleteUsersJson, the body of DELETE /admin/delete_users.
// The list is required (a missing or null list is a 400). Its elements are
// pointers: a JSON null element reaches the Kotlin code, where the
// `String.toUuidOrNull()` extension receives null and fails (NullPointerException,
// 500), so it must not decode to "".
type DeleteUsersJSON struct {
	UserIDsToDelete []*string `json:"userIDsToDelete"`
}

// UsersIDListJSON is UsersIdListJson, the body of the upgrade and downgrade routes.
type UsersIDListJSON struct {
	UsersID []*string `json:"usersID"`
}
