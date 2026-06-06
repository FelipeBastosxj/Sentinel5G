# Getting Started

This guide covers everything needed to run the EventStream Observability Engine locally.

---

## Prerequisites

| Tool | Required | Notes |
|------|----------|-------|
| Docker Desktop | ✅ **Required** | https://www.docker.com/products/docker-desktop |
| Docker Compose v2 | ✅ **Required** | Bundled with Docker Desktop |
| Node.js >= 22 | ⚙️ Dev only | Only needed to run services locally without Docker |
| GNU Make | ⚙️ Linux/macOS | Optional — all commands also available via `npm run` |

> **Node.js is NOT required to run the project.** Everything runs inside Docker containers.

---

## Install Docker Desktop

### Windows

1. Download: https://www.docker.com/products/docker-desktop
2. Run the installer — WSL 2 backend is configured automatically
3. Start Docker Desktop and wait for the whale icon in the taskbar to be steady

```powershell
# Verify in PowerShell
docker --version
docker compose version
```

### Linux

```bash
# Ubuntu/Debian
sudo apt-get update -y
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker $USER && newgrp docker
```

### macOS

```bash
# Via Homebrew
brew install --cask docker
# Then open Docker.app and wait for it to start
```

---

## Running the Project

### 1. Clone and configure

```bash
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine
cp .env.example .env
```

### 2. Start infrastructure

```bash
docker compose -f infra/docker-compose.yml up -d
```

Starts: **Zookeeper**, **Kafka**, **Redis**, **ClickHouse**, **Kafka UI**

```bash
# Verify — all containers should show (healthy) or (running)
docker ps
```

> ⚠️ **Wait ~60 seconds** for Kafka to be fully ready before the next step.
> Watch with: `docker logs eventstream-kafka -f`

### 3. Build and start backend services

```bash
docker compose -f infra/docker-compose.yml --profile services up -d --build
```

This builds the Docker images and starts all 4 services:

| Container | URL |
|-----------|-----|
| `eventstream-ingestion` | http://localhost:3001 |
| `eventstream-webhook` | http://localhost:3002 |
| `eventstream-processing` | http://localhost:3003 |
| `eventstream-gateway` | http://localhost:3004 |

Follow build logs:
```bash
docker compose -f infra/docker-compose.yml --profile services logs -f
```

### 4. (Optional) Start observability stack

```bash
docker compose -f infra/docker-compose.yml --profile observability up -d
```

Starts: **Prometheus** (:9090), **Loki** (:3100), **Tempo** (:3200), **Grafana** (:3000), **OTel Collector**

---

## Verify Everything Works

### Health checks

```bash
curl http://localhost:3001/health   # {"status":"ok"}
curl http://localhost:3002/health   # {"status":"ok"}
curl http://localhost:3003/health   # {"status":"ok"}
curl http://localhost:3004/health   # {"status":"ok"}
```

### Send a test event

```bash
curl -X POST http://localhost:3001/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "sms",
    "source": "twilio",
    "payload": { "messageId": "MSG-001", "status": "delivered", "to": "+5511999999999" }
  }'
# Expected: {"accepted":true,"eventId":"...","correlationId":"..."}
```

### Simulate a Twilio webhook

```bash
curl -X POST http://localhost:3002/integrations/twilio/webhook \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123&MessageStatus=delivered&To=%2B5511999999999&From=%2B15017122661"
# Expected: {"accepted":true}
```

### Watch events flow through Kafka

Open http://localhost:8080 → Topics:
- `events.raw` — events published by ingestion-service
- `events.processed` — events enriched by processing-service
- `events.metrics` — aggregated metrics
- `events.alerts` — DLQ (failed events)

### Prometheus metrics

```bash
curl http://localhost:3001/metrics
```

---

## Service URLs Reference

