#!/usr/bin/env bash
set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

APP_DIR="/opt/fastasiapanel"
DATA_DIR="/var/lib/fastasiapanel"
CONF_DIR="/etc/fastasiapanel"
ENV_FILE="${CONF_DIR}/panel.env"
BIN_PATH="/usr/local/bin/fastasiapanel"
CLI_PATH="/usr/local/bin/fastasian"
SERVICE_FILE="/etc/systemd/system/fastasiapanel.service"

if [[ $EUID -ne 0 ]]; then
  echo -e "${RED}Error: Please run this installer as root (sudo ./install.sh).${NC}"
  exit 1
fi

gen_rand_str() {
  local len="${1:-12}"
  tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "$len"
}

gen_rand_port() {
  local port
  while true; do
    port=$(shuf -i 12000-58000 -n 1)
    if ! ss -tuln 2>/dev/null | grep -q ":${port} "; then
      echo "$port"
      return
    fi
  done
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "64" ;;
    aarch64|arm64) echo "arm64-v8a" ;;
    *) echo "64" ;;
  esac
}

install_dependencies() {
  echo -e "${CYAN}==> Installing required system packages...${NC}"
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq curl unzip openssl ca-certificates jq sqlite3 ufw iproute2
  elif command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm --needed curl unzip openssl ca-certificates jq sqlite ufw iproute2
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q curl unzip openssl ca-certificates jq sqlite ufw iproute
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q curl unzip openssl ca-certificates jq sqlite iproute
  fi
}

install_xray() {
  echo -e "${CYAN}==> Checking / Installing Xray-core...${NC}"
  if command -v xray >/dev/null 2>&1 && [[ -f /usr/local/bin/xray ]]; then
    echo -e "${GREEN}Xray-core already installed at /usr/local/bin/xray.${NC}"
    return
  fi
  local arch
  arch=$(detect_arch)
  local tmp_dir
  tmp_dir=$(mktemp -d)
  if curl -fsSL "https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${arch}.zip" -o "${tmp_dir}/xray.zip"; then
    unzip -qo "${tmp_dir}/xray.zip" -d "${tmp_dir}"
    install -m 755 "${tmp_dir}/xray" /usr/local/bin/xray
    mkdir -p /usr/local/share/xray
    [[ -f "${tmp_dir}/geoip.dat" ]] && install -m 644 "${tmp_dir}/geoip.dat" /usr/local/share/xray/geoip.dat
    [[ -f "${tmp_dir}/geosite.dat" ]] && install -m 644 "${tmp_dir}/geosite.dat" /usr/local/share/xray/geosite.dat
    echo -e "${GREEN}Xray-core installed.${NC}"
  else
    echo -e "${YELLOW}Warning: Could not download Xray-core automatically. You can install it later via fastasian menu.${NC}"
  fi
  rm -rf "${tmp_dir}"
}

setup_postgres() {
  echo -e "${CYAN}==> Configuring PostgreSQL database...${NC}"
  if ! command -v psql >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      apt-get install -y -qq postgresql postgresql-contrib
    elif command -v pacman >/dev/null 2>&1; then
      pacman -Sy --noconfirm --needed postgresql
      if [[ ! -d /var/lib/postgres/data/base ]]; then
        su - postgres -c "initdb --locale=C.UTF-8 -E UTF8 -D '/var/lib/postgres/data'"
      fi
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y -q postgresql-server postgresql-contrib
      postgresql-setup --initdb || true
    fi
  fi
  systemctl enable --now postgresql

  local pg_user="fastasia"
  local pg_db="fastasiapanel"
  local pg_pass
  pg_pass=$(gen_rand_str 20)

  su - postgres -c "psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='${pg_user}'\" | grep -q 1 || psql -c \"CREATE USER ${pg_user} WITH PASSWORD '${pg_pass}';\""
  su - postgres -c "psql -c \"ALTER USER ${pg_user} WITH PASSWORD '${pg_pass}';\""
  su - postgres -c "psql -tc \"SELECT 1 FROM pg_database WHERE datname='${pg_db}'\" | grep -q 1 || psql -c \"CREATE DATABASE ${pg_db} OWNER ${pg_user};\""

  PG_DSN="postgres://${pg_user}:${pg_pass}@127.0.0.1:5432/${pg_db}?sslmode=disable"
}

