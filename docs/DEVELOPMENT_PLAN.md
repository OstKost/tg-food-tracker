# План разработки

## Подход

Работа разбита на SMART-задачи в `TASKS.md`. Каждый срез заканчивается наблюдаемым
пользовательским результатом, fake-driven тестами и независимой проверкой. Живые
интеграции используются только в финальном smoke test с разрешения владельца.

## Этап 0. Основа

- зафиксировать ADR Python, Google Sheets и AI-провайдеров;
- создать Python 3.14.7-пакет и единый entrypoint;
- настроить Ruff, mypy, pytest и CI;
- создать доменные модели, протоколы и fake adapters;
- реализовать идемпотентный bootstrap трёх листов.

Результат: весь основной сценарий воспроизводится без сети.

## Этап 1. Вертикальный срез дневника

- закрытый aiogram long polling для одного Telegram ID;
- OpenAI или Gemini Structured Output с локальной валидацией;
- идемпотентный append и reconciliation после timeout;
- ответ с записью и формульным итогом Dashboard;
- `/today`, `/history` и `/edit_last`.

Результат: владелец ведёт и исправляет личный дневник текстом.

## Этап 2. Отчёт и приватность

- scheduler после 21:00 и ReportLog claim;
- безопасные rule-based рекомендации;
- `/delete_me` с подтверждением;
- структурные логи без secrets/PII;
- graceful shutdown.

Результат: бот работает автономно и не дублирует дневные отчёты.

## Этап 3. Доставка

- один production Docker image;
- compose-конфигурация с read-only credentials;
- финальные unit/contract/integration tests;
- подробная настройка Telegram, OpenAI/Gemini, Google Cloud/Sheets и VPS;
- ручной smoke test на отдельной тестовой таблице.

Результат: новый экземпляр разворачивается по инструкции не более чем за 45 минут.

## Тестовая стратегия

- unit: модели, лимиты, пересчёт, рекомендации и timezone;
- contract: OpenAI/Gemini request/response и Sheets row schema;
- integration: fake Sheets с дублями, сортировкой, timeout и report claim;
- handler: allowlist, команды и callbacks без Telegram API;
- smoke: реальные сервисы только вручную с бюджетным лимитом.

## После MVP

Фото, несколько пользователей, персональные цели и надёжная внешняя очередь
рассматриваются отдельными ADR только после фактической потребности.
