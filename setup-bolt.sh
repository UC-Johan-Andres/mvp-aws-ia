#!/bin/bash
# =============================================================================
# setup-bolt.sh — Clona bolt.diy, aplica Dockerfile optimizado y configura
#                 las variables de entorno desde AWS Parameter Store.
#
# Uso:
#   chmod +x setup-bolt.sh
#   ./setup-bolt.sh
#
# Requisitos:
#   - git
#   - docker o podman
#   - aws cli configurado con permisos ssm:GetParameter
# =============================================================================

set -euo pipefail

# -----------------------------------------------------------------------------
# CONFIGURACIÓN — ajustá estos valores según tu entorno
# -----------------------------------------------------------------------------

# Repositorio fuente (proyecto open source, no modificar el código)
REPO_URL="https://github.com/stackblitz-labs/bolt.diy.git"
REPO_BRANCH="main"

# Directorio donde se clonará el proyecto
INSTALL_DIR="./bolt.diy"

# Nombre de la imagen Docker
IMAGE_NAME="bolt-ai"
IMAGE_TAG="production"

# Comando docker (usar "docker" en EC2/Linux, "podman" en otros entornos)
# Podés sobreescribir desde el entorno: DOCKER_CMD=pdr ./setup-bolt.sh
DOCKER_CMD="${DOCKER_CMD:-docker}"

# Puerto en el que correrá la app
APP_PORT="5173"

# AWS Parameter Store — rutas de los parámetros
PARAM_OPENROUTER_KEY="/ai-ecosystem/openrouter-key"

# Región AWS (podés sobreescribir: AWS_REGION=us-east-1 ./setup-bolt.sh)
AWS_REGION="${AWS_REGION:-us-east-1}"

# Valores por defecto para .env.local
DEFAULT_LOG_LEVEL="debug"
DEFAULT_NUM_CTX="32768"

