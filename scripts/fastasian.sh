#!/usr/bin/env bash
set -u
if [[ -n "${TERM:-}" ]] && ! infocmp "${TERM}" >/dev/null 2>&1; then
  export TERM="xterm-256color"
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

[[ $EUID -ne 0 ]] && exec sudo "$0" "$@"

CONF_DIR="/etc/fastasiapanel"
ENV_FILE="${CONF_DIR}/fastasiapanel.env"
[[ -f "${ENV_FILE}" ]] || ENV_FILE="${CONF_DIR}/panel.env"
DATA_DIR="/var/lib/fastasiapanel"
APP_DIR="/opt/fastasiapanel"
BIN_PATH="/usr/local/bin/fastasiapanel"
CLI_PATH="/usr/local/bin/fastasian"
SERVICE_NAME="fastasiapanel"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
INSTALL_URL="https://raw.githubusercontent.com/VahidDashtbani/fastasiapanel/main/install.sh"
CLI_URL="https://raw.githubusercontent.com/VahidDashtbani/fastasiapanel/main/scripts/fastasian.sh"

LOGI() { echo -e "${GREEN}[INF]${NC} $*"; }
LOGW() { echo -e "${YELLOW}[WRN]${NC} $*"; }
LOGE() { echo -e "${RED}[ERR]${NC} $*"; }

load_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
    set +a
  fi
}

save_env_key() {
  local key="$1"
  local val="$2"
  mkdir -p "${CONF_DIR}"
  touch "${ENV_FILE}"
  chmod 600 "${ENV_FILE}"
  local escaped
  escaped=$(printf '%s' "${val}" | sed 's/[&/\]/\\&/g')
  if grep -q "^${key}=" "${ENV_FILE}"; then
    sed -i "s|^${key}=.*|${key}=\"${escaped}\"|" "${ENV_FILE}"
  else
    echo "${key}=\"${escaped}\"" >> "${ENV_FILE}"
  fi
}

gen_rand_str() {
  local len="${1:-12}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 $((len * 2)) | tr -dc 'a-zA-Z0-9' | head -c "${len}"
  else
    tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "${len}"
  fi
}

confirm() {
  local prompt="$1"
  local def="${2:-n}"
  local ans=""
  if [[ -r /dev/tty ]]; then
    read -rp "${prompt} [${def}]: " ans </dev/tty
  else
    read -rp "${prompt} [${def}]: " ans
  fi
  [[ -z "${ans}" ]] && ans="${def}"
  [[ "${ans,,}" == "y" || "${ans,,}" == "yes" ]]
}

pause_key() {
  echo ""
  if [[ -r /dev/tty ]]; then
    read -rp "Press Enter to continue..." _ </dev/tty
  else
    read -rp "Press Enter to continue..." _
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

is_port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | awk -v p=":${port}$" '$4 ~ p {found=1} END {exit !found}'
    return
  fi
  return 1
}

is_domain() {
  [[ "$1" =~ ^([A-Za-z0-9](-*[A-Za-z0-9])*\.)+(xn--[a-z0-9]{2,}|[A-Za-z]{2,})$ ]]
}

panel_installed() {
  [[ -x "${BIN_PATH}" && -f "${SERVICE_FILE}" ]]
}

panel_running() {
  systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null
}

panel_enabled() {
  systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null
}

xray_running() {
  pgrep -f "(^|/)(xray|xray-linux)" >/dev/null 2>&1
}

show_status_summary() {
  if ! panel_installed; then
    echo -e "  Panel State    : ${RED}Not Installed${NC}"
  elif panel_running; then
    echo -e "  Panel State    : ${GREEN}● Running${NC}"
  else
    echo -e "  Panel State    : ${RED}● Stopped${NC}"
  fi

  if panel_installed; then
    if panel_enabled; then
      echo -e "  Autostart      : ${GREEN}Enabled${NC}"
    else
      echo -e "  Autostart      : ${YELLOW}Disabled${NC}"
    fi
  fi

  if xray_running; then
    echo -e "  Xray Core      : ${GREEN}● Running${NC}"
  else
    echo -e "  Xray Core      : ${YELLOW}● Idle / Stopped${NC}"
  fi
}

