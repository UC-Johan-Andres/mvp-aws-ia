# AI Ecosystem

Plataforma de herramientas de IA integradas, desplegada en AWS EC2 con Docker Compose. Combina varios servicios open-source bajo un único reverse proxy nginx con HTTPS, activados bajo demanda para optimizar el uso de memoria en instancias pequeñas (t3.micro / free tier).

---

## Servicios

| Servicio | URL | Descripción |
|---|---|---|
| **n8n** | https://n8ntest.soylideria.com | Automatización de flujos de trabajo |
| **LibreChat** | https://chat.soylideria.com | Chat con modelos de IA vía OpenRouter |
| **Chatwoot** | https://chatwoottest.soylideria.com | CRM y atención al cliente |
| **Marimo** | https://marimo.soylideria.com | Notebooks de Python reactivos con IA |
| **Bolt.diy** | https://bolttest.soylideria.com | IDE de IA para generar aplicaciones web |

---

## Arquitectura

```
Internet
    │
    ▼  (HTTP → HTTPS redirect)
Nginx  —  puertos 80 / 443  —  Let's Encrypt SSL
    ├── n8ntest.soylideria.com       → n8n:5678
    ├── chatwoottest.soylideria.com  → chatwoot:3000
    ├── chat.soylideria.com          → librechat:3080
    ├── marimo.soylideria.com        → marimo:8080
    └── bolttest.soylideria.com      → bolt:5173
              │
              │  upstream caído (502/503/504)
              ▼
         Launcher :8090
    (arranca el contenedor + página de espera)

Bases de datos  —  siempre encendidas
    ├── PostgreSQL  (compartida: Chatwoot + n8n)
    ├── MongoDB     (LibreChat)
    └── Redis       (Chatwoot)
```

### Servicios siempre encendidos (`restart: unless-stopped`)
`postgres` · `redis` · `mongo` · `nginx` · `launcher`

### Servicios on-demand (`restart: "no"`)
`n8n` · `librechat` · `chatwoot` · `chatwoot_sidekiq` · `marimo` · `bolt`

---

## El Launcher — activación bajo demanda

Servicio propio escrito en **Go** que mantiene apagados los servicios pesados cuando no se usan, ahorrando RAM en instancias pequeñas.

### Flujo
1. Los servicios on-demand se pre-crean parados: `docker-compose up --no-start`
2. Nginx no puede conectar con el upstream → devuelve 502 → redirige al Launcher
3. El Launcher identifica el servicio por el `Host` header y ejecuta `docker start <servicio>`
4. Devuelve una página de espera con auto-refresh cada 6 segundos
5. Cuando el servicio arranca, el siguiente refresh carga la aplicación real

### Límite LRU
Máximo **2 servicios** on-demand activos simultáneamente. Si se solicita un tercero, el más antiguo se detiene automáticamente.

### Docker-outside-of-Docker
El Launcher controla contenedores del host montando el socket Docker:
```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
```
Usa `docker start` / `docker stop` directamente, sin docker-compose dentro del contenedor.

---

## Infraestructura AWS

- **EC2**: t3.micro — 1 GB RAM, 1 vCPU (free tier)
- **Disco**: EBS 27 GB gp3, expandido al arranque con `growpart` + `xfs_growfs`
- **Swap**: 7 GB en `/swapfile`, `vm.swappiness=80`
- **Secretos**: AWS Parameter Store (SSM) — ningún secreto en el repositorio
- **IP fija**: Elastic IP asociada automáticamente al crear el stack

### Despliegue desde la consola de AWS

**Prerequisitos antes de crear el stack:**

1. Tener los parámetros SSM creados (ver sección más abajo)
2. Tener un Key Pair de EC2 creado
3. Tener una Elastic IP reservada y obtener su Allocation ID:
   ```
   EC2 → Red y seguridad → Direcciones IP elásticas
   → Seleccionar la IP → copiar "ID de asignación" (eipalloc-xxxxxxxx)
   ```

**Crear el stack:**
```
CloudFormation → Crear stack → Cargar archivo de plantilla → cloudformation.yaml
```

Parámetros a completar:

| Parámetro | Valor |
|---|---|
| `InstanceType` | `t3.micro` (default) |
| `KeyName` | Nombre de tu Key Pair |
| `SSHLocation` | Tu IP (`x.x.x.x/32`) o `0.0.0.0/0` |
| `EIPAllocationId` | `eipalloc-xxxxxxxx` (Allocation ID de tu Elastic IP) |

**Después de que el stack esté en `CREATE_COMPLETE`:**

