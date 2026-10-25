#!/usr/bin/env bash
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

echo "=== [1/6] Locking deployment ==="
exec 9>/var/lock/tg-food-tracker-deploy.lock
flock -w 300 9 || { echo "ERROR: Could not acquire deploy lock" >&2; exit 1; }

echo "=== [2/6] Validating AmneziaWG (awg0) & Telegram routing ==="
# Check awg-quick@awg0 status. On M0, Telegram subnets (91.108.*, 149.154.*) are routed via awg0.
if ! systemctl is-active --quiet awg-quick@awg0; then
    echo "awg0 is not active. Attempting systemctl restart awg-quick@awg0..."
    systemctl restart awg-quick@awg0 || true
    sleep 2
fi

if ! systemctl is-active --quiet awg-quick@awg0; then
    echo "ERROR: awg-quick@awg0 is not active. Telegram routing requires awg tunnel on M0." >&2
    exit 1
fi

# Verify Telegram API connectivity over the tunnel
if ! curl --silent --head --connect-timeout 8 https://api.telegram.org > /dev/null; then
    echo "ERROR: api.telegram.org is unreachable over awg0." >&2
    exit 1
fi
echo "AmneziaWG (awg0) and Telegram connectivity: OK"

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
docker compose run --rm bot python -m food_tracker.cli

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

echo "=== Deployment to M0 completed successfully! ==="
