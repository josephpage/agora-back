package content

import (
	"agora/internal/app"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/store"
)

// AppFeedbackJSON is infrastructure.appFeedback.AppFeedbackJson (request body).
type AppFeedbackJSON struct {
	Type        string                     `json:"type"`
	Description string                     `json:"description"`
	DeviceInfo  *AppFeedbackDeviceInfoJSON `json:"deviceInfo"`
}

// AppFeedbackDeviceInfoJSON is AppFeedbackDeviceInfoJson.
type AppFeedbackDeviceInfoJSON struct {
	Model      string `json:"model"`
	OsVersion  string `json:"osVersion"`
	AppVersion string `json:"appVersion"`
}

// appFeedbackType is domain.AppFeedbackType, stored as "bug" / "feature" / "comment".
type appFeedbackType string

// appFeedbackInserting is domain.AppFeedbackInserting.
type appFeedbackInserting struct {
	userID      string
	typ         appFeedbackType
	description string
	deviceInfo  *AppFeedbackDeviceInfoJSON
}

// feedbackToDomain is AppFeedbackJsonMapper.toDomain: the type is matched on
// `lowercase()` (Locale.ROOT) of the JSON value, any other value gives null (HTTP 400).
func feedbackToDomain(j AppFeedbackJSON, userID string) (appFeedbackInserting, bool) {
	var typ appFeedbackType
	switch javacompat.KotlinLowercase(j.Type) {
	case "bug":
		typ = "bug"
	case "feature":
		typ = "feature"
	case "comment":
		typ = "comment"
	default:
		return appFeedbackInserting{}, false
	}
	return appFeedbackInserting{userID: userID, typ: typ, description: j.Description, deviceInfo: j.DeviceInfo}, true
}

// insertAppFeedback is AppFeedbackRepositoryImpl.insertAppFeedback +
// AppFeedbackMapper.toDto: false when the user id is not a UUID (never the case
// for an authenticated user); the database errors are uncaught (HTTP 500).
//
// Kotlin `save` of an entity with the preset id 0000… is a merge: a SELECT by
// that id finds nothing, then an INSERT with a freshly generated random UUID.
func insertAppFeedback(a *app.App, c *httpx.Ctx, fb appFeedbackInserting) bool {
	userUUID, ok := javacompat.ToUUIDOrNull(fb.userID)
	if !ok {
		return false
	}
	var model, osVersion, appVersion *string
	if fb.deviceInfo != nil {
		model, osVersion, appVersion = &fb.deviceInfo.Model, &fb.deviceInfo.OsVersion, &fb.deviceInfo.AppVersion
	}
	// created_date is a java.util.Date (millisecond precision)
	_, err := a.DB.Pool.Exec(c.Context(),
		"INSERT INTO app_feedbacks (id, app_version, created_date, description, device_model, os_version, type, user_id) "+
			"VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)",
		appVersion, store.Millis(a.Now()), fb.description, model, osVersion, string(fb.typ), userUUID)
	if err != nil {
		panic(err)
	}
	return true
}

// feedbackHandler is AppFeedbackController.insertAppFeedback (POST /feedback;
// authentication is required by the security rules, anyRequest().authenticated()).
func feedbackHandler(a *app.App) httpx.HandlerFunc {
	return func(c *httpx.Ctx) *httpx.Response {
		var body AppFeedbackJSON
		c.BindBody(&body) // @RequestBody is bound first
		fb, ok := feedbackToDomain(body, c.UserID())
		if !ok || !insertAppFeedback(a, c, fb) {
			if mediaTypeUnacceptable(c) {
				// the status of a ResponseEntity >= 400 survives a failed negotiation (see notFoundAdvice)
				return httpx.Empty(400)
			}
			return httpx.Unit(400) // ResponseEntity.badRequest().body(Unit)
		}
		return httpx.Unit(200) // ResponseEntity.ok().body(Unit)
	}
}
