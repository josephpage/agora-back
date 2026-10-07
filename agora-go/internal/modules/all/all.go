// Package all registers every module's routes, in an order approximating
// Spring's handler registration order (only visible in Allow headers).
package all

import (
	"agora/internal/app"
	"agora/internal/modules/acme"
	"agora/internal/modules/admin"
	"agora/internal/modules/apidocs"
	"agora/internal/modules/consultation"
	"agora/internal/modules/consultationlist"
	"agora/internal/modules/consultationresponse"
	"agora/internal/modules/content"
	"agora/internal/modules/login"
	"agora/internal/modules/moderatus"
	"agora/internal/modules/notification"
	"agora/internal/modules/profile"
	"agora/internal/modules/qag"
	"agora/internal/modules/qaglist"
	"agora/internal/modules/referentiel"
	"agora/internal/modules/responseqag"
	"agora/internal/modules/thematique"
	"agora/internal/modules/themehebdo"
)

// Routes registers all HTTP routes.
func Routes(a *app.App) {
	acme.Routes(a)
	admin.Routes(a)
	content.Routes(a)
	consultation.Routes(a)
	consultationlist.Routes(a)
	consultationresponse.Routes(a)
	login.Routes(a)
	moderatus.Routes(a)
	notification.Routes(a)
	profile.Routes(a)
	qag.Routes(a)
	qaglist.Routes(a)
	referentiel.Routes(a)
	responseqag.Routes(a)
	thematique.Routes(a)
	themehebdo.Routes(a)
	apidocs.Routes(a)
}