start_panel() {
  if ! panel_installed; then
    LOGE "FastAsiaPanel is not installed."
    return 1
  fi
  if panel_running; then
    LOGI "FastAsiaPanel is already running."
    return 0
  fi
  systemctl start "${SERVICE_NAME}"
  sleep 2
  if panel_running; then
    LOGI "FastAsiaPanel started successfully."
  else
    LOGE "Failed to start FastAsiaPanel. Check logs with: fastasian log"
    return 1
  fi
}

stop_panel() {
  if ! panel_installed; then
    LOGE "FastAsiaPanel is not installed."
    return 1
  fi
  if ! panel_running; then
    LOGW "FastAsiaPanel is already stopped."
    return 0
  fi
  systemctl stop "${SERVICE_NAME}"
  sleep 1
  if ! panel_running; then
    LOGI "FastAsiaPanel stopped successfully."
  else
    LOGE "Failed to stop FastAsiaPanel."
    return 1
  fi
}

restart_panel() {
  if ! panel_installed; then
    LOGE "FastAsiaPanel is not installed."
    return 1
  fi
  systemctl restart "${SERVICE_NAME}"
  sleep 2
  if panel_running; then
    LOGI "FastAsiaPanel restarted successfully."
  else
    LOGE "FastAsiaPanel failed to restart. Check logs with: fastasian log"
    return 1
  fi
}

restart_xray() {
  load_env
  local port="${FAP_PORT:-2053}"
  local base="${FAP_BASE_PATH:-}"
  if curl -fsS --max-time 4 -X POST "http://127.0.0.1:${port}${base}/api/xray/restart" >/dev/null 2>&1; then
    LOGI "Xray Core restart signal sent via panel API."
    return 0
  fi
  LOGW "Direct Xray API endpoint unavailable; restarting ${SERVICE_NAME} service..."
  restart_panel
}

enable_autostart() {
  if ! panel_installed; then
    LOGE "FastAsiaPanel is not installed."
    return 1
  fi
  systemctl enable "${SERVICE_NAME}" && LOGI "Autostart enabled."
}

disable_autostart() {
  if ! panel_installed; then
    LOGE "FastAsiaPanel is not installed."
    return 1
  fi
  systemctl disable "${SERVICE_NAME}" && LOGI "Autostart disabled."
}

show_logs_menu() {
  while true; do
    echo ""
    echo -e "${CYAN}--- Logs Management ---${NC}"
    echo "1) Follow live logs (journalctl -f)"
    echo "2) View last 80 log lines"
    echo "3) Vacuum / trim old journal logs"
    echo "0) Back"
    local opt=""
    read -rp "Select option [0-3]: " opt </dev/tty
    case "${opt}" in
      1) journalctl -u "${SERVICE_NAME}" -e --no-pager -f; break ;;
      2) journalctl -u "${SERVICE_NAME}" -n 80 --no-pager; pause_key ;;
      3)
        journalctl --rotate >/dev/null 2>&1 || true
        journalctl --vacuum-time=1d >/dev/null 2>&1 || true
        LOGI "Old journal logs trimmed."
        pause_key
        ;;
      0) return 0 ;;
      *) LOGE "Invalid selection." ;;
    esac
  done
}

