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

  install -d -m 0700 "${APP_DIR}/data"
  umask 077
  cat > "${ENV_FILE}" <<EOF
PANEL_ADDR=:${port}
PANEL_USERNAME=${username}
PANEL_PASSWORD=${password}
PANEL_COOKIE_SECRET=${secret}
PANEL_SECURE_COOKIE=false
DB_FILE=${DB_FILE}
XRAY_BIN=${XRAY_BIN}
XRAY_CONFIG=${XRAY_CONFIG}
EOF
  chmod 0600 "${ENV_FILE}"
}

ensure_env() {
  local existing_port
  if [[ -f "${ENV_FILE}" ]]; then
    log "Existing panel.env found; preserving credentials and settings."
    existing_port="$(read_env_value PANEL_ADDR)"
    if [[ -z "${existing_port}" ]]; then
      printf '\nPANEL_ADDR=:8080\n' >> "${ENV_FILE}"
    fi
    grep -q '^DB_FILE=' "${ENV_FILE}" || printf 'DB_FILE=%s\n' "${DB_FILE}" >> "${ENV_FILE}"
    grep -q '^XRAY_BIN=' "${ENV_FILE}" || printf 'XRAY_BIN=%s\n' "${XRAY_BIN}" >> "${ENV_FILE}"
    grep -q '^XRAY_CONFIG=' "${ENV_FILE}" || printf 'XRAY_CONFIG=%s\n' "${XRAY_CONFIG}" >> "${ENV_FILE}"
    grep -q '^PANEL_COOKIE_SECRET=' "${ENV_FILE}" || printf 'PANEL_COOKIE_SECRET=%s\n' "$(openssl rand -hex 32)" >> "${ENV_FILE}"
    grep -q '^PANEL_USERNAME=' "${ENV_FILE}" || printf 'PANEL_USERNAME=admin\n' >> "${ENV_FILE}"
    if ! grep -q '^PANEL_PASSWORD=' "${ENV_FILE}"; then
      printf 'PANEL_PASSWORD=%s\n' "$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-20)" >> "${ENV_FILE}"
      warn "A new admin password was generated because none existed in panel.env."
    fi
    chmod 0600 "${ENV_FILE}"
  else
    local port
    port="$(select_port 8080)"
    write_initial_env "${port}"
  fi
}

download_panel() {
  local url="https://github.com/${REPO}/releases/latest/download/fastasiapanel-linux-${ARCH}.tar.gz"
  local archive="${TMP_DIR}/panel.tar.gz"

  log "Downloading FastAsiaPanel (${ARCH})..."
  curl -fL --retry 3 --connect-timeout 15 "${url}" -o "${archive}"
  tar -tzf "${archive}" >/dev/null || die "Downloaded release archive is invalid."
  tar -xzf "${archive}" -C "${TMP_DIR}"

  [[ -s "${TMP_DIR}/fastasiapanel" ]] || die "Release archive does not contain the fastasiapanel binary."
  [[ -d "${TMP_DIR}/web" ]] || die "Release archive does not contain web assets."
  [[ -f "${TMP_DIR}/web/index.html" ]] || die "web/index.html is missing from release archive."

  install -d -m 0755 "${APP_DIR}"
  install -m 0755 "${TMP_DIR}/fastasiapanel" "${BIN_PATH}"
  rm -rf "${APP_DIR}/web"
  cp -a "${TMP_DIR}/web" "${APP_DIR}/web"
  find "${APP_DIR}/web" -type d -exec chmod 0755 {} \;
  find "${APP_DIR}/web" -type f -exec chmod 0644 {} \;
}

install_xray_if_needed() {
  local archive="${TMP_DIR}/xray.zip"
  local url="https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${XRAY_ARCH}.zip"

  if [[ -x "${XRAY_BIN}" ]]; then
    log "Xray already installed at ${XRAY_BIN}; keeping existing binary."
    return 0
  fi

  log "Downloading Xray-core..."
  if ! command -v unzip >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get install -y unzip
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y unzip
    elif command -v yum >/dev/null 2>&1; then
      yum install -y unzip
    elif command -v apk >/dev/null 2>&1; then
      apk add --no-cache unzip
    else
      die "Install unzip and rerun the installer."
    fi
  fi

  curl -fL --retry 3 --connect-timeout 15 "${url}" -o "${archive}"
  unzip -oq "${archive}" xray -d "${TMP_DIR}/xray"
  [[ -s "${TMP_DIR}/xray/xray" ]] || die "Xray archive does not contain the xray binary."
  install -m 0755 "${TMP_DIR}/xray/xray" "${XRAY_BIN}"
}

