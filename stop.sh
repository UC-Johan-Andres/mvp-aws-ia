#!/bin/bash
# stop.sh — detiene un servicio sin afectar las bases de datos ni otros servicios
# Uso: ./stop.sh <servicio>
# Servicios disponibles: chatwoot, n8n, librechat, marimo, bolt

SERVICE=$1
cd "$(dirname "$0")"
COMPOSE="docker-compose --env-file .env"

if [ -z "$SERVICE" ]; then
  echo "Uso: ./stop.sh <servicio>"
  echo "Servicios: chatwoot | n8n | librechat | marimo | bolt"
  exit 1
fi

case "$SERVICE" in

  chatwoot)
    echo "[chatwoot] Deteniendo chatwoot + sidekiq..."
    $COMPOSE stop chatwoot chatwoot_sidekiq
    ;;

  n8n)
    echo "[n8n] Deteniendo n8n..."
    $COMPOSE stop n8n
    ;;

  librechat)
    echo "[librechat] Deteniendo librechat..."
    $COMPOSE stop librechat
    ;;

  marimo)
    echo "[marimo] Deteniendo marimo..."
    $COMPOSE stop marimo
    ;;

  bolt)
    echo "[bolt] Deteniendo bolt..."
    $COMPOSE stop bolt
    ;;

  *)
    echo "Servicio desconocido: $SERVICE"
    echo "Servicios disponibles: chatwoot | n8n | librechat | marimo | bolt"
    exit 1
    ;;
esac

echo "[$SERVICE] Detenido."