show_settings() {
  load_env
  local ip
  ip=$(server_ip)
  local port="${FAP_PORT:-}"
  if [[ -z "${port}" && -n "${FAP_LISTEN:-}" ]]; then
    port="${FAP_LISTEN##*:}"
  fi
  port="${port:-2053}"
  local base="${FAP_BASE_PATH:-/}"
  [[ -z "${base}" ]] && base="/"
  local db="${FAP_DB_TYPE:-${FAP_DB:-sqlite}}"
  local user="${FAP_ADMIN_USER:-admin}"
  local pass="${FAP_ADMIN_PASS:-********}"

  echo ""
  echo -e "${CYAN}========================================================${NC}"
  echo -e "${BOLD}  FastAsiaPanel Configuration & Access Info${NC}"
  echo -e "${CYAN}========================================================${NC}"
  show_status_summary
  echo -e "  Server IPv4    : ${CYAN}${ip:-unknown}${NC}"
  echo -e "  Listen Port    : ${YELLOW}${port}${NC}"
  echo -e "  Web Base Path  : ${YELLOW}${base:-/}${NC}"
  echo -e "  Database Type  : ${YELLOW}${db}${NC}"
  if [[ "${db}" == "postgres" ]]; then
    local safe_dsn
    safe_dsn=$(printf '%s' "${FAP_DB_DSN:-${FAP_PG_DSN:-}}" | sed -E 's|(://[^:/@]+:)[^@]+@|\1****@|')
    echo -e "  Postgres DSN   : ${YELLOW}${safe_dsn:-not set}${NC}"
  else
    echo -e "  SQLite File    : ${YELLOW}${FAP_DB_PATH:-/etc/fastasiapanel/fastasiapanel.db}${NC}"
  fi
  echo -e "  Admin Username : ${YELLOW}${user}${NC}"
  echo -e "  Admin Password : ${YELLOW}${pass}${NC}"
  if [[ -n "${ip}" ]]; then
    echo -e "  Panel URL      : ${GREEN}http://${ip}:${port}${base}/${NC}"
  fi
  echo -e "${CYAN}========================================================${NC}"
}

reset_credentials() {
  load_env
  local new_user="" new_pass=""
  read -rp "New Admin Username [empty = random]: " new_user </dev/tty
  read -rp "New Admin Password [empty = random]: " new_pass </dev/tty
  [[ -z "${new_user}" ]] && new_user="admin_$(gen_rand_str 5)"
  [[ -z "${new_pass}" ]] && new_pass="$(gen_rand_str 14)"

  save_env_key "FAP_ADMIN_USER" "${new_user}"
  save_env_key "FAP_ADMIN_PASS" "${new_pass}"
  load_env

  (cd "${DATA_DIR}" && "${BIN_PATH}" reset-admin "${new_user}" "${new_pass}") >/dev/null 2>&1 || true
  restart_panel
  LOGI "Admin credentials updated."
  show_settings
}

reset_web_base_path() {
  load_env
  local new_path=""
  read -rp "New Web Base Path (e.g. /mysecret or empty = random): " new_path </dev/tty
  if [[ -z "${new_path}" ]]; then
    new_path="/$(gen_rand_str 10)"
  elif [[ "${new_path:0:1}" != "/" ]]; then
    new_path="/${new_path}"
  fi
  new_path="${new_path%/}"

  save_env_key "FAP_BASE_PATH" "${new_path}"
  restart_panel
  LOGI "Web Base Path updated to: ${new_path}"
  show_settings
}

change_panel_port() {
  load_env
  local new_port=""
  read -rp "New Panel Port [1-65535, empty = random]: " new_port </dev/tty
  if [[ -z "${new_port}" ]]; then
    while true; do
      new_port=$(shuf -i 12000-58000 -n 1)
      is_port_in_use "${new_port}" || break
    done
  fi
  if ! [[ "${new_port}" =~ ^[0-9]+$ ]] || (( new_port < 1 || new_port > 65535 )); then
    LOGE "Invalid port number."
    return 1
  fi

  save_env_key "FAP_PORT" "${new_port}"
  save_env_key "FAP_ADDR" "0.0.0.0:${new_port}"
  save_env_key "FAP_LISTEN" "0.0.0.0:${new_port}"

  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${new_port}/tcp" >/dev/null 2>&1 || true
  fi

  restart_panel
  LOGI "Panel port changed to ${new_port}."
  show_settings
}

update_panel() {
  if ! confirm "Update FastAsiaPanel from GitHub (existing data and config are kept)?" "y"; then
    LOGW "Update cancelled."
    return 0
  fi
  LOGI "Downloading latest installer and updating FastAsiaPanel..."
  local tmp_ins="/tmp/fastasia-install.$$.sh"
  curl -fsSL "${INSTALL_URL}?v=$(date +%s)" -o "${tmp_ins}"
  NON_INTERACTIVE=1 bash "${tmp_ins}"
  rm -f "${tmp_ins}"
  systemctl restart "${SERVICE_NAME}" || true
}