create_cli() {
  cat > "${CLI_PATH}" <<'CLIEOF'
#!/usr/bin/env bash
set -Eeuo pipefail

APP_DIR="/opt/fastasiapanel"
SERVICE="fastasiapanel.service"
ENV_FILE="${APP_DIR}/panel.env"
PLAIN=$'\033[0m'
CYAN=$'\033[1;36m'
GREEN=$'\033[1;32m'
RED=$'\033[1;31m'

require_root() {
  [[ "${EUID}" -eq 0 ]] || { printf 'Run fastpanel as root.\n' >&2; exit 1; }
}

read_setting() {
  local key="$1"
  sed -n "s/^${key}=//p" "${ENV_FILE}" 2>/dev/null | tail -n1
}

panel_port() {
  local addr
  addr="$(read_setting PANEL_ADDR)"
  printf '%s' "${addr##*:}"
}

show_status() {
  systemctl --no-pager --full status "${SERVICE}" || true
}

show_info() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    printf 'FastAsiaPanel is not installed.\n'
    return 1
  fi
  printf 'FastAsiaPanel information\n'
  printf '%s\n' '-------------------------'
  printf 'Address: %s\n' "$(read_setting PANEL_ADDR)"
  printf 'Port: %s\n' "$(panel_port)"
  printf 'Username: %s\n' "$(read_setting PANEL_USERNAME)"
  printf 'Data file: %s\n' "$(read_setting DB_FILE)"
  printf 'Xray binary: %s\n' "$(read_setting XRAY_BIN)"
  printf 'Service: %s\n' "${SERVICE}"
}

restart_panel() {
  systemctl restart "${SERVICE}"
  sleep 2
  if systemctl is-active --quiet "${SERVICE}"; then
    printf '%bPanel restarted successfully.%b\n' "${GREEN}" "${PLAIN}"
  else
    printf '%bPanel failed to start. Check: journalctl -u %s -n 80 --no-pager%b\n' "${RED}" "${SERVICE}" "${PLAIN}"
    return 1
  fi
}

change_port() {
  local current new_port tmp
  current="$(panel_port)"
  read -r -p "New panel port [${current}]: " new_port
  new_port="${new_port:-$current}"
  if [[ ! "${new_port}" =~ ^[0-9]{1,5}$ ]] || ((10#${new_port} < 1 || 10#${new_port} > 65535)); then
    printf 'Invalid port.\n'
    return 1
  fi
  if [[ "${new_port}" == "${current}" ]]; then
    printf 'Port unchanged.\n'
    return 0
  fi
  tmp="$(mktemp)"
  sed -E "s|^PANEL_ADDR=.*|PANEL_ADDR=:${new_port}|" "${ENV_FILE}" > "${tmp}"
  chmod 0600 "${tmp}"
  cp "${ENV_FILE}" "${ENV_FILE}.bak"
  mv "${tmp}" "${ENV_FILE}"
  if systemctl restart "${SERVICE}" && sleep 2 && systemctl is-active --quiet "${SERVICE}"; then
    rm -f "${ENV_FILE}.bak"
    printf 'Panel port changed to %s.\n' "${new_port}"
    printf 'If remote access is blocked, allow TCP port %s in your firewall/provider panel.\n' "${new_port}"
  else
    mv "${ENV_FILE}.bak" "${ENV_FILE}"
    systemctl restart "${SERVICE}" || true
    printf 'Port change failed; previous configuration restored.\n' >&2
    return 1
  fi
}

uninstall_panel() {
  local answer
  printf 'This removes FastAsiaPanel, its service and its data in %s.\n' "${APP_DIR}"
  read -r -p 'Type YES to confirm complete removal: ' answer
  [[ "${answer}" == "YES" ]] || { printf 'Cancelled.\n'; return; }
  systemctl disable --now "${SERVICE}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE}"
  systemctl daemon-reload
  rm -rf -- "${APP_DIR}"
  rm -f -- "/usr/local/bin/fastpanel"
  printf 'FastAsiaPanel uninstalled.\n'
  exit 0
}

pause_menu() {
  printf '\n'
  read -r -p "Press Enter to continue..."
}

run_menu() {
  local choice
  while true; do
    clear 2>/dev/null || true
    printf '%b========================================%b\n' "${CYAN}" "${PLAIN}"
    printf '             FastAsiaPanel\n'
    printf '           Management Console\n'
    printf '%b========================================%b\n\n' "${CYAN}" "${PLAIN}"
    printf '  1. Panel status\n'
    printf '  2. Start panel\n'
    printf '  3. Stop panel\n'
    printf '  4. Restart panel and Xray supervisor\n'
    printf '  5. Change panel port\n'
    printf '  6. Panel information\n'
    printf '  7. View logs\n'
    printf '  8. Uninstall panel\n'
    printf '  0. Exit\n\n'
    read -r -p "Select an option [0-8]: " choice || exit 0
    case "${choice}" in
      1) show_status; pause_menu ;;
      2) systemctl start "${SERVICE}"; printf 'Start command sent.\n'; pause_menu ;;
      3) systemctl stop "${SERVICE}"; printf 'Stop command sent.\n'; pause_menu ;;
      4) restart_panel; pause_menu ;;
      5) change_port; pause_menu ;;
      6) show_info; pause_menu ;;
      7) journalctl -u "${SERVICE}" -f ;;
      8) uninstall_panel ;;
      0) exit 0 ;;
      *) printf 'Invalid option.\n'; sleep 1 ;;
    esac
  done
}

