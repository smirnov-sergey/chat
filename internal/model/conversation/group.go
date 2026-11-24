package conversation

type Group struct {
	Conversation
	OwnerUserID int64
	Name        string
}