build_or_fetch_panel() {
  mkdir -p "${APP_DIR}" "${DATA_DIR}" "${CONF_DIR}"
  if [[ -f "./fastasiapanel" ]]; then
    install -m 755 "./fastasiapanel" "${BIN_PATH}"
  elif [[ -f "./go.mod" ]] && command -v go >/dev/null 2>&1; then
    echo -e "${CYAN}==> Building FastAsiaPanel from local source...${NC}"
    go build -trimpath -ldflags="-s -w" -o "${BIN_PATH}" ./cmd/panel
  else
    echo -e "${RED}Error: Binary or Go source not found in current directory.${NC}"
    exit 1
  fi
}

write_cli_manager() {
  cat <<'EOF' > "${CLI_PATH}"
#!/usr/bin/env bash
set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

CONF_DIR="/etc/fastasiapanel"
ENV_FILE="${CONF_DIR}/panel.env"
DATA_DIR="/var/lib/fastasiapanel"
BIN_PATH="/usr/local/bin/fastasiapanel"
SERVICE_NAME="fastasiapanel"

load_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    set -a
    source "${ENV_FILE}"
    set +a
  fi
}

save_env_key() {
  local key="$1"
  local val="$2"
  mkdir -p "${CONF_DIR}"
  touch "${ENV_FILE}"
  if grep -q "^${key}=" "${ENV_FILE}"; then
    sed -i "s|^${key}=.*|${key}=\"${val}\"|" "${ENV_FILE}"
  else
    echo "${key}=\"${val}\"" >> "${ENV_FILE}"
  fi
}

server_ip() {
  curl -4 -fsSL --max-time 4 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}'
}

status_badge() {
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    echo -e "${GREEN}● Running${NC}"
  else
    echo -e "${RED}● Stopped${NC}"
  fi
}

pause() {
  echo ""
  read -rp "Press Enter to return to FastAsiaPanel menu..."
}

show_credentials() {
  load_env
  local ip
  ip=$(server_ip)
  local base="${FAP_BASE_PATH:-}"
  local port="${FAP_PORT:-2053}"
  echo -e "${CYAN}====================================================${NC}"
  echo -e "${BOLD}  FastAsiaPanel Active Configuration${NC}"
  echo -e "${CYAN}====================================================${NC}"
  echo -e "  Service Status : $(status_badge)"
  echo -e "  Panel URL      : ${GREEN}http://${ip}:${port}${base}/${NC}"
  echo -e "  Listen Port    : ${YELLOW}${port}${NC}"
  echo -e "  Web Base Path  : ${YELLOW}${base:-/}${NC}"
  echo -e "  Database Type  : ${YELLOW}${FAP_DB:-sqlite}${NC}"
  echo -e "  Admin User     : ${YELLOW}${FAP_ADMIN_USER:-admin}${NC}"
  echo -e "  Admin Password : ${YELLOW}${FAP_ADMIN_PASS:-********}${NC}"
  echo -e "${CYAN}====================================================${NC}"
}

reset_credentials() {
  load_env
  read -rp "New Admin Username [leave empty for random]: " new_user
  read -rp "New Admin Password [leave empty for random]: " new_pass
  [[ -z "${new_user}" ]] && new_user="admin_$(tr -dc 'a-z0-9' </dev/urandom | head -c 5)"
  [[ -z "${new_pass}" ]] && new_pass="$(tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 14)"

  save_env_key "FAP_ADMIN_USER" "${new_user}"
  save_env_key "FAP_ADMIN_PASS" "${new_pass}"
  load_env
  cd "${DATA_DIR}" && "${BIN_PATH}" reset-admin "${new_user}" "${new_pass}" || true
  systemctl restart "${SERVICE_NAME}"
  echo -e "${GREEN}Credentials updated successfully!${NC}"
  show_credentials
}

change_web_path() {
  load_env
  read -rp "Enter new Web Base Path (e.g. /secretpanel or empty to remove): " new_path
  if [[ -n "${new_path}" && "${new_path:0:1}" != "/" ]]; then
    new_path="/${new_path}"
  fi
  new_path="${new_path%/}"
  save_env_key "FAP_BASE_PATH" "${new_path}"
  systemctl restart "${SERVICE_NAME}"
  echo -e "${GREEN}Web Base Path updated to: ${new_path:-/}${NC}"
  show_credentials
}

