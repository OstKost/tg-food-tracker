package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"github.com/OstKost/tg-food-tracker/internal/ports"
	"github.com/OstKost/tg-food-tracker/internal/service"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const HelpText = `Отправьте обычным сообщением, что вы съели, например:
«гречка 200 г, куриная грудка 150 г и огурец».

Команды:
/today — итог за сегодня
/history — последние 10 записей
/edit_last — исправить массу позиции в последней записи
/delete_me — удалить личные данные

Оценки приблизительные и не заменяют консультацию врача или нутрициолога.`

type TelegramClient interface {
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
	EditMessageText(ctx context.Context, params *bot.EditMessageTextParams) (*models.Message, error)
	AnswerCallbackQuery(ctx context.Context, params *bot.AnswerCallbackQueryParams) (bool, error)
}

type BotApp struct {
	client        TelegramClient
	service       *service.MealService
	repo          ports.SheetsRepository
	allowedUserID int64
}

func NewBotApp(
	client TelegramClient,
	svc *service.MealService,
	repo ports.SheetsRepository,
	allowedUserID int64,
) *BotApp {
	return &BotApp{
		client:        client,
		service:       svc,
		repo:          repo,
		allowedUserID: allowedUserID,
	}
}

func (a *BotApp) isAllowedUser(userID int64) bool {
	return userID == a.allowedUserID
}

func (a *BotApp) rejectMessage(ctx context.Context, chatID int64) {
	_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "Доступ закрыт.",
	})
}

func (a *BotApp) rejectCallback(ctx context.Context, queryID string) {
	_, _ = a.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: queryID,
		Text:            "Доступ закрыт.",
		ShowAlert:       true,
	})
}

func (a *BotApp) HandleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message != nil {
		a.handleMessage(ctx, update.Message)
		return
	}
	if update.CallbackQuery != nil {
		a.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}
}

func (a *BotApp) handleMessage(ctx context.Context, msg *models.Message) {
	if msg.From == nil {
		return
	}
	if !a.isAllowedUser(msg.From.ID) {
		a.rejectMessage(ctx, msg.Chat.ID)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	switch {
	case strings.HasPrefix(text, "/start") || strings.HasPrefix(text, "/help"):
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   HelpText,
		})
	case strings.HasPrefix(text, "/today"):
		a.handleToday(ctx, msg.Chat.ID)
	case strings.HasPrefix(text, "/history"):
		a.handleHistory(ctx, msg.Chat.ID)
	case strings.HasPrefix(text, "/edit_last"):
		a.handleEditLast(ctx, msg.Chat.ID)
	case strings.HasPrefix(text, "/delete_me"):
		a.handleDeleteMe(ctx, msg.Chat.ID)
	default:
		a.handleTextMessage(ctx, msg)
	}
}

func (a *BotApp) handleToday(ctx context.Context, chatID int64) {
	daily, err := a.service.Today(ctx, "")
	if err != nil {
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Не удалось прочитать итог. Попробуйте чуть позже.",
		})
		return
	}
	_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   FormatDaily(daily),
	})
}

func (a *BotApp) handleHistory(ctx context.Context, chatID int64) {
	meals, err := a.service.History(ctx, 10)
	if err != nil {
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Не удалось прочитать историю. Попробуйте чуть позже.",
		})
		return
	}
	if len(meals) == 0 {
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "История пока пуста.",
		})
		return
	}

	lines := []string{"Последние записи:"}
	for _, m := range meals {
		lines = append(lines, fmt.Sprintf("%s — %s: %.0f ккал", m.LoggedAt.Format("02.01 15:04"), m.Summary, m.Kcal))
	}
	_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   strings.Join(lines, "\n"),
	})
}

func (a *BotApp) handleEditLast(ctx context.Context, chatID int64) {
	meal, err := a.service.BeginEdit(ctx, a.allowedUserID)
	if err != nil {
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   err.Error(),
		})
		return
	}
	itemParts := make([]string, 0, len(meal.Items))
	for _, it := range meal.Items {
		itemParts = append(itemParts, fmt.Sprintf("%s — %.0f г", it.Name, it.AmountG))
	}
	text := fmt.Sprintf("Исправляем: %s. В течение 15 минут напишите, например: «курицы было 100 г».", strings.Join(itemParts, ", "))
	_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}

func (a *BotApp) handleDeleteMe(ctx context.Context, chatID int64) {
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "Удалить всё", CallbackData: "delete_confirm"},
				{Text: "Отмена", CallbackData: "delete_cancel"},
			},
		},
	}
	_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        "Удалить все записи Meals и ReportLog? Это действие нельзя отменить.",
		ReplyMarkup: kb,
	})
}

