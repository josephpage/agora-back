package moderatus

import (
	"fmt"

	"agora/internal/common"
	"agora/internal/httpx"
	"agora/internal/javacompat"
	"agora/internal/modules/qag"
)

type handlers struct{ svc *Service }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// Error messages of the controllers.
const (
	loginFailedMessage       = "Password invalide: aucun compte associé ou utilisateur non autorisé"
	lockLoginFailedMessage   = "Password invalide"
	moderateDatabaseErrorMsg = "Error lors de la modification en base de données"
)

// getQagToModerate is ModeratusGetQagController.getQagToModerate (GET /moderatus/new_qags).
func (h *handlers) getQagToModerate(c *httpx.Ctx) *httpx.Response {
	loginToken := c.RequiredParam("password")
	c.RequiredParam("mediaType")

	ctx := c.Context()
	if !must(h.svc.Login.Login(ctx, loginToken)).Success {
		return httpx.JSON(401, ModeratusQagListErrorXML{ErrorMessage: loginFailedMessage})
	}
	return httpx.OK(toListXML(must(h.svc.List.GetQagModeratingList(ctx))))
}

// toListXML is ModeratusQagListXmlMapper.toXml.
func toListXML(qags []ModeratusQag) ModeratusQagListXML {
	list := make([]ModeratusQagXML, len(qags))
	for i, q := range qags {
		list[i] = ModeratusQagXML{
			QagID:       q.QagID,
			PostDate:    common.FormatDate(q.Date),
			UserID:      q.UserID,
			Username:    q.Username,
			Title:       q.Title,
			Description: q.Description,
			MessageType: messageTypeQuestion,
		}
	}
	return ModeratusQagListXML{QagToModerateCount: len(qags), QagsToModerate: list}
}

// lockQag is ModeratusLockQagController.lockQag (GET /moderatus/ack_qags).
func (h *handlers) lockQag(c *httpx.Ctx) *httpx.Response {
	loginToken := c.RequiredParam("password")
	c.RequiredParam("mediaType")
	lockedQagIDs := c.RequiredParam("content_id")

	lockedQagIDList := splitLockedQagIDs(lockedQagIDs)

	ctx := c.Context()
	if !must(h.svc.Login.Login(ctx, loginToken)).Success {
		return httpx.JSON(401, lockResultsXML(errorLockResults(lockedQagIDList, lockLoginFailedMessage)))
	}
	return httpx.OK(lockResultsXML(toLockResultsXML(must(h.svc.Lock.LockQagIDs(ctx, lockedQagIDList)))))
}

// lockResultsXML wraps the confirmations; an empty list is the field-less <contents/> (see emptyLockResultsXML).
func lockResultsXML(results []ModeratusQagLockResultXML) any {
	if len(results) == 0 {
		return emptyLockResultsXML{}
	}
	return ModeratusQagLockResultsXML{QagLockedResults: results}
}

// errorLockResults is ModeratusQagLockResultXmlMapper.toErrorXml.
func errorLockResults(lockedQagIDs []string, errorMessage string) []ModeratusQagLockResultXML {
	out := make([]ModeratusQagLockResultXML, len(lockedQagIDs))
	for i, id := range lockedQagIDs {
		msg := errorMessage
		out[i] = ModeratusQagLockResultXML{QagID: id, Result: "ERROR", Comment: &msg}
	}
	return out
}

// toLockResultsXML is ModeratusQagLockResultXmlMapper.toXml.
func toLockResultsXML(results []ModeratusQagLockResult) []ModeratusQagLockResultXML {
	out := make([]ModeratusQagLockResultXML, len(results))
	for i, r := range results {
		res := "NOTFOUND"
		if r.LockResult == LockSuccess {
			res = "OK"
		}
		out[i] = ModeratusQagLockResultXML{QagID: r.QagID, Result: res}
	}
	return out
}

// moderateQag is ModeratusModerateQagController.lockQag (GET /moderatus/result_qag).
func (h *handlers) moderateQag(c *httpx.Ctx) *httpx.Response {
	loginToken := c.RequiredParam("password")
	c.RequiredParam("mediaType")
	qagID := c.RequiredParam("content_id")
	status := c.RequiredParam("status")
	reason := c.OptionalParam("motif")
	shouldDeleteFlag := c.OptionalParam("kill")
	motifID := c.OptionalParam("motif_id")

	ctx := c.Context()
	login := must(h.svc.Login.Login(ctx, loginToken))
	if !login.Success {
		return httpx.JSON(401, toModerateResultXML(ModerateFailure, loginFailedMessage))
	}

	flag := 0
	if shouldDeleteFlag != nil {
		if n, ok := javacompat.KotlinToIntOrNull(*shouldDeleteFlag); ok {
			flag = n
		}
	}
	options := ToModerateQagOptions(qagID, login.UserID, status, reason, &flag, motifID)
	if options.Error != nil {
		return httpx.JSON(400, toModerateResultXML(ModerateFailure, *options.Error))
	}

	result := must(h.svc.Moderate.ModerateQag(ctx, options.Options))
	return httpx.OK(toModerateResultXML(result, moderateDatabaseErrorMsg))
}

// toModerateResultXML is ModeratusQagModerateResultPageXmlMapper.toXml (and toErrorXml with FAILURE).
func toModerateResultXML(result ModeratusQagModerateResult, errorMessage string) ModeratusQagModerateResultPageXML {
	switch result {
	case ModerateSuccess:
		return ModeratusQagModerateResultPageXML{Result: "OK"}
	case ModerateNotFound:
		return ModeratusQagModerateResultPageXML{Result: "NOTFOUND"}
	}
	return ModeratusQagModerateResultPageXML{Result: "ERROR", Error: &errorMessage}
}

// updateQagStatus is AdminQagController.updateQagStatus (PUT /admin/qags/{qagId}/status).
func (h *handlers) updateQagStatus(c *httpx.Ctx) *httpx.Response {
	qagID := c.PathVar("qagId")
	// `status: QagStatus` is an enum, not a String: a repeated parameter is converted from its FIRST value
	// (Spring's ArrayToObjectConverter), it is not joined with "," like a String parameter.
	raw, ok := c.Param("status")
	if !ok {
		panic(&httpx.SpringError{Status: 400, Cause: "MissingServletRequestParameterException: status"})
	}
	status := springQagStatus(raw)

	ctx := c.Context()
	result := must(h.svc.qags.AdminUpdateQagStatus(ctx, qagID, status))
	if result == qag.AdminUpdateSuccess {
		EvictQagLists(ctx, h.svc.a)
	}
	switch result {
	case qag.AdminUpdateSuccess:
		return httpx.String(200, fmt.Sprintf("Statut de la question %s mis à jour : %s", qagID, status))
	case qag.AdminUpdateNotFound:
		return httpx.Empty(404)
	}
	return httpx.String(500, "Échec de la mise à jour du statut")
}

// springQagStatus is the conversion of a @RequestParam QagStatus (StringToEnumConverterFactory):
// the value is trimmed (String.trim) and must be exactly a constant name; an empty value is a
// missing parameter. Any other value is a 400.
func springQagStatus(v string) qag.QagStatus {
	trimmed := javacompat.JavaStringTrim(v)
	if v == "" {
		panic(&httpx.SpringError{Status: 400, Cause: "MissingServletRequestParameterException: status"})
	}
	s, ok := qag.ParseStatus(trimmed)
	if !ok {
		panic(&httpx.SpringError{Status: 400, Cause: "MethodArgumentTypeMismatchException: status " + trimmed})
	}
	return s
}