update_cli_script() {
  if ! confirm "Update the fastasian CLI script from GitHub?" "y"; then
    LOGW "Update cancelled."
    return 0
  fi
  local tmp_cli="/tmp/fastasian.sh.$$"
  if curl -fsSL "${CLI_URL}?v=$(date +%s)" -o "${tmp_cli}" && [[ -s "${tmp_cli}" ]]; then
    install -m 755 "${tmp_cli}" "${CLI_PATH}"
    rm -f "${tmp_cli}"
    LOGI "fastasian CLI updated. Re-running..."
    exec "${CLI_PATH}"
  else
    rm -f "${tmp_cli}"
    LOGE "Failed to download updated CLI script."
    return 1
  fi
}

update_geodata() {
  LOGI "Updating GeoIP and GeoSite files..."
  mkdir -p /usr/local/share/xray
  local ok=1
  curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat" -o /usr/local/share/xray/geoip.dat || ok=0
  curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat" -o /usr/local/share/xray/geosite.dat || ok=0
  if [[ "${ok}" -eq 1 ]]; then
    LOGI "GeoIP and GeoSite updated successfully."
    restart_panel
  else
    LOGE "Failed to download one or more Geo data files."
    return 1
  fi
}

install_acme_if_needed() {
  if [[ -x "${HOME}/.acme.sh/acme.sh" ]]; then
    return 0
  fi
  LOGI "Installing acme.sh..."
  curl -fsSL https://get.acme.sh | sh
}

ssl_menu() {
  while true; do
    echo ""
    echo -e "${CYAN}--- SSL Certificate Management (Let's Encrypt / acme.sh) ---${NC}"
    echo "1) Issue SSL for Domain (standalone HTTP-01)"
    echo "2) Issue short-lived SSL for Server IPv4"
    echo "3) Force renew existing domain certificate"
    echo "4) List certificates in /root/cert"
    echo "0) Back"
    local opt=""
    read -rp "Select option [0-4]: " opt </dev/tty
    case "${opt}" in
      1)
        local domain="" webport=""
        read -rp "Enter domain name: " domain </dev/tty
        domain="${domain// /}"
        if ! is_domain "${domain}"; then
          LOGE "Invalid domain name."
          pause_key
          continue
        fi
        read -rp "Standalone HTTP port [default 80]: " webport </dev/tty
        webport="${webport:-80}"
        if is_port_in_use "${webport}"; then
          LOGE "Port ${webport} is currently in use. Stop the service using port ${webport} or choose another port."
          pause_key
          continue
        fi
        install_acme_if_needed || { pause_key; continue; }
        mkdir -p "/root/cert/${domain}"
        "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force
        if "${HOME}/.acme.sh/acme.sh" --issue -d "${domain}" --standalone --httpport "${webport}" --force && \
           "${HOME}/.acme.sh/acme.sh" --installcert --force -d "${domain}" \
             --key-file "/root/cert/${domain}/privkey.pem" \
             --fullchain-file "/root/cert/${domain}/fullchain.pem" \
             --reloadcmd "systemctl restart ${SERVICE_NAME}"; then
          chmod 600 "/root/cert/${domain}/privkey.pem"
          chmod 644 "/root/cert/${domain}/fullchain.pem"
          LOGI "SSL installed at /root/cert/${domain}/"
        else
          LOGE "Certificate issuance/installation failed."
        fi
        pause_key
        ;;
      2)
        local ip webport=""
        ip=$(server_ip)
        if [[ -z "${ip}" ]]; then
          LOGE "Could not detect public IPv4."
          pause_key
          continue
        fi
        read -rp "Standalone HTTP port for IP ${ip} [default 80]: " webport </dev/tty
        webport="${webport:-80}"
        if is_port_in_use "${webport}"; then
          LOGE "Port ${webport} is currently in use."
          pause_key
          continue
        fi
        install_acme_if_needed || { pause_key; continue; }
        mkdir -p "/root/cert/ip"
        "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force
        if "${HOME}/.acme.sh/acme.sh" --issue -d "${ip}" --standalone --server letsencrypt --certificate-profile shortlived --days 6 --httpport "${webport}" --force && \
           "${HOME}/.acme.sh/acme.sh" --installcert --force -d "${ip}" \
             --key-file "/root/cert/ip/privkey.pem" \
             --fullchain-file "/root/cert/ip/fullchain.pem" \
             --reloadcmd "systemctl restart ${SERVICE_NAME}"; then
          chmod 600 "/root/cert/ip/privkey.pem"
          chmod 644 "/root/cert/ip/fullchain.pem"
          LOGI "Short-lived IP certificate installed at /root/cert/ip/"
        else
          LOGE "Failed to issue IP certificate."
        fi
        pause_key
        ;;
      3)
        local dom=""
        read -rp "Enter domain to renew: " dom </dev/tty
        if [[ -n "${dom}" && -x "${HOME}/.acme.sh/acme.sh" ]]; then
          "${HOME}/.acme.sh/acme.sh" --renew -d "${dom}" --force
          pause_key
        else
          LOGE "Domain empty or acme.sh not installed."
          pause_key
        fi
        ;;
      4)
        if [[ -d /root/cert ]]; then
          ls -la /root/cert
        else
          LOGW "No /root/cert directory found."
        fi
        pause_key
        ;;
      0) return 0 ;;
      *) LOGE "Invalid selection." ;;
    esac
  done
}