change_port() {
  load_env
  read -rp "Enter new Panel Port [leave empty for random]: " new_port
  if [[ -z "${new_port}" ]]; then
    new_port=$(shuf -i 12000-58000 -n 1)
  fi
  if ! [[ "${new_port}" =~ ^[0-9]+$ ]] || (( new_port < 1 || new_port > 65535 )); then
    echo -e "${RED}Invalid port number.${NC}"
    return
  fi
  save_env_key "FAP_PORT" "${new_port}"
  save_env_key "FAP_ADDR" "0.0.0.0:${new_port}"
  if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
    ufw allow "${new_port}/tcp" >/dev/null 2>&1 || true
  fi
  systemctl restart "${SERVICE_NAME}"
  echo -e "${GREEN}Panel port changed to ${new_port}.${NC}"
  show_credentials
}

update_geodata() {
  echo -e "${CYAN}Downloading latest GeoIP and GeoSite files...${NC}"
  mkdir -p /usr/local/share/xray
  curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat" -o /usr/local/share/xray/geoip.dat
  curl -fsSL "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat" -o /usr/local/share/xray/geosite.dat
  systemctl restart "${SERVICE_NAME}"
  echo -e "${GREEN}GeoIP and GeoSite updated successfully.${NC}"
}

enable_bbr() {
  echo -e "${CYAN}Enabling TCP BBR congestion control...${NC}"
  cat <<SYSCTL > /etc/sysctl.d/99-fastasia-bbr.conf
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
SYSCTL
  sysctl --system >/dev/null
  echo -e "${GREEN}BBR enabled: $(sysctl -n net.ipv4.tcp_congestion_control)${NC}"
}

issue_ssl() {
  read -rp "Enter your domain pointing to this server: " domain
  read -rp "Enter email for Let's Encrypt notifications: " email
  if [[ -z "${domain}" || -z "${email}" ]]; then
    echo -e "${RED}Domain and email are required.${NC}"
    return
  fi
  if ! command -v certbot >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      apt-get update -qq && apt-get install -y -qq certbot
    elif command -v pacman >/dev/null 2>&1; then
      pacman -Sy --noconfirm --needed certbot
    fi
  fi
  certbot certonly --standalone --non-interactive --agree-tos -m "${email}" -d "${domain}"
  echo -e "${GREEN}SSL Certificates saved at:${NC}"
  echo "  Cert: /etc/letsencrypt/live/${domain}/fullchain.pem"
  echo "  Key : /etc/letsencrypt/live/${domain}/privkey.pem"
}

firewall_menu() {
  load_env
  echo "1) Enable UFW & Allow SSH + Panel Port (${FAP_PORT:-2053})"
  echo "2) Open custom port"
  echo "3) Check UFW status"
  read -rp "Select option: " fw_opt
  case "${fw_opt}" in
    1)
      ufw allow 22/tcp
      ufw allow "${FAP_PORT:-2053}/tcp"
      ufw --force enable
      ;;
    2)
      read -rp "Port number: " p
      ufw allow "${p}"
      ;;
    3)
      ufw status verbose
      ;;
  esac
}

postgres_menu() {
  load_env
  echo "1) Check PostgreSQL service status"
  echo "2) Restart PostgreSQL"
  echo "3) Backup PostgreSQL database"
  read -rp "Select option: " pg_opt
  case "${pg_opt}" in
    1) systemctl status postgresql --no-pager ;;
    2) systemctl restart postgresql && echo -e "${GREEN}PostgreSQL restarted.${NC}" ;;
    3)
      mkdir -p /root/fastasia-backups
      local out="/root/fastasia-backups/pg-$(date +%F-%H%M).sql"
      su - postgres -c "pg_dump fastasiapanel" > "${out}"
      echo -e "${GREEN}Backup saved to ${out}${NC}"
      ;;
  esac
}

uninstall_panel() {
  read -rp "Are you sure you want to completely remove FastAsiaPanel? [y/N]: " ans
  if [[ "${ans,,}" != "y" ]]; then
    return
  fi
  systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
  rm -rf /opt/fastasiapanel "${CONF_DIR}" "${BIN_PATH}" "${CLI_PATH}"
  read -rp "Also delete client data directory (${DATA_DIR})? [y/N]: " del_data
  if [[ "${del_data,,}" == "y" ]]; then
    rm -rf "${DATA_DIR}"
  fi
  echo -e "${GREEN}FastAsiaPanel uninstalled.${NC}"
  exit 0
}

