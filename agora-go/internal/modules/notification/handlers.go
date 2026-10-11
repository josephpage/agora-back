package notification

import (
	"errors"

	"agora/internal/app"
	"agora/internal/domain"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/modules/consultation"
)

type handlers struct {
	a   *app.App
	svc *Service
}

// The @RequestParam default values of the controllers, character for character.
const (
	defaultUserTitle          = "Une nouvelle consultation est disponible"
	defaultUserDescription    = "Venez découvrir la nouvelle consultation sur l'éducation"
	defaultDepartmentDescr    = "Venez découvrir la nouvelle consultation sur l'Ain"
	defaultAllUsersType       = "Type de la notification pour rediriger l'utilisateur vers la bonne page ALL_REPONSES_QAGS, HOME_QAGS, DETAILS_QAG, HOME_CONSULTATIONS, DETAILS_CONSULTATION, REPONSE_SUPPORT"
	defaultDepartmentType     = "HOME_QAGS"
	defaultRouteArgument      = "f55a68a7-8ec3-46c8-ba6b-1484fbd77ec6"
	defaultUpdateTitle        = "L'analyse de la consultation sur l'éducation est disponible"
	defaultUpdateDescription  = "Venez découvrir ce que les gens ont pensé de la consultation sur l'éducation"
	defaultDeadlineTitle      = "Attention, la consultation sur le l'éducation se termine le 29 septembre"
	defaultDeadlineDescription = "Répondez à la consultation sur l'éducation avant qu'elle ne se termine"
)

// raise panics with what DefaultControllerAdvice and Spring make of an exception of the use cases.
func raise(err error) {
	var notFound *consultation.ConsultationNotFoundError
	if errors.As(err, &notFound) {
		consultation.Raise(err)
	}
	Raise(err)
}

func must(res Result, err error) Result {
	if err != nil {
		raise(err)
	}
	return res
}

// result is the `when (result)` of the controllers.
func result(res Result) *httpx.Response {
	switch res {
	case Success:
		return httpx.Unit(200)
	default:
		return httpx.Unit(400)
	}
}

// typeParam is `@RequestParam("type", defaultValue = def) type: TypeNotification`: an absent or empty
// parameter takes the default value; several values are an array converted to the enum through its
// first element (ArrayToObjectConverter); the text is converted with Enum.valueOf(source.trim()), an
// empty text is a null (missing parameter). Any failure is a 400 Spring error body.
func typeParam(c *httpx.Ctx, name, def string) TypeNotification {
	vals := c.ParamValues(name)
	var source string
	switch {
	case len(vals) == 0, len(vals) == 1 && vals[0] == "":
		source = def
	default:
		source = vals[0]
	}
	if source == "" {
		panic(&httpx.SpringError{Status: 400, Cause: "MissingServletRequestParameterException: " + name})
	}
	t, ok := TypeByName(javacompat.JavaStringTrim(source))
	if !ok {
		panic(&httpx.SpringError{Status: 400, Cause: "MethodArgumentTypeMismatchException: " + name})
	}
	return t
}

// consultationUpdateNotification is ConsultationNotificationController.consultationUpdateNotification.
func (h *handlers) consultationUpdateNotification(c *httpx.Ctx) *httpx.Response {
	title := c.ParamDefault("title", defaultUpdateTitle)
	description := c.ParamDefault("description", defaultUpdateDescription)
	consultationID := c.PathVar("consultationId")
	return result(must(h.svc.SendConsultationUpdate(c.Context(), title, description, consultationID)))
}

// consultationDeadlineNotification is ConsultationNotificationController.consultationDeadlineNotification.
func (h *handlers) consultationDeadlineNotification(c *httpx.Ctx) *httpx.Response {
	title := c.ParamDefault("title", defaultDeadlineTitle)
	description := c.ParamDefault("description", defaultDeadlineDescription)
	consultationID := c.PathVar("consultationId")
	return result(must(h.svc.SendConsultationDeadline(c.Context(), title, description, consultationID)))
}

// getNotificationPaginatedList is NotificationPaginatedController.getNotificationPaginatedList.
func (h *handlers) getNotificationPaginatedList(c *httpx.Ctx) *httpx.Response {
	pageNumber := c.PathVar("pageNumber")
	userID := c.UserID()
	page, ok := javacompat.KotlinToIntOrNull(pageNumber)
	if !ok {
		return httpx.Unit(400)
	}
	notifications, err := h.svc.GetNotificationPaginated(c.Context(), userID, page)
	if err != nil {
		panic(err)
	}
	if notifications == nil {
		return httpx.Unit(400)
	}
	return httpx.OK(ToJSON(notifications))
}

// notifyUser is NotificationUserController.notifyUser.
func (h *handlers) notifyUser(c *httpx.Ctx) *httpx.Response {
	title := c.ParamDefault("title", defaultUserTitle)
	description := c.ParamDefault("description", defaultUserDescription)
	userID := c.PathVar("userId")
	return result(must(h.svc.SendUser(c.Context(), title, description, userID)))
}

// notifyAllUsers is NotificationUserController.notifyAllUsers. Its `type` default value is not an
// enum constant: without a `type` parameter the conversion fails (400).
func (h *handlers) notifyAllUsers(c *httpx.Ctx) *httpx.Response {
	title := c.ParamDefault("title", defaultUserTitle)
	description := c.ParamDefault("description", defaultUserDescription)
	typ := typeParam(c, "type", defaultAllUsersType)
	pageArgument := c.ParamDefault("routeArgument", defaultRouteArgument)
	return result(must(h.svc.SendUsers(c.Context(), title, description, &pageArgument, typ)))
}

// notifyUsersInDepartement is NotificationUserController.notifyUsersInDepartement.
func (h *handlers) notifyUsersInDepartement(c *httpx.Ctx) *httpx.Response {
	codeDepartement := c.PathVar("codeDepartement")
	title := c.ParamDefault("title", defaultUserTitle)
	description := c.ParamDefault("description", defaultDepartmentDescr)
	typ := typeParam(c, "type", defaultDepartmentType)
	pageArgument := c.ParamDefault("routeArgument", defaultRouteArgument)
	departement := domain.FindDepartmentByCode(&codeDepartement)
	if departement == nil {
		return httpx.Unit(400)
	}
	return result(must(h.svc.SendDepartement(c.Context(), departement, title, description, &pageArgument, typ)))
}
