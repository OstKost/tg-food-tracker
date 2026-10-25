# ADR-0001: Python runtime

- Status: Accepted
- Date: 2026-09-14

## Context

Личный бот выполняет Telegram long polling, два HTTP API и минутный scheduler. Ему не
нужны несколько процессов, CPU-параллелизм или отдельная очередь.

## Decision

Использовать Python 3.14.7, asyncio, aiogram и один процесс. Зависимости фиксируются
lock-файлом, production запускается одним Docker-контейнером.

## Consequences

Синхронный Google SDK вызывается через `asyncio.to_thread`. Весь изменяемый Sheets
workflow сериализуется одним lock. Go/TypeScript не используются в MVP.
