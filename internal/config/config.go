package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Settings struct {
	TelegramBotToken          string
	AllowedTelegramUserID     int64
	AIProvider                string // "openai" or "gemini"
	OpenAIAPIKey              string
	OpenAIModel               string
	OpenAIBaseURL             string
	GeminiAPIKey              string
	GeminiModel               string
	GeminiBaseURL             string
	GoogleSheetsSpreadsheetID string
	GoogleServiceAccountFile  string
	AppTimezone               string
	Location                  *time.Location
	ReportHour                int
	ReportMinute              int
	OpenAITimeoutSeconds      float64
	LogLevel                  string
}

func Load(envFile string) (*Settings, error) {
	if envFile != "" {
		_ = godotenv.Load(envFile)
	} else {
		_ = godotenv.Load(".env")
	}

	botToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if botToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required")
	}

	rawUserID := strings.TrimSpace(os.Getenv("ALLOWED_TELEGRAM_USER_ID"))
	if rawUserID == "" {
		return nil, errors.New("ALLOWED_TELEGRAM_USER_ID is required")
	}
	userID, err := strconv.ParseInt(rawUserID, 10, 64)
	if err != nil || userID <= 0 {
		return nil, fmt.Errorf("invalid ALLOWED_TELEGRAM_USER_ID: %s", rawUserID)
	}

	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER")))
	if provider == "" {
		provider = "openai"
	}
	if provider != "openai" && provider != "gemini" {
		return nil, fmt.Errorf("AI_PROVIDER must be 'openai' or 'gemini', got %q", provider)
	}

	openaiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	openaiModel := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if openaiModel == "" {
		openaiModel = "gpt-5.4-mini-2026-03-17"
	}
	openaiBaseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if openaiBaseURL == "" {
		openaiBaseURL = "https://api.openai.com/v1"
	}

	geminiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	geminiModel := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if geminiModel == "" {
		geminiModel = "gemini-3.8-flash"
	}
	geminiBaseURL := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL"))
	if geminiBaseURL == "" {
		geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	if provider == "openai" && openaiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required when AI_PROVIDER=openai")
	}
	if provider == "gemini" && geminiKey == "" {
		return nil, errors.New("GEMINI_API_KEY is required when AI_PROVIDER=gemini")
	}

	spreadsheetID := strings.TrimSpace(os.Getenv("GOOGLE_SHEETS_SPREADSHEET_ID"))
	if len(spreadsheetID) < 10 {
		return nil, errors.New("GOOGLE_SHEETS_SPREADSHEET_ID must be at least 10 characters")
	}

	saFile := strings.TrimSpace(os.Getenv("GOOGLE_SERVICE_ACCOUNT_FILE"))
	if saFile == "" {
		return nil, errors.New("GOOGLE_SERVICE_ACCOUNT_FILE is required")
	}

	tzStr := strings.TrimSpace(os.Getenv("APP_TIMEZONE"))
	if tzStr == "" {
		tzStr = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q: %w", tzStr, err)
	}

	reportTimeStr := strings.TrimSpace(os.Getenv("REPORT_TIME"))
	reportHour, reportMinute := 21, 0
	if reportTimeStr != "" {
		parts := strings.Split(reportTimeStr, ":")
		if len(parts) >= 2 {
			h, errH := strconv.Atoi(parts[0])
			m, errM := strconv.Atoi(parts[1])
			if errH == nil && errM == nil && h >= 0 && h < 24 && m >= 0 && m < 60 {
				reportHour, reportMinute = h, m
			}
		}
	}

	timeoutSec := 30.0
	if rawTimeout := strings.TrimSpace(os.Getenv("OPENAI_TIMEOUT_SECONDS")); rawTimeout != "" {
		if val, err := strconv.ParseFloat(rawTimeout, 64); err == nil {
			if val <= 0 || val > 120 {
				return nil, fmt.Errorf("OPENAI_TIMEOUT_SECONDS must be between 0 and 120, got %f", val)
			}
			timeoutSec = val
		}
	}

	logLevel := strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if logLevel == "" {
		logLevel = "INFO"
	}

	s := &Settings{
		TelegramBotToken:          botToken,
		AllowedTelegramUserID:     userID,
		AIProvider:                provider,
		OpenAIAPIKey:              openaiKey,
		OpenAIModel:               openaiModel,
		OpenAIBaseURL:             openaiBaseURL,
		GeminiAPIKey:              geminiKey,
		GeminiModel:               geminiModel,
		GeminiBaseURL:             geminiBaseURL,
		GoogleSheetsSpreadsheetID: spreadsheetID,
		GoogleServiceAccountFile:  saFile,
		AppTimezone:               tzStr,
		Location:                  loc,
		ReportHour:                reportHour,
		ReportMinute:              reportMinute,
		OpenAITimeoutSeconds:      timeoutSec,
		LogLevel:                  logLevel,
	}

	return s, nil
}

func (s *Settings) ValidateServiceAccountFile() error {
	path := filepath.Clean(s.GoogleServiceAccountFile)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_FILE does not point to a readable file: %s (%w)", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_FILE points to a directory: %s", path)
	}
	return nil
}
