package notification

import (
	"errors"
	"time"

	"agora/internal/httpx"
)

// TypeNotification is infrastructure/notification/NotificationUserController.kt's enum. The ordinal is
// what the database holds (Hibernate maps the enum with its default ORDINAL strategy; the column is TEXT).
type TypeNotification int

// The constants follow the Kotlin declaration order (the ordinals are persisted).
const (
	AllReponsesQags TypeNotification = iota
	HomeQags
	DetailsQag
	HomeConsultations
	DetailsConsultation
	ReponseSupport
)

var typeNames = [...]string{
	"ALL_REPONSES_QAGS",
	"HOME_QAGS",
	"DETAILS_QAG",
	"HOME_CONSULTATIONS",
	"DETAILS_CONSULTATION",
	"REPONSE_SUPPORT",
}

// Name is Enum.name.
func (t TypeNotification) Name() string { return typeNames[t] }

// Ordinal is Enum.ordinal.
func (t TypeNotification) Ordinal() int { return int(t) }

// TypeByName is Enum.valueOf: the exact constant name, nothing else.
func TypeByName(name string) (TypeNotification, bool) {
	for i, n := range typeNames {
		if n == name {
			return TypeNotification(i), true
		}
	}
	return 0, false
}

// TypeByOrdinal is EnumClass.getEnumConstants()[ordinal].
func TypeByOrdinal(o int) (TypeNotification, bool) {
	if o < 0 || o >= len(typeNames) {
		return 0, false
	}
	return TypeNotification(o), true
}

// Notification is domain/Notification.
type Notification struct {
	Title       string
	Description string
	Type        TypeNotification
	Date        time.Time
}

// Result is NotificationResult.
type Result int

// NotificationResult constants.
const (
	Success Result = iota
	Failure
)

// UserIDInconnuError is UserIdInconnuException.
type UserIDInconnuError struct{ UserID string }

func (e *UserIDInconnuError) Error() string { return "User with id: " + e.UserID + " does not exist." }

// FcmTokenVideError is FcmTokenVideException.
type FcmTokenVideError struct{}

func (*FcmTokenVideError) Error() string { return "Fcm token should not be empty" }

// QagIDInconnuError is QagIdInconnuException.
type QagIDInconnuError struct{ QagID string }

func (e *QagIDInconnuError) Error() string { return "There is no qag with id " + e.QagID }

// Raise panics with what DefaultControllerAdvice makes of the exceptions of this slice (a
// {"title"} body with the status), anything else is an HTTP 500. Other slices call it for the
// errors of SendQag* (S7: the Moderatus moderation).
func Raise(err error) {
	var user *UserIDInconnuError
	var fcm *FcmTokenVideError
	var qag *QagIDInconnuError
	switch {
	case errors.As(err, &user):
		panic(&httpx.AdviceError{Status: 404, Title: "Veuillez renseigner un id d'utilisateur existant."})
	case errors.As(err, &fcm):
		panic(&httpx.AdviceError{Status: 400, Title: "Veuillez renseigner un FcmToken."})
	case errors.As(err, &qag):
		panic(&httpx.AdviceError{Status: 404, Title: "Veuillez renseigner un id de qag existant."})
	}
	panic(err)
}