while true; do
  clear
  load_env
  echo -e "${CYAN}┌──────────────────────────────────────────────────────┐${NC}"
  echo -e "${CYAN}│${NC}  ${BOLD}FastAsiaPanel Control Center${NC}                        ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  Status: $(status_badge)   Port: ${YELLOW}${FAP_PORT:-2053}${NC}   DB: ${YELLOW}${FAP_DB:-sqlite}${NC}   ${CYAN}│${NC}"
  echo -e "${CYAN}├──────────────────────────────────────────────────────┤${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}1.${NC} Start Panel          ${GREEN}9.${NC}  View Panel Info & URL    ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}2.${NC} Stop Panel           ${GREEN}10.${NC} Reset Admin User/Pass    ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}3.${NC} Restart Panel        ${GREEN}11.${NC} Change Web Base Path     ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}4.${NC} Restart Xray Core    ${GREEN}12.${NC} Change Panel Port        ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}5.${NC} Live Service Logs    ${GREEN}13.${NC} Update GeoIP & GeoSite   ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}6.${NC} Enable Autostart     ${GREEN}14.${NC} Issue Let's Encrypt SSL  ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}7.${NC} Disable Autostart    ${GREEN}15.${NC} Enable TCP BBR           ${CYAN}│${NC}"
  echo -e "${CYAN}│${NC}  ${GREEN}8.${NC} Firewall (UFW)       ${GREEN}16.${NC} PostgreSQL Management    ${CYAN}│${NC}"
  echo -e "${CYAN}├──────────────────────────────────────────────────────┤${NC}"
  echo -e "${CYAN}│${NC}  ${RED}17.${NC} Uninstall Panel     ${YELLOW}0.${NC}  Exit                     ${CYAN}│${NC}"
  echo -e "${CYAN}└──────────────────────────────────────────────────────┘${NC}"
  read -rp "Select an option [0-17]: " choice

  case "${choice}" in
    1) systemctl start "${SERVICE_NAME}" && echo -e "${GREEN}Started.${NC}"; pause ;;
    2) systemctl stop "${SERVICE_NAME}" && echo -e "${YELLOW}Stopped.${NC}"; pause ;;
    3) systemctl restart "${SERVICE_NAME}" && echo -e "${GREEN}Restarted.${NC}"; pause ;;
    4)
      load_env
      curl -fsSL -X POST "http://127.0.0.1:${FAP_PORT:-2053}${FAP_BASE_PATH:-}/api/xray/restart" >/dev/null 2>&1 || systemctl restart "${SERVICE_NAME}"
      echo -e "${GREEN}Xray Core restarted.${NC}"
      pause
      ;;
    5) journalctl -u "${SERVICE_NAME}" -n 60 --no-pager; pause ;;
    6) systemctl enable "${SERVICE_NAME}" && echo -e "${GREEN}Autostart enabled.${NC}"; pause ;;
    7) systemctl disable "${SERVICE_NAME}" && echo -e "${YELLOW}Autostart disabled.${NC}"; pause ;;
    8) firewall_menu; pause ;;
    9) show_credentials; pause ;;
    10) reset_credentials; pause ;;
    11) change_web_path; pause ;;
    12) change_port; pause ;;
    13) update_geodata; pause ;;
    14) issue_ssl; pause ;;
    15) enable_bbr; pause ;;
    16) postgres_menu; pause ;;
    17) uninstall_panel ;;
    0) exit 0 ;;
    *) echo -e "${RED}Invalid selection.${NC}"; sleep 1 ;;
  esac
done
EOF
  chmod 755 "${CLI_PATH}"
}

clear
echo -e "${CYAN}====================================================${NC}"
echo -e "${BOLD}  FastAsiaPanel Interactive Installer${NC}"
echo -e "${CYAN}====================================================${NC}"

echo ""
echo -e "${BOLD}1) Panel Port Configuration:${NC}"
echo "   1) Choose a custom port manually"
echo "   2) Generate a permanent random port (Recommended)"
read -rp "Select option [1-2, default 2]: " port_choice
if [[ "${port_choice}" == "1" ]]; then
  while true; do
    read -rp "Enter desired port (1-65535): " PANEL_PORT
    if [[ "${PANEL_PORT}" =~ ^[0-9]+$ ]] && (( PANEL_PORT >= 1 && PANEL_PORT <= 65535 )); then
      break
    fi
    echo -e "${RED}Invalid port. Try again.${NC}"
  done