# -----------------------------------------------------------------------------
# COLORES para output legible
# -----------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info()    { echo -e "${BLUE}[INFO]${NC}  $1"; }
log_success() { echo -e "${GREEN}[OK]${NC}    $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}  $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# -----------------------------------------------------------------------------
# PASO 1 — Verificar prerrequisitos
# -----------------------------------------------------------------------------
log_info "Verificando prerrequisitos..."

command -v git        &>/dev/null || log_error "git no está instalado."
command -v aws        &>/dev/null || log_error "aws cli no está instalado. Ver: https://aws.amazon.com/cli/"
command -v "$DOCKER_CMD" &>/dev/null || log_error "'$DOCKER_CMD' no está instalado."

log_success "Prerrequisitos OK (git, aws, $DOCKER_CMD)"

# -----------------------------------------------------------------------------
# PASO 2 — Obtener variables desde AWS Parameter Store
# -----------------------------------------------------------------------------
log_info "Obteniendo variables desde AWS Parameter Store (región: $AWS_REGION)..."

OPENROUTER_KEY=$(aws ssm get-parameter \
  --name "$PARAM_OPENROUTER_KEY" \
  --with-decryption \
  --region "$AWS_REGION" \
  --query "Parameter.Value" \
  --output text 2>/dev/null) \
  || log_error "No se pudo obtener '$PARAM_OPENROUTER_KEY' de Parameter Store.
  Verificá que:
  - Estés autenticado en AWS (aws configure)
  - El parámetro exista en la región $AWS_REGION
  - Tu rol/usuario tenga permiso ssm:GetParameter"

[[ -z "$OPENROUTER_KEY" ]] && log_error "El parámetro '$PARAM_OPENROUTER_KEY' está vacío."

log_success "OPEN_ROUTER_API_KEY obtenida correctamente."

# -----------------------------------------------------------------------------
# PASO 3 — Clonar el repositorio
# -----------------------------------------------------------------------------
if [[ -d "$INSTALL_DIR" ]]; then
  log_warn "El directorio '$INSTALL_DIR' ya existe. Actualizando..."
  git -C "$INSTALL_DIR" pull origin "$REPO_BRANCH"
else
  log_info "Clonando bolt.diy desde GitHub..."
  git clone --depth=1 --branch "$REPO_BRANCH" "$REPO_URL" "$INSTALL_DIR"
fi

log_success "Repositorio listo en '$INSTALL_DIR'."

# -----------------------------------------------------------------------------
# PASO 4 — Aplicar Dockerfile optimizado
# (Reemplaza el Dockerfile original con la versión que resuelve los problemas
#  conocidos: imagen limpia, glibc, bash, ca-certificates, wrangler, etc.)
# -----------------------------------------------------------------------------
log_info "Aplicando Dockerfile optimizado..."

cat > "$INSTALL_DIR/Dockerfile" << 'DOCKERFILE'
# ---- base stage ----
# bookworm-slim required: Cloudflare workerd binary needs glibc (incompatible with Alpine/musl)
FROM node:22-bookworm-slim AS base
WORKDIR /app
ENV HUSKY=0
ENV CI=true
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# ---- build stage ----
FROM base AS build

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
  && update-ca-certificates \
  && rm -rf /var/lib/apt/lists/*

# Accept (optional) build-time public URL for Remix/Vite (Coolify can pass it)
ARG VITE_PUBLIC_APP_URL
ENV VITE_PUBLIC_APP_URL=${VITE_PUBLIC_APP_URL}

# Install deps efficiently (cache layer separate from source)
COPY package.json pnpm-lock.yaml* ./
RUN pnpm fetch

# Copy source and build
COPY . .
RUN pnpm install --offline --frozen-lockfile

# Build the Remix app (SSR + client)
RUN NODE_OPTIONS=--max-old-space-size=4096 pnpm run build

# ---- production stage (clean image — no source code, no pnpm cache) ----
# wrangler is in devDependencies but is required at runtime as the server,
# so we copy node_modules from the build stage (before any pruning).
FROM node:22-bookworm-slim AS bolt-ai-production
WORKDIR /app

ENV NODE_ENV=production
ENV PORT=5173
ENV HOST=0.0.0.0
ENV WRANGLER_SEND_METRICS=false
ENV RUNNING_IN_DOCKER=true

ARG VITE_LOG_LEVEL=debug
ARG DEFAULT_NUM_CTX
ENV VITE_LOG_LEVEL=${VITE_LOG_LEVEL} \
    DEFAULT_NUM_CTX=${DEFAULT_NUM_CTX}

# Enable pnpm, install curl, bash and ca-certificates (workerd needs valid CA certs for outbound fetch)
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate && \
    apt-get update && apt-get install -y --no-install-recommends curl bash ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy only what is needed to run — no source code, no pnpm store cache
COPY --from=build /app/build /app/build
COPY --from=build /app/node_modules /app/node_modules
COPY --from=build /app/package.json /app/package.json
COPY --from=build /app/bindings.sh /app/bindings.sh
# functions/ contains the Cloudflare Pages SSR worker (functions/[[path]].ts)
# wrangler pages dev needs it to serve Remix SSR — without it returns 404
COPY --from=build /app/functions /app/functions
# wrangler.toml provides compatibility_date, nodejs_compat flag and pages_build_output_dir
# without it wrangler fails to compile functions/[[path]].ts
COPY --from=build /app/wrangler.toml /app/wrangler.toml
# worker-configuration.d.ts is read by bindings.sh to discover env var names
COPY --from=build /app/worker-configuration.d.ts /app/worker-configuration.d.ts

# Pre-configure wrangler, fix possible CRLF line endings (Windows/WSL2), set permissions
RUN mkdir -p /root/.config/.wrangler && \
    echo '{"enabled":false}' > /root/.config/.wrangler/metrics.json && \
    sed -i 's/\r$//' /app/bindings.sh && \
    chmod +x /app/bindings.sh

EXPOSE 5173

# Healthcheck for deployment platforms
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
  CMD curl -fsS http://localhost:5173/ || exit 1

CMD ["pnpm", "run", "dockerstart"]


# ---- development stage ----
FROM build AS development

ARG VITE_LOG_LEVEL=debug
ARG DEFAULT_NUM_CTX
ENV VITE_LOG_LEVEL=${VITE_LOG_LEVEL} \
    DEFAULT_NUM_CTX=${DEFAULT_NUM_CTX} \
    RUNNING_IN_DOCKER=true

RUN mkdir -p /app/run
CMD ["pnpm", "run", "dev", "--host"]
DOCKERFILE

log_success "Dockerfile optimizado aplicado."

# -----------------------------------------------------------------------------
# PASO 5 — Aplicar .dockerignore optimizado
# -----------------------------------------------------------------------------
log_info "Aplicando .dockerignore optimizado..."

cat > "$INSTALL_DIR/.dockerignore" << 'DOCKERIGNORE'
# Git
.git
.github/

# Husky
.husky/

# Documentation (not needed for build)
docs/
CONTRIBUTING.md
LICENSE
README.md
FAQ.md
CHANGES.md
changelog.md
PROJECT.md
*.md

# Environment / secrets
.env
*.local
*.example

# Node / build artifacts
**/*.log
**/node_modules
**/dist
**/build
**/.cache
logs
dist-ssr
.DS_Store

# Electron (not needed for web build)
electron/

# Test / lint / CI config
playwright.config*.ts
.lighthouserc.json
.depcheckrc.json
eslint.config.mjs
vitest.config.*
test-workflows.sh

# Scripts not needed inside container
scripts/
DOCKERIGNORE

log_success ".dockerignore optimizado aplicado."

# -----------------------------------------------------------------------------
# PASO 6 — Crear .env.local con las variables obtenidas de Parameter Store
# -----------------------------------------------------------------------------
log_info "Creando .env.local..."

cat > "$INSTALL_DIR/.env.local" << EOF
OPEN_ROUTER_API_KEY=${OPENROUTER_KEY}
VITE_LOG_LEVEL=${DEFAULT_LOG_LEVEL}
DEFAULT_NUM_CTX=${DEFAULT_NUM_CTX}
EOF

log_success ".env.local creado con las variables de Parameter Store."

# -----------------------------------------------------------------------------
# PASO 7 — Construir la imagen Docker
# -----------------------------------------------------------------------------
log_info "Construyendo imagen Docker '$IMAGE_NAME:$IMAGE_TAG'..."
log_warn "Esto puede tardar varios minutos la primera vez."

"$DOCKER_CMD" build \
  -t "$IMAGE_NAME:$IMAGE_TAG" \
  --target bolt-ai-production \
  "$INSTALL_DIR"

log_success "Imagen '$IMAGE_NAME:$IMAGE_TAG' construida correctamente."

# -----------------------------------------------------------------------------
# RESUMEN FINAL
# -----------------------------------------------------------------------------
echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  Setup completado exitosamente${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo -e "  Proyecto:  ${BLUE}$INSTALL_DIR${NC}"
echo -e "  Imagen:    ${BLUE}$IMAGE_NAME:$IMAGE_TAG${NC}"
echo -e "  Puerto:    ${BLUE}$APP_PORT${NC}"
echo ""
echo -e "  Para correr el contenedor:"
echo -e "  ${YELLOW}$DOCKER_CMD run -p $APP_PORT:$APP_PORT --env-file $INSTALL_DIR/.env.local $IMAGE_NAME:$IMAGE_TAG${NC}"
echo ""
echo -e "  Luego abrí: ${BLUE}http://localhost:$APP_PORT${NC}"
echo ""
