package dto

import (
	"time"

	"chat/internal/model/conversation"
)

type ConversationMessageRequest struct {
	Text string `json:"text"`
}

type ConversationMessageResponse struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	SenderUserID   int64     `json:"sender_user_id"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
}

func ToConversationMessageResponse(message *conversation.Message) ConversationMessageResponse {
	return ConversationMessageResponse{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		SenderUserID:   message.SenderUserID,
		Text:           message.Text,
		CreatedAt:      message.CreatedAt.UTC(),
	}
}
