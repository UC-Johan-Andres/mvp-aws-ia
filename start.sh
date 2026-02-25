#!/bin/bash
# start.sh — levanta un servicio bajo demanda
# Uso: ./start.sh <servicio>
# Servicios disponibles: chatwoot, n8n, librechat, marimo, bolt

set -e

SERVICE=$1
cd "$(dirname "$0")"
COMPOSE="docker-compose --env-file .env"

if [ -z "$SERVICE" ]; then
  echo "Uso: ./start.sh <servicio>"
  echo "Servicios: chatwoot | n8n | librechat | marimo | bolt"
  exit 1
fi

case "$SERVICE" in

  chatwoot)
    echo "[chatwoot] Verificando postgres y redis..."
    $COMPOSE up -d postgres redis
    until sudo docker exec postgres pg_isready -U chatwoot &>/dev/null; do sleep 2; done
    echo "[chatwoot] Levantando chatwoot + sidekiq..."
    $COMPOSE up -d chatwoot chatwoot_sidekiq
    ;;

  n8n)
    echo "[n8n] Verificando postgres..."
    $COMPOSE up -d postgres
    until sudo docker exec postgres pg_isready -U chatwoot &>/dev/null; do sleep 2; done
    echo "[n8n] Levantando n8n..."
    $COMPOSE up -d n8n
    ;;

  librechat)
    echo "[librechat] Verificando mongo..."
    $COMPOSE up -d mongo
    until sudo docker exec mongo mongosh --eval "db.adminCommand('ping')" &>/dev/null; do sleep 2; done
    echo "[librechat] Levantando librechat..."
    $COMPOSE up -d librechat
    ;;

  marimo)
    echo "[marimo] Levantando marimo..."
    $COMPOSE up -d marimo
    ;;

  bolt)
    echo "[bolt] Levantando bolt..."
    $COMPOSE up -d bolt
    ;;

  *)
    echo "Servicio desconocido: $SERVICE"
    echo "Servicios disponibles: chatwoot | n8n | librechat | marimo | bolt"
    exit 1
    ;;
esac

echo ""
echo "[$SERVICE] Listo. Logs:"
$COMPOSE logs --tail=20 $SERVICE
