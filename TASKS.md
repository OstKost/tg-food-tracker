# TASKS.md

Статусы: `TODO`, `IN PROGRESS`, `BLOCKED`, `REVIEW`, `DONE`.
Каждая задача содержит конкретный результат, измеримую приёмку, владельца,
зависимости и timebox. При завершении обязательно заполнить Evidence.

## Milestone 0 — Architecture and foundation

### FT-001 — Зафиксировать архитектуру

- Status: DONE
- Owner: `product-architect`
- Depends on: —
- Timebox: 0,5 рабочего дня
- Result: зафиксировать Python, выбранный AI-провайдер и Google Sheets, убрать устаревшие варианты.
- Acceptance:
  - README, архитектура, план, AGENTS и ADR описывают один стек;
  - в активной архитектуре нет PostgreSQL, SQLite, Go, ORM, очереди и worker.
- Evidence: документы и ADR обновлены в текущей реализации.

### FT-002 — Создать Python-каркас

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-001
- Timebox: 0,5 дня
- Result: Python 3.14.7-проект с `pyproject.toml`, lock-файлом, `src` и `tests`.
- Acceptance:
  - пакет импортируется;
  - конфигурация валидируется при старте;
  - тестовая команда запускается локально.
- Evidence: `pyproject.toml`, `requirements.lock`, entrypoints и валидируемая конфигурация;
  пакет собран локально и внутри Docker.

### FT-003 — Настроить quality gate

- Status: DONE
- Owner: `qa-operator`
- Depends on: FT-002
- Timebox: 0,5 дня
- Result: Ruff, mypy и pytest запускаются одной командой.
- Acceptance:
  - проверки настроены локально и в CI;
  - `.env` и service-account JSON исключены из репозитория.
- Evidence: `make quality`, настройки `pyproject.toml`, `.gitignore` и CI; 29 тестов,
  Ruff и mypy проходят 2026-09-14.

### FT-004 — Реализовать bootstrap Google Sheets

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-002
- Timebox: 1 день
- Result: команда setup создаёт/проверяет Meals, Dashboard и ReportLog.
- Acceptance:
  - повторный setup не дублирует листы или заголовки;
  - формулы и timezone устанавливаются детерминированно;
  - повреждённая схема диагностируется понятной ошибкой.
- Evidence: CLI и Google Sheets adapter; тесты повторного bootstrap, формул, timezone и
  повреждённого заголовка проходят.

### FT-005 — Реализовать контракты и fake adapters

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-002
- Timebox: 1 день
- Result: доменные модели и порты работают с fake-анализатором и fake Sheets.
- Acceptance:
  - сценарий записи и чтения дневного итога работает без сети;
  - итоги вычисляются из отдельных позиций.
- Evidence: протоколы, модели, fake и сервисный слой добавлены.

## Milestone 1 — Personal diary

### FT-101 — Реализовать личный Telegram-контур

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-005
- Timebox: 1 день
- Result: long polling и команды `/start`, `/help` доступны владельцу.
- Acceptance:
  - разрешённый ID получает ответ;
  - остальные пользователи не запускают AI и Sheets-вызовы.
- Evidence: aiogram router и fake Telegram session; handler test доказывает, что чужой ID
  не вызывает анализатор и repository.

### FT-102 — Реализовать OpenAI-анализатор

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-005
- Timebox: 1,5 дня
- Result: русский текст преобразуется в версионированный `MealAnalysis`.
- Acceptance:
  - запрос использует JSON Schema и `store=false`;
  - timeout, один retry и числовые границы покрыты тестами;
  - сырой ответ и ключ не логируются.
- Evidence: HTTP adapter и contract tests проверяют JSON Schema, `store=false`, HTTP 500,
  timeout и ровно один retry.

### FT-108 — Добавить Gemini как альтернативный анализатор

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-005
- Timebox: 0,75 дня
- Result: `AI_PROVIDER=gemini` выбирает Gemini Interactions API без изменений Telegram,
  Google Sheets или доменного контракта.
- Acceptance:
  - Gemini получает JSON Schema через `response_format`, а ответ проходит ту же
    Pydantic-валидацию, что и OpenAI;
  - выбранному провайдеру обязателен свой API key; второй ключ не требуется;
  - один transient retry покрыт contract test; автоматического fallback нет;
  - `.env.example`, setup и ADR описывают выбор провайдера.
- Evidence: `GeminiFoodAnalyzer`, provider-neutral schema, Settings validation, 2 contract
  tests и ADR-0004 добавлены; живой Gemini вызов намеренно не выполнялся.

### FT-103 — Записывать еду идемпотентно

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-004, FT-101, FT-102
- Timebox: 1,5 дня
- Result: сообщение создаёт одну строку и возвращает дневной баланс.
- Acceptance:
  - повторный и параллельный update не создают вторую строку;
  - timeout после append сверяется по `event_key` до ошибки/повтора.
- Evidence: inflight deduplication и adapter append reconciliation реализованы.

