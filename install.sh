#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

PANEL_NAME="fastasiapanel"
PANEL_FOLDER="/usr/local/fastasiapanel"
PANEL_SERVICE="/etc/systemd/system/fastasiapanel.service"
PANEL_BIN="${PANEL_FOLDER}/fastasiapanel"
XRAY_BIN="${PANEL_FOLDER}/xray"
GITHUB_REPO="VahidDashtbani/fastasiapanel"

[[ $EUID -ne 0 ]] && echo -e "${red}Fatal: run as root${plain}" && exit 1

if [[ -f /etc/os-release ]]; then
    source /etc/os-release
    release=$ID
else
    echo -e "${red}Cannot detect OS${plain}" && exit 1
fi
echo -e "${green}OS: ${release}${plain}"

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo 'amd64' ;;
        aarch64 | arm64) echo 'arm64' ;;
        armv7* | armv7) echo 'armv7' ;;
        *) echo -e "${red}Unsupported arch${plain}" && exit 1 ;;
    esac
}
ARCH=$(detect_arch)
echo -e "${green}Arch: ${ARCH}${plain}"

is_port_in_use() {
    local port="$1"
    if command -v ss > /dev/null 2>&1; then
        ss -ltn 2> /dev/null | awk -v p=":${port}$" '$4 ~ p {found=1} END {exit !found}'
        return
    fi
    if command -v netstat > /dev/null 2>&1; then
        netstat -lnt 2> /dev/null | awk -v p=":${port}$" '$4 ~ p {found=1} END {exit !found}'
        return
    fi
    return 1
}

normalize_port() {
    local port="$1"
    [[ "$port" =~ ^[0-9]+$ ]] || return 1
    port="${port#"${port%%[!0]*}"}"
    [[ -n "$port" && ${#port} -le 5 ]] || return 1
    ((10#$port <= 65535)) || return 1
    printf '%s\n' "$port"
}

select_panel_port() {
    local confirm port
    read -rp "Customize panel port? (random if no) [y/N]: " confirm
    confirm="${confirm// /}"
    if [[ -z "$confirm" || "$confirm" =~ ^[nN]$ ]]; then
        for _ in $(seq 100); do
            port=$(shuf -i 1024-62000 -n 1)
            if port=$(normalize_port "$port") && ! is_port_in_use "$port"; then
                echo -e "${green}Random port: $port${plain}" >&2
                printf '%s\n' "$port"; return 0
            fi
        done
        echo -e "${red}Could not find free port${plain}" >&2; return 1
    fi
    while true; do
        read -rp "Panel port [1024-65535]: " port
        if ! port=$(normalize_port "$port"); then
            echo -e "${red}Invalid port${plain}" >&2
        elif is_port_in_use "$port"; then
            echo -e "${red}Port $port in use${plain}" >&2
        else
            printf '%s\n' "$port"; return 0
        fi
    done
}

gen_random() {
    local length="$1"
    openssl rand -base64 $((length * 2)) | tr -dc 'a-zA-Z0-9' | head -c "$length"
}

install_deps() {
    case "${release}" in
        ubuntu | debian) apt-get update -q && apt-get install -y -q curl tar unzip openssl ca-certificates ;;
        fedora | rhel | almalinux | rocky) dnf install -y -q curl tar unzip openssl ca-certificates ;;
        centos) yum install -y curl tar unzip openssl ca-certificates ;;
        arch | manjaro) pacman -Sy --noconfirm curl tar unzip openssl ca-certificates ;;
        alpine) apk update && apk add curl tar unzip openssl ca-certificates ;;
        *) apt-get update -q && apt-get install -y -q curl tar unzip openssl ca-certificates ;;
    esac
}

download_panel() {
    echo -e "${green}Downloading FastAsiaPanel...${plain}"
    mkdir -p "${PANEL_FOLDER}"
    local tmp_dir=$(mktemp -d)
    local url="https://github.com/${GITHUB_REPO}/releases/latest/download/fastasiapanel-linux-${ARCH}.tar.gz"
    if ! curl -sL "$url" -o "${tmp_dir}/panel.tar.gz"; then
        echo -e "${red}Failed to download panel from ${url}${plain}" && exit 1
    fi
    tar -xzf "${tmp_dir}/panel.tar.gz" -C "${tmp_dir}"
    mv "${tmp_dir}/fastasiapanel" "${PANEL_BIN}"
    chmod +x "${PANEL_BIN}"
    rm -rf "${tmp_dir}"
    echo -e "${green}Panel binary installed${plain}"
}

download_xray() {
    echo -e "${green}Downloading Xray-core...${plain}"
    local xray_arch
    case "${ARCH}" in
        amd64) xray_arch="64" ;;
        arm64) xray_arch="arm64-v8a" ;;
        armv7) xray_arch="arm32-v7a" ;;
    esac
    local xray_url="https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${xray_arch}.zip"
    local tmp_dir=$(mktemp -d)
    curl -sL "$xray_url" -o "${tmp_dir}/xray.zip"
    unzip -q "${tmp_dir}/xray.zip" -d "${tmp_dir}"
    mv "${tmp_dir}/xray" "${XRAY_BIN}"
    chmod +x "${XRAY_BIN}"
    rm -rf "${tmp_dir}"
    echo -e "${green}Xray-core installed${plain}"
}

