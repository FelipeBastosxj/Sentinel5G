# EventStream Observability Engine

> Distributed Messaging Observability Platform — real-time event ingestion, processing, telemetry and reliability analytics.

---

## Quick Start

> **Only Docker is required.** No Node.js, no WSL config, no npm install.

```bash
# 1. Clone
git clone https://github.com/FelipeBastosxj/eventstream-observability-engine.git
cd eventstream-observability-engine

# 2. Environment
cp .env.example .env

# 3. Start infrastructure (Kafka, Redis, ClickHouse)
docker compose -f infra/docker-compose.yml up -d

# 4. Wait ~60s for Kafka to be ready, then build and start all services
docker compose -f infra/docker-compose.yml --profile services up -d --build

# 5. Check everything is running
docker ps
```

---

## Service URLs

| Service | URL | Description |
|---------|-----|-------------|
| Ingestion Service | http://localhost:3001 | `POST /ingest` |
| Webhook Service | http://localhost:3002 | `/integrations/*` |
| Processing Service | http://localhost:3003 | Internal consumer |
| Realtime Gateway | http://localhost:3004 | WebSocket `/ws` |
| Kafka UI | http://localhost:8080 | Browse topics |

---

## Test it

```bash
# Health checks
curl http://localhost:3001/health
curl http://localhost:3002/health
curl http://localhost:3003/health
curl http://localhost:3004/health

# Ingest a test event
curl -X POST http://localhost:3001/ingest \
  -H "Content-Type: application/json" \
  -d '{"channel":"sms","source":"twilio","payload":{"messageId":"MSG-001","status":"delivered"}}'

# Simulate a Twilio webhook
curl -X POST http://localhost:3002/integrations/twilio/webhook \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123&MessageStatus=delivered&To=%2B5511999999999&From=%2B15017122661"

# Watch events flow through Kafka
# Open http://localhost:8080 → Topics → events.raw / events.processed
```

---

## Stop everything

```bash
docker compose -f infra/docker-compose.yml --profile services down
```

---

## Prerequisites

| Tool | Version | Required |
|------|---------|----------|
| Node.js | >= 22.x | ✅ |
| npm | >= 10.x | ✅ |
| Docker | >= 27.x | ✅ |
| Docker Compose | >= 2.x | ✅ |
| WSL 2 (Windows only) | Ubuntu 22.04+ | ✅ Windows |
| GNU Make | any | ✅ Linux/Mac |

---

## Windows Setup (WSL + Docker)

> Skip this section if you are on Linux or macOS.

### 1. Install WSL 2

Open PowerShell as Administrator:

```powershell
wsl --install
# Restart the computer after installation
```

### 2. Install Docker inside WSL

Open a WSL terminal and run:

```bash
# Install Docker Engine
sudo apt-get update -y
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Allow running Docker without sudo
sudo usermod -aG docker $USER
sudo chmod 666 /var/run/docker.sock

# Start Docker service
sudo service docker start

# Verify
docker --version
docker compose version
```

### 3. Auto-start Docker on every WSL session

```bash
echo 'sudo service docker start 2>/dev/null' >> ~/.bashrc
echo 'sudo chmod 666 /var/run/docker.sock 2>/dev/null' >> ~/.bashrc
source ~/.bashrc
```

### 4. Install Node.js inside WSL

```bash
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.7/install.sh | bash
source ~/.bashrc
nvm install 22
nvm use 22
node --version  # v22.x.x
npm --version   # 10.x.x
```

### 5. Open the project inside WSL

```bash
cd /mnt/c/Users/<YourUser>/Desktop/eventstream-observability-engine
```

---

## Running the Project

### Step 1 — Copy environment file

```bash
cp .env.example .env
```

### Step 2 — Install dependencies

```bash
npm install
```

### Step 3 — Start infrastructure

```bash
npm run infra:up
```

Starts: **Kafka**, **Zookeeper**, **Redis**, **ClickHouse**, **Kafka UI**

Verify:
```bash
docker ps
# Should show 5 containers running
```

### Step 4 — Start backend services

Open **4 separate terminals** and run one command per terminal:

```bash
# Terminal 1 — Ingestion Service  →  http://localhost:3001
npm run services:ingestion
```

```bash
# Terminal 2 — Webhook Service    →  http://localhost:3002
npm run services:webhook
```

```bash
# Terminal 3 — Processing Service →  http://localhost:3003
npm run services:processing
```

```bash
# Terminal 4 — Realtime Gateway   →  http://localhost:3004
npm run services:gateway
```

Wait for each service to print:
```
Application is running on: http://[::1]:300X
```

### Step 5 — Start frontend

```bash
# Terminal 5 — Angular Dashboard  →  http://localhost:4200
npm run frontend
```

### Step 6 (Optional) — Start observability stack

```bash
npm run obs:up
```

Starts: **Prometheus**, **Loki**, **Tempo**, **Grafana**, **OTel Collector**

