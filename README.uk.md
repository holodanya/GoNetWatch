# GoNetWatch

[English version](README.md)

GoNetWatch — це невелика, практична система моніторингу мережі, написана на Go. Вона регулярно перевіряє HTTP, TCP і DNS цілі, записує метрики в InfluxDB, показує їх у Grafana та може надсилати повідомлення в Telegram, коли сервіс справді змінює стан.

Проєкт зручно запускати локально через Docker Compose, але сам застосунок залишається звичайним Go-сервісом з YAML-конфігурацією.

## Що вміє проєкт

- Перевіряє цілі типів `http`, `http-head`, `tcp` і `dns`.
- Записує latency, success/failure, кількість спроб, HTTP status code і кількість DNS-відповідей.
- Зберігає метрики в InfluxDB 2.x через асинхронний write API.
- Має готовий Grafana dashboard, який автоматично підтягується з репозиторію.
- Надсилає Telegram alerts тільки при переходах UP/DOWN, а не на кожну невдалу перевірку.
- Імпортує цілі з TXT або CSV файлів і робить backup конфігу перед записом.
- Валідовує конфігурацію перед стартом моніторингу, щоб типові помилки було видно одразу.

```text
GoNetWatch ── пише метрики ──> InfluxDB ── показує ──> Grafana
     │
     └── надсилає alerts при зміні стану ──> Telegram
```

## Швидкий старт

### 1. Створіть `.env`

```bash
cp .env.example .env
```

Відредагуйте `.env` перед першим запуском. Найважливіше значення — `INFLUX_TOKEN`: цей самий токен використовується GoNetWatch для запису метрик і Grafana для читання.

Приклад:

```env
INFLUXDB_ADMIN_USER=admin
INFLUXDB_ADMIN_PASSWORD=change_me
INFLUXDB_ORG=gonetwatch
INFLUXDB_BUCKET=network_metrics
INFLUX_TOKEN=replace_with_a_long_random_token
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=change_me
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_IDS=
```

Не додавайте `.env` у git — там зберігаються секрети.

### 2. Створіть runtime config

```bash
cp configs/config.example.yaml configs/config.yaml
```

У прикладі вже є HTTP, TCP, DNS, тестова failing-ціль і protected endpoint. Для першого запуску можна залишити їх або одразу замінити на свої.

### 3. Запустіть stack

```bash
docker compose up -d --build
```

InfluxDB ініціалізується при першому старті та має healthcheck. Grafana і GoNetWatch чекають, поки InfluxDB стане healthy.

### 4. Відкрийте Grafana

```text
http://localhost:3000
```

Увійдіть з `GRAFANA_ADMIN_USER` і `GRAFANA_ADMIN_PASSWORD` з `.env`. Dashboard створюється автоматично у папці `GoNetWatch` з назвою **GoNetWatch - Network Monitoring**.

## Конфігурація

За замовчуванням застосунок читає `configs/config.yaml`. Шлях можна змінити через `CONFIG_PATH`.

Основні секції:

```yaml
targets:
  - name: "GitHub HTTP HEAD"
    type: "http-head"
    protocol: "http-head"
    address: "https://github.com"
    interval_sec: 30
    timeout_sec: 5
    retries: 1
    retry_delay_ms: 500

influxdb:
  url: "http://influxdb:8086"
  token: "" # зазвичай задається через INFLUX_TOKEN
  org: "gonetwatch"
  bucket: "network_metrics"

telegram:
  bot_token: "" # зазвичай задається через TELEGRAM_BOT_TOKEN
  chat_ids: []  # зазвичай задається через TELEGRAM_CHAT_IDS
```

Секрети можна не тримати в YAML і передавати через environment variables:

| Environment variable | Що перевизначає |
| --- | --- |
| `INFLUX_TOKEN` | `influxdb.token` |
| `TELEGRAM_BOT_TOKEN` | `telegram.bot_token` |
| `TELEGRAM_CHAT_IDS` | `telegram.chat_ids` як comma-separated values |

Після зміни `configs/config.yaml` достатньо перезапустити тільки застосунок:

```bash
docker compose restart gonetwatch
```

## Типи цілей

| Type | Формат адреси | Умова успіху |
| --- | --- | --- |
| `http` | `https://example.com/health` | HTTP status `200..399`, якщо не перевизначено |
| `http-head` | `https://example.com` | HTTP status `200..399`, якщо не перевизначено |
| `tcp` | `host:port` | TCP connection успішний |
| `dns` | домен без схеми, наприклад `example.com` | resolved хоча б один IP |

`timeout_sec: 0` означає runtime default: 5 секунд для HTTP і 3 секунди для TCP/DNS. `retries` — це додаткові спроби після першої. Якщо `retry_delay_ms` не позитивний, використовується fallback 300 ms.

### HTTP status overrides

Деякі endpoint-и є робочими навіть коли повертають `401` або `403`. Для HTTP і HTTP HEAD можна задати `expected_statuses`:

```yaml
- name: "Protected Admin Page"
  type: "http-head"
  protocol: "http-head"
  address: "https://example.com/admin"
  interval_sec: 30
  expected_statuses: [200, 401, 403]
```

Якщо `expected_statuses` задано, успішними вважаються тільки перелічені коди.

### DNS з власним resolver