firewall_menu() {
  load_env
  local port="${FAP_PORT:-2053}"
  while true; do
    echo ""
    echo -e "${CYAN}--- Firewall (UFW) Management ---${NC}"
    echo "1) Enable UFW & allow SSH (22/tcp) + Panel Port (${port}/tcp)"
    echo "2) Open custom port or range (e.g. 443/tcp or 10000-20000/udp)"
    echo "3) Delete rule by number"
    echo "4) Show UFW status"
    echo "0) Back"
    local opt=""
    read -rp "Select option [0-4]: " opt </dev/tty
    case "${opt}" in
      1)
        if ! command -v ufw >/dev/null 2>&1; then
          LOGE "ufw is not installed."
          pause_key
          continue
        fi
        ufw allow 22/tcp >/dev/null 2>&1 || true
        ufw allow "${port}/tcp" >/dev/null 2>&1 || true
        ufw --force enable
        pause_key
        ;;
      2)
        local rule=""
        read -rp "Enter port/rule (e.g. 443/tcp or 8443): " rule </dev/tty
        if [[ -n "${rule}" ]]; then
          ufw allow "${rule}"
        else
          LOGE "Rule cannot be empty."
        fi
        pause_key
        ;;
      3)
        ufw status numbered
        local num=""
        read -rp "Enter rule number to delete: " num </dev/tty
        if [[ "${num}" =~ ^[0-9]+$ ]]; then
          ufw --force delete "${num}"
        else
          LOGE "Invalid rule number."
        fi
        pause_key
        ;;
      4)
        ufw status verbose
        pause_key
        ;;
      0) return 0 ;;
      *) LOGE "Invalid selection." ;;
    esac
  done
}

