#!/usr/bin/env bash
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

echo "=== [1/6] Locking deployment ==="
exec 9>/var/lock/tg-food-tracker-deploy.lock
flock -w 300 9 || { echo "ERROR: Could not acquire deploy lock" >&2; exit 1; }

echo "=== [2/6] Validating Telegram connectivity ==="
if ! curl --silent --head --connect-timeout 8 https://api.telegram.org > /dev/null; then
    echo "ERROR: api.telegram.org is unreachable from host." >&2
    exit 1
fi
echo "Telegram connectivity: OK"

echo "=== [3/6] Verifying secrets & filesystem permissions ==="
if [[ ! -f "$APP_DIR/.env" ]]; then
    echo "ERROR: Missing $APP_DIR/.env file" >&2
    exit 1
fi
if [[ ! -f "$APP_DIR/secrets/google-service-account.json" ]]; then
    echo "ERROR: Missing $APP_DIR/secrets/google-service-account.json" >&2
    exit 1
fi

chmod 755 "$APP_DIR/secrets"
chmod 600 "$APP_DIR/.env"
chmod 644 "$APP_DIR/secrets/google-service-account.json"

echo "=== [4/6] Building container image ==="
docker compose build --pull

echo "=== [5/6] Verifying Google Sheets bootstrap / schema ==="
if docker compose run --rm bot python -m food_tracker.cli; then
    echo "Google Sheets schema verification: OK"
else
    echo "WARNING: Google Sheets bootstrap failed. Please verify credentials in secrets/google-service-account.json"
fi

echo "=== [6/6] Restarting bot container ==="
# Gracefully stop the previous instance so aiogram releases the Telegram polling session cleanly
docker compose stop bot 2>/dev/null || true
docker compose up -d --remove-orphans bot

# Verify container is running
sleep 3
if ! docker compose ps --status running | grep -q bot; then
    echo "ERROR: Bot container failed to start or exited immediately!" >&2
    docker compose logs --tail=50 bot
    exit 1
fi

echo "Bot container status:"
docker compose ps bot

echo "=== Deployment completed successfully! ==="
