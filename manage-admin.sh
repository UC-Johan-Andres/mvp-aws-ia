#!/bin/bash
# manage-admin.sh — Gestión de acceso para n8n y Chatwoot
# Ejecutar como root: sudo ./manage-admin.sh <comando>
set -euo pipefail

COMPOSE_DIR="/opt/mvp-aws-ia"

# ── Seguridad: solo root ───────────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: ejecutar como root (sudo $0 $*)" >&2; exit 1
fi

# ── Helpers ───────────────────────────────────────────────────────────────
read_password() {
  local prompt="$1" var ref_var
  while true; do
    read -s -r -p "$prompt: " var; echo >&2
    read -s -r -p "Confirmar contraseña: " ref_var; echo >&2
    [ "$var" = "$ref_var" ] || { echo "Las contraseñas no coinciden. Intenta de nuevo." >&2; continue; }
    [ ${#var} -ge 10 ]      || { echo "Mínimo 10 caracteres." >&2; continue; }
    break
  done
  printf '%s' "$var"
}

container_running() {
  docker ps --format '{{.Names}}' | grep -q "^$1$"
}

# ── Subcomandos ───────────────────────────────────────────────────────────

cmd_n8n_basic_auth() {
  echo "=== Cambiar credenciales HTTP Basic Auth de n8n ==="
  ENV_FILE="$COMPOSE_DIR/.env.n8n"
  [ -f "$ENV_FILE" ] || { echo "No se encontró $ENV_FILE"; exit 1; }

  current_user=$(grep '^N8N_BASIC_AUTH_USER=' "$ENV_FILE" | cut -d= -f2-)
  read -r -p "Nuevo usuario [$current_user]: " new_user
  new_user="${new_user:-$current_user}"
  new_pass=$(read_password "Nueva contraseña")

  sed -i "s|^N8N_BASIC_AUTH_USER=.*|N8N_BASIC_AUTH_USER=${new_user}|" "$ENV_FILE"
  sed -i "s|^N8N_BASIC_AUTH_PASSWORD=.*|N8N_BASIC_AUTH_PASSWORD=${new_pass}|" "$ENV_FILE"

  cd "$COMPOSE_DIR"
  docker-compose --env-file .env up -d n8n
  echo "OK: credenciales actualizadas y n8n reiniciado."
}

cmd_n8n_reset_users() {
  echo "=== Resetear usuarios internos de n8n ==="
  echo "ADVERTENCIA: Esto borra TODOS los usuarios registrados en la UI de n8n."
  echo "Después deberás re-registrar el owner desde el navegador."
  read -r -p "Escribe CONFIRMAR para continuar: " confirm
  [ "$confirm" = "CONFIRMAR" ] || { echo "Cancelado."; exit 0; }

  container_running n8n || { echo "ERROR: el container n8n no está corriendo."; exit 1; }
  docker exec n8n n8n user-management:reset
  echo "OK: usuarios de n8n eliminados. Visita el panel para registrar el owner."
}

cmd_chatwoot_set_password() {
  echo "=== Cambiar contraseña de usuario Chatwoot ==="
  container_running chatwoot || { echo "ERROR: el container chatwoot no está corriendo."; exit 1; }

  read -r -p "Email del usuario: " email
  [ -n "$email" ] || { echo "Email requerido."; exit 1; }
  new_pass=$(read_password "Nueva contraseña")

  ADMIN_EMAIL="$email" ADMIN_PASS="$new_pass" \
  docker exec \
    -e ADMIN_EMAIL \
    -e ADMIN_PASS \
    chatwoot bundle exec rails runner '
      user = User.find_by(email: ENV["ADMIN_EMAIL"])
      unless user
        $stderr.puts "ERROR: usuario #{ENV["ADMIN_EMAIL"]} no encontrado."
        exit 1
      end
      user.update!(password: ENV["ADMIN_PASS"], password_confirmation: ENV["ADMIN_PASS"])
      puts "OK: contraseña actualizada para #{user.email}"
    '
}

cmd_chatwoot_create_admin() {
  echo "=== Crear nuevo administrador en Chatwoot ==="
  container_running chatwoot || { echo "ERROR: el container chatwoot no está corriendo."; exit 1; }

  read -r -p "Nombre: " full_name
  read -r -p "Email: " email
  [ -n "$full_name" ] && [ -n "$email" ] || { echo "Nombre y email requeridos."; exit 1; }
  new_pass=$(read_password "Contraseña")

  ADMIN_NAME="$full_name" ADMIN_EMAIL="$email" ADMIN_PASS="$new_pass" \
  docker exec \
    -e ADMIN_NAME \
    -e ADMIN_EMAIL \
    -e ADMIN_PASS \
    chatwoot bundle exec rails runner '
      if User.exists?(email: ENV["ADMIN_EMAIL"])
        $stderr.puts "ERROR: ya existe un usuario con ese email."
        exit 1
      end
      user = User.create!(
        name:                  ENV["ADMIN_NAME"],
        email:                 ENV["ADMIN_EMAIL"],
        password:              ENV["ADMIN_PASS"],
        password_confirmation: ENV["ADMIN_PASS"],
        confirmed_at:          Time.now
      )
      SuperAdmin.create!(user: user)
      account = Account.first
      if account
        AccountUser.create!(account: account, user: user, role: :administrator)
        puts "OK: #{user.email} creado como SuperAdmin y admin de la cuenta \"#{account.name}\""
      else
        puts "OK: #{user.email} creado como SuperAdmin (sin cuentas aún)"
      end
    '
}

# ── Dispatcher ────────────────────────────────────────────────────────────
case "${1:-}" in
  n8n-basic-auth)        cmd_n8n_basic_auth ;;
  n8n-reset-users)       cmd_n8n_reset_users ;;
  chatwoot-set-password) cmd_chatwoot_set_password ;;
  chatwoot-create-admin) cmd_chatwoot_create_admin ;;
  *)
    echo "Uso: sudo $0 <comando>"
    echo ""
    echo "  n8n-basic-auth         Cambia usuario/contraseña de HTTP Basic Auth de n8n"
    echo "  n8n-reset-users        Borra todos los usuarios internos de n8n"
    echo "  chatwoot-set-password  Cambia la contraseña de un usuario Chatwoot existente"
    echo "  chatwoot-create-admin  Crea un nuevo SuperAdmin + administrador de cuenta"
    exit 1 ;;
esac