```bash
# 1. Conectarse a la instancia
ssh -i tu-key.pem ec2-user@<elastic-ip>

# 2. Subir la imagen de bolt (desde tu máquina local)
scp -i tu-key.pem bolt.tar ec2-user@<elastic-ip>:/home/ec2-user/bolt.tar

# 3. Ejecutar el script de aprovisionamiento
sudo bash /opt/mvp-aws-ia/user_data.sh
```

El script instala Docker, configura swap, descarga secretos de SSM, levanta los servicios y obtiene los certificados SSL. Tarda ~10 minutos.

### Parámetros SSM

| Parámetro | Tipo | Descripción | Default si no existe |
|---|---|---|---|
| `/ai-ecosystem/openrouter-key` | SecureString | API key de OpenRouter | — (requerido) |
| `/ai-ecosystem/chatwoot-secret` | SecureString | SECRET_KEY_BASE de Chatwoot | — (requerido) |
| `/ai-ecosystem/postgres-password` | SecureString | Password de PostgreSQL | `chatwoot_secure_pass_2024` |
| `/ai-ecosystem/redis-password` | SecureString | Password de Redis | `redis_secure_pass_2024` |
| `/ai-ecosystem/n8n-db-password` | SecureString | Password de n8n en PostgreSQL | `n8n_secure_pass_2024` |
| `/ai-ecosystem/jwt-secret` | SecureString | JWT secret de LibreChat | valor hardcoded |
| `/ai-ecosystem/jwt-refresh-secret` | SecureString | JWT refresh secret de LibreChat | valor hardcoded |
| `/ai-ecosystem/session-secret` | SecureString | Session secret de LibreChat | valor hardcoded |
| `/ai-ecosystem/n8n-encryption-key` | SecureString | Cifra credenciales de n8n. Si no existe, n8n genera una propia al primer arranque — **definirlo garantiza que los workflows persistan entre deploys** | generado por n8n |
| `/ai-ecosystem/n8n-basic-auth-user` | String | Usuario de acceso a la UI de n8n | `admin` |
| `/ai-ecosystem/n8n-basic-auth-password` | SecureString | Password de acceso a la UI de n8n | `N8nSecure2024!` |
| `/ai-ecosystem/mongo-root-username` | String | Usuario root de MongoDB | `librechat` |
| `/ai-ecosystem/mongo-root-password` | SecureString | Password root de MongoDB | `mongo_secure_pass_2024` |

#### Crear parámetros desde CLI

```bash
# Requeridos
aws ssm put-parameter --name "/ai-ecosystem/openrouter-key" --value "sk-or-..." --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/chatwoot-secret" --value "$(openssl rand -hex 64)" --type SecureString

# Bases de datos
aws ssm put-parameter --name "/ai-ecosystem/postgres-password" --value "TuPassword" --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/redis-password"    --value "TuPassword" --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/n8n-db-password"   --value "TuPassword" --type SecureString

# LibreChat
aws ssm put-parameter --name "/ai-ecosystem/jwt-secret"         --value "$(openssl rand -hex 32)" --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/jwt-refresh-secret" --value "$(openssl rand -hex 32)" --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/session-secret"     --value "$(openssl rand -hex 32)" --type SecureString

# n8n
aws ssm put-parameter --name "/ai-ecosystem/n8n-encryption-key"      --value "$(openssl rand -hex 32)" --type SecureString
aws ssm put-parameter --name "/ai-ecosystem/n8n-basic-auth-user"     --value "admin"         --type String
aws ssm put-parameter --name "/ai-ecosystem/n8n-basic-auth-password" --value "TuPassword"    --type SecureString

# MongoDB
aws ssm put-parameter --name "/ai-ecosystem/mongo-root-username" --value "librechat"    --type String
aws ssm put-parameter --name "/ai-ecosystem/mongo-root-password" --value "TuPassword"   --type SecureString
```

---

## HTTPS / SSL

Certificados gestionados con **Let's Encrypt + certbot** en Docker.

- Almacenados en `/etc/letsencrypt/` en el host EC2
- Nginx los monta como volumen read-only
- Renovación automática vía cron cada 12 horas

**Requisito previo:** puerto **443** abierto en el Security Group de AWS.

El bootstrap SSL está incluido en `user_data.sh`:
1. Genera cert autofirmado placeholder → nginx puede arrancar
2. Sirve el ACME challenge a través de `/var/www/certbot`
3. Certbot obtiene el cert real para los 5 dominios
4. Recarga nginx con el cert de Let's Encrypt

---

## Imágenes Docker

