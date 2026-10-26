# Telegram Food Tracker

Личный Telegram-дневник питания. Бот принимает свободный русский текст, получает
приблизительную оценку калорий и КБЖУ от выбранного AI-провайдера, сохраняет запись в Google Sheets и
возвращает текущий итог дня. После 21:00 бот один раз присылает дневной отчёт.

Это информационный помощник, а не медицинский сервис. Все значения приблизительные.

## MVP

- доступ только для одного Telegram ID;
- текстовый ввод и структурированный анализ еды;
- Google Sheets как единственное постоянное хранилище;
- `/today`, `/history`, `/edit_last`, `/delete_me`, `/help`;
- ежедневный отчёт после 21:00 в настроенной timezone;
- защита от повторных Telegram update и неизвестного результата Sheets append;
- безопасная локальная валидация каждого ответа модели.

## Стек

- Go 1.24+;
- github.com/go-telegram/bot (Telegram Bot API, long polling);
- net/http для OpenAI Responses API / Chat Completions и Gemini Interactions API;
- официальный Google API Go client (`google.golang.org/api/sheets/v4`);
- встроенный `go test` с детектором гонок (`-race`);
- минимальный multi-stage Docker-контейнер на Alpine (< 25 МБ RAM).

## Документы

- [Архитектура](docs/ARCHITECTURE.md)
- [План разработки](docs/DEVELOPMENT_PLAN.md)
- [Настройка и запуск](docs/SETUP.md)
- [SMART-задачи](TASKS.md)
- [Правила агентов](AGENTS.md)
- [Агентный процесс](docs/AGENT_WORKFLOW.md)

Начните с [инструкции настройки](docs/SETUP.md). Автоматические тесты не требуют
Telegram, выбранного AI-провайдера или Google credentials.
