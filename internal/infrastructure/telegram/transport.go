package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type BotTransport struct {
	b *bot.Bot
}

func NewBotTransport(b *bot.Bot) *BotTransport {
	return &BotTransport{b: b}
}

func (t *BotTransport) SendMessage(ctx context.Context, chatID int64, text string) (int, error) {
	msg, err := t.b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// Ensure interface compliance
var _ = models.Message{}
