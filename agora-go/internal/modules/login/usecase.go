package login

import (
	"context"

	"agora/internal/modules/users"
)

// UserRepository is usecase/login/repository/UserRepository (the part LoginUseCase uses).
type UserRepository interface {
	GetUserByID(ctx context.Context, userID string) (*users.UserInfo, error)
	UpdateUser(ctx context.Context, req users.LoginRequest) (*users.UserInfo, error)
	GenerateUser(ctx context.Context, req users.SignupRequest) (*users.UserInfo, error)
}

type signupNotifier interface {
	NotifySignup(ctx context.Context, ipAddressHash, userAgent string) error
}

// LoginUseCase is usecase/login/LoginUseCase.
type LoginUseCase struct {
	users    UserRepository
	userData UserDataRepository
	notifier signupNotifier
}

// FindUser is findUser.
func (u *LoginUseCase) FindUser(ctx context.Context, userID string) (*users.UserInfo, error) {
	return u.users.GetUserByID(ctx, userID)
}

// Login is login: nil when the user is unknown.
func (u *LoginUseCase) Login(ctx context.Context, req users.LoginRequest) (*users.UserInfo, error) {
	user, err := u.users.GetUserByID(ctx, req.UserID)
	if err != nil || user == nil {
		return nil, err
	}
	if err := u.userData.AddLoginData(ctx, req); err != nil {
		return nil, err
	}
	return u.users.UpdateUser(ctx, req)
}

// SignUp is signUp.
func (u *LoginUseCase) SignUp(ctx context.Context, req users.SignupRequest) (*users.UserInfo, error) {
	user, err := u.users.GenerateUser(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := u.userData.AddSignupData(ctx, req, user.UserID); err != nil {
		return nil, err
	}
	if err := u.notifier.NotifySignup(ctx, req.IPAddressHash, req.UserAgent); err != nil {
		return nil, err
	}
	return user, nil
}