### FT-104 — Реализовать корректировку последнего приёма

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-103
- Timebox: 1 день
- Result: `/edit_last` меняет вес позиции и пересчитывает строку.
- Acceptance:
  - сессия истекает через 15 минут;
  - duplicate correction безопасен;
  - другие приёмы пищи не меняются.
- Evidence: correction contract, 15-минутная session, revision update и duplicate test.

### FT-105 — Реализовать `/today` и `/history`

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-103
- Timebox: 0,75 дня
- Result: бот показывает сегодняшний итог и последние 10 записей.
- Acceptance:
  - граница дня определяется `APP_TIMEZONE`;
  - ручная сортировка строк не ломает порядок истории.
- Evidence: repository queries и handlers добавлены.

### FT-106 — Реализовать ежедневный отчёт

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-105
- Timebox: 1 день
- Result: после 21:00 отправляется один отчёт с базовой рекомендацией.
- Acceptance:
  - повтор scheduler не дублирует сообщение;
  - пустой день пропускается;
  - restart и граница суток покрыты тестами.
- Evidence: scheduler, ReportLog claim и rule-based recommendations; тесты пустого дня,
  времени 20:59, перезапуска и следующей локальной даты проходят.

### FT-107 — Реализовать удаление данных

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-104, FT-105
- Timebox: 0,5 дня
- Result: `/delete_me` очищает личные строки после подтверждения.
- Acceptance:
  - Meals и ReportLog очищаются;
  - Dashboard показывает нули;
  - повторная команда безопасна.
- Evidence: confirmation callback и repeatable clear test; после очистки summary равен нулю.

## Milestone 2 — Delivery

### FT-201 — Подготовить Docker и VPS-запуск

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-106, FT-107
- Timebox: 1 день
- Result: один контейнер запускает бот с read-only secrets и restart policy.
- Acceptance:
  - cold start проверяет конфигурацию и схему Sheets;
  - graceful shutdown завершает polling и scheduler.
- Evidence: `docker compose config --no-interpolate` проходит; актуальный образ
  `tg-food-tracker:test` на Python 3.14.7 успешно собран 2026-09-14; исходники без
  `.env` не содержат secrets.

### FT-202 — Выполнить финальную QA-проверку

- Status: REVIEW
- Owner: `qa-operator`
- Depends on: FT-003, FT-201
- Timebox: 1 день
- Result: подтвердить основной сценарий и эксплуатационные ограничения.
- Acceptance:
  - pytest, Ruff и mypy зелёные;
  - blocker/high замечаний нет;
  - результаты внесены в Evidence.
- Evidence: 2026-09-14 — `pytest`: 29 passed; Ruff: passed; mypy по `src tests`: passed;
  `pip check`: passed; Compose config: passed; Docker build: passed. Gemini contract tests
  проходят без сети. Живой smoke test
  ожидает Telegram, выбранного AI-провайдера и Google credentials и явного разрешения владельца.

### FT-203 — Написать подробную инструкцию по настройке

- Status: REVIEW
- Owner: `qa-operator`
- Depends on: FT-202
- Timebox: 1 день
- Result: пошаговый `docs/SETUP.md` для человека без знания архитектуры.
- Acceptance:
  - описаны BotFather, Telegram ID, OpenAI/Gemini, Google Cloud/Sheets, env, локальный запуск,
    Docker/VPS, smoke test, обновление, backup, ротация ключей и troubleshooting;
  - команды копируются без ручного исправления;
  - секреты не попадают в Git или логи;
  - чистое развёртывание укладывается в 45 минут.
- Evidence: `docs/SETUP.md` добавлен и команды сверены с реализацией; независимое чистое
  развёртывание за 45 минут остаётся частью живой проверки FT-202.

### FT-204 — Настроить CI/CD для развёртывания на VPS (109.107.176.224)

- Status: DONE
- Owner: `python-integrator`
- Depends on: FT-201
- Timebox: 0,5 дня
- Result: GitHub Actions пайплайн для проверок и автоматического деплоя на зарубежный VPS (109.107.176.224) из веток main/master.
- Acceptance:
  - workflow запускает quality gate (Ruff, mypy, pytest, Docker build);
  - при push в main/master выполняется деплой на 109.107.176.224 через SSH;
  - прямое подключение к Telegram API и Google Cloud работает без VPN/туннелей;
  - скрипт развёртывания `deploy/deploy.sh` сериализует вызовы через flock, проверяет права секретов, валидирует схему Sheets и мягко перезапускает бот.
- Evidence: `.github/workflows/ci.yml`, `deploy/deploy.sh`, `compose.yaml`, `.gitignore` обновлены;
  сборка и окружение проверены на хосте 109.107.176.224.

## Recommended next task

Завершить `FT-202` и `FT-203`: по явному разрешению владельца выполнить живой smoke test
на отдельной таблице, затем дать другому оператору пройти `docs/SETUP.md` с нуля и
зафиксировать фактическое время.
