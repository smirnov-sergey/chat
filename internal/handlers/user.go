package handlers

import (
	"context"
	"net/http"
	"strconv"

	"chat/internal/dto"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/service"
)

const (
	maxRegistrationBodyBytes = 4 << 10
	maxLoginBodyBytes        = 4 << 10
)

type UserService interface {
	Register(ctx context.Context, username, name, password string) (*service.UserSession, error)
	Login(ctx context.Context, username, password string) (*service.UserSession, error)
	Logout(ctx context.Context, token string) error
	GetByID(ctx context.Context, id int64) (*user.User, error)
	UploadPhoto(ctx context.Context, userID int64, data []byte) error
	GetPhoto(ctx context.Context, userID int64) ([]byte, error)
}

type UserHandler struct {
	userService   UserService
	secureCookies bool
	maxPhotoSize  int64
}

func NewUserHandler(userService UserService, secureCookies bool, maxPhotoSize int64) *UserHandler {
	return &UserHandler{userService: userService, secureCookies: secureCookies, maxPhotoSize: maxPhotoSize}
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

func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	id, err := parseUserID(r)
	if err != nil {
		handleError(w, err)
		return
	}

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	if currentUserID != id {
		handleError(w, errAccessDenied)
		return
	}

	u, err := h.userService.GetByID(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, dto.ToUserResponse(u))
}

func (h *UserHandler) Logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var token string

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		token = cookie.Value
	}

	if err := h.userService.Logout(r.Context(), token); err != nil {
		handleError(w, err)
		return
	}

	clearSessionCookie(w, h.secureCookies)
	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	id, err := parseUserID(r)
	if err != nil {
		handleError(w, err)
		return
	}

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	if currentUserID != id {
		handleError(w, errAccessDenied)
		return
	}

	data, err := readPhotoRequest(w, r, h.maxPhotoSize)
	if err != nil {
		handleError(w, err)
		return
	}

	if err := h.userService.UploadPhoto(r.Context(), id, data); err != nil {
		handleError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandler) GetPhoto(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	id, err := parseUserID(r)
	if err != nil {
		handleError(w, err)
		return
	}

	if _, ok := userIDFromContext(r.Context()); !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	data, err := h.userService.GetPhoto(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}

	writePNG(w, data)
}

func parseUserID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errInvalidUserID
	}
	return id, nil
}
