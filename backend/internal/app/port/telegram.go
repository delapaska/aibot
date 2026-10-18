package port

import "context"

// TelegramUser identifies the sender of a message.
type TelegramUser struct {
	ID        int64
	Username  string
	FirstName string
}

// TelegramChat is the conversation a message arrived in. Type is the
// Telegram chat type: "private", "group", "supergroup", "channel".
type TelegramChat struct {
	ID   int64
	Type string
}

// TelegramMessage is one incoming message. Only text messages are
// meaningful to the bot; everything else arrives with an empty Text.
type TelegramMessage struct {
	MessageID int64
	From      TelegramUser
	Chat      TelegramChat
	Text      string
}

// TelegramUpdate is one long-polling event. Message is nil for updates
// the bot does not consume (edits, callback queries, ...).
type TelegramUpdate struct {
	UpdateID int64
	Message  *TelegramMessage
}

// TelegramAPI is the Bot API boundary: the long-polling fetch and outgoing
// messages. Adapters translate these calls to the HTTP wire format.
type TelegramAPI interface {
	// GetUpdates returns the updates after the given offset, waiting up to
	// timeoutSec seconds for new ones (long polling).
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]TelegramUpdate, error)
	// SendMessage delivers one text message to the chat.
	SendMessage(ctx context.Context, chatID int64, text string) error
}
