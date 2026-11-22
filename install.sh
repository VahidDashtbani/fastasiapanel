#!/usr/bin/env bash
set -euo pipefail

APP_NAME="fastasiapanel"
CLI_NAME="fastasian"
REPO="VahidDashtbani/fastasiapanel"

APP_DIR="/usr/local/${APP_NAME}"
CONFIG_DIR="/etc/${APP_NAME}"
ENV_FILE="${CONFIG_DIR}/${APP_NAME}.env"
RESULT_FILE="${CONFIG_DIR}/install-result.env"
BIN_PATH="/usr/local/bin/${APP_NAME}"
CLI_PATH="/usr/local/bin/${CLI_NAME}"
XRAY_BIN="${APP_DIR}/bin/xray"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INF]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WRN]${NC} $*"; }
log_err()  { echo -e "${RED}[ERR]${NC} $*" >&2; }

require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        log_err "Please run this installer as root (use sudo)."
        exit 1
    fi
}

detect_os_and_arch() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        source /etc/os-release
        OS_ID="${ID:-linux}"
    else
        log_err "Cannot detect operating system (/etc/os-release missing)."
        exit 1
    fi

    local raw_arch
    raw_arch="$(uname -m)"
    case "${raw_arch}" in
        x86_64|amd64)
            ARCH="amd64"
            XRAY_ARCH="64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            XRAY_ARCH="arm64-v8a"
            ;;
        *)
            log_err "Unsupported CPU architecture: ${raw_arch}"
            exit 1
            ;;
    esac
}

install_dependencies() {
    log_info "Checking and installing required packages..."
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq
        apt-get install -y -qq curl wget tar unzip socat cron ca-certificates openssl >/dev/null
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y -q curl wget tar unzip socat cronie ca-certificates openssl >/dev/null
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q curl wget tar unzip socat cronie ca-certificates openssl >/dev/null
    else
        log_warn "Unknown package manager; ensure curl, tar, unzip, socat, and openssl are installed."
    fi
}

tty_read() {
    local prompt="$1"
    local var_name="$2"
    local default_val="${3:-}"
    local input=""

    if [[ "${NON_INTERACTIVE:-0}" == "1" ]]; then
        printf -v "${var_name}" '%s' "${default_val}"
        return
    fi

    if [[ -r /dev/tty ]]; then
        printf "%b" "${prompt}" > /dev/tty
        IFS= read -r input < /dev/tty || input=""
    else
        input=""
    fi

    if [[ -z "${input}" ]]; then
        printf -v "${var_name}" '%s' "${default_val}"
    else
        printf -v "${var_name}" '%s' "${input}"
    fi
}

gen_random_string() {
    local len="${1:-12}"
    tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${len}" || true
}

gen_random_port() {
    local port
    while true; do
        port="$(shuf -i 10000-60000 -n 1 2>/dev/null || echo $((10000 + RANDOM % 50000)))"
        if ! ss -tulpn 2>/dev/null | grep -q ":${port} "; then
            echo "${port}"
            return
        fi
    done
}

