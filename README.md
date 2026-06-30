# GoNetWatch

[Українська версія](README.uk.md)

GoNetWatch is a small, practical network monitoring system written in Go. It checks HTTP, TCP, and DNS targets on a schedule, writes metrics to InfluxDB, shows them in Grafana, and can notify Telegram when a target actually changes state.

It is built to be easy to run locally with Docker Compose, but the application itself is a regular Go service with a YAML configuration file.

## What it does

- Checks `http`, `http-head`, `tcp`, and `dns` targets.
- Records latency, success/failure, attempts, HTTP status codes, and DNS resolution count.
- Stores metrics in InfluxDB 2.x using the async write API.
- Ships with a Grafana dashboard provisioned from the repository.
- Sends Telegram alerts only on UP/DOWN transitions, not on every failed check.
- Imports targets from TXT or CSV files and creates a backup before writing config changes.
- Validates configuration before monitoring starts, so common mistakes fail fast.

```text
GoNetWatch ── writes metrics ──> InfluxDB ── visualized by ──> Grafana
     │
     └── sends transition alerts ──> Telegram
```

## Quick start

### 1. Create the environment file

```bash
cp .env.example .env
```

Edit `.env` before the first start. The most important value is `INFLUX_TOKEN`; the same token is used by GoNetWatch for writes and by Grafana for queries.

Example:

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

Do not commit `.env`; it contains credentials.

### 2. Create the runtime config

```bash
cp configs/config.example.yaml configs/config.yaml
```

The example config already contains HTTP, TCP, DNS, failing-test, and protected-endpoint examples. You can keep them for a first run or replace them with your own targets.

### 3. Start everything

```bash
docker compose up -d --build
```

InfluxDB initializes on first start and exposes a healthcheck. Grafana and GoNetWatch wait for InfluxDB to become healthy.

### 4. Open Grafana

```text
http://localhost:3000
```

Log in with `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD` from `.env`. The dashboard is provisioned automatically under the `GoNetWatch` folder with the title **GoNetWatch - Network Monitoring**.

## Configuration

Runtime configuration is read from `configs/config.yaml` by default. You can override the path with `CONFIG_PATH`.

Top-level sections:

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
  token: "" # usually provided by INFLUX_TOKEN
  org: "gonetwatch"
  bucket: "network_metrics"

telegram:
  bot_token: "" # usually provided by TELEGRAM_BOT_TOKEN
  chat_ids: []  # usually provided by TELEGRAM_CHAT_IDS
```

Sensitive values can be kept out of YAML and supplied through environment variables:

| Environment variable | Overrides |
| --- | --- |
| `INFLUX_TOKEN` | `influxdb.token` |
| `TELEGRAM_BOT_TOKEN` | `telegram.bot_token` |
| `TELEGRAM_CHAT_IDS` | `telegram.chat_ids` as comma-separated values |

After editing `configs/config.yaml`, restart only the app container:

```bash
docker compose restart gonetwatch
```

## Target types

| Type | Address format | Success condition |
| --- | --- | --- |
| `http` | `https://example.com/health` | HTTP status `200..399`, unless overridden |
| `http-head` | `https://example.com` | HTTP status `200..399`, unless overridden |
| `tcp` | `host:port` | TCP connection succeeds |
| `dns` | bare domain, e.g. `example.com` | at least one IP address resolves |

`timeout_sec: 0` means protocol defaults are used: 5 seconds for HTTP checks and 3 seconds for TCP/DNS checks. `retries` means additional attempts after the first one. If `retry_delay_ms` is not positive, the runtime fallback is 300 ms.

### HTTP status overrides

Some endpoints are healthy even when they return `401` or `403`. For HTTP and HTTP HEAD checks, use `expected_statuses`:

```yaml
- name: "Protected Admin Page"
  type: "http-head"
  protocol: "http-head"
  address: "https://example.com/admin"
  interval_sec: 30
  expected_statuses: [200, 401, 403]
```

When `expected_statuses` is present, only the listed codes are treated as success.

### DNS with a custom resolver

```yaml
- name: "Google DNS via Cloudflare"
  type: "dns"
  protocol: "dns"
  address: "google.com"
  resolver: "1.1.1.1:53"
  interval_sec: 30
  timeout_sec: 3
```

For DNS targets, the address must be a bare domain, not a URL.

## Importing targets

GoNetWatch has a built-in `import-targets` command. It reads a TXT or CSV file, skips duplicates, validates the final config, creates a timestamped backup, and writes the updated YAML.

