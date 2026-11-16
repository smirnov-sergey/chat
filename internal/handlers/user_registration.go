package handlers

import (
	"context"
	"net/http"

	"chat/internal/dto"
	"chat/internal/service"
)

const maxRegistrationBodyBytes = 4 << 10

type UserRegistrationService interface {
	Register(ctx context.Context, username, name, password string) (*service.UserSession, error)
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

	setSessionCookie(w, result.Token, h.secureCookies)

	writeJSON(w, http.StatusCreated, dto.ToUserResponse(result.User))
}