create_service() {
    echo -e "${green}Creating systemd service...${plain}"
    cat > "${PANEL_SERVICE}" << EOF
[Unit]
Description=FastAsiaPanel
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${PANEL_FOLDER}
Environment="PANEL_ADDR=:${PANEL_PORT}"
Environment="PANEL_PATH=/${WEB_BASE_PATH}"
Environment="PANEL_USERNAME=${PANEL_USER}"
Environment="PANEL_PASSWORD=${PANEL_PASS}"
Environment="XRAY_BIN=${XRAY_BIN}"
Environment="XRAY_CONFIG=${PANEL_FOLDER}/config.json"
Environment="DB_FILE=${PANEL_FOLDER}/fastasiapanel.db"
ExecStart=${PANEL_BIN}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable fastasiapanel.service
}

create_cli() {
    echo -e "${green}Creating 'fastpanel' command...${plain}"
    cat > /usr/local/bin/fastpanel << 'CLIEOF'
#!/bin/bash
SERVICE="fastasiapanel.service"
FOLDER="/usr/local/fastasiapanel"
CONFIG_FILE="${FOLDER}/config.json"
red='\033[0;31m'; green='\033[0;32m'; yellow='\033[0;33m'; plain='\033[0m'

case "$1" in
    start) systemctl start "$SERVICE"; echo -e "${green}Panel started${plain}" ;;
    stop) systemctl stop "$SERVICE"; echo -e "${green}Panel stopped${plain}" ;;
    restart) systemctl restart "$SERVICE"; echo -e "${green}Panel restarted${plain}" ;;
    status)
        systemctl status "$SERVICE" --no-pager
        echo ""
        if pgrep -f "${FOLDER}/xray" > /dev/null; then
            echo -e "${green}Xray: running${plain}"
        else
            echo -e "${red}Xray: not running${plain}"
        fi
        ;;
    info)
        local port=$(grep -oP 'PANEL_ADDR=:\K[0-9]+' /etc/systemd/system/fastasiapanel.service 2>/dev/null || echo "8080")
        local path=$(grep -oP 'PANEL_PATH=/\K\S+' /etc/systemd/system/fastasiapanel.service 2>/dev/null || echo "")
        local ip=$(curl -s --max-time 3 https://api4.ipify.org 2>/dev/null || echo "SERVER_IP")
        echo -e "${green}=== FastAsiaPanel Info ===${plain}"
        echo -e "Access URL: http://${ip}:${port}/${path}"
        echo -e "Config:     ${CONFIG_FILE}"
        echo -e "Database:   ${FOLDER}/fastasiapanel.db"
        ;;
    log) journalctl -u "$SERVICE" -f ;;
    xray-restart)
        pkill -f "${FOLDER}/xray"
        sleep 1
        systemctl restart "$SERVICE"
        echo -e "${green}Xray restarted${plain}"
        ;;
    uninstall)
        read -rp "Really uninstall FastAsiaPanel? [y/N]: " c
        [[ "$c" =~ ^[yY]$ ]] || exit 0
        systemctl stop "$SERVICE" 2>/dev/null
        systemctl disable "$SERVICE" 2>/dev/null
        rm -f "/etc/systemd/system/${SERVICE}"
        rm -rf "${FOLDER}"
        rm -f /usr/local/bin/fastpanel
        echo -e "${green}Uninstalled${plain}"
        ;;
    *)
        echo -e "${green}FastAsiaPanel management${plain}"
        echo "  fastpanel start          - start panel"
        echo "  fastpanel stop           - stop panel"
        echo "  fastpanel restart        - restart panel"
        echo "  fastpanel status         - show status"
        echo "  fastpanel info           - show access URL"
        echo "  fastpanel log            - follow logs"
        echo "  fastpanel xray-restart   - restart Xray only"
        echo "  fastpanel uninstall      - remove panel"
        ;;
esac
CLIEOF
    chmod +x /usr/local/bin/fastpanel
}

install_panel() {
    install_deps
    echo -e "${green}=== FastAsiaPanel Installer ===${plain}"

    PANEL_PORT=$(select_panel_port) || exit 1
    echo -e "${green}Panel port: ${PANEL_PORT}${plain}"

    WEB_BASE_PATH=$(gen_random 18)
    echo -e "${green}Web path: /${WEB_BASE_PATH}${plain}"

    PANEL_USER=$(gen_random 10)
    PANEL_PASS=$(gen_random 12)
    echo -e "${green}Username: ${PANEL_USER}${plain}"
    echo -e "${green}Password: ${PANEL_PASS}${plain}"

    download_panel
    download_xray
    create_service
    create_cli

    systemctl start fastasiapanel.service
    sleep 2

    local ip=$(curl -s --max-time 3 https://api4.ipify.org 2>/dev/null || echo "YOUR_SERVER_IP")

    echo ""
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}     FastAsiaPanel installed!             ${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}Username:   ${PANEL_USER}${plain}"
    echo -e "${green}Password:   ${PANEL_PASS}${plain}"
    echo -e "${green}Port:       ${PANEL_PORT}${plain}"
    echo -e "${green}Path:       /${WEB_BASE_PATH}${plain}"
    echo -e "${green}Access URL: http://${ip}:${PANEL_PORT}/${WEB_BASE_PATH}${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${yellow}Management: fastpanel start|stop|restart|status|info|log|uninstall${plain}"
}

install_panel