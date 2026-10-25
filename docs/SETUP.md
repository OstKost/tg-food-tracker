# Настройка личного Telegram-дневника

Инструкция рассчитана на первый запуск без знания внутренней архитектуры. Бот работает
только для одного Telegram ID, хранит записи в одной Google-таблице и обращается к OpenAI
или Gemini для приблизительной оценки КБЖУ.

## Что понадобится

- Telegram-аккаунт;
- Telegram и Google-аккаунт, а также ключ выбранного AI-провайдера;
- компьютер с Python 3.14.7 и Git для локальной проверки;
- VPS с Docker для постоянной работы;
- выбранный вами лимит расходов у OpenAI или Gemini (ниже $20 приведены лишь как
  стартовый контрольный бюджет, а не обещание фиксированной стоимости).

Никому не отправляйте Telegram-токен, OpenAI/Gemini API key и JSON service account. Не вставляйте
их в issue, коммит, скриншот или сообщение боту.

## 1. Создать Telegram-бота

1. Откройте официальный чат [@BotFather](https://t.me/BotFather).
2. Отправьте `/newbot`.
3. Задайте отображаемое имя и уникальный username, заканчивающийся на `bot`.
4. Сохраните выданный токен — это значение `TELEGRAM_BOT_TOKEN`.
5. В `/mybots` → Bot Settings → Allow Groups отключите добавление в группы: дневник
   предназначен для личной переписки.
6. Через `/setcommands` задайте команды:

   ```text
   today - итог за сегодня
   history - последние 10 записей
   edit_last - исправить последнюю запись
   delete_me - удалить личные данные
   help - помощь
   ```

Официальное описание создания бота: [Telegram BotFather](https://core.telegram.org/bots/features#botfather).

## 2. Узнать свой Telegram ID

1. Напишите созданному боту `/start`.
2. Временно задайте токен в текущем терминале:

   ```bash
   read -s TELEGRAM_BOT_TOKEN
   export TELEGRAM_BOT_TOKEN
   ```

   Вставьте токен и нажмите Enter. Ввод не будет показан.
3. Получите последние обновления:

   ```bash
   curl --silent "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getUpdates"
   ```

4. Найдите число в `message.from.id`. Это `ALLOWED_TELEGRAM_USER_ID`.
5. Удалите временную переменную:

   ```bash
   unset TELEGRAM_BOT_TOKEN
   ```

Если `result` пуст, снова отправьте боту сообщение и повторите запрос. Не запускайте
основной сервис до завершения этого шага: long polling забирает те же обновления.

## 3. Выбрать AI-провайдера и создать ключ

Укажите только одного провайдера в `AI_PROVIDER`: `openai` или `gemini`. Бот не делает
автоматический fallback между ними, поэтому текст приёма пищи уходит только в выбранный
сервис.

### Вариант A: OpenAI

1. В [OpenAI Platform](https://platform.openai.com/) создайте отдельный project для бота.
2. Добавьте платёжный метод в Billing.
3. В настройках project задайте месячный бюджет $20 и уведомления о расходах. Бюджет и
   жёсткий лимит могут быть разными настройками интерфейса — проверьте, что именно
   ограничивает расходы в вашем аккаунте.
4. Создайте project API key и сразу сохраните его как `OPENAI_API_KEY`: повторно полностью
   показать ключ обычно нельзя.
5. Не используйте персональный admin key. Для MVP достаточно project key.

Приложение отправляет запросы Responses API с `store=false`; текст еды всё равно покидает
ваш VPS и обрабатывается OpenAI.

### Вариант B: Gemini

1. Откройте [Google AI Studio](https://aistudio.google.com/) и создайте API key для
   отдельного Google Cloud project.
2. Ограничьте ключ Gemini API в настройках проекта и настройте billing/quota так, как
   подходит вашему личному лимиту расходов.
3. Сохраните ключ как `GEMINI_API_KEY`. Не публикуйте его и не используйте один ключ для
   разных приложений.
4. В `.env` задайте `AI_PROVIDER=gemini` и оставьте `OPENAI_API_KEY` пустым.

Gemini использует Interactions API с JSON Schema. Google описывает ключи и их ограничения
в [официальной инструкции](https://ai.google.dev/gemini-api/docs/generate-content/api-key).

## 4. Подготовить Google Cloud

1. Откройте [Google Cloud Console](https://console.cloud.google.com/) и создайте отдельный
   project.
2. В APIs & Services → Library найдите **Google Sheets API** и нажмите Enable.
3. В IAM & Admin → Service Accounts создайте service account, например
   `food-tracker-bot`. Роли на весь project ему не требуются: доступ будет выдан к одной
   таблице через Share.
4. Откройте service account → Keys → Add key → Create new key → JSON.
5. Сохраните скачанный файл в менеджер секретов. JSON нельзя скачать повторно.
6. Выпишите `client_email` из JSON — этот адрес понадобится для расшаривания таблицы.

Google рекомендует бережно хранить пользовательские service-account keys и удалять
скомпрометированные ключи: [официальная инструкция](https://cloud.google.com/iam/docs/keys-create-delete).

## 5. Создать и расшарить Google-таблицу

1. Создайте пустую Google-таблицу. Листы вручную создавать не нужно.
2. Нажмите Share и добавьте `client_email` service account с ролью **Editor**.
3. Скопируйте ID из адреса:

   ```text
   https://docs.google.com/spreadsheets/d/ЭТОТ_ФРАГМЕНТ/edit
   ```

   Это `GOOGLE_SHEETS_SPREADSHEET_ID`.
4. Не публикуйте таблицу в интернет и не включайте доступ «Anyone with the link».

Команда setup создаст `Meals`, `Dashboard`, `ReportLog`, заголовки и формулы, а также
установит timezone таблицы равным `APP_TIMEZONE`.

## 6. Локальная установка

Установите Python 3.14.7 с [python.org](https://www.python.org/downloads/) или системным
менеджером пакетов и проверьте `python3.14 --version`: команда должна показать
`Python 3.14.7`.

В каталоге проекта выполните:

```bash
python3.14 -m venv .venv
source .venv/bin/activate
python3 -m pip install --upgrade pip
python3 -m pip install -r requirements.lock
python3 -m pip install --no-deps -e .
cp .env.example .env
mkdir -p secrets
read -r GOOGLE_KEY_PATH
cp "$GOOGLE_KEY_PATH" secrets/google-service-account.json
chmod 700 secrets
chmod 600 .env
chmod 644 secrets/google-service-account.json
```

После команды `read` вставьте полный путь к скачанному JSON и нажмите Enter. Каталог
`secrets` закрыт от других пользователей хоста, а сам файл доступен непривилегированному
пользователю внутри контейнера только для чтения.

Откройте `.env` в редакторе и замените значения. Для локального запуска путь должен быть:

```dotenv
GOOGLE_SERVICE_ACCOUNT_FILE=secrets/google-service-account.json
```

Для OpenAI:

```dotenv
TELEGRAM_BOT_TOKEN=<токен BotFather>
ALLOWED_TELEGRAM_USER_ID=<ваш числовой ID>
AI_PROVIDER=openai
OPENAI_API_KEY=<project API key>
OPENAI_MODEL=gpt-5.4-mini-2026-03-17
GOOGLE_SHEETS_SPREADSHEET_ID=<ID таблицы>
APP_TIMEZONE=Europe/Moscow
REPORT_TIME=21:00
```

Для Gemini замените AI-блок следующим; остальные переменные оставьте без изменений:

```dotenv
AI_PROVIDER=gemini
GEMINI_API_KEY=<Gemini API key>
GEMINI_MODEL=gemini-3.8-flash
```

Проверьте код без внешних вызовов:

```bash
make quality
```

Создайте структуру таблицы и запустите бота:

```bash
make setup
make run
```

Повторный `make setup` безопасен. Если существующие заголовки или формулы повреждены,
команда остановится с понятной ошибкой, а не перезапишет данные.

## 7. Smoke-test

Проверьте по порядку:

1. `/start` возвращает справку.
2. Сообщение `гречка 200 г и куриная грудка 150 г` создаёт ровно одну строку `Meals`.
3. Пересланный вручную текст создаёт новую запись, а повторная доставка того же Telegram
   update — нет; обычный пользователь не обязан симулировать повтор update.
4. `/today` совпадает с `Dashboard`.
5. `/history` показывает запись.
6. `/edit_last`, затем `курицы было 100 г` увеличивает `revision`, меняет существующую
   строку и пересчитывает итог.
7. С другого Telegram-аккаунта бот отвечает «Доступ закрыт», а в таблице строка не
   появляется.
8. Временно задайте `REPORT_TIME` на 2–3 минуты вперёд, перезапустите процесс и убедитесь,
   что пришёл один отчёт и появилась строка `ReportLog`. Верните `21:00`.
9. `/delete_me` проверяйте только после резервной копии: команда очищает личные строки.

## 8. Запуск на VPS через Docker и AmneziaWG (awg)

На VPS (например, нода M0) установите Docker Engine и Compose plugin.
Учтите, что для взаимодействия с серверами Telegram на хосте используется AmneziaWG (`awg-quick@awg0`),
через который маршрутизируются диапазоны `91.108.0.0/16` и `149.154.160.0/20`.
Так как MTU туннеля `awg0` равен 1280, сеть Docker Compose (`compose.yaml`) сконфигурирована с MTU 1280,
что предотвращает фрагментацию и зависание TLS-пакетов при взаимодействии с Telegram API.

Скопируйте проект и создайте `.env` и `secrets/google-service-account.json`. В `.env` укажите:

```dotenv
GOOGLE_SERVICE_ACCOUNT_FILE=/run/secrets/google-service-account.json
```

Запустите:

```bash
chmod 755 secrets
chmod 600 .env
chmod 644 secrets/google-service-account.json
docker compose build
docker compose run --rm bot python -m food_tracker.cli
docker compose up -d
docker compose logs --tail=100 bot
```

Контейнер работает одним процессом, использует long polling и автоматически перезапускается.
JSON подключён read-only и не входит в image.

Остановка и повторный запуск:

```bash
docker compose down
docker compose up -d
```

## 9. Непрерывное развёртывание (CI/CD) на M0

Проект настроен для автоматической сборки, проверок и развёртывания через GitHub Actions (`.github/workflows/ci.yml`):

- **Триггеры**: `push` в ветки `main` и `master`, `workflow_dispatch` (ручной запуск), `pull_request` (только quality gate).
- **Quality Gate**:
  - Ruff, mypy, pytest на Python 3.14;
  - Сборка и валидация Dockerfile.
- **Deploy на M0**:
  - Проверка статуса сервиса `awg-quick@awg0` и доступности `https://api.telegram.org`;
  - Синхронизация кода через `git archive` в `/opt/tg-food-tracker` (без перезаписи `.env` и `secrets/`);
  - Запуск `deploy/deploy.sh`: блокировка релиза через flock, сборка образа, вызов `python -m food_tracker.cli` для проверки схемы Sheets, перезапуск контейнера и проверка статуса.

### Необходимые секреты в GitHub:

- `VPS_SSH_KEY` (или `DEPLOY_SSH_KEY`): приватный SSH-ключ для доступа к M0 (соответствующий публичный ключ `github-actions-deploy` уже прописан в `~/.ssh/authorized_keys` на M0).
- `VPS_HOST`: адрес хоста (по умолчанию `176.53.174.118`).
- `VPS_USER`: пользователь SSH (по умолчанию `root`).
- `VPS_KNOWN_HOSTS`: опционально, host key fingerprint (если не задан, считывается автоматически через `ssh-keyscan`).

## 10. Резервная копия и восстановление

- В Google Sheets используйте File → Make a copy перед обновлением и не реже раза в месяц.
- Для дополнительной копии используйте File → Download → Microsoft Excel (`.xlsx`).
- Храните копию отдельно от VPS. JSON service account и API keys не включайте в архив
  проекта.
- Для восстановления сначала остановите контейнер, создайте копию нужной таблицы,
  расшарьте её service account и замените `GOOGLE_SHEETS_SPREADSHEET_ID`.

## 11. Ротация и отзыв ключей

### Telegram

В @BotFather вызовите `/revoke` или создайте новый token через `/token`, замените
`TELEGRAM_BOT_TOKEN` и перезапустите контейнер. Старый токен перестанет работать.

### OpenAI

Создайте новый project key, замените `OPENAI_API_KEY`, перезапустите и удалите старый key
после smoke-test.

### Gemini

Создайте новый Gemini API key в Google AI Studio, замените `GEMINI_API_KEY`, перезапустите
контейнер и проверьте `/today`. После этого отзовите старый ключ в AI Studio.

### Google

Создайте новый JSON key для того же service account, замените файл на VPS, перезапустите,
проверьте `/today`, затем удалите старый key в Google Cloud. Никогда не храните два ключа
дольше необходимого.

## 12. Troubleshooting

### `Доступ закрыт`

Сверьте `ALLOWED_TELEGRAM_USER_ID` с `from.id`, перезапустите сервис. Username использовать
нельзя.

### `403` от Google Sheets

Проверьте, что Sheets API включён, `client_email` из используемого JSON добавлен в Share
именно этой таблицы как Editor, а ID таблицы скопирован без `/edit`.

### Ошибка timezone или схемы

Не переименовывайте листы и колонки. Выполните `python -m food_tracker.cli`. Команда
исправляет timezone и создаёт отсутствующие листы, но намеренно не затирает несовместимые
заголовки или формулы.

### OpenAI `401`

Ключ неверный, отозван или относится к недоступному project. Создайте project key и не
добавляйте вокруг него кавычки/пробелы в `.env`.

### OpenAI `429`

Проверьте billing, project budget и rate limits. Приложение делает только один повтор;
сообщение можно отправить снова после устранения причины.

### Gemini `401`, `403` или `429`

Проверьте, что `AI_PROVIDER=gemini`, ключ относится к нужному Google Cloud project и имеет
разрешение на Gemini API. Затем проверьте quota, billing и ограничения ключа в Google AI
Studio. Бот делает лишь один повтор временной ошибки; после исправления причины отправьте
сообщение снова.

### Бот не отвечает, но контейнер запущен

```bash
docker compose ps
docker compose logs --tail=200 bot
```

Убедитесь, что второй экземпляр с тем же Telegram token не использует long polling. В логах
не должно быть самих API keys или полного ответа OpenAI/Gemini.

### Отчёт не пришёл

Проверьте `APP_TIMEZONE`, `REPORT_TIME`, наличие приёмов за локальную дату и строку
`ReportLog`. Для пустого дня отчёт намеренно не отправляется. Статус `sending` означает,
что отправка началась, но не была подтверждена; автоматический повтор отключён, чтобы не
создать дубликат.