| Servicio | Imagen / Origen |
|---|---|
| `postgres` | `blacknoob20/pg15vector-alpine` (PostgreSQL 15 + pgvector) |
| `redis` | `redis:7-alpine` |
| `mongo` | `mongo:6.0` |
| `chatwoot` | `chatwoot/chatwoot:latest` |
| `n8n` | `docker.n8n.io/n8nio/n8n` |
| `librechat` | `ghcr.io/danny-avila/librechat:latest` |
| `nginx` | `nginx:1.25-alpine` |
| `launcher` | Build local — `./launcher/Dockerfile` (Go multi-stage) |
| `marimo` | Build local — `./marimo/Dockerfile` (Python 3.14 + uv) |
| `bolt` | Tar pre-compilado subido manualmente → taggeado como `bolt-ai:production` |

### Despliegue de la imagen de bolt
Antes de aprovisionar la instancia, subir el tar a EC2:
```bash
scp bolt.tar ec2-user@<IP_EC2>:/home/ec2-user/bolt.tar
```
`user_data.sh` la carga automáticamente si la encuentra en esa ruta. Si no existe, hace pull de `ghcr.io/stackblitz-labs/bolt.diy:latest` como fallback.

---

## Estructura del repositorio

```
.
├── docker-compose.yml        # Orquestación principal
├── nginx.conf                # Reverse proxy, SSL, COOP/COEP para Bolt
├── user_data.sh              # Aprovisionamiento EC2 completo (boot script)
├── librechat.yaml            # Configuración de modelos de LibreChat (OpenRouter)
├── index.html                # Página de inicio accesible por IP
├── launcher/
│   ├── main.go               # Activador on-demand con LRU (Go)
│   ├── Dockerfile            # Multi-stage: golang:1.22-alpine → alpine:3.19
│   └── go.mod
└── marimo/
    ├── Dockerfile            # python:3.14-slim + uv
    ├── entrypoint.sh         # Genera marimo.toml con OpenRouter y lanza el servidor
    └── requirements.txt      # marimo, pandas, psycopg2-binary, pymongo, sqlalchemy
```

---

## Configuraciones técnicas destacadas

### nginx — DNS dinámico
Variable en vez de bloque `upstream` estático para que nginx arranque aunque el upstream esté parado y re-resuelva el nombre Docker en cada request:
```nginx
resolver 127.0.0.11 valid=30s ipv6=off;
set $backend "n8n:5678";
proxy_pass http://$backend;
```

### nginx — activación on-demand
```nginx
error_page 502 503 504 = @launcher;
proxy_intercept_errors on;
location @launcher {
    proxy_pass http://launcher:8090;
}
```

### Bolt.diy — Cross-Origin Isolation
WebContainers requiere `SharedArrayBuffer`, disponible solo en HTTPS con estos headers (aplicados también en la página de espera del launcher):
```nginx
add_header Cross-Origin-Opener-Policy  "same-origin"  always;
add_header Cross-Origin-Embedder-Policy "require-corp" always;
```

### n8n — X-Forwarded-For
Para evitar el error `ERR_ERL_UNEXPECTED_X_FORWARDED_FOR` de express-rate-limit:
```nginx
proxy_set_header X-Forwarded-For "";
```

### Marimo — configuración de IA
El `entrypoint.sh` genera `marimo.toml` al arrancar inyectando la API key de OpenRouter:
```toml
[ai.openrouter]
api_key = "<OPENROUTER_KEY>"
base_url = "https://openrouter.ai/api/v1/"
```

---

## Memoria estimada

| Servicio | Límite | Modo |
|---|---|---|
| PostgreSQL | 200 MB | Siempre activo |
| Redis | 64 MB | Siempre activo |
| MongoDB | 256 MB | Siempre activo |
| Launcher | 32 MB | Siempre activo |
| nginx | ~30 MB | Siempre activo |
| **Base total** | **~582 MB** | |
| n8n | 300 MB | On-demand |
| LibreChat | 256 MB | On-demand |
| Chatwoot + Sidekiq | 700 MB | On-demand |
| Marimo | 300 MB | On-demand |
| Bolt.diy | 512 MB | On-demand |

Con **7 GB de swap** la instancia maneja los servicios on-demand sin OOM.

---

## Comandos útiles

```bash
# Estado general
sudo docker-compose ps
sudo docker logs launcher -f

# Iniciar un servicio on-demand manualmente
sudo docker start n8n

# Actualizar configuración de nginx
sudo git pull
sudo docker-compose --env-file .env up -d --force-recreate nginx

# Rebuild de imágenes propias
sudo docker-compose --env-file .env build launcher marimo

# Renovar certificados SSL manualmente
sudo docker run --rm \
  -v /etc/letsencrypt:/etc/letsencrypt \
  -v /var/www/certbot:/var/www/certbot \
  certbot/certbot:latest renew --quiet
sudo docker exec nginx nginx -s reload

# Uso de disco Docker
sudo docker system df
```

---

## Known issues

Ninguno conocido actualmente.
