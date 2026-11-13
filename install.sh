#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="fastasiapanel"
APP_DIR="/opt/fastasiapanel"
BIN_PATH="${APP_DIR}/fastasiapanel"
SERVICE_PATH="/etc/systemd/system/${APP_NAME}.service"
CLI_PATH="/usr/local/bin/fastpanel"
REPO="VahidDashtbani/fastasiapanel"
XRAY_BIN="/usr/local/bin/xray"
XRAY_CONFIG="${APP_DIR}/data/xray.json"
DB_FILE="${APP_DIR}/data/fastasiapanel.json"
ENV_FILE="${APP_DIR}/panel.env"
TMP_DIR=""
PLAIN=$'\033[0m'
CYAN=$'\033[1;36m'
GREEN=$'\033[1;32m'
RED=$'\033[1;31m'
YELLOW=$'\033[1;33m'

log()  { printf '%b[INFO]%b %s\n' "$GREEN" "$PLAIN" "$*"; }
warn() { printf '%b[WARN]%b %s\n' "$YELLOW" "$PLAIN" "$*" >&2; }
die()  { printf '%b[ERROR]%b %s\n' "$RED" "$PLAIN" "$*" >&2; exit 1; }

cleanup() {
  if [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]]; then
    rm -rf -- "${TMP_DIR}"
  fi
}
trap cleanup EXIT

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "Run this installer as root."
}

install_dependencies() {
  local missing=()
  local cmd
  for cmd in curl tar openssl; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done
  ((${#missing[@]} == 0)) && return 0

  log "Installing required packages: ${missing[*]}"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y curl tar openssl ca-certificates
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y curl tar openssl ca-certificates
  elif command -v yum >/dev/null 2>&1; then
    yum install -y curl tar openssl ca-certificates
  elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache curl tar openssl ca-certificates
  else
    die "Cannot install dependencies automatically. Install curl, tar and openssl manually."
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64"; XRAY_ARCH="64" ;;
    aarch64|arm64) ARCH="arm64"; XRAY_ARCH="arm64-v8a" ;;
    armv7l|armv7*) ARCH="armv7"; XRAY_ARCH="arm32-v7a" ;;
    *) die "Unsupported CPU architecture: $(uname -m)" ;;
  esac
}

read_env_value() {
  local key="$1"
  [[ -f "${ENV_FILE}" ]] || return 0
  sed -n "s/^${key}=//p" "${ENV_FILE}" | tail -n1
}

valid_port() {
  [[ "$1" =~ ^[0-9]{1,5}$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535))
}

select_port() {
  local default_port="${1:-8080}" answer
  while true; do
    read -r -p "Panel port [${default_port}]: " answer
    answer="${answer:-$default_port}"
    if valid_port "$answer"; then
      printf '%s' "$answer"
      return
    fi
    warn "Enter a valid port between 1 and 65535."
  done
}

write_initial_env() {
  local port="$1" username password secret
  username="admin"
  password="$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-20)"
  secret="$(openssl rand -hex 32)"
