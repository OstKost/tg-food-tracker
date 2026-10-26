package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/service"
	"github.com/OstKost/tg-food-tracker/internal/testutil"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type mockTelegramClient struct {
	sentMessages   []*bot.SendMessageParams
	editedMessages []*bot.EditMessageTextParams
	answeredCBs    []*bot.AnswerCallbackQueryParams
}

func (m *mockTelegramClient) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	m.sentMessages = append(m.sentMessages, params)
	var chatID int64
	if cid, ok := params.ChatID.(int64); ok {
		chatID = cid
	}
	return &models.Message{ID: len(m.sentMessages), Chat: models.Chat{ID: chatID}, Text: params.Text}, nil
}

func (m *mockTelegramClient) EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error) {
	m.editedMessages = append(m.editedMessages, params)
	return &models.Message{ID: params.MessageID, Chat: models.Chat{ID: params.ChatID.(int64)}, Text: params.Text}, nil
}

func (m *mockTelegramClient) AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error) {
	m.answeredCBs = append(m.answeredCBs, params)
	return true, nil
}

func TestUnauthorizedTextNeverReachesAnalyzerOrSheets(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := service.NewMealService(repo, analyzer, loc, nil)

	mockClient := &mockTelegramClient{}
	allowedUserID := int64(12345)
	app := NewBotApp(mockClient, svc, repo, allowedUserID)

	ctx := context.Background()
	// Message from intruder (ID 99999)
	intruderMsg := &models.Message{
		ID:   1,
		From: &models.User{ID: 99999},
		Chat: models.Chat{ID: 99999},
		Text: "гречка 200г",
	}

	app.handleMessage(ctx, intruderMsg)

	if analyzer.AnalyzeCount != 0 {
		t.Fatalf("expected analyzer not to be called, got count %d", analyzer.AnalyzeCount)
	}
	if len(repo.Meals) != 0 {
		t.Fatalf("expected repo not to be touched, got meals %d", len(repo.Meals))
	}
	if len(mockClient.sentMessages) != 1 {
		t.Fatalf("expected 1 rejection message, got %d", len(mockClient.sentMessages))
	}
	if mockClient.sentMessages[0].Text != "Доступ закрыт." {
		t.Fatalf("expected 'Доступ закрыт.', got %s", mockClient.sentMessages[0].Text)
	}
}

func TestAllowedUserGetsHelpWithoutExternalCalls(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := service.NewMealService(repo, analyzer, loc, nil)

	mockClient := &mockTelegramClient{}
	allowedUserID := int64(12345)
	app := NewBotApp(mockClient, svc, repo, allowedUserID)

	ctx := context.Background()
	msg := &models.Message{
		ID:   1,
		From: &models.User{ID: allowedUserID},
		Chat: models.Chat{ID: allowedUserID},
		Text: "/start",
	}

	app.handleMessage(ctx, msg)

	if analyzer.AnalyzeCount != 0 {
		t.Fatalf("expected analyzer count 0, got %d", analyzer.AnalyzeCount)
	}
	if len(repo.Meals) != 0 {
		t.Fatalf("expected meals count 0, got %d", len(repo.Meals))
	}
	if len(mockClient.sentMessages) != 1 {
		t.Fatalf("expected 1 response message, got %d", len(mockClient.sentMessages))
	}
	if !strings.Contains(mockClient.sentMessages[0].Text, "/today — итог за сегодня") {
		t.Fatalf("expected help text, got %s", mockClient.sentMessages[0].Text)
	}
}

func TestTextMessageRecordsMeal(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := service.NewMealService(repo, analyzer, loc, nil)

	mockClient := &mockTelegramClient{}
	allowedUserID := int64(12345)
	app := NewBotApp(mockClient, svc, repo, allowedUserID)

	ctx := context.Background()
	msg := &models.Message{
		ID:   10,
		From: &models.User{ID: allowedUserID},
		Chat: models.Chat{ID: allowedUserID},
		Text: "гречка 100г",
	}

	app.handleMessage(ctx, msg)

	if analyzer.AnalyzeCount != 1 {
		t.Fatalf("expected analyzer count 1, got %d", analyzer.AnalyzeCount)
	}
	if len(repo.Meals) != 1 {
		t.Fatalf("expected 1 meal stored, got %d", len(repo.Meals))
	}
	if len(mockClient.sentMessages) != 1 {
		t.Fatalf("expected 1 response message, got %d", len(mockClient.sentMessages))
	}
	if !strings.HasPrefix(mockClient.sentMessages[0].Text, "Записал:") {
		t.Fatalf("expected 'Записал:', got %s", mockClient.sentMessages[0].Text)
	}
}
