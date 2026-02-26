#!/bin/bash
set -e

echo "========================================"
echo "AI Ecosystem - EC2 Provisioning Script"
echo "========================================"

# Expand root partition to use all available disk space
echo "[0/10] Expanding disk partition..."
sudo growpart /dev/nvme0n1 1 2>/dev/null || true
sudo xfs_growfs / 2>/dev/null || true

echo "[1/10] Updating system and installing dependencies..."
sudo yum update -y
sudo yum install -y git unzip openssl

echo "[2/10] Installing Docker..."
if ! command -v docker &>/dev/null; then
  sudo yum install -y docker
  sudo systemctl enable docker
  sudo systemctl start docker
  sudo usermod -aG docker ec2-user
fi

# Configure Docker data directory on larger volume if available
echo "[2.5/10] Configuring Docker storage..."
if lsblk /dev/nvme1n1 &>/dev/null; then
  echo "Additional volume detected"
  if ! mountpoint -q /var/lib/docker 2>/dev/null; then
    sudo mkfs.xfs -f /dev/nvme1n1 2>/dev/null || true
    sudo mkdir -p /var/lib/docker
    sudo mount /dev/nvme1n1 /var/lib/docker
    echo "/dev/nvme1n1 /var/lib/docker xfs defaults 0 0" | sudo tee -a /etc/fstab
  fi
fi

# Restart Docker to apply changes
sudo systemctl restart docker

echo "[3/10] Installing Docker Compose..."
# Install Docker Compose plugin (recommended for Amazon Linux 2023)
sudo yum install -y docker-compose-plugin 2>/dev/null || true

# Ensure 'docker-compose' (hyphen) command is available
if ! docker-compose version &>/dev/null; then
  if docker compose version &>/dev/null; then
    # Plugin disponible como 'docker compose' — crear shim para compatibilidad
    sudo tee /usr/local/bin/docker-compose > /dev/null <<'SHIM'
#!/bin/sh
exec docker compose "$@"
SHIM
    sudo chmod +x /usr/local/bin/docker-compose
  else
    # Descargar binario standalone para la arquitectura correcta (x86_64 o aarch64)
    ARCH=$(uname -m)
    sudo curl -SL "https://github.com/docker/compose/releases/download/v2.24.0/docker-compose-linux-${ARCH}" -o /usr/local/bin/docker-compose
    sudo chmod +x /usr/local/bin/docker-compose
  fi
fi

# Verify docker-compose works
if docker-compose version &>/dev/null; then
  echo "Docker Compose installed: $(docker-compose version)"
else
  echo "WARNING: Docker Compose may not be installed correctly"
fi

echo "[4/10] Configuring Swap (7GB)..."
if ! swapon --show | grep -q /swapfile; then
  sudo fallocate -l 7G /swapfile || sudo dd if=/dev/zero of=/swapfile bs=1M count=7168
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile
  sudo swapon /swapfile
  echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
fi

echo "[5/10] Tuning swap parameters..."
echo 'vm.swappiness=80' | sudo tee -a /etc/sysctl.conf
echo 'vm.vfs_cache_pressure=50' | sudo tee -a /etc/sysctl.conf
sudo sysctl -p

echo "[6/10] Configuring Docker daemon..."
sudo mkdir -p /etc/docker
sudo cat >/etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  },
  "storage-driver": "overlay2"
}
EOF
sudo systemctl restart docker

echo "[7/10] Installing AWS CLI..."
if ! command -v aws &>/dev/null; then
  cd /tmp
  ARCH=$(uname -m)  # x86_64 o aarch64
  curl "https://awscli.amazonaws.com/awscli-exe-linux-${ARCH}.zip" -o "awscliv2.zip"
  unzip -q awscliv2.zip
  sudo ./aws/install
  rm -rf awscliv2.zip aws
fi

echo "[8/10] Cloning repository..."
sudo mkdir -p /opt
cd /opt

if [ -d "mvp-aws-ia" ]; then
  cd mvp-aws-ia
  sudo git pull
else
  sudo git clone https://github.com/UC-Johan-Andres/mvp-aws-ia.git
  cd mvp-aws-ia
fi

# Domain configuration (customize if DNS is pointing to this server)
CHATWOOT_DOMAIN="chatwoottest.soylideria.com"
N8N_DOMAIN="n8ntest.soylideria.com"
LIBRECHAT_DOMAIN="chat.soylideria.com"

