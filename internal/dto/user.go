package dto

import (
	"time"

	"chat/internal/model/user"
)

type UserRegistrationRequest struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type UserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ToUserResponse(u *user.User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Name:      u.Name,
		CreatedAt: u.CreatedAt.UTC(),
		UpdatedAt: u.UpdatedAt.UTC(),
	}
}
