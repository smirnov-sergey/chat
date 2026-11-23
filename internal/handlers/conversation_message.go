package handlers

import (
	"context"
	"net/http"
	"strconv"

	"chat/internal/dto"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
)

const maxConversationMessageBodyBytes = 64 << 10

type ConversationMessageService interface {
	Create(ctx context.Context, conversationID, currentUserID int64, text string) (*conversation.Message, error)
}

type ConversationMessageHandler struct {
	conversationMessageService ConversationMessageService
}

func NewConversationMessageHandler(conversationMessageService ConversationMessageService) *ConversationMessageHandler {
	return &ConversationMessageHandler{conversationMessageService: conversationMessageService}
}

func (h *ConversationMessageHandler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	currentUserID, ok := userIDFromContext(r.Context())
	if !ok {
		handleError(w, session.ErrInvalid)
		return
	}

	conversationID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || conversationID <= 0 {
		handleError(w, errInvalidConversationID)
		return
	}

	var request *dto.ConversationMessageRequest
	if err := decodeJSONRequest(w, r, maxConversationMessageBodyBytes, &request); err != nil {
		handleError(w, err)
		return
	}

	if request == nil {
		handleError(w, errInvalidRequestBody)
		return
	}

	message, err := h.conversationMessageService.Create(r.Context(), conversationID, currentUserID, request.Text)
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, dto.ToConversationMessageResponse(message))
}