echo "[9/10] Downloading parameters from AWS Parameter Store..."
OPENROUTER_KEY=$(aws ssm get-parameter --name "/ai-ecosystem/openrouter-key" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
CHATWOOT_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/chatwoot-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
POSTGRES_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/postgres-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "chatwoot_secure_pass_2024")
REDIS_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/redis-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "redis_secure_pass_2024")
N8N_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/n8n-db-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "n8n_secure_pass_2024")
JWT_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/jwt-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b")
JWT_REFRESH_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/jwt-refresh-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c")
SESSION_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/session-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d")

# New parameters for n8n and mongo
N8N_ENCRYPTION_KEY=$(aws ssm get-parameter --name "/ai-ecosystem/n8n-encryption-key" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
N8N_BASIC_AUTH_USER=$(aws ssm get-parameter --name "/ai-ecosystem/n8n-basic-auth-user" --query "Parameter.Value" --output text 2>/dev/null || echo "admin")
N8N_BASIC_AUTH_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/n8n-basic-auth-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
MONGO_ROOT_USERNAME=$(aws ssm get-parameter --name "/ai-ecosystem/mongo-root-username" --query "Parameter.Value" --output text 2>/dev/null || echo "librechat")
MONGO_ROOT_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/mongo-root-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")

# N8N_ENCRYPTION_KEY: only use SSM value if provided, otherwise let n8n generate its own
N8N_ENCRYPTION_KEY_FROM_SSM="$N8N_ENCRYPTION_KEY"

if [ -z "$N8N_BASIC_AUTH_PASSWORD" ]; then
  N8N_BASIC_AUTH_PASSWORD="N8nSecure2024!"
fi

if [ -z "$MONGO_ROOT_PASSWORD" ]; then
  MONGO_ROOT_PASSWORD="mongo_secure_pass_2024"
fi

cat >.env.librechat <<EOF
HOST=0.0.0.0
PORT=3080
MONGO_URI=mongodb://${MONGO_ROOT_USERNAME}:${MONGO_ROOT_PASSWORD}@mongo:27017/LibreChat?authSource=admin
JWT_SECRET=${JWT_SECRET}
JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}
SESSION_SECRET=${SESSION_SECRET}
ALLOW_REGISTRATION=true
OPENROUTER_KEY=${OPENROUTER_KEY}
DOMAIN_CLIENT=https://${LIBRECHAT_DOMAIN}
DOMAIN_SERVER=https://${LIBRECHAT_DOMAIN}
MONGO_INITDB_ROOT_USERNAME=${MONGO_ROOT_USERNAME}
MONGO_INITDB_ROOT_PASSWORD=${MONGO_ROOT_PASSWORD}
EOF

sudo cat >.env.chatwoot <<EOF
RAILS_ENV=production
POSTGRES_HOST=postgres
POSTGRES_USERNAME=chatwoot
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DATABASE=chatwoot
REDIS_URL=redis://:${REDIS_PASSWORD}@redis:6379
SECRET_KEY_BASE=${CHATWOOT_SECRET}
FRONTEND_URL=https://${CHATWOOT_DOMAIN}
WEB_CONCURRENCY=1
RAILS_MAX_THREADS=1
EOF

cat >.env.n8n <<EOF
DB_TYPE=postgresdb
DB_POSTGRESDB_HOST=postgres
DB_POSTGRESDB_PORT=5432
DB_POSTGRESDB_DATABASE=n8n
DB_POSTGRESDB_USER=n8n
DB_POSTGRESDB_PASSWORD=${N8N_PASSWORD}
N8N_HOST=${N8N_DOMAIN}
N8N_PORT=5678
N8N_PROTOCOL=https
N8N_EDITOR_BASE_URL=https://${N8N_DOMAIN}
NODE_ENV=production
GENERIC_TIMEZONE=America/Bogota
N8N_SECURE_COOKIE=true
N8N_IGNORE_CORS=true
WEBHOOK_URL=https://${N8N_DOMAIN}/
N8N_BASIC_AUTH_ACTIVE=true
N8N_BASIC_AUTH_USER=${N8N_BASIC_AUTH_USER}
N8N_BASIC_AUTH_PASSWORD=${N8N_BASIC_AUTH_PASSWORD}
EXECUTIONS_DATA_PRUNE=true
EXECUTIONS_DATA_MAX_AGE=168
NODE_OPTIONS=--max-old-space-size=256
EOF

