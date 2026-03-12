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

  python3 -c "
import re, sys
content = open(sys.argv[1]).read()
content = re.sub(r'N8N_BASIC_AUTH_USER=.*', 'N8N_BASIC_AUTH_USER=' + sys.argv[2], content)
content = re.sub(r'N8N_BASIC_AUTH_PASSWORD=.*', 'N8N_BASIC_AUTH_PASSWORD=' + sys.argv[3], content)
open(sys.argv[1], 'w').write(content)
" "$ENV_FILE" "$new_user" "$new_pass"

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
  read -r -p "Nombre de la cuenta (enter para usar 'Mi Empresa'): " account_name
  account_name="${account_name:-Mi Empresa}"
  [ -n "$full_name" ] && [ -n "$email" ] || { echo "Nombre y email requeridos."; exit 1; }
  new_pass=$(read_password "Contraseña")

  ADMIN_NAME="$full_name" ADMIN_EMAIL="$email" ADMIN_PASS="$new_pass" ACCOUNT_NAME="$account_name" \
  docker exec \
    -e ADMIN_NAME \
    -e ADMIN_EMAIL \
    -e ADMIN_PASS \
    -e ACCOUNT_NAME \
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
      account = Account.first || Account.create!(name: ENV["ACCOUNT_NAME"])
      AccountUser.create!(account: account, user: user, role: :administrator)
      puts "OK: #{user.email} creado como SuperAdmin y admin de la cuenta \"#{account.name}\""
    '
}

cmd_chatwoot_create_account() {
  echo "=== Crear cuenta en Chatwoot y asociar usuario ==="
  container_running chatwoot || { echo "ERROR: el container chatwoot no está corriendo."; exit 1; }

  read -r -p "Nombre de la cuenta: " account_name
  read -r -p "Email del usuario a asociar: " email
  [ -n "$account_name" ] && [ -n "$email" ] || { echo "Nombre y email requeridos."; exit 1; }

  ACCOUNT_NAME="$account_name" ADMIN_EMAIL="$email" \
  docker exec \
    -e ACCOUNT_NAME \
    -e ADMIN_EMAIL \
    chatwoot bundle exec rails runner '
      user = User.find_by(email: ENV["ADMIN_EMAIL"])
      unless user
        $stderr.puts "ERROR: usuario #{ENV["ADMIN_EMAIL"]} no encontrado."
        exit 1
      end
      account = Account.create!(name: ENV["ACCOUNT_NAME"])
      AccountUser.create!(account: account, user: user, role: :administrator)
      puts "OK: cuenta \"#{account.name}\" creada y #{user.email} asociado como administrador"
    '
}

cmd_librechat_list_users() {
  echo "=== Usuarios de LibreChat ==="
  MONGO_USER=$(grep MONGO_ROOT_USERNAME "$COMPOSE_DIR/.env" | cut -d'=' -f2)
  MONGO_PASS=$(grep MONGO_ROOT_PASSWORD "$COMPOSE_DIR/.env" | cut -d'=' -f2)
  container_running mongo || { echo "ERROR: el container mongo no está corriendo."; exit 1; }
  docker exec mongo mongosh \
    -u "$MONGO_USER" -p "$MONGO_PASS" \
    --authenticationDatabase admin \
    --quiet \
    --eval "db.getSiblingDB('LibreChat').users.find({}, {email:1, name:1, role:1, createdAt:1}).toArray()"
}

