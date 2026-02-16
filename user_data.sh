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

cat > .env.librechat << EOF
HOST=0.0.0.0
PORT=3080
MONGO_URI=mongodb://mongo:27017/LibreChat
JWT_SECRET=33b78cc2287e073df750a300ba7b3ce07f12b6b4d894363f8d9b3d3707698525
JWT_REFRESH_SECRET=66c031df692eff45d92d0db85266fe7431589c23f33484d9efd8d1b99cdcb9e
SESSION_SECRET=supersecret
ALLOW_REGISTRATION=true
OPENROUTER_KEY=${OPENROUTER_KEY}
EOF

cat > .env.chatwoot << EOF
RAILS_ENV=production
POSTGRES_HOST=postgres
POSTGRES_USERNAME=chatwoot
POSTGRES_PASSWORD=chatwoot
POSTGRES_DATABASE=chatwoot
REDIS_URL=redis://redis:6379
SECRET_KEY_BASE=${CHATWOOT_SECRET:-2838b638e3c7a1d39317f7e9e8b4b8f54a113def244a773923abcb779bdb9032443fb8db2d385d41292bf92e798a1af1b1d3fba8f5b178593aa68b36e1e7262e}
FRONTEND_URL=http://chatwoot.local
EOF

cat > .env.n8n << EOF
DB_TYPE=postgresdb
DB_POSTGRESDB_HOST=postgres
DB_POSTGRESDB_PORT=5432
DB_POSTGRESDB_DATABASE=n8n
DB_POSTGRESDB_USER=n8n
DB_POSTGRESDB_PASSWORD=n8n
N8N_HOST=n8n.local
N8N_PORT=5678
N8N_PROTOCOL=http
NODE_ENV=production
GENERIC_TIMEZONE=America/Bogota
N8N_SECURE_COOKIE=false
EOF

mkdir -p bridge
cat > bridge/.env << EOF
BRIDGE_API_KEY=${BRIDGE_API_KEY:-deepnote-api-key-change-me}
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_DB=chatwoot
POSTGRES_USER=chatwoot
POSTGRES_PASSWORD=chatwoot
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
