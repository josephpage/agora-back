package qag

import (
	"agora/internal/app"
	"agora/internal/httpx"
)

type handlers struct {
	a   *app.App
	svc *Service
}

// must unwraps a (value, error) pair: an error is an exception Kotlin does not
// catch (HTTP 500).
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// getQagDetails is QagDetailsController.getQagDetails.
func (h *handlers) getQagDetails(c *httpx.Ctx) *httpx.Response {
	qagID := c.PathVar("qagId")
	userID := c.UserID()
	result := must(h.svc.Details.GetQagDetails(c.Context(), qagID, userID))
	switch result.Kind {
	case QagResultSuccess:
		return httpx.OK(ToQagJSON(result.Qag))
	case QagResultRejectedStatus:
		return httpx.Unit(423)
	}
	return httpx.Unit(404)
}

// getPublicQagDetails is QagDetailsController.getPublicQagDetails.
func (h *handlers) getPublicQagDetails(c *httpx.Ctx) *httpx.Response {
	qag := must(h.svc.PublicDetails.GetQagDetails(c.Context(), c.PathVar("qagId")))
	if qag == nil {
		return httpx.Unit(404)
	}
	return httpx.OK(ToPublicQagJSON(*qag))
}

// deleteQagByID is DeleteQagController.deleteQagById.
func (h *handlers) deleteQagByID(c *httpx.Ctx) *httpx.Response {
	qagID := c.PathVar("qagId")
	result := must(h.svc.Delete.DeleteQagByID(c.Context(), c.UserID(), qagID))
	if result.Success() {
		return httpx.Unit(200)
	}
	return httpx.Unit(400)
}

// insertQag is InsertQagController.insertQag.
func (h *handlers) insertQag(c *httpx.Ctx) *httpx.Response {
	var body QagInsertingJSON
	c.BindBody(&body)
	userID := c.UserID()
	ctx := c.Context()
	return executeTask(&h.svc.q.insert, insertQagTask{userID: userID},
		func() *httpx.Response {
			if must(h.svc.AskStatus.GetAskQagStatus(ctx, userID)) != AskEnabled {
				return httpx.Unit(400)
			}
			result := must(h.svc.Insert.InsertQag(ctx, body.toDomain(userID, h.a.Now)))
			if !result.Success() {
				return httpx.Unit(400)
			}
			return httpx.OK(QagInsertionResultJSON{QagID: result.Info.ID})
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// insertSupportQag is SupportQagController.insertSupportQag.
func (h *handlers) insertSupportQag(c *httpx.Ctx) *httpx.Response {
	userAgent := c.RequiredHeader("User-Agent")
	qagID := c.PathVar("qagId")
	userID := c.UserID()
	ctx := c.Context()
	return executeTask(&h.svc.q.support, supportTask{add: true, userID: userID},
		func() *httpx.Response {
			if must(h.svc.suspicious.IsSuspiciousActivity(ctx, c.IPHash(), userAgent)) {
				return httpx.Unit(200)
			}
			if must(h.svc.Supports.InsertSupportQag(ctx, SupportQagInserting{QagID: qagID, UserID: userID})) == SupportSuccess {
				return httpx.Unit(200)
			}
			return httpx.Unit(400)
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// deleteSupportQag is SupportQagController.deleteSupportQag.
func (h *handlers) deleteSupportQag(c *httpx.Ctx) *httpx.Response {
	qagID := c.PathVar("qagId")
	userID := c.UserID()
	ctx := c.Context()
	return executeTask(&h.svc.q.support, supportTask{add: false, userID: userID},
		func() *httpx.Response {
			if must(h.svc.Supports.DeleteSupportQag(ctx, SupportQagDeleting{QagID: qagID, UserID: userID})) == SupportSuccess {
				return httpx.Unit(200)
			}
			return httpx.Unit(400)
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// insertFeedbackQag is FeedbackQagController.insertFeedbackQag.
func (h *handlers) insertFeedbackQag(c *httpx.Ctx) *httpx.Response {
	qagID := c.PathVar("qagId")
	var body FeedbackQagJSON
	c.BindBody(&body)
	userID := c.UserID()
	ctx := c.Context()
	return executeTask(&h.svc.q.feedback, feedbackTask{userID: userID},
		func() *httpx.Response {
			result, results, err := h.svc.Feedback.InsertFeedbackQag(ctx, FeedbackQagInserting{QagID: qagID, UserID: userID, IsHelpful: body.IsHelpful})
			if err != nil {
				panic(err)
			}
			switch result {
			case InsertFeedbackSuccess:
				return httpx.OK(ToFeedbackResultsJSON(*results))
			case InsertFeedbackSuccessDisabled:
				return httpx.Unit(200)
			}
			return httpx.Unit(400)
		},
		func() *httpx.Response { return httpx.Unit(400) })
}

// askStatus is QagHomeV2Controller.askStatus.
func (h *handlers) askStatus(c *httpx.Ctx) *httpx.Response {
	text := must(h.svc.ErrorText.GetQagErrorText(c.Context(), c.UserID()))
	return httpx.OK(QagAskStatusJSON{AskQagErrorText: text}).CacheControl(60, false)
}
