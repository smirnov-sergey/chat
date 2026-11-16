package handlers

import (
	"context"
	"net/http"

	"chat/internal/dto"
	"chat/internal/service"
)

const (
	maxRegistrationBodyBytes = 4 << 10
	maxLoginBodyBytes        = 4 << 10
)

type UserService interface {
	Register(ctx context.Context, username, name, password string) (*service.UserSession, error)
	Login(ctx context.Context, username, password string) (*service.UserSession, error)
}

type UserHandler struct {
	userService   UserService
	secureCookies bool
}

func NewUserHandler(userService UserService, secureCookies bool) *UserHandler {
	return &UserHandler{userService: userService, secureCookies: secureCookies}
}

func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request *dto.UserRegistrationRequest

	if err := decodeJSONRequest(w, r, maxRegistrationBodyBytes, &request); err != nil {
		handleError(w, err)
		return
	}

	if request == nil {
		handleError(w, errInvalidRequestBody)
		return
	}

	result, err := h.userService.Register(r.Context(), request.Username, request.Name, request.Password)
	if err != nil {
		handleError(w, err)
		return
	}

	setSessionCookie(w, result.Token, h.secureCookies)

	writeJSON(w, http.StatusCreated, dto.ToUserResponse(result.User))
}

func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.userService.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		handleError(w, err)
		return
	}

	setSessionCookie(w, result.Token, h.secureCookies)

	writeJSON(w, http.StatusOK, dto.ToUserResponse(result.User))
}
