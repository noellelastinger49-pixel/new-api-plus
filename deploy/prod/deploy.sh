#!/usr/bin/env bash
#
# Production deployment for new-api.
#
# Local (build machine):
#   ./deploy.sh export          Export the latest local new-api container/image to images/new-api.tar
#
# Production server:
#   ./deploy.sh                 Load image, ensure secrets, and start the stack
#   ./deploy.sh deploy          Same as above
#   ./deploy.sh restart         Restart services without reloading the image
#   ./deploy.sh stop            Stop services
#   ./deploy.sh fix-env          Re-encode SQL_DSN / REDIS_CONN_STRING from existing passwords
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

IMAGE_NAME="new-api:prod-local"
IMAGE_TAR="images/new-api.tar"
ENV_FILE=".env"
COMPOSE_FILE="docker-compose.yml"

PASSWORD_LENGTH=8

log() {
  printf '[deploy] %s\n' "$*"
}

die() {
  printf '[deploy] ERROR: %s\n' "$*" >&2
  exit 1
}

require_docker() {
  command -v docker >/dev/null 2>&1 || die "docker is required but not installed"
  docker compose version >/dev/null 2>&1 || die "docker compose plugin is required"
}

shuffle_chars() {
  local input="$1"
  if command -v shuf >/dev/null 2>&1; then
    printf '%s' "$input" | fold -w1 | shuf | tr -d '\n'
    return
  fi

  awk -v str="$input" '
    BEGIN {
      split(str, chars, "")
      n = length(str)
      srand()
      for (i = n; i >= 1; i--) {
        j = int(rand() * i) + 1
        tmp = chars[i]
        chars[i] = chars[j]
        chars[j] = tmp
      }
      for (i = 1; i <= n; i++) {
        printf "%s", chars[i]
      }
    }
  '
}

random_from() {
  local charset="$1"
  local count="$2"
  local result=""
  local byte
  local index
  local len=${#charset}

  while [ "${#result}" -lt "$count" ]; do
    if command -v od >/dev/null 2>&1; then
      byte=$(od -An -N1 -tu1 /dev/urandom 2>/dev/null | tr -d ' ')
    else
      byte=$((RANDOM % 256))
    fi
    index=$((byte % len))
    result="${result}${charset:index:1}"
  done

  printf '%s' "$result"
}

generate_password() {
  local upper lower digit special mixed
  upper=$(random_from 'ABCDEFGHJKLMNPQRSTUVWXYZ' 2)
  lower=$(random_from 'abcdefghijkmnopqrstuvwxyz' 2)
  digit=$(random_from '23456789' 2)
  special=$(random_from '!#$%^&*' 2)
  mixed="${upper}${lower}${digit}${special}"
  shuffle_chars "$mixed"
}

generate_session_secret() {
  random_from 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789' 32
}

urlencode() {
  local input="$1"

  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$input"
    return
  fi

  local length="${#input}"
  local i c

  for ((i = 0; i < length; i++)); do
    c="${input:i:1}"
    case "$c" in
      [a-zA-Z0-9.~_-]) printf '%s' "$c" ;;
      *) printf '%%%02X' "'$c" ;;
    esac
  done
}

read_env_value() {
  local key="$1"
  if [ ! -f "$ENV_FILE" ]; then
    return 1
  fi
  grep -E "^${key}=" "$ENV_FILE" | tail -n1 | cut -d= -f2-
}

write_env_file() {
  local postgres_password="$1"
  local redis_password="$2"
  local session_secret="$3"
  local postgres_user="${4:-root}"
  local postgres_db="${5:-new-api}"
  local tz="${6:-Asia/Shanghai}"
  local node_name="${7:-new-api-prod-1}"
  local encoded_postgres_password encoded_redis_password sql_dsn redis_conn_string

  encoded_postgres_password="$(urlencode "$postgres_password")"
  encoded_redis_password="$(urlencode "$redis_password")"
  sql_dsn="postgresql://${postgres_user}:${encoded_postgres_password}@postgres:5432/${postgres_db}"
  redis_conn_string="redis://:${encoded_redis_password}@redis:6379"

  {
    printf 'POSTGRES_USER=%s\n' "$postgres_user"
    printf 'POSTGRES_PASSWORD=%s\n' "$postgres_password"
    printf 'POSTGRES_DB=%s\n' "$postgres_db"
    printf 'REDIS_PASSWORD=%s\n' "$redis_password"
    printf 'SESSION_SECRET=%s\n' "$session_secret"
    printf 'SQL_DSN=%s\n' "$sql_dsn"
    printf 'REDIS_CONN_STRING=%s\n' "$redis_conn_string"
    printf 'TZ=%s\n' "$tz"
    printf 'NODE_NAME=%s\n' "$node_name"
  } >"$ENV_FILE"

  chmod 600 "$ENV_FILE"
}

sync_connection_strings() {
  local postgres_password redis_password session_secret postgres_user postgres_db tz node_name
  postgres_password="$(read_env_value POSTGRES_PASSWORD || true)"
  redis_password="$(read_env_value REDIS_PASSWORD || true)"
  session_secret="$(read_env_value SESSION_SECRET || true)"
  postgres_user="$(read_env_value POSTGRES_USER || true)"
  postgres_db="$(read_env_value POSTGRES_DB || true)"
  tz="$(read_env_value TZ || true)"
  node_name="$(read_env_value NODE_NAME || true)"

  [ -n "$postgres_password" ] || die "POSTGRES_PASSWORD is missing from ${ENV_FILE}"
  [ -n "$redis_password" ] || die "REDIS_PASSWORD is missing from ${ENV_FILE}"
  [ -n "$session_secret" ] || die "SESSION_SECRET is missing from ${ENV_FILE}"

  write_env_file \
    "$postgres_password" \
    "$redis_password" \
    "$session_secret" \
    "${postgres_user:-root}" \
    "${postgres_db:-new-api}" \
    "${tz:-Asia/Shanghai}" \
    "${node_name:-new-api-prod-1}"
}

