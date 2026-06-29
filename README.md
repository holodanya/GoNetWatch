# GoNetWatch

Network monitoring system written in Go. Periodically checks HTTP, TCP, and DNS endpoints, stores time-series metrics in InfluxDB, visualizes them in Grafana, and sends alerts via Telegram on UP/DOWN transitions.

## Architecture

```
GoNetWatch (Go)
  │
  ├── writes metrics ──► InfluxDB 2.x ──► Grafana (auto-provisioned dashboard)
  │
  └── sends alerts ────► Telegram (UP/DOWN transitions only)
```

All services run via Docker Compose. No manual UI setup required — InfluxDB and Grafana are fully configured on first start.

---

## Quick Start

### 1. Copy and edit the env file

```bash
cp .env.example .env
```

Open `.env` and set a strong token (used by both InfluxDB and GoNetWatch):

```bash
# Generate a random token
openssl rand -hex 32
```

Paste the result as `INFLUX_TOKEN` in `.env`. Change passwords from the defaults.

### 2. Create your config

```bash
cp configs/config.example.yaml configs/config.yaml
# Edit configs/config.yaml — add your own targets or keep the examples
```

### 3. Start the stack

```bash
docker compose up -d
```

InfluxDB initialises automatically on first start (~30 seconds). GoNetWatch waits for it to be healthy before starting.

### 4. Open Grafana

```
http://localhost:3000
```

Login with `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD` from your `.env` (defaults: `admin` / `changeme`).

The **GoNetWatch — Network Monitoring** dashboard is available under the **GoNetWatch** folder immediately after startup.

---

## Configuring Targets

Edit `configs/config.yaml`. After changes, restart only the app:

```bash
docker compose restart gonetwatch
```

### Supported check types

| Type | What it does | Address format |
|------|-------------|----------------|
| `http` | HTTP GET, checks 2xx status | `https://example.com/path` |
| `http-head` | HTTP HEAD, no body — lightweight | `https://example.com/path` |
| `tcp` | TCP connect to host:port | `host:443` |
| `dns` | DNS resolution of a domain | `example.com` |

### Target fields

```yaml
- name: "My Service"          # human-readable name shown in Grafana and logs
  type: "http-head"
  protocol: "http-head"       # same as type (kept for backward compatibility)
  address: "https://api.example.com/health"
  interval_sec: 30            # how often to check (seconds)
  timeout_sec: 5              # per-attempt timeout; 0 = default (HTTP: 5s, TCP/DNS: 3s)
  retries: 2                  # extra attempts on failure; 0 = no retry
  retry_delay_ms: 300         # delay between retries (milliseconds)
```

### DNS check with a specific resolver

```yaml
- name: "GitHub DNS via Google"
  type: "dns"
  protocol: "dns"
  address: "github.com"       # bare domain — no http:// prefix
  resolver: "8.8.8.8:53"     # optional; omit to use the system default
  interval_sec: 30
  timeout_sec: 3
  retries: 1
  retry_delay_ms: 300
```

### Import targets

TXT input: one target per line. Empty lines and lines starting with `#` are ignored.

```txt
https://example.com
https://www.google.com
github.com:443
google.com

# comment
```

CSV input supports these headers:

```csv
name,type,address,interval_sec,timeout_sec,retries,retry_delay_ms,resolver
Google HTTP HEAD,http-head,https://www.google.com,30,5,1,300,
GitHub TCP,tcp,github.com:443,20,3,1,300,
Google DNS,dns,google.com,30,3,1,300,1.1.1.1:53
```

Dry-run:

```bash
go run ./cmd/gonetwatch import-targets --input targets.txt --output configs/config.yaml --dry-run
```

Import:

```bash
go run ./cmd/gonetwatch import-targets --input targets.txt --output configs/config.yaml
```

Docker usage:

```bash
docker compose run --rm gonetwatch import-targets --input /app/import/targets.txt --output /app/configs/config.yaml
```

After import:

```bash
docker compose restart gonetwatch
```

---

## Telegram Alerts

Alerts fire only on **state transitions** (UP → DOWN and DOWN → UP), not on every check result.

1. Create a bot via [@BotFather](https://t.me/BotFather) and copy the token.
2. Get your chat ID: send any message to the bot, then open:
   `https://api.telegram.org/bot<TOKEN>/getUpdates`
3. Edit `.env`:
   ```
   TELEGRAM_BOT_TOKEN=1234567890:ABC-xyz...
   TELEGRAM_CHAT_IDS=123456789
   ```
   Multiple recipients (comma-separated): `TELEGRAM_CHAT_IDS=111,222,333`
4. Restart the app:
   ```bash
   docker compose restart gonetwatch
   ```

---

## Checking That Everything Works

```bash
# Follow GoNetWatch logs
docker compose logs -f gonetwatch

# All services
docker compose logs -f

# Container health
docker compose ps
```

Healthy startup output:
```
level=INFO msg="GoNetWatch starting" targets=6
level=INFO msg="Starting network monitoring" targets=6
level=INFO msg="Target check successful" target="Google HTTP HEAD" protocol=http-head latency=42ms
```

---

## Stopping

```bash
docker compose down
```

Data volumes (InfluxDB metrics, Grafana settings) are preserved for the next start.

---

## Resetting All Data

To wipe stored metrics and Grafana customizations and start fresh:

```bash
docker compose down -v
docker compose up -d
```

> **Warning:** `-v` deletes all Docker volumes. All metrics history is lost.

---

## Metrics Reference

Measurement name: `network_latency`

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool (0/1) | Whether the check passed |
| `latency_ms` | int | Round-trip time in milliseconds |
| `attempts` | int | Attempts made, including retries |
| `status_code` | int | HTTP status code (HTTP checks only) |
| `resolved_count` | int | IPs resolved (DNS checks only) |

Tags:

| Tag | Description |
|-----|-------------|
| `target` | Human-readable target name from config |
| `address` | Technical endpoint (URL, host:port, or domain) |
| `protocol` | Check type: `http`, `http-head`, `tcp`, `dns` |
| `resolver` | DNS server used (DNS checks with custom resolver only) |

---

## Dashboard

The auto-provisioned **GoNetWatch — Network Monitoring** dashboard contains:

| Panel | Description |
|-------|-------------|
| **Current Status** | Live UP/DOWN state per target, color-coded |
| **Availability %** | Success rate over the selected time range, per target |
| **Latency (ms)** | Mean response time over time, per target |
| **Target Status Table** | Snapshot of latest status, latency, and retry count per target |

Default refresh: 30 seconds. Default time range: last 1 hour.
