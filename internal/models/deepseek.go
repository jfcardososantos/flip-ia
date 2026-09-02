package models

type DeepSeekAuth struct {
	Cookie string
	Token  string
}

type DeepSeekChatResult struct {
	Content        string
	ReasoningText  string
	MessageID      string
	ConversationID string
	Usage          Usage
}