bbr_menu() {
  local conf_file="/etc/sysctl.d/99-fastasia-bbr.conf"
  while true; do
    local cur_cc cur_qdisc
    cur_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo "unknown")
    cur_qdisc=$(sysctl -n net.core.default_qdisc 2>/dev/null || echo "unknown")
    echo ""
    echo -e "${CYAN}--- TCP BBR Management (cc=${cur_cc}, qdisc=${cur_qdisc}) ---${NC}"
    echo "1) Enable BBR + fq"
    echo "2) Disable / restore previous congestion control"
    echo "0) Back"
    local opt=""
    read -rp "Select option [0-2]: " opt </dev/tty
    case "${opt}" in
      1)
        mkdir -p /etc/sysctl.d
        {
          echo "#prev:${cur_qdisc}:${cur_cc}"
          echo "net.core.default_qdisc = fq"
          echo "net.ipv4.tcp_congestion_control = bbr"
        } > "${conf_file}"
        sysctl -p "${conf_file}" >/dev/null 2>&1 || sysctl --system >/dev/null 2>&1
        LOGI "Current TCP congestion control: $(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)"
        pause_key
        ;;
      2)
        if [[ -f "${conf_file}" ]]; then
          local prev_line prev_q prev_c
          prev_line=$(head -n 1 "${conf_file}")
          prev_q=$(echo "${prev_line}" | cut -d: -f2)
          prev_c=$(echo "${prev_line}" | cut -d: -f3)
          [[ -n "${prev_q}" && "${prev_q}" != "unknown" ]] && sysctl -w "net.core.default_qdisc=${prev_q}" >/dev/null 2>&1 || true
          [[ -n "${prev_c}" && "${prev_c}" != "unknown" ]] && sysctl -w "net.ipv4.tcp_congestion_control=${prev_c}" >/dev/null 2>&1 || true
          rm -f "${conf_file}"
          LOGI "Restored congestion control: $(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)"
        else
          LOGW "No BBR config file found at ${conf_file}."
        fi
        pause_key
        ;;
      0) return 0 ;;
      *) LOGE "Invalid selection." ;;
    esac
  done
}

postgres_menu() {
  while true; do
    echo ""
    echo -e "${CYAN}--- PostgreSQL Management ---${NC}"
    echo "1) Check PostgreSQL service & port 5432"
    echo "2) Start / Restart PostgreSQL"
    echo "3) Backup fastasiapanel database"
    echo "0) Back"
    local opt=""
    read -rp "Select option [0-3]: " opt </dev/tty
    case "${opt}" in
      1)
        systemctl status postgresql --no-pager 2>/dev/null || true
        if command -v ss >/dev/null 2>&1; then
          ss -ltnp 2>/dev/null | grep ':5432' || LOGW "Port 5432 is not listening."
        fi
        pause_key
        ;;
      2)
        systemctl restart postgresql && LOGI "PostgreSQL restarted."
        pause_key
        ;;
      3)
        if ! command -v pg_dump >/dev/null 2>&1; then
          LOGE "pg_dump is not installed."
          pause_key
          continue
        fi
        mkdir -p /root/fastasia-backups
        local out="/root/fastasia-backups/pg-fastasiapanel-$(date +%F-%H%M%S).sql"
        if runuser -u postgres -- pg_dump fastasiapanel > "${out}"; then
          LOGI "Database backup saved to ${out}"
        else
          rm -f "${out}"
          LOGE "Failed to dump database fastasiapanel."
        fi
        pause_key
        ;;
      0) return 0 ;;
      *) LOGE "Invalid selection." ;;
    esac
  done
}

uninstall_panel() {
  echo ""
  if ! confirm "Are you sure you want to uninstall FastAsiaPanel?" "n"; then
    LOGW "Uninstall cancelled."
    return 0
  fi
  systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl disable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  rm -f "${SERVICE_FILE}"
  systemctl daemon-reload
  rm -rf "${APP_DIR}" "${CONF_DIR}" "${BIN_PATH}" "${CLI_PATH}"
  if confirm "Also delete panel data directory (${DATA_DIR})?" "n"; then
    rm -rf "${DATA_DIR}"
  fi
  LOGI "FastAsiaPanel uninstalled."
  exit 0
}

show_usage() {
  echo -e "${CYAN}FastAsiaPanel CLI Usage:${NC}"
  echo "  fastasian                 Open interactive management menu"
  echo "  fastasian start           Start fastasiapanel service"
  echo "  fastasian stop            Stop fastasiapanel service"
  echo "  fastasian restart         Restart fastasiapanel service"
  echo "  fastasian restart-xray    Restart Xray core"
  echo "  fastasian status          Show service and Xray status"
  echo "  fastasian settings        Show panel URL, port, DB, and credentials"
  echo "  fastasian enable          Enable autostart on boot"
  echo "  fastasian disable         Disable autostart on boot"
  echo "  fastasian log             Follow live service logs"
  echo "  fastasian update          Update FastAsiaPanel from GitHub"
  echo "  fastasian uninstall       Uninstall FastAsiaPanel"
}

