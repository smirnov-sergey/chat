package conversation

import "errors"

var (
	ErrNotFound            = errors.New("conversation not found")
	ErrAlreadyExists       = errors.New("direct conversation already exists")
	ErrInvalidParticipants = errors.New("direct conversation requires two distinct positive user IDs")
)
