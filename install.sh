#!/usr/bin/env bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

[[ $EUID -ne 0 ]] && exec sudo "$0" "$@"

if ! command -v infocmp >/dev/null 2>&1 || ! infocmp "${TERM:-}" >/dev/null 2>&1; then
  export TERM=xterm-256color
fi

REPO_URL="https://github.com/VahidDashtbani/fastasiapanel.git"
CONF_DIR="/etc/fastasiapanel"
ENV_FILE="${CONF_DIR}/panel.env"
DATA_DIR="/var/lib/fastasiapanel"
APP_DIR="/opt/fastasiapanel"
SRC_DIR="/usr/local/src/fastasiapanel"
BIN_PATH="/usr/local/bin/fastasiapanel"
CLI_PATH="/usr/local/bin/fastasian"
SERVICE_NAME="fastasiapanel"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

log_info() { echo -e "${GREEN}[INF]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WRN]${NC} $*"; }
log_err()  { echo -e "${RED}[ERR]${NC} $*"; }

gen_rand_str() {
  local len="${1:-12}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 $((len * 2)) | tr -dc 'a-zA-Z0-9' | head -c "${len}"
  else
    tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "${len}"
  fi
}

server_ip() {
  local urls=(
    "https://api4.ipify.org"
    "https://ipv4.icanhazip.com"
    "https://v4.api.ipinfo.io/ip"
    "https://ipv4.myexternalip.com/raw"
  )
  local u ip
  for u in "${urls[@]}"; do
    ip=$(curl -4 -fsSL --max-time 3 "${u}" 2>/dev/null | tr -d '[:space:]')
    if [[ "${ip}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
      echo "${ip}"
      return 0
    fi
  done
  hostname -I 2>/dev/null | awk '{print $1}'
}

install_deps() {
  log_info "Checking system dependencies..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -y
    apt-get install -y curl git ca-certificates build-essential openssl
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y curl git ca-certificates gcc make openssl
  elif command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm curl git ca-certificates base-devel openssl
  fi

  if ! command -v go >/dev/null 2>&1; then
    log_info "Installing Go..."
    local go_ver="1.23.4"
    local arch
    arch=$(uname -m)
    case "${arch}" in
      x86_64|amd64) arch="amd64" ;;
      aarch64|arm64) arch="arm64" ;;
      *) log_err "Unsupported architecture: ${arch}"; exit 1 ;;
    esac
    curl -fsSL "https://go.dev/dl/go${go_ver}.linux-${arch}.tar.gz" -o /tmp/go.tgz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tgz
    rm -f /tmp/go.tgz
  fi

  export PATH="/usr/local/go/bin:${PATH}"
  log_info "Go version: $(go version)"
}

sync_source() {
  log_info "Syncing FastAsiaPanel source from ${REPO_URL}..."
  mkdir -p "${APP_DIR}" "${DATA_DIR}" "${CONF_DIR}"
  if [[ -d "${SRC_DIR}/.git" ]]; then
    git -C "${SRC_DIR}" fetch --all --prune
    git -C "${SRC_DIR}" reset --hard origin/main
  else
    rm -rf "${SRC_DIR}"
    git clone --depth 1 "${REPO_URL}" "${SRC_DIR}"
  fi
}

build_panel() {
  log_info "Building fastasiapanel binary..."
  cd "${SRC_DIR}"
  go mod tidy >/dev/null 2>&1 || true
  go build -o "${BIN_PATH}" .
  chmod 755 "${BIN_PATH}"
  log_info "Binary built at ${BIN_PATH}"
}

