package handlers

import (
	"context"
	"net/http"

	"chat/internal/dto"
	"chat/internal/service"
)

const maxLoginBodyBytes = 4 << 10

type UserLoginService interface {
	Login(ctx context.Context, username, password string) (*service.UserSession, error)
}

type UserLoginHandler struct {
	loginService  UserLoginService
	secureCookies bool
}

func NewUserLoginHandler(loginService UserLoginService, secureCookies bool) *UserLoginHandler {
	return &UserLoginHandler{loginService: loginService, secureCookies: secureCookies}
}

func (h *UserLoginHandler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request *dto.UserLoginRequest

	if err := decodeJSONRequest(w, r, maxLoginBodyBytes, &request); err != nil {
		handleError(w, err)
		return
	}

	if request == nil {
		handleError(w, errInvalidRequestBody)
		return
	}

	result, err := h.loginService.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		handleError(w, err)
		return
	}

	setSessionCookie(w, result.Token, h.secureCookies)

	writeJSON(w, http.StatusOK, dto.ToUserResponse(result.User))
}