| Service | URL | Notes |
|---------|-----|-------|
| Ingestion Service | http://localhost:3001 | `POST /ingest`, `GET /health`, `GET /metrics` |
| Webhook Service | http://localhost:3002 | `POST /integrations/{provider}/webhook` |
| Processing Service | http://localhost:3003 | Internal Kafka consumer |
| Realtime Gateway | http://localhost:3004 | WebSocket at `/ws` |
| Kafka UI | http://localhost:8080 | Browse topics and messages |
| Grafana | http://localhost:3000 | Dashboards — no login required |
| Prometheus | http://localhost:9090 | Metrics query browser |
| Loki | http://localhost:3100 | Log aggregation |
| Tempo | http://localhost:3200 | Distributed traces |

---

## All Commands Reference

### Docker (recommended — works on any OS)

```bash
# Infrastructure
docker compose -f infra/docker-compose.yml up -d                          # start infra
docker compose -f infra/docker-compose.yml down                           # stop infra
docker compose -f infra/docker-compose.yml down -v                        # stop + remove volumes
docker compose -f infra/docker-compose.yml logs -f                        # tail infra logs

# Backend services
docker compose -f infra/docker-compose.yml --profile services up -d --build  # build + start
docker compose -f infra/docker-compose.yml --profile services down           # stop
docker compose -f infra/docker-compose.yml --profile services logs -f        # tail logs
docker compose -f infra/docker-compose.yml --profile services build          # rebuild images

# Observability
docker compose -f infra/docker-compose.yml --profile observability up -d     # start
docker compose -f infra/docker-compose.yml --profile observability down      # stop
```

### npm aliases (same commands, shorter)

```bash
npm run infra:up          # start infrastructure
npm run infra:down        # stop infrastructure
npm run infra:reset       # stop + remove volumes
npm run infra:logs        # tail infrastructure logs

npm run services:up       # build + start all services (Docker)
npm run services:down     # stop services
npm run services:build    # rebuild Docker images
npm run services:logs     # tail service logs

npm run obs:up            # start observability stack
npm run obs:down          # stop observability stack
npm run obs:logs          # tail observability logs
```

### Makefile (Linux/macOS only)

```bash
make infra-up       # start infrastructure
make infra-down     # stop infrastructure
make services-up    # build + start services (Docker)
make services-down  # stop services
make obs-up         # start observability
make obs-down       # stop observability
```

---

## Stopping the Project

```bash
# Stop services + infra
docker compose -f infra/docker-compose.yml --profile services down
docker compose -f infra/docker-compose.yml down

# Full reset — removes all containers, volumes and data
docker compose -f infra/docker-compose.yml down -v
```

---

## Local Development (without Docker for services)

> Only needed if you want to edit service code and see changes immediately.
> Requires Node.js >= 22 installed locally.

```bash
# Install dependencies
npm install

# Start infra first (still uses Docker)
npm run infra:up

# Run each service locally in separate terminals
npm run services:ingestion   # Terminal 1 — :3001
npm run services:webhook     # Terminal 2 — :3002
npm run services:processing  # Terminal 3 — :3003
npm run services:gateway     # Terminal 4 — :3004

# Run Angular dashboard
npm run frontend             # Terminal 5 — :4200
```

> **Windows users:** If `npm install` fails with EPERM/chmod errors,
> run it from **PowerShell** (not WSL) or move the project to the WSL
> native filesystem (`~/`) and run from there.

---

## Troubleshooting

### Kafka container is unhealthy

Kafka takes 30–60 seconds to start. Wait and check:
```bash
docker logs eventstream-kafka --tail 20
docker ps   # wait for (healthy)
```
If still failing:
```bash
docker compose -f infra/docker-compose.yml down -v
docker compose -f infra/docker-compose.yml up -d
```

### Docker permission denied (WSL/Linux)

```bash
sudo service docker start
sudo chmod 666 /var/run/docker.sock
# Or add user to docker group permanently:
sudo usermod -aG docker $USER && newgrp docker
```

### Port already in use

```bash
# Find and kill the process using the port
sudo lsof -i :3001
sudo kill -9 <PID>
```

### Service build fails

```bash
# Rebuild from scratch (no cache)
docker compose -f infra/docker-compose.yml --profile services build --no-cache
```

### Windows: npm install EPERM/chmod error

Run from PowerShell (not WSL terminal):
```powershell
npm install
```
Or move the project to WSL native filesystem and install there:
```bash
cd ~
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine && npm install
```