else
  PANEL_PORT=$(gen_rand_port)
  echo -e "${GREEN}Selected permanent random port: ${PANEL_PORT}${NC}"
fi

echo ""
echo -e "${BOLD}2) Database Engine Selection:${NC}"
echo "   1) SQLite (Lightweight, single-file, zero overhead)"
echo "   2) PostgreSQL (High concurrency, automated local setup)"
read -rp "Select database [1-2, default 1]: " db_choice
PG_DSN=""
if [[ "${db_choice}" == "2" ]]; then
  DB_TYPE="postgres"
else
  DB_TYPE="sqlite"
fi

echo ""
echo -e "${BOLD}3) Security & Access Credentials:${NC}"
read -rp "Admin Username [leave empty for random]: " ADMIN_USER
read -rp "Admin Password [leave empty for random]: " ADMIN_PASS
read -rp "Web Base Path [leave empty for random secret path]: " WEB_PATH

[[ -z "${ADMIN_USER}" ]] && ADMIN_USER="admin_$(gen_rand_str 5)"
[[ -z "${ADMIN_PASS}" ]] && ADMIN_PASS="$(gen_rand_str 14)"
if [[ -z "${WEB_PATH}" ]]; then
  WEB_PATH="/$(gen_rand_str 10)"
elif [[ "${WEB_PATH:0:1}" != "/" ]]; then
  WEB_PATH="/${WEB_PATH}"
fi
WEB_PATH="${WEB_PATH%/}"

install_dependencies
install_xray
build_or_fetch_panel

if [[ "${DB_TYPE}" == "postgres" ]]; then
  setup_postgres
fi

DATA_FILE="${DATA_DIR}/fastasiapanel.db"

mkdir -p "${CONF_DIR}" "${DATA_DIR}"
cat <<EOF > "${ENV_FILE}"
FAP_ADDR="0.0.0.0:${PANEL_PORT}"
FAP_LISTEN="0.0.0.0:${PANEL_PORT}"
FAP_PORT="${PANEL_PORT}"
FAP_BASE_PATH="${WEB_PATH}"
FAP_DB="${DB_TYPE}"
FAP_DB_TYPE="${DB_TYPE}"
FAP_DATA="${DATA_FILE}"
FAP_DATA_FILE="${DATA_FILE}"
FAP_PG_DSN="${PG_DSN}"
FAP_PG_URL="${PG_DSN}"
DATABASE_URL="${PG_DSN}"
FAP_XRAY="/usr/local/bin/xray"
FAP_XRAY_BIN="/usr/local/bin/xray"
FAP_ADMIN_USER="${ADMIN_USER}"
FAP_ADMIN_PASS="${ADMIN_PASS}"
EOF
chmod 600 "${ENV_FILE}"

set -a
source "${ENV_FILE}"
set +a
(cd "${DATA_DIR}" && "${BIN_PATH}" reset-admin "${ADMIN_USER}" "${ADMIN_PASS}") >/dev/null 2>&1 || true

cat <<EOF > "${SERVICE_FILE}"
[Unit]
Description=FastAsiaPanel Service
After=network.target postgresql.service

[Service]
Type=simple
WorkingDirectory=${DATA_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now fastasiapanel

if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 || true
fi

write_cli_manager

SERVER_IP=$(curl -4 -fsSL --max-time 4 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')

echo ""
echo -e "${GREEN}====================================================${NC}"
echo -e "${BOLD}  FastAsiaPanel Installation Completed!${NC}"
echo -e "${GREEN}====================================================${NC}"
echo -e "  Panel URL      : ${CYAN}http://${SERVER_IP}:${PANEL_PORT}${WEB_PATH}/${NC}"
echo -e "  Port           : ${YELLOW}${PANEL_PORT}${NC}"
echo -e "  Web Base Path  : ${YELLOW}${WEB_PATH}${NC}"
echo -e "  Database       : ${YELLOW}${DB_TYPE}${NC}"
echo -e "  Username       : ${YELLOW}${ADMIN_USER}${NC}"
echo -e "  Password       : ${YELLOW}${ADMIN_PASS}${NC}"
echo -e "${GREEN}====================================================${NC}"
echo -e "  Run ${BOLD}${CYAN}fastasian${NC} anytime in terminal to open management menu."
echo ""