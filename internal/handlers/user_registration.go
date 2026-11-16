package handlers

import (
	"context"
	"net/http"

	"chat/internal/dto"
	"chat/internal/service"
)

const (
	maxRegistrationBodyBytes = 4 << 10
	sessionCookieName        = "chat_session"
)

type UserRegistrationService interface {
	Register(ctx context.Context, username, name, password string) (*service.UserRegistration, error)
}

type UserRegistrationHandler struct {
	registrationService UserRegistrationService
	secureCookies       bool
}

func NewUserRegistrationHandler(registrationService UserRegistrationService, secureCookies bool) *UserRegistrationHandler {
	return &UserRegistrationHandler{registrationService: registrationService, secureCookies: secureCookies}
}

func (h *UserRegistrationHandler) Register(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.registrationService.Register(r.Context(), request.Username, request.Name, request.Password)
	if err != nil {
		handleError(w, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.Token.Value,
		Path:     "/",
		Expires:  result.Token.ExpiresAt,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusCreated, dto.ToUserResponse(result.User))
}
