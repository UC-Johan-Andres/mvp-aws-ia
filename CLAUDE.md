# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an **AI Ecosystem** platform that integrates multiple open-source services (Chatwoot, LibreChat, n8n, JupyterLab) via Docker Compose, deployed on AWS EC2. A custom Python **Bridge API** provides unified data access across services.

## Common Commands

### Docker Compose (main orchestration)
```bash
docker-compose up -d              # Start all services
docker-compose down               # Stop all services
docker-compose ps                 # Check service status
docker-compose logs -f <service>  # Tail logs for a specific service
docker-compose restart <service>  # Restart a service
```

### Bridge API development
```bash
cd bridge
docker build -t bridge:latest .
docker run --env-file .env -p 5000:5000 bridge:latest

# With uv (local development)
cd bridge
uv sync
uv run gunicorn main:app
```

### Database initialization (first deploy)
```bash
docker exec chatwoot bundle exec rails db:chatwoot_prepare
```

### AWS deployment
```bash
# Deploy infrastructure
aws cloudformation create-stack --stack-name ai-ecosystem \
  --template-body file://cloudformation.yaml \
  --capabilities CAPABILITY_IAM

# Manual provisioning on EC2
bash user_data.sh
```

## Architecture

### Service Topology
```
Nginx (port 80) — reverse proxy by subdomain
  ├── n8ntest.soylideria.com       → n8n:5678        (PostgreSQL backend)
  ├── chatwoottest.soylideria.com  → chatwoot:3000   (PostgreSQL + Redis)
  ├── chat.soylideria.com          → librechat:3080  (MongoDB backend)
  └── deepnotetest.soylideria.com  → jupyterlab:8888

Bridge API (port 5000) — custom data connector
  ├── Reads Chatwoot data from PostgreSQL
  └── Reads LibreChat data from MongoDB
```

### Databases
- **PostgreSQL** (pg15vector-alpine): shared by Chatwoot and n8n
- **MongoDB 6.0**: used by LibreChat
- **Redis 7**: used by Chatwoot (sessions/caching)

### Bridge API (`bridge/main.py`)
Flask API with API key auth (`X-API-Key` header). Exposes:
- `/chatwoot/conversations`, `/chatwoot/messages` — reads from PostgreSQL
- `/librechat/conversations`, `/librechat/messages`, `/librechat/users` — reads from MongoDB
- `/health` — health check

### Environment Files
Each service has its own `.env` file (all gitignored):
- `bridge/.env` — `BRIDGE_API_KEY`, Postgres/Mongo connection vars
- `.env.chatwoot` — Rails config, Postgres, Redis, `FRONTEND_URL`
- `.env.librechat` — Mongo URI, JWT secrets, `OPENROUTER_KEY`
- `.env.n8n` — Postgres config, `N8N_ENCRYPTION_KEY`, basic auth

### AWS Infrastructure (`cloudformation.yaml` + `user_data.sh`)
- VPC with 1 public + 2 private subnets, EC2 with IAM role
- Secrets stored in **AWS Parameter Store (SSM)** — `user_data.sh` pulls them at boot
- `user_data.sh` handles: disk expansion, Docker install, 4GB swap, repo clone, env file population, service startup with health checks

### LibreChat Model Configuration (`librechat.yaml`)
Uses OpenRouter with model `z-ai/glm-4.5-air:free`. Auto-fetches available models.

## Key Constraints

- **Memory limits** are set per service in `docker-compose.yml` — stay within them when modifying services (e.g., n8n: 300MB, Chatwoot: 400MB, MongoDB: 256MB).
- **Service startup order** matters: databases must pass health checks before apps start.
- The `ai_stack` Docker bridge network connects all services.
- `docker-compose.override.yml` is gitignored — use it for local overrides.
