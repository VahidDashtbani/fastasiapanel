#!/bin/bash
# Usage:
#   bash install.sh            # latest release
#   bash install.sh v1.0.0     # specific tag
#
# Optional env vars (also work with `curl ... | bash`):
#   FAP_REPO            GitHub "owner/repo"         (REQUIRED: set below or via env)
#   FAP_PORT            panel port                  (default: random free port)
#   FAP_USERNAME        admin username              (default: random)
#   FAP_PASSWORD        admin password              (default: random)
#   FAP_BASE_PATH       web base path               (default: random)
#   FAP_TLS_CERT / FAP_TLS_KEY   paths to an existing certificate (enables HTTPS)

set -uo pipefail

# ---------- ADJUST THESE TO MATCH YOUR PROJECT ----------
REPO="${FAP_REPO:-VahidDashtbani/fastasiapanel}"
APP_NAME="fastasiapanel"
# Release asset: ${APP_NAME}-linux-<arch>.tar.gz, which must contain a folder
# "${APP_NAME}/" with the binary "${APP_NAME}" and "bin/xray" inside.
# Env var names below must match what internal/config.Load() reads.
ENV_LISTEN="LISTEN"; ENV_BASEPATH="BASE_PATH"; ENV_DBTYPE="DB_TYPE"
ENV_ADMIN_USER="ADMIN_USER"; ENV_ADMIN_PASS="ADMIN_PASSWORD"
ENV_TLS_CERT="TLS_CERT"; ENV_TLS_KEY="TLS_KEY"; ENV_SECURE="SECURE_COOKIES"
# --------------------------------------------------------

red='\033[0;31m'; green='\033[0;32m'; yellow='\033[0;33m'; plain='\033[0m'

INSTALL_DIR="/usr/local/${APP_NAME}"
DATA_DIR="/etc/${APP_NAME}"
ENV_FILE="${DATA_DIR}/${APP_NAME}.env"
RESULT_FILE="${DATA_DIR}/install-result.env"
UNIT_FILE="/etc/systemd/system/${APP_NAME}.service"

die() { echo -e "${red}$*${plain}" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "Please run as root."
command -v systemctl > /dev/null 2>&1 || die "systemd is required."

# ---------- OS / arch ----------
[[ -f /etc/os-release ]] && . /etc/os-release || die "Cannot detect OS."
release="${ID:-unknown}"

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    armv7* | armv7l) arch=armv7 ;;
    s390x) arch=s390x ;;
    *) die "Unsupported CPU architecture: $(uname -m)" ;;
esac
echo "OS: ${release}, arch: ${arch}"

# ---------- helpers ----------
gen_random_string() {
    local n="$1"
    openssl rand -base64 $((n * 3)) | tr -dc 'a-zA-Z0-9' | head -c "$n"
}

is_port_in_use() {
    ss -ltn 2> /dev/null | awk -v p=":$1\$" '$4 ~ p {f=1} END {exit !f}'
}

pick_port() {
    local p i
    if [[ -n "${FAP_PORT:-}" ]]; then
        [[ "$FAP_PORT" =~ ^[0-9]+$ ]] && ((FAP_PORT >= 1 && FAP_PORT <= 65535)) \
            || die "Invalid FAP_PORT: $FAP_PORT"
        is_port_in_use "$FAP_PORT" && die "Port $FAP_PORT is already in use."
        echo "$FAP_PORT"; return
    fi
    for ((i = 0; i < 100; i++)); do
        p=$(shuf -i 20000-62000 -n 1)
        is_port_in_use "$p" || { echo "$p"; return; }
    done
    die "Could not find a free port."
}

install_base() {
    case "$release" in
        ubuntu | debian | armbian) apt-get update -y && apt-get install -y -q curl tar ca-certificates openssl iproute2 ;;
        fedora | rhel | almalinux | rocky | ol | amzn) dnf install -y -q curl tar ca-certificates openssl iproute ;;
        centos) (dnf install -y -q curl tar ca-certificates openssl iproute || yum install -y curl tar ca-certificates openssl iproute) ;;
        arch | manjaro) pacman -Sy --noconfirm curl tar ca-certificates openssl iproute2 ;;
        opensuse*) zypper -q install -y curl tar ca-certificates openssl iproute2 ;;
        *) apt-get update -y && apt-get install -y -q curl tar ca-certificates openssl iproute2 ;;
    esac || die "Failed to install base packages."
}

