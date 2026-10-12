package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/modules/users"
	"agora/internal/tasks"
)

type handlers struct {
	a *app.App
}

// boolParam is `@RequestParam(defaultValue = def) x: Boolean`. An absent value or a
// single empty value takes the default. Otherwise the first value is converted with
// StringToBooleanConverter, which turns a blank text into null: with a default value
// Spring does not report that null, and the non-null Kotlin parameter fails
// (NullPointerException, 500). Other texts are a 400 (SpringBoolParam).
func boolParam(c *httpx.Ctx, name, def string) bool {
	vals := c.ParamValues(name)
	source := def
	if len(vals) > 0 && !(len(vals) == 1 && vals[0] == "") {
		source = vals[0]
	}
	if javacompat.JavaStringTrim(source) == "" {
		panic(errors.New("NullPointerException: Parameter specified as non-null is null: parameter " + name))
	}
	return httpx.SpringBoolParam(source)
}

// runWeeklyTasks is AdminTasksController.runWeeklyTasks: the weekly task runs in the
// request, with force_question_selection as the string "true" or "false".
func (h *handlers) runWeeklyTasks(c *httpx.Ctx) *httpx.Response {
	force := boolParam(c, "force_question_selection", "false")
	err := tasks.Weekly(c.Context(), h.a, map[string]string{"force_question_selection": strconv.FormatBool(force)})
	if err != nil {
		panic(err)
	}
	return httpx.String(200, "Weekly tasks exécutées avec succès")
}

// clearCache is AdminTasksController.clearCache (FLUSHDB).
func (h *handlers) clearCache(c *httpx.Ctx) *httpx.Response {
	if err := clearAllCaches(c.Context(), func(ctx context.Context) error { return h.a.Cache.FlushDB(ctx) }); err != nil {
		panic(err)
	}
	h.a.Log.Info("Cache Redis entièrement vidé (flushDb)")
	return httpx.String(200, "Cache Redis entièrement vidé avec succès (flushDb)")
}

// deleteUsers is EditUsersController.deleteUsers.
func (h *handlers) deleteUsers(c *httpx.Ctx) *httpx.Response {
	var body DeleteUsersJSON
	c.BindBody(&body)
	ids, err := nonNullUserIDs(body.UserIDsToDelete)
	if err != nil {
		panic(err)
	}
	if err := DeleteUsers(c.Context(), h.a, ids); err != nil {
		panic(err)
	}
	return httpx.Unit(200)
}

// upgradeUsersToPublisher is EditUsersController.upgradeUsersToPublisher.
func (h *handlers) upgradeUsersToPublisher(c *httpx.Ctx) *httpx.Response {
	var body UsersIDListJSON
	c.BindBody(&body)
	return h.changeLevel(c, body.UsersID, users.LevelPublisher, "promus")
}

// downgradeUsers is EditUsersController.downgradeUsers.
func (h *handlers) downgradeUsers(c *httpx.Ctx) *httpx.Response {
	var body UsersIDListJSON
	c.BindBody(&body)
	return h.changeLevel(c, body.UsersID, users.LevelDefault, "révoqués")
}

// changeLevel answers "<updated> utilisateurs <verb> sur <size of the request list>".
func (h *handlers) changeLevel(c *httpx.Ctx, rawIDs []*string, level int, verb string) *httpx.Response {
	ids, err := nonNullUserIDs(rawIDs)
	if err != nil {
		panic(err)
	}
	n, err := changeUserAuthorizationLevel(c.Context(), h.a, ids, level)
	if err != nil {
		panic(err)
	}
	return httpx.String(200, fmt.Sprintf("%d utilisateurs %s sur %d", n, verb, len(ids)))
}
