package conversation

import "time"

type Type string

const (
	TypeDirect   Type = "direct"
	TypePersonal Type = "personal"
	TypeGroup    Type = "group"
)

type Conversation struct {
	ID        int64
	Type      Type
	CreatedAt time.Time
}