func (a *BotApp) handleTextMessage(ctx context.Context, msg *models.Message) {
	eventKey := fmt.Sprintf("%d:%d", msg.Chat.ID, msg.ID)
	var res *domain.RecordMealResult
	var err error

	if a.service.HasEditSession(ctx, a.allowedUserID) {
		res, err = a.service.CorrectLast(ctx, a.allowedUserID, eventKey, msg.Text)
	} else {
		clarified, clarErr := a.service.AnswerClarification(ctx, a.allowedUserID, eventKey, msg.Text)
		if clarErr != nil {
			err = clarErr
		} else if clarified != nil {
			res = clarified
		} else {
			res, err = a.service.RecordMeal(ctx, a.allowedUserID, eventKey, msg.Text)
		}
	}

	if err != nil {
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   err.Error(),
		})
		return
	}

	a.sendResult(ctx, msg.Chat.ID, res)
}

func (a *BotApp) handleCallbackQuery(ctx context.Context, cq *models.CallbackQuery) {
	if !a.isAllowedUser(cq.From.ID) {
		a.rejectCallback(ctx, cq.ID)
		return
	}

	data := cq.Data
	switch {
	case data == "delete_confirm":
		delRes, err := a.repo.ClearPersonalData(ctx)
		_, _ = a.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cq.ID,
		})
		if err == nil && cq.Message.Message != nil {
			msg := cq.Message.Message
			text := fmt.Sprintf("Данные удалены: записей — %d, отчётов — %d.", delRes.DeletedMeals, delRes.DeletedReports)
			_, _ = a.client.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msg.Chat.ID,
				MessageID: msg.ID,
				Text:      text,
			})
		}
	case data == "delete_cancel":
		_, _ = a.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cq.ID,
		})
		if cq.Message.Message != nil {
			msg := cq.Message.Message
			_, _ = a.client.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msg.Chat.ID,
				MessageID: msg.ID,
				Text:      "Удаление отменено.",
			})
		}
	case strings.HasPrefix(data, "meal_confirm:"):
		token := strings.TrimPrefix(data, "meal_confirm:")
		res, err := a.service.ConfirmMeal(ctx, a.allowedUserID, token)
		if err != nil {
			_, _ = a.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: cq.ID,
				Text:            err.Error(),
				ShowAlert:       true,
			})
			return
		}
		_, _ = a.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cq.ID,
			Text:            "Записано",
		})
		if cq.Message.Message != nil {
			msg := cq.Message.Message
			_, _ = a.client.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    msg.Chat.ID,
				MessageID: msg.ID,
				Text:      FormatResult(res),
			})
		}
	}
}

func (a *BotApp) sendResult(ctx context.Context, chatID int64, res *domain.RecordMealResult) {
	switch res.Status {
	case domain.StatusNeedsConfirmation:
		kb := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{
						Text:         "Записать",
						CallbackData: "meal_confirm:" + res.ConfirmationToken,
					},
				},
			},
		}
		prompt := res.Prompt
		if prompt == "" {
			prompt = "Подтвердите запись."
		}
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        prompt,
			ReplyMarkup: kb,
		})
	case domain.StatusNeedsClarification:
		prompt := res.Prompt
		if prompt == "" {
			prompt = "Уточните размер порции."
		}
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   prompt,
		})
	default:
		_, _ = a.client.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   FormatResult(res),
		})
	}
}

func FormatResult(res *domain.RecordMealResult) string {
	if res.Meal == nil || res.Daily == nil {
		return "Запись не готова."
	}
	prefix := "Записал"
	if res.Status == domain.StatusDuplicate {
		prefix = "Уже было записано"
	}
	return fmt.Sprintf("%s: %s\n\n%s", prefix, FormatMeal(res.Meal), FormatDaily(res.Daily))
}

func FormatMeal(m *domain.MealRecord) string {
	return fmt.Sprintf("%s — %.0f ккал; Б/Ж/У %.1f/%.1f/%.1f г", m.Summary, m.Kcal, m.ProteinG, m.FatG, m.CarbsG)
}

func FormatDaily(d *domain.DailySummary) string {
	return fmt.Sprintf("Сегодня: %.0f ккал; Б/Ж/У %.1f/%.1f/%.1f г", d.Kcal, d.ProteinG, d.FatG, d.CarbsG)
}

func StartBot(ctx context.Context, token string, app *BotApp) (*bot.Bot, error) {
	opts := []bot.Option{
		bot.WithDefaultHandler(app.HandleUpdate),
	}
	b, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}
	app.client = b
	return b, nil
}

// Suppress slog unused warning
var _ = slog.Info