# Only add N8N_ENCRYPTION_KEY if it came from SSM
if [ -n "$N8N_ENCRYPTION_KEY_FROM_SSM" ]; then
  echo "N8N_ENCRYPTION_KEY=${N8N_ENCRYPTION_KEY_FROM_SSM}" >>.env.n8n
fi


# Generate .env file for docker-compose variable interpolation
echo "Generating .env for docker-compose..."
cat >.env <<EOF
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
REDIS_PASSWORD=${REDIS_PASSWORD}
MONGO_ROOT_USERNAME=${MONGO_ROOT_USERNAME}
MONGO_ROOT_PASSWORD=${MONGO_ROOT_PASSWORD}
OPENROUTER_KEY=${OPENROUTER_KEY}
TIMEZONE=America/Bogota
EOF

echo "[9.5/10] Setting up bolt.diy..."
sudo mkdir -p bolt.diy
cat > bolt.diy/.env.local << BOLTENV
OPEN_ROUTER_API_KEY=${OPENROUTER_KEY}
VITE_LOG_LEVEL=debug
DEFAULT_NUM_CTX=32768
BOLTENV
echo "Building bolt.diy image (installs wrangler on top of base image)..."
sudo docker-compose --env-file .env build bolt
echo "bolt.diy image ready."

echo "[10/10] Starting services..."

# Step 1: Start only databases first
echo "Starting databases (postgres, redis, mongo)..."
sudo docker-compose --env-file .env up -d postgres redis mongo

# Step 2: Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to be ready..."
PG_RETRIES=0
until sudo docker exec postgres pg_isready -U chatwoot &>/dev/null; do
  PG_RETRIES=$((PG_RETRIES + 1))
  if [ $PG_RETRIES -ge 30 ]; then
    echo "ERROR: PostgreSQL did not become ready after 60 seconds. Aborting."
    exit 1
  fi
  sleep 2
done

# Step 3: Create n8n user and database
echo "Creating n8n user and database..."
sudo docker exec postgres psql -U chatwoot -d chatwoot -c "CREATE USER n8n WITH PASSWORD '${N8N_PASSWORD}';" 2>/dev/null || true
sudo docker exec postgres psql -U chatwoot -d chatwoot -c "CREATE DATABASE n8n OWNER n8n;" 2>/dev/null || true

# Step 4: Wait for MongoDB to be ready
echo "Waiting for MongoDB to be ready..."
MONGO_RETRIES=0
until sudo docker exec mongo mongosh --eval "db.adminCommand('ping')" &>/dev/null; do
  MONGO_RETRIES=$((MONGO_RETRIES + 1))
  if [ $MONGO_RETRIES -ge 30 ]; then
    echo "ERROR: MongoDB did not become ready after 60 seconds. Aborting."
    exit 1
  fi
  sleep 2
done

# Step 5: Start chatwoot for migrations only
echo "Starting Chatwoot for database preparation..."
sudo docker-compose --env-file .env up -d chatwoot
echo "Waiting for Chatwoot to be ready for migrations..."
CW_RETRIES=0
until sudo docker exec chatwoot bundle exec rails runner "puts 'ok'" &>/dev/null; do
  CW_RETRIES=$((CW_RETRIES + 1))
  if [ $CW_RETRIES -ge 40 ]; then
    echo "WARNING: Chatwoot did not become ready after 80 seconds, attempting migration anyway..."
    break
  fi
  sleep 2
done
echo "Running Chatwoot database migrations..."
sudo docker exec chatwoot bundle exec rails db:chatwoot_prepare 2>/dev/null || echo "Chatwoot DB prepare completed (or already done)"
sudo docker-compose --env-file .env stop chatwoot

# Step 6: Build custom images and start core services
echo "Building custom images (launcher, marimo)..."
sudo docker-compose --env-file .env build launcher marimo

echo "Pre-creating on-demand containers (stopped)..."
sudo docker-compose --env-file .env up --no-start n8n librechat chatwoot chatwoot_sidekiq marimo bolt

echo "Starting core services (postgres, redis, mongo, launcher)..."
sudo docker-compose --env-file .env up -d launcher

# ======================
# SSL Bootstrap con Let's Encrypt
# Requiere que los dominios apunten a esta instancia y el puerto 80 esté accesible
# ======================
LETSENCRYPT_EMAIL="admin@soylideria.com"  # Cambiar si es necesario
CERT_DIR="/etc/letsencrypt/live/n8ntest.soylideria.com"