show_menu() {
  while true; do
    clear
    load_env
    local port="${FAP_PORT:-}"
    if [[ -z "${port}" && -n "${FAP_LISTEN:-}" ]]; then
      port="${FAP_LISTEN##*:}"
    fi
    port="${port:-2053}"
    local db="${FAP_DB_TYPE:-${FAP_DB:-sqlite}}"
    local base="${FAP_BASE_PATH:-/}"
    [[ -z "${base}" ]] && base="/"
    echo -e "${CYAN}┌────────────────────────────────────────────────────────────┐${NC}"
    echo -e "${CYAN}│${NC}  ${BOLD}FastAsiaPanel Management Menu${NC}                             ${CYAN}│${NC}"
    echo -e "${CYAN}├────────────────────────────────────────────────────────────┤${NC}"
    show_status_summary
    echo -e "  Port / Path    : ${YELLOW}${port}${NC} (${YELLOW}${base}${NC})   DB: ${YELLOW}${db}${NC}"
    echo -e "${CYAN}├────────────────────────────────────────────────────────────┤${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}1.${NC} Start Panel            ${GREEN}9.${NC}  View Info & URL          ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}2.${NC} Stop Panel             ${GREEN}10.${NC} Reset Admin User/Pass  ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}3.${NC} Restart Panel          ${GREEN}11.${NC} Reset Web Base Path    ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}4.${NC} Restart Xray Core      ${GREEN}12.${NC} Change Panel Port      ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}5.${NC} Logs Management        ${GREEN}13.${NC} Update GeoIP/GeoSite   ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}6.${NC} Enable Autostart       ${GREEN}14.${NC} SSL Certificate Menu   ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}7.${NC} Disable Autostart      ${GREEN}15.${NC} Firewall (UFW) Menu    ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}8.${NC} Update Panel           ${GREEN}16.${NC} TCP BBR Menu           ${CYAN}│${NC}"
    echo -e "${CYAN}├────────────────────────────────────────────────────────────┤${NC}"
    echo -e "${CYAN}│${NC}  ${GREEN}17.${NC} PostgreSQL Menu       ${GREEN}18.${NC} Update fastasian CLI   ${CYAN}│${NC}"
    echo -e "${CYAN}│${NC}  ${RED}19.${NC} Uninstall Panel       ${YELLOW}0.${NC}  Exit                   ${CYAN}│${NC}"
    echo -e "${CYAN}└────────────────────────────────────────────────────────────┘${NC}"
    local choice=""
    read -rp "Select an option [0-19]: " choice </dev/tty

    case "${choice}" in
      1) start_panel; pause_key ;;
      2) stop_panel; pause_key ;;
      3) restart_panel; pause_key ;;
      4) restart_xray; pause_key ;;
      5) show_logs_menu ;;
      6) enable_autostart; pause_key ;;
      7) disable_autostart; pause_key ;;
      8) update_panel; pause_key ;;
      9) show_settings; pause_key ;;
      10) reset_credentials; pause_key ;;
      11) reset_web_base_path; pause_key ;;
      12) change_panel_port; pause_key ;;
      13) update_geodata; pause_key ;;
      14) ssl_menu ;;
      15) firewall_menu ;;
      16) bbr_menu ;;
      17) postgres_menu ;;
      18) update_cli_script ;;
      19) uninstall_panel ;;
      0) exit 0 ;;
      *) LOGE "Invalid selection."; sleep 1 ;;
    esac
  done
}

if [[ $# -gt 0 ]]; then
  case "$1" in
    start) start_panel ;;
    stop) stop_panel ;;
    restart) restart_panel ;;
    restart-xray) restart_xray ;;
    status) show_status_summary ;;
    settings) show_settings ;;
    enable) enable_autostart ;;
    disable) disable_autostart ;;
    log) journalctl -u "${SERVICE_NAME}" -e --no-pager -f ;;
    update) update_panel ;;
    uninstall) uninstall_panel ;;
    *) show_usage ;;
  esac
else
  show_menu
fi