require_root
case "${1:-}" in
  "") run_menu ;;
  start) systemctl start "${SERVICE}" ;;
  stop) systemctl stop "${SERVICE}" ;;
  restart) restart_panel ;;
  status) show_status ;;
  info) show_info ;;
  port) change_port ;;
  log|logs) journalctl -u "${SERVICE}" -f ;;
  uninstall) uninstall_panel ;;
  *)
    printf 'Usage: fastpanel [start|stop|restart|status|info|port|log|uninstall]\n'
    exit 2
    ;;
esac
CLIEOF
  chmod 0750 "${CLI_PATH}"
}

create_service() {
  cat > "${SERVICE_PATH}" <<UNIT
[Unit]
Description=FastAsiaPanel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${APP_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN_PATH}
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
ReadWritePaths=${APP_DIR}

[Install]
WantedBy=multi-user.target
UNIT
  chmod 0644 "${SERVICE_PATH}"
  systemctl daemon-reload
  systemctl enable "${APP_NAME}.service"
  systemctl restart "${APP_NAME}.service"
  sleep 2
  systemctl is-active --quiet "${APP_NAME}.service" || {
    journalctl -u "${APP_NAME}.service" -n 60 --no-pager >&2 || true
    die "FastAsiaPanel service did not become active."
  }
}

main() {
  require_root
  command -v systemctl >/dev/null 2>&1 || die "systemd is required on this Linux host."
  install_dependencies
  detect_arch
  TMP_DIR="$(mktemp -d)"
  install -d -m 0755 "${APP_DIR}"
  ensure_env
  download_panel
  install_xray_if_needed
  create_cli
  create_service

  local panel_user panel_pass panel_addr panel_port
  panel_user="$(read_env_value PANEL_USERNAME)"
  panel_pass="$(read_env_value PANEL_PASSWORD)"
  panel_addr="$(read_env_value PANEL_ADDR)"
  panel_port="${panel_addr##*:}"

  printf '\n%bFastAsiaPanel installed.%b\n' "${GREEN}" "${PLAIN}"
  printf 'Username: %s\n' "${panel_user}"
  printf 'Password: %s\n' "${panel_pass}"
  printf 'Port: %s\n' "${panel_port}"
  printf 'Credentials: %s\n' "${ENV_FILE}"
  printf 'Service: systemctl status %s\n' "${APP_NAME}"
  printf 'Management menu: fastpanel\n'
  printf 'Open: http://SERVER-IP:%s/\n' "${panel_port}"
  warn "The panel currently uses HTTP. Put it behind HTTPS before exposing it publicly."
}

main "$@"
