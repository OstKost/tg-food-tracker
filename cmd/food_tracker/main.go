package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/config"
	"github.com/OstKost/tg-food-tracker/internal/infrastructure/analyzer"
	"github.com/OstKost/tg-food-tracker/internal/infrastructure/sheets"
	"github.com/OstKost/tg-food-tracker/internal/infrastructure/telegram"
	"github.com/OstKost/tg-food-tracker/internal/ports"
	"github.com/OstKost/tg-food-tracker/internal/scheduler"
	"github.com/OstKost/tg-food-tracker/internal/service"
)

func main() {
	setupFlag := flag.Bool("setup", false, "Bootstrap and validate Google Sheets schema")
	flag.Parse()

	// Also support "setup" positional subcommand
	isSetup := *setupFlag
	for _, arg := range flag.Args() {
		if arg == "setup" {
			isSetup = true
			break
		}
	}

	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.ValidateServiceAccountFile(); err != nil {
		fmt.Fprintf(os.Stderr, "Service account error: %v\n", err)
		os.Exit(1)
	}

	setupLogging(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := sheets.NewGoogleSheetsRepository(
		ctx,
		cfg.GoogleSheetsSpreadsheetID,
		cfg.GoogleServiceAccountFile,
		cfg.AllowedTelegramUserID,
		cfg.Location,
		nil,
	)
	if err != nil {
		slog.Error("failed to create Google Sheets repository", "err", err)
		os.Exit(1)
	}

	if isSetup {
		slog.Info("bootstrapping Google Sheets schema...")
		if err := repo.Bootstrap(ctx); err != nil {
			slog.Error("failed to bootstrap Google Sheets schema", "err", err)
			os.Exit(1)
		}
		fmt.Println("Google Sheets schema is ready.")
		return
	}

	slog.Info("validating Google Sheets schema...")
	if err := repo.ValidateSchema(ctx); err != nil {
		slog.Error("Google Sheets schema validation failed", "err", err)
		os.Exit(1)
	}

	var foodAnalyzer ports.FoodAnalyzer
	timeout := time.Duration(cfg.OpenAITimeoutSeconds * float64(time.Second))
	if cfg.AIProvider == "openai" {
		foodAnalyzer = analyzer.NewOpenAIFoodAnalyzer(
			cfg.OpenAIAPIKey,
			cfg.OpenAIModel,
			cfg.OpenAIBaseURL,
			timeout,
			&http.Client{Timeout: timeout},
		)
	} else {
		foodAnalyzer = analyzer.NewGeminiFoodAnalyzer(
			cfg.GeminiAPIKey,
			cfg.GeminiModel,
			cfg.GeminiBaseURL,
			timeout,
			&http.Client{Timeout: timeout},
		)
	}
	defer foodAnalyzer.Close()

	mealService := service.NewMealService(repo, foodAnalyzer, cfg.Location, nil)
	app := telegram.NewBotApp(nil, mealService, repo, cfg.AllowedTelegramUserID)

	tgBot, err := telegram.StartBot(ctx, cfg.TelegramBotToken, app)
	if err != nil {
		slog.Error("failed to initialize Telegram bot", "err", err)
		os.Exit(1)
	}

	transport := telegram.NewBotTransport(tgBot)
	sched := scheduler.NewDailyReportScheduler(
		repo,
		transport,
		cfg.AllowedTelegramUserID,
		cfg.Location,
		cfg.ReportHour,
		cfg.ReportMinute,
		nil,
	)

	go sched.RunForever(ctx)

	slog.Info("Telegram food tracker bot started",
		"provider", cfg.AIProvider,
		"timezone", cfg.AppTimezone,
		"allowed_user_id", cfg.AllowedTelegramUserID,
	)

	// Start bot polling
	tgBot.Start(ctx)

	slog.Info("shutting down cleanly...")
}

func setupLogging(levelStr string) {
	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