ensure_env() {
  if [ -f "$ENV_FILE" ]; then
    local postgres_password redis_password session_secret
    postgres_password="$(read_env_value POSTGRES_PASSWORD || true)"
    redis_password="$(read_env_value REDIS_PASSWORD || true)"
    session_secret="$(read_env_value SESSION_SECRET || true)"

    if [ -n "$postgres_password" ] && [ -n "$redis_password" ] && [ -n "$session_secret" ]; then
      log "Refreshing connection strings in ${ENV_FILE}"
      sync_connection_strings
      return
    fi

    log "Incomplete ${ENV_FILE} detected; regenerating missing credentials"
  else
    log "Generating random production credentials (${PASSWORD_LENGTH}-character passwords)"
  fi

  local postgres_password redis_password session_secret
  postgres_password="$(generate_password)"
  redis_password="$(generate_password)"
  session_secret="$(generate_session_secret)"
  write_env_file "$postgres_password" "$redis_password" "$session_secret"

  log "Credentials written to ${ENV_FILE}"
  log "PostgreSQL password: ${postgres_password}"
  log "Redis password: ${redis_password}"
  log "Store ${ENV_FILE} securely — passwords are not shown again on redeploy"
}

find_latest_container() {
  local name container
  for name in new-api new-api-dev; do
    container="$(docker ps -q -f "name=^/${name}$" 2>/dev/null | head -n1 || true)"
    if [ -n "$container" ]; then
      printf '%s' "$container"
      return 0
    fi
  done

  for name in new-api new-api-dev; do
    container="$(docker ps -aq -f "name=^/${name}$" --format '{{.ID}}' 2>/dev/null | head -n1 || true)"
    if [ -n "$container" ]; then
      printf '%s' "$container"
      return 0
    fi
  done

  return 1
}

resolve_source_image() {
  local candidate
  for candidate in "$IMAGE_NAME" "new-api-dev:local" "new-api:local"; do
    if docker image inspect "$candidate" >/dev/null 2>&1; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}

export_image() {
  require_docker
  mkdir -p images

  local container source_image
  if container="$(find_latest_container)"; then
    log "Committing container ${container} to ${IMAGE_NAME}"
    docker commit "$container" "$IMAGE_NAME" >/dev/null
  elif source_image="$(resolve_source_image)"; then
    if [ "$source_image" != "$IMAGE_NAME" ]; then
      log "Tagging local image ${source_image} as ${IMAGE_NAME}"
      docker tag "$source_image" "$IMAGE_NAME"
    else
      log "Using existing image ${IMAGE_NAME}"
    fi
  else
    die "No local new-api container or image found. Build one first, for example: docker compose -f docker-compose.dev.yml up -d --build"
  fi

  log "Saving ${IMAGE_NAME} to ${IMAGE_TAR}"
  docker save "$IMAGE_NAME" -o "$IMAGE_TAR"
  log "Export complete: ${IMAGE_TAR}"
  log "Copy deploy/prod/ (including ${IMAGE_TAR}) to the production server, then run ./deploy.sh"
}

load_image() {
  [ -f "$IMAGE_TAR" ] || die "Missing ${IMAGE_TAR}. Run './deploy.sh export' on your build machine first."

  log "Loading image from ${IMAGE_TAR}"
  docker load -i "$IMAGE_TAR"

  if ! docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
    die "Loaded tar did not provide ${IMAGE_NAME}"
  fi
}

compose() {
  docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

prepare_data_dir() {
  mkdir -p data
  chmod 777 data
}

deploy_stack() {
  require_docker
  load_image
  ensure_env
  prepare_data_dir

  log "Starting production stack"
  compose up -d --remove-orphans
  # podman-compose does not auto-recreate containers when the image digest changes;
  # force-recreate new-api so the new binary is always picked up on each deploy.
  compose up -d --force-recreate new-api
  log "Deployment complete"
  log "Application: http://localhost:3000"
  log "Check status: docker compose -f ${COMPOSE_FILE} ps"
}

fix_env() {
  require_docker
  [ -f "$ENV_FILE" ] || die "Missing ${ENV_FILE}"
  sync_connection_strings
  log "Connection strings updated in ${ENV_FILE}"
  compose up -d --force-recreate new-api
}

restart_stack() {
  require_docker
  [ -f "$ENV_FILE" ] || die "Missing ${ENV_FILE}. Run './deploy.sh' first."
  compose restart
}

stop_stack() {
  require_docker
  compose down
}

show_logs() {
  require_docker
  compose logs -f --tail=200
}

usage() {
  cat <<EOF
Usage: $0 [command]

Commands:
  export    Export the latest local new-api container/image to ${IMAGE_TAR}
  deploy    Load image, ensure secrets, and start services (default)
  restart   Restart services without reloading the image
  stop      Stop and remove containers
  fix-env     Re-encode SQL_DSN / REDIS_CONN_STRING and recreate new-api
  logs        Follow service logs
  help      Show this help message
EOF
}

main() {
  local command="${1:-deploy}"

  case "$command" in
    export)
      export_image
      ;;
    deploy)
      deploy_stack
      ;;
    fix-env)
      fix_env
      ;;
    restart)
      restart_stack
      ;;
    stop)
      stop_stack
      ;;
    logs)
      show_logs
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      die "Unknown command: ${command}. Run './deploy.sh help' for usage."
      ;;
  esac
}

main "$@"