setup_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    log_info "Existing ${ENV_FILE} found; keeping current configuration."
    return 0
  fi

  local port base_path db_type admin_user admin_pass
  port="2053"
  base_path="/$(gen_rand_str 10)"
  db_type="sqlite"
  admin_user="admin"
  admin_pass="$(gen_rand_str 14)"

  if [[ -r /dev/tty ]]; then
    local input_port="" input_base="" input_db=""
    read -rp "Panel Port [default 2053]: " input_port </dev/tty || true
    [[ -n "${input_port}" ]] && port="${input_port}"

    read -rp "Web Base Path [default ${base_path}]: " input_base </dev/tty || true
    if [[ -n "${input_base}" ]]; then
      [[ "${input_base:0:1}" != "/" ]] && input_base="/${input_base}"
      base_path="${input_base%/}"
    fi

    read -rp "Database Type (sqlite / postgres) [default sqlite]: " input_db </dev/tty || true
    [[ "${input_db,,}" == "postgres" ]] && db_type="postgres"
  fi

  cat <<EOF > "${ENV_FILE}"
FAP_PORT="${port}"
FAP_ADDR="0.0.0.0:${port}"
FAP_LISTEN="0.0.0.0:${port}"
FAP_BASE_PATH="${base_path}"
FAP_DB="${db_type}"
FAP_DATA="${DATA_DIR}/fastasiapanel.db"
FAP_ADMIN_USER="${admin_user}"
FAP_ADMIN_PASS="${admin_pass}"
EOF
  chmod 600 "${ENV_FILE}"
  log_info "Configuration written to ${ENV_FILE}"
}

setup_service() {
  log_info "Configuring systemd service (${SERVICE_NAME}.service)..."
  cat <<EOF > "${SERVICE_FILE}"
[Unit]
Description=FastAsiaPanel Xray Management Service
After=network.target

[Service]
Type=simple
WorkingDirectory=${DATA_DIR}
EnvironmentFile=-${ENV_FILE}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE_NAME}"
  sleep 2

  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    log_info "Service ${SERVICE_NAME} is running."
  else
    log_warn "Service ${SERVICE_NAME} is not active yet. Check logs with: journalctl -u ${SERVICE_NAME} -n 50 --no-pager"
  fi
}

install_cli() {
  if [[ -f "${SRC_DIR}/scripts/fastasian.sh" ]]; then
    install -m 755 "${SRC_DIR}/scripts/fastasian.sh" "${CLI_PATH}"
    log_info "CLI manager installed at ${CLI_PATH}"
  else
    log_err "scripts/fastasian.sh not found in ${SRC_DIR}"
    exit 1
  fi
}

show_summary() {
  set -a
  # shellcheck disable=SC1090
  [[ -f "${ENV_FILE}" ]] && source "${ENV_FILE}"
  set +a

  local ip
  ip=$(server_ip || true)
  echo ""
  echo -e "${CYAN}========================================================${NC}"
  echo -e "${BOLD}  FastAsiaPanel Installation Completed${NC}"
  echo -e "${CYAN}========================================================${NC}"
  [[ -n "${ip}" ]] && echo -e "  Panel URL      : ${GREEN}http://${ip}:${FAP_PORT:-2053}${FAP_BASE_PATH:-}/admin/${NC}"
  echo -e "  Listen Port    : ${YELLOW}${FAP_PORT:-2053}${NC}"
  echo -e "  Web Base Path  : ${YELLOW}${FAP_BASE_PATH:-/}${NC}"
  echo -e "  Database       : ${YELLOW}${FAP_DB:-sqlite}${NC}"
  echo -e "  Admin Username : ${YELLOW}${FAP_ADMIN_USER:-admin}${NC}"
  echo -e "  Admin Password : ${YELLOW}${FAP_ADMIN_PASS:-admin123}${NC}"
  echo -e "  CLI Command    : ${CYAN}fastasian${NC}"
  echo -e "${CYAN}========================================================${NC}"
}

main() {
  echo -e "${CYAN}${BOLD}=== FastAsiaPanel Installer ===${NC}"
  install_deps
  sync_source
  build_panel
  setup_env
  setup_service
  install_cli
  show_summary
}

main "$@"