package login

import (
	"agora/internal/app"
	"agora/internal/auth"
	"agora/internal/httpx"
	"agora/internal/modules/users"
)

// Routes registers POST /signup and POST /login.
func Routes(a *app.App) {
	h := &handlers{a: a, svc: Get(a)}
	a.Server.POST("/signup", h.signup)
	a.Server.POST("/login", h.login)
}

type handlers struct {
	a   *app.App
	svc *Service
}

func orDefault(v *string, def string) string {
	if v == nil {
		return def
	}
	return *v
}

// mustBool unwraps a (value, error) pair, turning an error into an uncaught
// exception (HTTP 500).
func mustBool(v bool, err error) bool {
	if err != nil {
		panic(err)
	}
	return v
}

// signup is SignupController.signup.
func (h *handlers) signup(c *httpx.Ctx) *httpx.Response {
	userAgent := c.RequiredHeader("User-Agent")
	fcmToken := c.OptionalHeader("fcmToken")
	versionName := c.OptionalHeader("versionName")
	versionCode := c.RequiredHeader("versionCode")
	platform := c.RequiredHeader("platform")
	ctx := c.Context()

	if !mustBool(h.svc.Flags.IsFeatureEnabled(ctx, FeatureSignUp)) {
		return httpx.Unit(401)
	}
	switch h.svc.AppVersion.GetAppVersionStatus(platform, versionCode) {
	case InvalidApp:
		return httpx.Unit(401)
	case UpdateRequired:
		return httpx.Unit(412)
	}
	req := users.SignupRequest{
		IPAddressHash: c.IPHash(),
		UserAgent:     userAgent,
		FCMToken:      orDefault(fcmToken, ""),
		Platform:      platform,
		VersionName:   orDefault(versionName, "("+versionCode+")"),
		VersionCode:   versionCode,
	}
	user, err := h.svc.Login.SignUp(ctx, req)
	if err != nil {
		panic(err)
	}
	// SignupInfoJsonMapper.toJson: a login token that cannot be built → 401.
	loginToken, err := h.a.LoginTokens.Build(user.UserID)
	if err != nil {
		h.a.Log.Error("Exception while buildLoginToken: " + err.Error())
		return httpx.Unit(401)
	}
	jwt, expiration, err := h.a.JWT.Generate(user.UserID)
	if err != nil {
		panic(err)
	}
	return httpx.OK(SignupInfoJSON{
		UserID:                  user.UserID,
		JWTToken:                jwt,
		JWTExpirationEpochMilli: expiration,
		LoginToken:              loginToken,
		IsModerator:             false,
	})
}

// login is LoginController.login.
func (h *handlers) login(c *httpx.Ctx) *httpx.Response {
	userAgent := c.RequiredHeader("User-Agent")
	fcmToken := c.OptionalHeader("fcmToken")
	versionName := c.OptionalHeader("versionName")
	versionCode := c.RequiredHeader("versionCode")
	platform := c.RequiredHeader("platform")
	var body LoginRequestJSON
	c.BindBody(&body)
	ctx := c.Context()

	if !mustBool(h.svc.Flags.IsFeatureEnabled(ctx, FeatureLogin)) {
		return httpx.Unit(401)
	}
	switch h.svc.AppVersion.GetAppVersionStatus(platform, versionCode) {
	case InvalidApp:
		return httpx.Unit(401)
	case UpdateRequired:
		return httpx.Unit(412)
	}
	userID, err := h.a.LoginTokens.Decode(body.LoginToken)
	if err != nil {
		return httpx.Unit(401)
	}
	req := users.LoginRequest{
		UserID:        userID,
		IPAddressHash: c.IPHash(),
		UserAgent:     userAgent,
		FCMToken:      orDefault(fcmToken, ""),
		Platform:      platform,
		VersionName:   orDefault(versionName, "("+versionCode+")"),
		VersionCode:   versionCode,
	}
	user, err := h.svc.Login.Login(ctx, req)
	if err != nil {
		panic(err)
	}
	if user == nil {
		return httpx.Unit(401)
	}
	jwt, expiration, err := h.a.JWT.Generate(user.UserID)
	if err != nil {
		panic(err)
	}
	// ResponseCookie.from(...).httpOnly(true).secure(true).path("/")
	// .maxAge(JWT_TOKEN_VALIDITY): the Long is a number of *seconds* (millis of
	// one day), hence Max-Age=86400000.
	maxAge := int64(auth.JWTValidity.Milliseconds())
	expires := h.a.Now().UTC().Add(auth.JWTValidity * 1000)
	cookie := auth.JWTCookieName + "=" + jwt + "; Path=/; Max-Age=" + itoa(maxAge) +
		"; Expires=" + expires.Format("Mon, 02 Jan 2006 15:04:05 GMT") + "; Secure; HttpOnly"
	return httpx.OK(LoginInfoJSON{
		JWTToken:                jwt,
		JWTExpirationEpochMilli: expiration,
		IsModerator:             false,
	}).With("Set-Cookie", cookie)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