---

## Service URLs

| Service | URL | Description |
|---------|-----|-------------|
| Angular Dashboard | http://localhost:4200 | Frontend |
| Ingestion Service | http://localhost:3001 | Event ingestion API |
| Webhook Service | http://localhost:3002 | Provider webhooks |
| Processing Service | http://localhost:3003 | Event processing |
| Realtime Gateway | http://localhost:3004 | WebSocket gateway |
| Kafka UI | http://localhost:8080 | Kafka browser |
| Grafana | http://localhost:3000 | Dashboards (admin/admin) |
| Prometheus | http://localhost:9090 | Metrics |
| Loki | http://localhost:3100 | Logs |

---

## Testing

### Health checks

```bash
curl http://localhost:3001/health
curl http://localhost:3002/health
curl http://localhost:3003/health
curl http://localhost:3004/health
```

### Ingest a test event

```bash
curl -X POST http://localhost:3001/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "sms",
    "source": "twilio",
    "payload": {
      "messageId": "MSG-001",
      "status": "delivered",
      "to": "+5511999999999"
    }
  }'
```

### Simulating a provider webhook

```bash
# Twilio webhook
curl -X POST http://localhost:3002/integrations/twilio/webhook \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "MessageSid=SM123&MessageStatus=delivered&To=%2B5511999999999&From=%2B15017122661"

# Infobip webhook
curl -X POST http://localhost:3002/integrations/infobip/webhook \
  -H "Content-Type: application/json" \
  -d '{"results":[{"messageId":"MSG-002","status":{"name":"DELIVERED"},"to":"+5511999999999"}]}'
```

### Prometheus metrics

```bash
curl http://localhost:3001/metrics
```

### Run all unit tests

```bash
npm run test
```

---

## All Available Commands

```bash
# Infrastructure
npm run infra:up          # Start Kafka, Redis, ClickHouse, Zookeeper
npm run infra:down        # Stop infrastructure
npm run infra:reset       # Stop and remove all volumes
npm run infra:logs        # Tail infrastructure logs

# Observability stack
npm run obs:up            # Start Prometheus, Loki, Tempo, Grafana, OTel Collector
npm run obs:down          # Stop observability stack
npm run obs:logs          # Tail observability logs

# Backend services
npm run services:ingestion   # Start ingestion-service  (:3001)
npm run services:webhook     # Start webhook-service    (:3002)
npm run services:processing  # Start processing-service (:3003)
npm run services:gateway     # Start realtime-gateway   (:3004)

# Frontend
npm run frontend          # Start Angular dashboard (:4200)
npm run frontend:build    # Build Angular for production

# Build
npm run build             # Build all services
npm run build:ingestion   # Build ingestion-service only
npm run build:webhook     # Build webhook-service only
npm run build:processing  # Build processing-service only
npm run build:gateway     # Build realtime-gateway only

# Lint
npm run lint              # Lint all services
npm run lint:ingestion    # Lint ingestion-service only
npm run lint:webhook      # Lint webhook-service only
npm run lint:processing   # Lint processing-service only
npm run lint:gateway      # Lint realtime-gateway only

# Tests
npm run test              # Run all tests
npm run test:ingestion    # Test ingestion-service only
npm run test:webhook      # Test webhook-service only
npm run test:processing   # Test processing-service only
npm run test:gateway      # Test realtime-gateway only

# Utilities
npm run env               # Copy .env.example to .env
npm run reset             # Full reset (stop + remove volumes)
npm run logs              # Tail all infrastructure logs
npm run help              # List all available commands
```

> **Linux/macOS users:** All commands above are also available via `make`.
> See the [Makefile](./Makefile) for the full list.

---

## Architecture

```
External Providers (Twilio, Infobip, SendGrid)
        │
        ▼
  webhook-service          ← validates + normalizes payloads
        │
        ▼ Kafka: events.raw
  ingestion-service        ← canonical event creation
        │
        ▼ Kafka: events.raw
  processing-service       ← enrichment + retry + DLQ
        │
        ├──▶ Kafka: events.processed
        │         │
        │         ▼
        │   realtime-gateway  ← WebSocket broadcast (Redis fan-out)
        │         │
        │         ▼
        │   Angular Dashboard ← live event stream + metrics
        │
        └──▶ Kafka: events.metrics
                  │
                  ▼
             ClickHouse       ← historical analytics (Phase 3)
```

---

## Documentation

| Document | Description |
|----------|-------------|
| [docs/getting-started.md](./docs/getting-started.md) | Detailed setup guide |
| [docs/architecture.md](./docs/architecture.md) | Architecture decisions |
| [docs/event-model.md](./docs/event-model.md) | Canonical Event Model |
| [docs/integrations.md](./docs/integrations.md) | Provider integrations |
| [docs/observability.md](./docs/observability.md) | Observability stack |

---

## License

MIT