resolve_latest_tag() {
    local url tag
    url=$(curl -sSLI -o /dev/null -w '%{url_effective}' --retry 3 --max-time 60 \
        "https://github.com/${REPO}/releases/latest" 2> /dev/null)
    tag="${url##*/tag/}"
    [[ "$tag" != "$url" && -n "$tag" ]] && echo "$tag"
}

verify_checksum() {
    local url="$1" file="$2" code expected actual
    code=$(curl -sL --retry 3 --max-time 60 -o "${file}.sha256" -w '%{http_code}' "${url}.sha256")
    if [[ "$code" == "404" ]]; then
        rm -f "${file}.sha256"
        echo -e "${yellow}No checksum published, skipping verification.${plain}"
        return 0
    fi
    [[ "$code" == "200" ]] || { rm -f "${file}" "${file}.sha256"; die "Checksum download failed (HTTP $code)."; }
    expected=$(awk 'NR==1 {print $1}' "${file}.sha256")
    actual=$(sha256sum "$file" | awk '{print $1}')
    rm -f "${file}.sha256"
    [[ "$expected" == "$actual" ]] || { rm -f "$file"; die "Checksum mismatch."; }
    echo -e "${green}Checksum verified.${plain}"
}

get_public_ip() {
    local u ip
    for u in https://api4.ipify.org https://ipv4.icanhazip.com https://4.ident.me; do
        ip=$(curl -s --max-time 3 "$u" | tr -d '[:space:]')
        [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && { echo "$ip"; return; }
    done
    echo "SERVER_IP"
}

# ---------- install ----------
install_panel() {
    local tag="${1:-}"
    [[ -n "$tag" ]] || tag=$(resolve_latest_tag)
    [[ -n "$tag" ]] || die "Could not determine the latest version (is FAP_REPO='${REPO}' correct?)"
    echo "Installing ${APP_NAME} ${tag}..."

    local tmp archive url
    tmp=$(mktemp -d) || die "mktemp failed."
    trap 'rm -rf "$tmp"' EXIT
    archive="${tmp}/${APP_NAME}.tar.gz"
    url="https://github.com/${REPO}/releases/download/${tag}/${APP_NAME}-linux-${arch}.tar.gz"

    curl -fLR --retry 5 --retry-delay 3 --connect-timeout 15 -o "$archive" "$url" \
        || die "Download failed: $url"
    [[ -s "$archive" ]] || die "Downloaded archive is empty."
    verify_checksum "$url" "$archive"

    # Extract BEFORE touching the running install, so a bad archive never
    # leaves the panel removed.
    tar -xzf "$archive" -C "$tmp" || die "Failed to extract archive."
    [[ -s "${tmp}/${APP_NAME}/${APP_NAME}" ]] || die "Archive is missing the ${APP_NAME} binary."

    systemctl stop "$APP_NAME" 2> /dev/null || true

    # Keep admin-placed files from bin/ (custom geo files etc.)
    local bin_backup=""
    if [[ -d "${INSTALL_DIR}/bin" ]]; then
        bin_backup="${tmp}/bin-backup"
        cp -a "${INSTALL_DIR}/bin" "$bin_backup"
    fi

    rm -rf "$INSTALL_DIR"
    mv "${tmp}/${APP_NAME}" "$INSTALL_DIR" || die "Failed to move files into ${INSTALL_DIR}."
    chmod +x "${INSTALL_DIR}/${APP_NAME}"
    [[ -f "${INSTALL_DIR}/bin/xray-linux-${arch}" ]] && chmod +x "${INSTALL_DIR}/bin/xray-linux-${arch}"

    if [[ -n "$bin_backup" ]]; then
        # restore only files the new release does not ship
        (cd "$bin_backup" && find . -type f -print0) | while IFS= read -r -d '' f; do
            if [[ ! -e "${INSTALL_DIR}/bin/${f}" ]]; then
                mkdir -p "${INSTALL_DIR}/bin/$(dirname "$f")"
                cp -a "${bin_backup}/${f}" "${INSTALL_DIR}/bin/${f}"
            fi
        done
    fi

    ln -sf "${INSTALL_DIR}/${APP_NAME}" "/usr/local/bin/${APP_NAME}"
    mkdir -p "$DATA_DIR" /var/log/"$APP_NAME"
    chmod 700 "$DATA_DIR"
}

# ---------- config (only on first install; updates keep existing credentials) ----------
write_config() {
    local user pass port base scheme="http" secure=false host
    user="${FAP_USERNAME:-$(gen_random_string 10)}"
    pass="${FAP_PASSWORD:-$(gen_random_string 14)}"
    base="${FAP_BASE_PATH:-$(gen_random_string 16)}"
    port=$(pick_port)

    # umask so the env file is never world-readable, even briefly
    local old_umask; old_umask=$(umask); umask 077
    {
        echo "${ENV_LISTEN}=:${port}"
        echo "${ENV_BASEPATH}=/${base}"
        echo "${ENV_DBTYPE}=sqlite"
        echo "${ENV_ADMIN_USER}=${user}"
        echo "${ENV_ADMIN_PASS}=${pass}"
        if [[ -n "${FAP_TLS_CERT:-}" && -n "${FAP_TLS_KEY:-}" ]]; then
            [[ -r "$FAP_TLS_CERT" && -r "$FAP_TLS_KEY" ]] || { umask "$old_umask"; die "TLS cert/key not readable."; }
            echo "${ENV_TLS_CERT}=${FAP_TLS_CERT}"
            echo "${ENV_TLS_KEY}=${FAP_TLS_KEY}"
            scheme="https"; secure=true
        fi
        echo "${ENV_SECURE}=${secure}"
    } > "$ENV_FILE"
    umask "$old_umask"
    chmod 600 "$ENV_FILE"

    host=$(get_public_ip)
    {
        printf 'USERNAME=%q\nPASSWORD=%q\nPORT=%q\nBASE_PATH=%q\nURL=%q\n' \
            "$user" "$pass" "$port" "$base" "${scheme}://${host}:${port}/${base}"
    } > "$RESULT_FILE"
    chmod 600 "$RESULT_FILE"

    CFG_USER="$user"; CFG_PASS="$pass"; CFG_PORT="$port"; CFG_BASE="$base"
    CFG_URL="${scheme}://${host}:${port}/${base}"
    FRESH_INSTALL=1
}

install_service() {
    cat > "$UNIT_FILE" << EOF
[Unit]
Description=FastAsiaPanel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/${APP_NAME}
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535
# Give Go's graceful shutdown + xray stop time to finish
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
EOF
    chmod 644 "$UNIT_FILE"
    systemctl daemon-reload
    systemctl enable "$APP_NAME" > /dev/null 2>&1
    systemctl restart "$APP_NAME"
}

verify_running() {
    local i
    for i in 1 2 3 4 5 6 7 8 9 10; do
        systemctl is-active --quiet "$APP_NAME" && { sleep 2; systemctl is-active --quiet "$APP_NAME" && return 0; }
        sleep 1
    done
    echo -e "${red}Service failed to stay running. Last logs:${plain}"
    journalctl -u "$APP_NAME" -n 30 --no-pager 2> /dev/null
    return 1
}

# ---------- main ----------
FRESH_INSTALL=0
install_base
install_panel "${1:-}"

if [[ -f "$ENV_FILE" ]]; then
    echo -e "${yellow}Existing config found at ${ENV_FILE}; keeping current settings and credentials.${plain}"
else
    write_config
fi

install_service
verify_running || exit 1

echo ""
echo -e "${green}${APP_NAME} installed and running.${plain}"
if [[ "$FRESH_INSTALL" == 1 ]]; then
    echo -e "${green}════════════════════════════════════════${plain}"
    echo -e "${green}Username:   ${CFG_USER}${plain}"
    echo -e "${green}Password:   ${CFG_PASS}${plain}"
    echo -e "${green}Port:       ${CFG_PORT}${plain}"
    echo -e "${green}Base path:  /${CFG_BASE}${plain}"
    echo -e "${green}URL:        ${CFG_URL}${plain}"
    echo -e "${green}════════════════════════════════════════${plain}"
    echo -e "${yellow}Saved to ${RESULT_FILE} (root only). Save the password now.${plain}"
    [[ "$CFG_URL" == http://* ]] && echo -e "${yellow}Panel is plain HTTP. Put it behind a reverse proxy / SSH tunnel, or re-run with FAP_TLS_CERT and FAP_TLS_KEY.${plain}"
fi
echo ""
echo "Useful commands:"
echo "  systemctl status ${APP_NAME}"
echo "  journalctl -u ${APP_NAME} -f"
echo "  ${APP_NAME} reset-admin <user> <pass>   # needs the env vars: set -a; . ${ENV_FILE}; set +a"
