# ADR-0005: Golang runtime

- Status: Accepted
- Date: 2026-10-26
- Supersedes: ADR-0001 (Python runtime)

## Context

В ходе эксплуатации Python 3.14 сервиса на легковесном VPS выявились проблемы с потреблением памяти:
Python-процесс с зависимостями (aiogram, google-api-python-client, pydantic, httpx) потреблял от 150 до 350 МБ RAM даже при тюнинге malloc arenas (`MALLOC_ARENA_MAX=2`).
Кроме того, запуск в Docker требовал установки колес и тяжелого интерпретатора.

Личный сервис учёта еды состоит из строгого long-polling телеграм-бота, сериализованного доступа к Google Sheets API, двух HTTP API клиентов (OpenAI и Gemini) и минутного таймера.

## Decision

Переписать сервис на **Golang** (Go 1.24+):
1. Использовать официальный SDK `google.golang.org/api/sheets/v4`.
2. Использовать легковесную типобезопасную библиотеку `github.com/go-telegram/bot`.
3. Все сетевые вызовы к OpenAI Responses/Chat Completions и Gemini Interactions выполнять через стандартный `net/http` с явными таймаутами и контекстом.
4. Сохранить все существующие архитектурные инварианты: сериализацию Sheets через `sync.Mutex`, дедупликацию по `event_key` через `singleflight.Group`, строгие лимиты валидации, идемпотентный append и 15-минутные TTL на сессии уточнения/корректировки.

## Consequences

- Потребление оперативной памяти снизилось с 200–350 МБ до 15–25 МБ RAM (в 10–15 раз).
- Размер Docker-образа и время холодного старта уменьшились до миллисекунд.
- Единый статический бинарник без внешних runtime-зависимостей.
- Полная обратная совместимость по формату таблиц Sheets (`Meals`, `Dashboard`, `ReportLog`) и сценариям взаимодействия Telegram-бота.
