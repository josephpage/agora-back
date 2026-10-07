package login

// SignupInfoJSON is infrastructure/login/SignupInfoJson.
type SignupInfoJSON struct {
	UserID                  string `json:"userId"`
	JWTToken                string `json:"jwtToken"`
	JWTExpirationEpochMilli int64  `json:"jwtExpirationEpochMilli"`
	LoginToken              string `json:"loginToken"`
	IsModerator             bool   `json:"isModerator"`
}

// JavaName is the XML root element.
func (SignupInfoJSON) JavaName() string { return "SignupInfoJson" }

// LoginInfoJSON is infrastructure/login/LoginInfoJson.
type LoginInfoJSON struct {
	JWTToken                string `json:"jwtToken"`
	JWTExpirationEpochMilli int64  `json:"jwtExpirationEpochMilli"`
	IsModerator             bool   `json:"isModerator"`
}

// JavaName is the XML root element.
func (LoginInfoJSON) JavaName() string { return "LoginInfoJson" }

// LoginRequestJSON is infrastructure/login/LoginRequestJson (request body).
type LoginRequestJSON struct {
	LoginToken string `json:"loginToken"`
}
