package login

import (
	"agora/internal/app"
)

// errorTextWithinTheWeek is ErrorMessagesRepositoryImpl.ERROR_TEXT_WITHIN_THE_WEEK.
const errorTextWithinTheWeek = "Vous avez déjà posé une question au Gouvernement cette semaine. L’appli propose actuellement une question par semaine pour chaque utilisateur afin que le plus grand nombre de citoyens puisse participer. Rendez-vous lundi à partir de 10h pour poser une nouvelle question. D’ici là, n’hésitez pas à soutenir les questions des autres utilisateurs, sans limite !"

// ErrorMessages is ErrorMessagesRepositoryImpl.
type ErrorMessages struct{ a *app.App }

// QagDisabledErrorMessage is getQagDisabledErrorMessage: the
// ERROR_TEXT_QAG_DISABLED variable; unset is a NullPointerException (the Kotlin
// return type is a non-null String).
func (e *ErrorMessages) QagDisabledErrorMessage() string {
	if e.a.Cfg.ErrorTextQagDisabled == nil {
		panic("NullPointerException: ERROR_TEXT_QAG_DISABLED is not set")
	}
	return *e.a.Cfg.ErrorTextQagDisabled
}

// QagErrorMessageOneByWeek is getQagErrorMessageOneByWeek.
func (e *ErrorMessages) QagErrorMessageOneByWeek() string {
	return errorTextWithinTheWeek
}
