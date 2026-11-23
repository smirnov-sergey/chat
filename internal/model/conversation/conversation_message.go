package conversation

import "time"

type Message struct {
	ID             int64
	ConversationID int64
	SenderUserID   int64
	Text           string
	CreatedAt      time.Time
}