### TXT import

Input example:

```txt
https://example.com
github.com:443
google.com

# comments and empty lines are ignored
```

Run a dry-run first:

```bash
go run ./cmd/gonetwatch import-targets --input import/targets.txt --output configs/config.yaml --dry-run
```

Then apply it:

```bash
go run ./cmd/gonetwatch import-targets --input import/targets.txt --output configs/config.yaml
```

Inference rules for TXT input:

- `http://` and `https://` lines become `http-head` targets.
- `host:port` lines become `tcp` targets.
- Other lines use `--default-type` (`http-head` by default).
- Bare hosts imported as HTTP/HTTP HEAD get an `https://` prefix.

### CSV import

Allowed header:

```csv
name,type,address,interval_sec,timeout_sec,retries,retry_delay_ms,resolver,expected_statuses
```

Example:

```csv
name,type,address,interval_sec,timeout_sec,retries,retry_delay_ms,resolver,expected_statuses
Google HTTP HEAD,http-head,https://www.google.com,30,5,1,300,,
GitHub TCP,tcp,github.com:443,20,3,1,300,,
Google DNS,dns,google.com,30,3,1,300,1.1.1.1:53,
Protected API,http-head,https://example.com/admin,30,5,1,300,,200;401;403
```

In CSV, `expected_statuses` are semicolon-separated.

### Import inside Docker

The repository's `import/` directory is mounted to `/app/import` in the container:

```bash
docker compose run --rm gonetwatch import-targets --input /app/import/targets.txt --output /app/configs/config.yaml
docker compose restart gonetwatch
```

## Telegram alerts

Telegram is optional. Leave `TELEGRAM_BOT_TOKEN` or `TELEGRAM_CHAT_IDS` empty to run without alerts.

When enabled, GoNetWatch sends:

- a startup message;
- a DOWN alert when a target changes from UP to DOWN;
- a resolved message when it changes from DOWN to UP;
- a shutdown message on graceful stop.

It does not send a new alert for every failed check while the target is already down.

Setup:

1. Create a bot with [@BotFather](https://t.me/BotFather).
2. Send a message to the bot.
3. Open `https://api.telegram.org/bot<TOKEN>/getUpdates` and find your chat ID.
4. Put values into `.env`:

```env
TELEGRAM_BOT_TOKEN=1234567890:ABC...
TELEGRAM_CHAT_IDS=111111111,222222222
```

Then restart the app:

```bash
docker compose restart gonetwatch
```

## Metrics

InfluxDB measurement:

```text
network_latency
```

Fields:

| Field | Type | Description |
| --- | --- | --- |
| `success` | bool | Final check result |
| `latency_ms` | int | Check latency in milliseconds |
| `attempts` | int | Attempts used, including retries |
| `status_code` | int | HTTP status code, when available |
| `resolved_count` | int | Number of resolved DNS addresses for DNS checks |

Tags:

| Tag | Description |
| --- | --- |
| `target` | Human-readable target name |
| `address` | URL, host:port, or domain |
| `protocol` | `http`, `http-head`, `tcp`, or `dns` |
| `resolver` | DNS resolver, only when configured |

## Grafana dashboard

The dashboard is provisioned from `grafana/provisioning/dashboards/gonetwatch.json` and uses the `influxdb` datasource from `grafana/provisioning/datasources/influxdb.yaml`.

Current panels include:

- Network Latency
- Availability History
- Availability Ratio - Selected Targets
- Uptime per Selected Target
- Average Latency per Selected Target
- Total Failed Checks
- Failure Rate by Target

The dashboard has a target variable populated from recent InfluxDB `target` tag values, so new targets appear after they start producing metrics.

## Useful commands

```bash
# Start or rebuild the stack
docker compose up -d --build

# Follow application logs
docker compose logs -f gonetwatch

# See all services
docker compose ps

# Restart only the monitor after config changes
docker compose restart gonetwatch

# Stop services but keep data volumes
docker compose down

# Remove services and all stored metrics/dashboard state
docker compose down -v
```

Local validation commands:

```bash
go test ./internal/config
go test ./...
```

## Project notes

- Go module: `GoNetWatch`
- Go version in `go.mod`: `1.24`
- Main package: `cmd/gonetwatch`
- Technical architecture notes: [`TECHNICAL.md`](TECHNICAL.md)

GoNetWatch is intentionally straightforward: configuration is readable, the runtime behavior is observable from logs and Grafana, and the main monitoring loop stays small enough to explain and maintain.