echo "[10.5/10] Setting up SSL certificates..."
sudo mkdir -p /var/www/certbot
sudo mkdir -p "${CERT_DIR}"

# Crear cert autofirmado placeholder para que nginx pueda arrancar
if [ ! -f "${CERT_DIR}/fullchain.pem" ]; then
  echo "Generating placeholder self-signed cert..."
  sudo openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
    -keyout "${CERT_DIR}/privkey.pem" \
    -out "${CERT_DIR}/fullchain.pem" \
    -subj '/CN=localhost' 2>/dev/null
fi

# Arrancar nginx con cert placeholder (para servir el ACME challenge)
echo "Starting nginx with placeholder cert..."
sudo docker-compose --env-file .env up -d nginx

# Esperar a que nginx esté listo antes de lanzar certbot
NGINX_RETRIES=0
until sudo docker exec nginx nginx -t &>/dev/null; do
  NGINX_RETRIES=$((NGINX_RETRIES + 1))
  if [ $NGINX_RETRIES -ge 15 ]; then
    echo "WARNING: nginx no respondió en 30s, continuando de todas formas..."
    break
  fi
  sleep 2
done

# Solicitar certificados reales a Let's Encrypt
echo "Requesting Let's Encrypt certificates..."
set +e
sudo docker run --rm \
  -v /etc/letsencrypt:/etc/letsencrypt \
  -v /var/www/certbot:/var/www/certbot \
  certbot/certbot:latest certonly \
  --webroot -w /var/www/certbot \
  -d n8ntest.soylideria.com \
  -d chatwoottest.soylideria.com \
  -d chat.soylideria.com \
  -d marimo.soylideria.com \
  -d bolttest.soylideria.com \
  --email "${LETSENCRYPT_EMAIL}" \
  --agree-tos \
  --no-eff-email \
  --non-interactive
CERTBOT_EXIT=$?
set -e

# Certbot crea el cert como -0001 si el placeholder ya ocupa el nombre original.
# Detectar y crear symlink para que nginx apunte al cert real.
if [ -d "${CERT_DIR}-0001" ]; then
  echo "Detected -0001 suffix, creating symlink..."
  sudo rm -rf "${CERT_DIR}"
  sudo ln -s "${CERT_DIR}-0001" "${CERT_DIR}"
  # Limpiar renewal config roto del placeholder
  sudo rm -f "/etc/letsencrypt/renewal/n8ntest.soylideria.com.conf"
fi

if [ $CERTBOT_EXIT -eq 0 ]; then
  sudo docker exec nginx nginx -s reload
  echo "SSL certificates obtained successfully!"
else
  echo "WARNING: Let's Encrypt request failed. Running with self-signed cert (HTTPS will show browser warning)."
fi

# Renovación automática de certificados cada 12 horas via systemd timer
echo "Setting up certbot renewal timer..."
sudo tee /etc/systemd/system/certbot-renew.service > /dev/null <<'EOF'
[Unit]
Description=Certbot renewal

[Service]
Type=oneshot
ExecStart=/usr/bin/docker run --rm \
  -v /etc/letsencrypt:/etc/letsencrypt \
  -v /var/www/certbot:/var/www/certbot \
  certbot/certbot:latest renew --quiet
ExecStartPost=/usr/bin/docker exec nginx nginx -s reload
EOF

sudo tee /etc/systemd/system/certbot-renew.timer > /dev/null <<'EOF'
[Unit]
Description=Run certbot renewal twice daily

[Timer]
OnCalendar=*-*-* 00,12:00:00
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now certbot-renew.timer
echo "Certbot renewal timer active: $(sudo systemctl status certbot-renew.timer --no-pager -l | grep Active)"

echo "App services available on demand via browser or ./start.sh"

echo "========================================"
echo "Provisioning complete!"
echo "========================================"
echo ""
echo "Docker storage info:"
sudo docker system df
echo ""
echo "Services available at:"
echo "  - n8n:       https://n8ntest.soylideria.com"
echo "  - LibreChat: https://chat.soylideria.com"
echo "  - Chatwoot:  https://chatwoottest.soylideria.com"
echo "  - Marimo:    https://marimo.soylideria.com"
echo "  - Bolt:      https://bolttest.soylideria.com"
echo ""
echo "NOTE: Puerto 443 debe estar abierto en el Security Group de AWS"
echo "To check status: sudo docker-compose ps"
echo "To view logs: sudo docker-compose logs -f"