cmd_librechat_create_user() {
  echo "=== Crear usuario en LibreChat ==="
  container_running librechat || { echo "ERROR: el container librechat no está corriendo."; exit 1; }
  container_running mongo || { echo "ERROR: el container mongo no está corriendo."; exit 1; }

  read -r -p "Email: " email
  read -r -p "Nombre: " name
  read -r -p "Rol (USER/ADMIN) [USER]: " role
  role="${role:-USER}"
  [ "$role" = "USER" ] || [ "$role" = "ADMIN" ] || { echo "Rol inválido. Usa USER o ADMIN."; exit 1; }
  new_pass=$(read_password "Contraseña")

  MONGO_USER=$(grep MONGO_ROOT_USERNAME "$COMPOSE_DIR/.env" | cut -d'=' -f2)
  MONGO_PASS=$(grep MONGO_ROOT_PASSWORD "$COMPOSE_DIR/.env" | cut -d'=' -f2)

  # Pasar contraseña como env var para evitar problemas con caracteres especiales
  HASH=$(docker exec -e "LC_PASS=${new_pass}" librechat node -e "
const bcrypt = require('bcryptjs');
bcrypt.hash(process.env.LC_PASS, 12, (err, hash) => process.stdout.write(hash));
" 2>/dev/null)

  [ -n "$HASH" ] || { echo "ERROR: no se pudo generar el hash de contraseña."; exit 1; }

  USERNAME=$(echo "$email" | cut -d'@' -f1)

  docker exec mongo mongosh \
    -u "$MONGO_USER" -p "$MONGO_PASS" \
    --authenticationDatabase admin \
    --quiet \
    --eval "
      const db2 = db.getSiblingDB('LibreChat');
      if (db2.users.findOne({email: '$email'})) {
        print('ERROR: ya existe un usuario con ese email.');
        quit(1);
      }
      db2.users.insertOne({
        name: '$name',
        username: '$USERNAME',
        email: '$email',
        password: '$HASH',
        role: '$role',
        provider: 'local',
        emailVerified: true,
        createdAt: new Date(),
        updatedAt: new Date()
      });
      print('OK: usuario $email creado con rol $role');
    "
}

cmd_librechat_set_password() {
  echo "=== Cambiar contraseña de usuario LibreChat ==="
  container_running librechat || { echo "ERROR: el container librechat no está corriendo."; exit 1; }
  container_running mongo || { echo "ERROR: el container mongo no está corriendo."; exit 1; }

  read -r -p "Email del usuario: " email
  [ -n "$email" ] || { echo "Email requerido."; exit 1; }
  new_pass=$(read_password "Nueva contraseña")

  MONGO_USER=$(grep MONGO_ROOT_USERNAME "$COMPOSE_DIR/.env" | cut -d'=' -f2)
  MONGO_PASS=$(grep MONGO_ROOT_PASSWORD "$COMPOSE_DIR/.env" | cut -d'=' -f2)

  HASH=$(docker exec -e "LC_PASS=${new_pass}" librechat node -e "
const bcrypt = require('bcryptjs');
bcrypt.hash(process.env.LC_PASS, 12, (err, hash) => process.stdout.write(hash));
" 2>/dev/null)

  [ -n "$HASH" ] || { echo "ERROR: no se pudo generar el hash de contraseña."; exit 1; }

  docker exec mongo mongosh \
    -u "$MONGO_USER" -p "$MONGO_PASS" \
    --authenticationDatabase admin \
    --quiet \
    --eval "
      const result = db.getSiblingDB('LibreChat').users.updateOne(
        {email: '$email'},
        {\$set: {password: '$HASH', updatedAt: new Date()}}
      );
      if (result.matchedCount === 0) {
        print('ERROR: usuario $email no encontrado.');
        quit(1);
      }
      print('OK: contraseña actualizada para $email');
    "
}

# ── Dispatcher ────────────────────────────────────────────────────────────
case "${1:-}" in
  n8n-basic-auth)           cmd_n8n_basic_auth ;;
  n8n-reset-users)          cmd_n8n_reset_users ;;
  chatwoot-set-password)    cmd_chatwoot_set_password ;;
  chatwoot-create-admin)    cmd_chatwoot_create_admin ;;
  chatwoot-create-account)  cmd_chatwoot_create_account ;;
  librechat-list-users)     cmd_librechat_list_users ;;
  librechat-create-user)    cmd_librechat_create_user ;;
  librechat-set-password)   cmd_librechat_set_password ;;
  *)
    echo "Uso: sudo $0 <comando>"
    echo ""
    echo "  n8n-basic-auth            Cambia usuario/contraseña de HTTP Basic Auth de n8n"
    echo "  n8n-reset-users           Borra todos los usuarios internos de n8n"
    echo "  chatwoot-set-password     Cambia la contraseña de un usuario Chatwoot existente"
    echo "  chatwoot-create-admin     Crea un nuevo SuperAdmin + cuenta si no existe"
    echo "  chatwoot-create-account   Crea una cuenta y asocia un usuario existente"
    echo "  librechat-list-users      Lista todos los usuarios de LibreChat"
    echo "  librechat-create-user     Crea un nuevo usuario en LibreChat"
    echo "  librechat-set-password    Cambia la contraseña de un usuario LibreChat"
    exit 1 ;;
esac