normalize_web_base_path() {
    local path="${1:-/}"
    [[ "${path}" != /* ]] && path="/${path}"
    if [[ "${#path}" -gt 1 ]]; then
        path="${path%/}"
    fi
    echo "${path}"
}

detect_server_ip() {
    SERVER_IP="$(curl -4 -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || \
                 curl -4 -fsSL --max-time 5 https://ifconfig.me 2>/dev/null || \
                 curl -6 -fsSL --max-time 5 https://api64.ipify.org 2>/dev/null || \
                 hostname -I 2>/dev/null | awk '{print $1}')"
    SERVER_IP="${SERVER_IP:-127.0.0.1}"
}

get_latest_version() {
    if [[ -n "${PANEL_VERSION:-}" ]]; then
        VERSION="${PANEL_VERSION}"
        return
    fi

    local api_url="https://api.github.com/repos/${REPO}/releases/latest"
    VERSION="$(curl -fsSL --max-time 10 "${api_url}" | grep -Po '"tag_name":\s*"\K[^"]+' | head -n 1 || true)"

    if [[ -z "${VERSION}" && -f "cmd/panel/main.go" ]]; then
        VERSION="$(grep -E 'var version\s*=' cmd/panel/main.go | head -n 1 | sed -E 's/.*"([^"]+)".*/\1/' || true)"
    fi

    if [[ -z "${VERSION}" ]]; then
        log_err "Failed to determine release version from GitHub or local source."
        exit 1
    fi
}

install_xray_core() {
    mkdir -p "${APP_DIR}/bin"

    if [[ "${FORCE_XRAY_UPDATE:-0}" != "1" && -x "${XRAY_BIN}" ]] && "${XRAY_BIN}" version >/dev/null 2>&1; then
        log_info "Xray core already installed: $("${XRAY_BIN}" version | head -n 1)"
        return
    fi

    log_info "Installing Xray core (${ARCH} -> ${XRAY_ARCH})..."
    local xray_tag="${XRAY_VERSION:-}"
    if [[ -z "${xray_tag}" ]]; then
        xray_tag="$(curl -fsSL --max-time 10 https://api.github.com/repos/XTLS/Xray-core/releases/latest \
            | grep -Po '"tag_name":\s*"\K[^"]+' | head -n 1 || true)"
    fi

    if [[ -z "${xray_tag}" ]]; then
        log_err "Failed to fetch latest Xray-core release tag from GitHub."
        exit 1
    fi

    local tmp_xray
    tmp_xray="$(mktemp -d)"
    local zip_name="Xray-linux-${XRAY_ARCH}.zip"
    local dl_url="https://github.com/XTLS/Xray-core/releases/download/${xray_tag}/${zip_name}"

    curl -fL --progress-bar "${dl_url}" -o "${tmp_xray}/${zip_name}"
    unzip -q "${tmp_xray}/${zip_name}" -d "${tmp_xray}"

    if [[ ! -f "${tmp_xray}/xray" ]]; then
        rm -rf "${tmp_xray}"
        log_err "Xray archive is missing xray binary."
        exit 1
    fi

    systemctl stop "${APP_NAME}" 2>/dev/null || true
    install -m 0755 "${tmp_xray}/xray" "${XRAY_BIN}"
    [[ -f "${tmp_xray}/geoip.dat" ]] && install -m 0644 "${tmp_xray}/geoip.dat" "${APP_DIR}/bin/geoip.dat"
    [[ -f "${tmp_xray}/geosite.dat" ]] && install -m 0644 "${tmp_xray}/geosite.dat" "${APP_DIR}/bin/geosite.dat"
    rm -rf "${tmp_xray}"

    log_info "Xray core installed: $("${XRAY_BIN}" version | head -n 1)"
}

build_binary_from_source() {
    local clean_ver="${VERSION#v}"
    local ldflags="-s -w -X main.version=${clean_ver}"

    if ! command -v go >/dev/null 2>&1; then
        log_err "Go is required to build from source with ldflags."
        exit 1
    fi

    log_info "Building ${APP_NAME} (${clean_ver}) with ldflags: ${ldflags}"
    local tmp_bin
    tmp_bin="$(mktemp)"
    CGO_ENABLED=0 go build -trimpath -ldflags "${ldflags}" -o "${tmp_bin}" ./cmd/panel

    systemctl stop "${APP_NAME}" 2>/dev/null || true
    mkdir -p "${APP_DIR}/bin" "${CONFIG_DIR}"
    install -m 0755 "${tmp_bin}" "${BIN_PATH}"
    rm -f "${tmp_bin}"

    if [[ -d "scripts" ]]; then
        mkdir -p "${APP_DIR}/scripts"
        cp -rf scripts/. "${APP_DIR}/scripts/"
    fi
}

download_and_install_binary() {
    mkdir -p "${APP_DIR}/bin" "${CONFIG_DIR}"

    if [[ "${BUILD_FROM_SOURCE:-0}" == "1" && -f "go.mod" && -d "cmd/panel" ]]; then
        build_binary_from_source
        return
    fi

    log_info "Got ${APP_NAME} version: ${VERSION}, beginning installation..."
    local tmp_dir
    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "${tmp_dir:-}"' RETURN

    local clean_tag="${VERSION}"
    local asset_name="${APP_NAME}-linux-${ARCH}.tar.gz"
    local base_url="https://github.com/${REPO}/releases/download/${clean_tag}"
    local archive_path="${tmp_dir}/${asset_name}"
    local sha_path="${tmp_dir}/${asset_name}.sha256"

    curl -fL --progress-bar "${base_url}/${asset_name}" -o "${archive_path}"

    if curl -fsSL "${base_url}/${asset_name}.sha256" -o "${sha_path}" 2>/dev/null && [[ -s "${sha_path}" ]]; then
        log_info "Verifying SHA256 checksum..."
        (cd "${tmp_dir}" && sha256sum -c "${asset_name}.sha256")
    else
        log_warn "No checksum published for this release, skipping verification"
    fi

    tar -xzf "${archive_path}" -C "${tmp_dir}"

    local extracted_bin=""
    if [[ -f "${tmp_dir}/${APP_NAME}/${APP_NAME}" ]]; then
        extracted_bin="${tmp_dir}/${APP_NAME}/${APP_NAME}"
    elif [[ -f "${tmp_dir}/${APP_NAME}" ]]; then
        extracted_bin="${tmp_dir}/${APP_NAME}"
    else
        extracted_bin="$(find "${tmp_dir}" -maxdepth 2 -type f -name "${APP_NAME}" | head -n 1 || true)"
    fi

    if [[ -z "${extracted_bin}" || ! -s "${extracted_bin}" ]]; then
        log_err "Archive is missing ${APP_NAME} binary"
        exit 1
    fi

    systemctl stop "${APP_NAME}" 2>/dev/null || true
    install -m 0755 "${extracted_bin}" "${BIN_PATH}"

    if [[ -d "${tmp_dir}/${APP_NAME}/scripts" ]]; then
        mkdir -p "${APP_DIR}/scripts"
        cp -rf "${tmp_dir}/${APP_NAME}/scripts/." "${APP_DIR}/scripts/"
    fi

    trap - RETURN
    rm -rf "${tmp_dir}"
}

install_cli_script() {
    if [[ -f "${APP_DIR}/scripts/fastasian.sh" ]]; then
        install -m 0755 "${APP_DIR}/scripts/fastasian.sh" "${CLI_PATH}"
    elif [[ -f "scripts/fastasian.sh" ]]; then
        install -m 0755 "scripts/fastasian.sh" "${CLI_PATH}"
    elif curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/scripts/fastasian.sh" -o "${CLI_PATH}" 2>/dev/null; then
        chmod 0755 "${CLI_PATH}"
    else
        log_warn "CLI script fastasian.sh not found in release or repo; keeping existing if present."
    fi
}

collect_user_configuration() {
    local default_user default_pass default_port default_path
    default_user="${ADMIN_USER:-$(gen_random_string 10)}"
    default_pass="${ADMIN_PASS:-$(gen_random_string 14)}"
    default_port="${PANEL_PORT:-$(gen_random_port)}"
    default_path="${WEB_BASE_PATH:-/$(gen_random_string 16)}"

    echo ""
    echo -e "${CYAN}--- FastAsiaPanel Configuration ---${NC}"

    tty_read "Enter admin username [default: ${default_user}]: " ADMIN_USER "${default_user}"
    tty_read "Enter admin password [default: ${default_pass}]: " ADMIN_PASS "${default_pass}"
    tty_read "Enter panel port [default: ${default_port}]: " PANEL_PORT "${default_port}"
    tty_read "Enter web base path [default: ${default_path}]: " WEB_BASE_PATH "${default_path}"
    WEB_BASE_PATH="$(normalize_web_base_path "${WEB_BASE_PATH}")"

    local db_choice
    tty_read "Select database (1: SQLite [default], 2: PostgreSQL): " db_choice "${DB_CHOICE:-1}"
    if [[ "${db_choice}" == "2" ]]; then
        local auto_pg=""
        tty_read "Auto-setup PostgreSQL locally without entering DSN? [Y/n]: " auto_pg "y"
        if [[ "${auto_pg,,}" == "y" || "${auto_pg,,}" == "yes" ]]; then
            if ! command -v psql >/dev/null 2>&1 || ! systemctl is-active --quiet postgresql 2>/dev/null; then
                if command -v apt-get >/dev/null 2>&1; then
                    log_info "Installing and starting PostgreSQL..."
                    apt-get update -y >/dev/null 2>&1 || apt-get update -y --allow-insecure-repositories >/dev/null 2>&1 || true
                    DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql postgresql-contrib >/dev/null 2>&1 || true
                fi
                systemctl enable --now postgresql >/dev/null 2>&1 || service postgresql start >/dev/null 2>&1 || true
            fi
            for _ in $(seq 1 15); do
                if command -v pg_isready >/dev/null 2>&1 && pg_isready -h 127.0.0.1 -p 5432 >/dev/null 2>&1; then
                    break
                fi
                sleep 1
            done

            local pg_user="fastasiapanel"
            local pg_db="fastasiapanel"
            local pg_pass
            pg_pass="$(gen_random_string 18)"

            if command -v psql >/dev/null 2>&1; then
                runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "CREATE USER ${pg_user} WITH PASSWORD '${pg_pass}';" >/dev/null 2>&1 || \
                runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "ALTER USER ${pg_user} WITH PASSWORD '${pg_pass}';" >/dev/null 2>&1 || true
                runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${pg_db} OWNER ${pg_user};" >/dev/null 2>&1 || true
                runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "GRANT ALL PRIVILEGES ON DATABASE ${pg_db} TO ${pg_user};" >/dev/null 2>&1 || true
            fi

            DB_TYPE="postgres"
            DB_DSN="postgres://${pg_user}:${pg_pass}@127.0.0.1:5432/${pg_db}?sslmode=disable"
            log_info "PostgreSQL configured automatically (user=${pg_user}, db=${pg_db})."
        else
            DB_TYPE="sqlite"
            DB_DSN="${CONFIG_DIR}/${APP_NAME}.db"
            log_info "Using SQLite (${DB_DSN})."
        fi
    else
        DB_TYPE="sqlite"
        DB_DSN="${CONFIG_DIR}/${APP_NAME}.db"
    fi
}

setup_ssl_certificate() {
    SSL_CERT_PATH=""
    SSL_KEY_PATH=""
    SSL_DOMAIN=""

    local ssl_choice
    echo ""
    echo -e "${CYAN}--- SSL Certificate Setup ---${NC}"
    echo "1) Skip SSL (HTTP only)"
    echo "2) Domain SSL via Let's Encrypt (acme.sh)"
    echo "3) IP Short-Lived SSL Certificate via acme.sh"
    tty_read "Choose SSL option [1-3, default: 1]: " ssl_choice "${SSL_CHOICE:-1}"

    [[ "${ssl_choice}" == "1" ]] && return 0

    local cert_dir="${CONFIG_DIR}/cert"
    mkdir -p "${cert_dir}"

    if [[ ! -f "${HOME}/.acme.sh/acme.sh" ]]; then
        log_info "Installing acme.sh..."
        curl -fsSL https://get.acme.sh | sh -s email="admin@$(hostname -f 2>/dev/null || echo example.com)" >/dev/null 2>&1 || true
    fi

    local acme_bin="${HOME}/.acme.sh/acme.sh"
    if [[ ! -x "${acme_bin}" ]]; then
        log_warn "acme.sh installation failed; continuing with HTTP."
        return 0
    fi

    if [[ "${ssl_choice}" == "2" ]]; then
        tty_read "Enter your domain pointing to ${SERVER_IP}: " SSL_DOMAIN "${SSL_DOMAIN:-}"
        if [[ -z "${SSL_DOMAIN}" ]]; then
            log_warn "No domain entered; skipping SSL."
            return 0
        fi
        log_info "Issuing Let's Encrypt certificate for ${SSL_DOMAIN}..."
        "${acme_bin}" --set-default-ca --server letsencrypt >/dev/null 2>&1 || true
        if "${acme_bin}" --issue -d "${SSL_DOMAIN}" --standalone --httpport 80 --force; then
            "${acme_bin}" --install-cert -d "${SSL_DOMAIN}" \
                --key-file "${cert_dir}/privkey.pem" \
                --fullchain-file "${cert_dir}/fullchain.pem" \
                --reloadcmd "systemctl restart ${APP_NAME}"
            SSL_CERT_PATH="${cert_dir}/fullchain.pem"
            SSL_KEY_PATH="${cert_dir}/privkey.pem"
            log_info "Domain SSL certificate installed successfully."
        else
            log_warn "Failed to issue domain certificate (ensure port 80 is free and DNS points to ${SERVER_IP}). Falling back to HTTP."
        fi
    elif [[ "${ssl_choice}" == "3" ]]; then
        log_info "Issuing short-lived IP certificate for ${SERVER_IP}..."
        "${acme_bin}" --set-default-ca --server letsencrypt >/dev/null 2>&1 || true
        if "${acme_bin}" --issue -d "${SERVER_IP}" --standalone --httpport 80 --certificate-profile shortlived --days 6 --force; then
            "${acme_bin}" --install-cert -d "${SERVER_IP}" \
                --key-file "${cert_dir}/privkey.pem" \
                --fullchain-file "${cert_dir}/fullchain.pem" \
                --reloadcmd "systemctl restart ${APP_NAME}"
            SSL_CERT_PATH="${cert_dir}/fullchain.pem"
            SSL_KEY_PATH="${cert_dir}/privkey.pem"
            log_info "IP SSL certificate installed successfully."
        else
            log_warn "Failed to issue IP certificate (ensure port 80 is free). Falling back to HTTP."
        fi
    fi
}

apply_panel_settings() {
    mkdir -p "${CONFIG_DIR}"
    WEB_BASE_PATH="$(normalize_web_base_path "${WEB_BASE_PATH}")"

    local ssl_enabled="false"
    if [[ -n "${SSL_CERT_PATH:-}" && -n "${SSL_KEY_PATH:-}" && -s "${SSL_CERT_PATH}" && -s "${SSL_KEY_PATH}" ]]; then
        ssl_enabled="true"
    else
        SSL_CERT_PATH=""
        SSL_KEY_PATH=""
    fi

    local go_base_path=""
    if [[ "${WEB_BASE_PATH}" != "/" ]]; then
        go_base_path="${WEB_BASE_PATH}"
    fi

    local db_type_val="sqlite"
    local db_path_val="${CONFIG_DIR}/fastasiapanel.db"
    local db_dsn_val=""
    if [[ "${DB_TYPE:-sqlite}" == "postgres" && -n "${DB_DSN:-}" ]]; then
        db_type_val="postgres"
        db_dsn_val="${DB_DSN}"
    elif [[ "${DB_TYPE:-}" == "sqlite" || "${DB_TYPE:-}" == "json" ]]; then
        db_type_val="${DB_TYPE}"
        db_path_val="${DB_DSN:-${CONFIG_DIR}/fastasiapanel.db}"
    fi

    local public_host_val="${SERVER_IP}"
    if [[ -n "${SSL_DOMAIN:-}" ]]; then
        public_host_val="${SSL_DOMAIN}"
    fi

    cat > "${ENV_FILE}" <<EOF
FAP_LISTEN=0.0.0.0:${PANEL_PORT}
FAP_BASE_PATH=${go_base_path}
FAP_DB_TYPE=${db_type_val}
FAP_DB_PATH=${db_path_val}
FAP_DB_DSN=${db_dsn_val}
FAP_XRAY_BIN=${XRAY_BIN}
FAP_XRAY_CONFIG=${CONFIG_DIR}/xray.json
FAP_PUBLIC_HOST=${public_host_val}
FAP_TLS_CERT=${SSL_CERT_PATH}
FAP_TLS_KEY=${SSL_KEY_PATH}
FAP_ADMIN_USER=${ADMIN_USER}
FAP_ADMIN_PASSWORD=${ADMIN_PASS}
FAP_SECURE_COOKIES=${ssl_enabled}
EOF
    chmod 600 "${ENV_FILE}"

    set -a
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
    set +a

    if [[ -x "${BIN_PATH}" ]]; then
        "${BIN_PATH}" reset-admin "${ADMIN_USER}" "${ADMIN_PASS}" >/dev/null 2>&1 || true
    fi
}

configure_firewall_port() {
    local port="$1"
    if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
        ufw allow "${port}/tcp" >/dev/null 2>&1 || true
        ufw allow "${port}/udp" >/dev/null 2>&1 || true
        log_info "Opened port ${port} in UFW."
    elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
        firewall-cmd --permanent --add-port="${port}/tcp" >/dev/null 2>&1 || true
        firewall-cmd --permanent --add-port="${port}/udp" >/dev/null 2>&1 || true
        firewall-cmd --reload >/dev/null 2>&1 || true
        log_info "Opened port ${port} in firewalld."
    fi
}

setup_systemd_service() {
    cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=FastAsiaPanel Xray Management Web Panel
After=network.target nss-lookup.target

[Service]
Type=simple
WorkingDirectory=${APP_DIR}
EnvironmentFile=-${ENV_FILE}
ExecStart=${BIN_PATH}
Restart=on-failure
RestartSec=3s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "${APP_NAME}" >/dev/null 2>&1
    systemctl restart "${APP_NAME}"
}

print_install_summary() {
    local proto="http"
    local access_host="${SERVER_IP}"

    if [[ -n "${SSL_CERT_PATH:-}" && -n "${SSL_KEY_PATH:-}" && -s "${SSL_CERT_PATH}" && -s "${SSL_KEY_PATH}" ]]; then
        proto="https"
        if [[ -n "${SSL_DOMAIN:-}" ]]; then
            access_host="${SSL_DOMAIN}"
        fi
    fi

    local url_host="${access_host}"
    if [[ "${url_host}" == *:* && "${url_host}" != \[*\] ]]; then
        url_host="[${url_host}]"
    fi

    local clean_path=""
    if [[ "${WEB_BASE_PATH}" != "/" ]]; then
        clean_path="${WEB_BASE_PATH}"
    fi
    ACCESS_URL="${proto}://${url_host}:${PANEL_PORT}${clean_path}"

    cat > "${RESULT_FILE}" <<EOF
PANEL_VERSION=${VERSION}
PANEL_USERNAME=${ADMIN_USER}
PANEL_PASSWORD=${ADMIN_PASS}
PANEL_PORT=${PANEL_PORT}
PANEL_WEB_BASE_PATH=${WEB_BASE_PATH}
DB_TYPE=${DB_TYPE}
XRAY_BIN=${XRAY_BIN}
SSL_ENABLED=$( [[ "${proto}" == "https" ]] && echo "true" || echo "false" )
ACCESS_URL=${ACCESS_URL}
EOF
    chmod 600 "${RESULT_FILE}"

    echo ""
    echo "═══════════════════════════════════════════"
    echo "     FastAsiaPanel Installation Complete!    "
    echo "═══════════════════════════════════════════"
    echo "Version:     ${VERSION}"
    echo "Username:    ${ADMIN_USER}"
    echo "Password:    ${ADMIN_PASS}"
    echo "Port:        ${PANEL_PORT}"
    echo "WebBasePath: ${WEB_BASE_PATH}"
    echo "Database:    ${DB_TYPE}"
    echo "Xray Core:   ${XRAY_BIN}"
    echo "SSL Mode:    ${proto^^}"
    echo "Access URL:  ${ACCESS_URL}"
    echo "═══════════════════════════════════════════"
}

main() {
    require_root
    detect_os_and_arch
    install_dependencies
    detect_server_ip
    get_latest_version
    install_xray_core
    download_and_install_binary
    install_cli_script
    collect_user_configuration
    setup_ssl_certificate
    apply_panel_settings
    configure_firewall_port "${PANEL_PORT}"
    setup_systemd_service
    print_install_summary
}

main "$@"