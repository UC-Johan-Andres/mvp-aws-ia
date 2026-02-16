#!/bin/bash
set -e

echo "========================================"
echo "AI Ecosystem - EC2 Provisioning Script"
echo "========================================"

export DEBIAN_FRONTEND=noninteractive

echo "[1/10] Updating system..."
apt-get update -y
apt-get upgrade -y
apt-get install -y curl wget git gnupg2 lsb-release ca-certificates unzip

echo "[2/10] Installing Docker..."
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com | sh
    usermod -aG docker ubuntu
    systemctl enable docker
    systemctl start docker
fi

echo "[3/10] Installing Docker Compose plugin..."
if ! command -v docker compose &> /dev/null; then
    apt-get install -y docker-compose-plugin
fi

echo "[4/10] Configuring Swap (2GB)..."
if ! swapon --show | grep -q /swapfile; then
    fallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab
fi

echo "[5/10] Tuning swap parameters..."
echo 'vm.swappiness=10' | tee -a /etc/sysctl.conf
echo 'vm.vfs_cache_pressure=50' | tee -a /etc/sysctl.conf
sysctl -p

echo "[6/10] Configuring Docker daemon..."
mkdir -p /etc/docker
cat > /etc/docker/daemon.json << 'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  },
  "storage-driver": "overlay2"
}
EOF
systemctl restart docker

echo "[7/10] Installing AWS CLI..."
if ! command -v aws &> /dev/null; then
    curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
    unzip -q awscliv2.zip
    ./aws/install
    rm -rf awscliv2.zip aws
fi

echo "[8/10] Cloning repository..."
mkdir -p /opt
cd /opt

if [ -d "mvp-aws-ia" ]; then
    cd mvp-aws-ia
    git pull
else
    git clone https://github.com/UC-Johan-Andres/mvp-aws-ia.git
    cd mvp-aws-ia
fi

cd new

echo "[9/10] Downloading parameters from AWS Parameter Store..."
OPENROUTER_KEY=$(aws ssm get-parameter --name "/ai-ecosystem/openrouter-key" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
CHATWOOT_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/chatwoot-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
BRIDGE_API_KEY=$(aws ssm get-parameter --name "/ai-ecosystem/bridge-api-key" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "")
POSTGRES_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/postgres-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "chatwoot_secure_pass_2024")
REDIS_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/redis-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "redis_secure_pass_2024")
N8N_PASSWORD=$(aws ssm get-parameter --name "/ai-ecosystem/n8n-db-password" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "n8n_secure_pass_2024")
JWT_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/jwt-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b")
JWT_REFRESH_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/jwt-refresh-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c")
SESSION_SECRET=$(aws ssm get-parameter --name "/ai-ecosystem/session-secret" --with-decryption --query "Parameter.Value" --output text 2>/dev/null || echo "c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d")

cat > .env.librechat << EOF
HOST=0.0.0.0
PORT=3080
MONGO_URI=mongodb://mongo:27017/LibreChat
JWT_SECRET=${JWT_SECRET}
JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}
SESSION_SECRET=${SESSION_SECRET}
ALLOW_REGISTRATION=true
OPENROUTER_KEY=${OPENROUTER_KEY}
EOF

cat > .env.chatwoot << EOF
RAILS_ENV=production
POSTGRES_HOST=postgres
POSTGRES_USERNAME=chatwoot
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DATABASE=chatwoot
REDIS_URL=redis://:${REDIS_PASSWORD}@redis:6379
SECRET_KEY_BASE=${CHATWOOT_SECRET}
FRONTEND_URL=http://chatwoot.local
EOF

cat > .env.n8n << EOF
DB_TYPE=postgresdb
DB_POSTGRESDB_HOST=postgres
DB_POSTGRESDB_PORT=5432
DB_POSTGRESDB_DATABASE=n8n
DB_POSTGRESDB_USER=n8n
DB_POSTGRESDB_PASSWORD=${N8N_PASSWORD}
N8N_HOST=n8n.local
N8N_PORT=5678
N8N_PROTOCOL=http
NODE_ENV=production
GENERIC_TIMEZONE=America/Bogota
N8N_SECURE_COOKIE=false
EOF

mkdir -p bridge
cat > bridge/.env << EOF
BRIDGE_API_KEY=${BRIDGE_API_KEY}
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_DB=chatwoot
POSTGRES_USER=chatwoot
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
MONGO_HOST=mongo
MONGO_PORT=27017
MONGO_DB=LibreChat
EOF

echo "[10/10] Starting services..."
docker compose up -d --build

echo "========================================"
echo "Provisioning complete!"
echo "========================================"
echo ""
echo "Services available at:"
echo "  - Chatwoot:  http://chatwoot.local"
echo "  - n8n:       http://n8n.local"
echo "  - LibreChat: http://librechat.local"
echo "  - Bridge:    http://bridge.local"
echo "  - Traefik:   http://localhost:8080"
echo ""
echo "To check status: docker compose ps"
echo "To view logs: docker compose logs -f"