```yaml
- name: "Google DNS via Cloudflare"
  type: "dns"
  protocol: "dns"
  address: "google.com"
  resolver: "1.1.1.1:53"
  interval_sec: 30
  timeout_sec: 3
```

Для DNS адреса має бути звичайним доменом, не URL.

## Імпорт цілей

У GoNetWatch є команда `import-targets`. Вона читає TXT або CSV файл, пропускає дублікати, валідовує фінальний config, створює timestamped backup і записує оновлений YAML.

### TXT import

Приклад файлу:

```txt
https://example.com
github.com:443
google.com

# comments and empty lines are ignored
```

Спочатку краще зробити dry-run:

```bash
go run ./cmd/gonetwatch import-targets --input import/targets.txt --output configs/config.yaml --dry-run
```

Потім застосувати імпорт:

```bash
go run ./cmd/gonetwatch import-targets --input import/targets.txt --output configs/config.yaml
```

Правила для TXT:

- Рядки з `http://` або `https://` стають `http-head` targets.
- Рядки `host:port` стають `tcp` targets.
- Інші рядки використовують `--default-type` (`http-head` за замовчуванням).
- Для bare host, імпортованих як HTTP/HTTP HEAD, додається `https://` prefix.

### CSV import

Дозволений header:

```csv
name,type,address,interval_sec,timeout_sec,retries,retry_delay_ms,resolver,expected_statuses
```

Приклад:

```csv
name,type,address,interval_sec,timeout_sec,retries,retry_delay_ms,resolver,expected_statuses
Google HTTP HEAD,http-head,https://www.google.com,30,5,1,300,,
GitHub TCP,tcp,github.com:443,20,3,1,300,,
Google DNS,dns,google.com,30,3,1,300,1.1.1.1:53,
Protected API,http-head,https://example.com/admin,30,5,1,300,,200;401;403
```

У CSV поле `expected_statuses` записується через крапку з комою.

### Імпорт у Docker

Папка `import/` з репозиторію монтується в контейнер як `/app/import`:

```bash
docker compose run --rm gonetwatch import-targets --input /app/import/targets.txt --output /app/configs/config.yaml
docker compose restart gonetwatch
```

## Telegram alerts

Telegram необов'язковий. Якщо `TELEGRAM_BOT_TOKEN` або `TELEGRAM_CHAT_IDS` порожні, GoNetWatch працює без alerts.

Коли Telegram увімкнений, GoNetWatch надсилає:

- повідомлення про старт;
- DOWN alert, коли target переходить з UP у DOWN;
- resolved message, коли target повертається з DOWN в UP;
- повідомлення про graceful shutdown.

Новий alert не надсилається на кожну failed-перевірку, якщо target вже DOWN.

Налаштування:

1. Створіть bot через [@BotFather](https://t.me/BotFather).
2. Надішліть будь-яке повідомлення боту.
3. Відкрийте `https://api.telegram.org/bot<TOKEN>/getUpdates` і знайдіть chat ID.
4. Додайте значення в `.env`:

```env
TELEGRAM_BOT_TOKEN=1234567890:ABC...
TELEGRAM_CHAT_IDS=111111111,222222222
```

Після цього перезапустіть застосунок:

```bash
docker compose restart gonetwatch
```

## Метрики

InfluxDB measurement:

```text
network_latency
```

Fields:

| Field | Type | Опис |
| --- | --- | --- |
| `success` | bool | Фінальний результат перевірки |
| `latency_ms` | int | Затримка перевірки в мілісекундах |
| `attempts` | int | Кількість використаних спроб, включно з retries |
| `status_code` | int | HTTP status code, якщо доступний |
| `resolved_count` | int | Кількість DNS-адрес для DNS checks |

Tags:

| Tag | Опис |
| --- | --- |
| `target` | Людська назва target з config |
| `address` | URL, host:port або domain |
| `protocol` | `http`, `http-head`, `tcp` або `dns` |
| `resolver` | DNS resolver, тільки якщо налаштований |

## Grafana dashboard

Dashboard береться з `grafana/provisioning/dashboards/gonetwatch.json`, а datasource — з `grafana/provisioning/datasources/influxdb.yaml`.

Поточні panels:

- Network Latency
- Availability History
- Availability Ratio - Selected Targets
- Uptime per Selected Target
- Average Latency per Selected Target
- Total Failed Checks
- Failure Rate by Target

Dashboard має змінну target, яка заповнюється з recent InfluxDB `target` tag values. Нові targets з'являться після того, як почнуть писати метрики.

## Корисні команди

```bash
# Start або rebuild stack
docker compose up -d --build

# Логи застосунку
docker compose logs -f gonetwatch

# Статус усіх services
docker compose ps

# Перезапуск monitor після зміни config
docker compose restart gonetwatch

# Зупинити services, але залишити volumes
docker compose down

# Видалити services і всі metrics/dashboard volumes
docker compose down -v
```

Локальні перевірки:

```bash
go test ./internal/config
go test ./...
```

## Нотатки про проєкт

- Go module: `GoNetWatch`
- Go version у `go.mod`: `1.24`
- Main package: `cmd/gonetwatch`
- Технічний опис архітектури: [`TECHNICAL.md`](TECHNICAL.md)

GoNetWatch спеціально зроблений простим: конфіг читається без зайвої магії, поведінку видно в логах і Grafana, а основний monitoring loop достатньо компактний, щоб його було легко пояснити й підтримувати.
