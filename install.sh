#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
plain='\033[0m'

REPO="${FAP_REPO:-VahidDashtbani/fastasiapanel}"
APP_NAME="fastasiapanel"
CLI_NAME="fastasian"

fap_folder="${FAP_MAIN_FOLDER:-/usr/local/fastasiapanel}"
fap_conf_folder="${FAP_CONF_FOLDER:-/etc/fastasiapanel}"
fap_log_folder="${FAP_LOG_FOLDER:-/var/log/fastasiapanel}"
fap_service="${FAP_SERVICE:-/etc/systemd/system}"
fap_service_name="fastasiapanel"

[[ $EUID -ne 0 ]] && echo -e "${red}Fatal error: ${plain} Please run this script with root privilege\n" && exit 1

if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    source /etc/os-release
    release="$ID"
elif [[ -f /usr/lib/os-release ]]; then
    # shellcheck disable=SC1091
    source /usr/lib/os-release
    release="$ID"
else
    echo "Failed to check the system OS, please contact the author!" >&2
    exit 1
fi
echo "The OS release is: $release"

arch() {
    case "$(uname -m)" in
        x86_64 | x64 | amd64) echo 'amd64' ;;
        i*86 | x86) echo '386' ;;
        armv8* | armv8 | arm64 | aarch64) echo 'arm64' ;;
        armv7* | armv7 | arm) echo 'armv7' ;;
        armv6* | armv6) echo 'armv6' ;;
        armv5* | armv5) echo 'armv5' ;;
        s390x) echo 's390x' ;;
        *) echo -e "${red}Unsupported CPU architecture! ${plain}" && exit 1 ;;
    esac
}

echo "Arch: $(arch)"

if [[ "${FAP_NONINTERACTIVE:-0}" == "1" ]] || [[ ! -t 0 && ! -r /dev/tty ]]; then
    NONINTERACTIVE=1
else
    NONINTERACTIVE=0
fi
export NONINTERACTIVE

tty_read() {
    local prompt="$1" varname="$2"
    if [[ -r /dev/tty ]]; then
        IFS= read -rp "$prompt" "$varname" </dev/tty
    else
        IFS= read -rp "$prompt" "$varname"
    fi
}

prompt_or_default() {
    local __var="$1" __prompt="$2" __default="$3" __env="${4:-$1}"
    if [[ "$NONINTERACTIVE" == "1" ]]; then
        printf -v "$__var" '%s' "${!__env:-$__default}"
    else
        tty_read "$__prompt" "$__var"
    fi
}

is_ipv4() {
    [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && return 0 || return 1
}

is_ipv6() {
    [[ "$1" =~ : ]] && return 0 || return 1
}

is_domain() {
    [[ "$1" =~ ^([A-Za-z0-9](-*[A-Za-z0-9])*\.)+(xn--[a-z0-9]{2,}|[A-Za-z]{2,})$ ]] && return 0 || return 1
}

acme_listen_flag() {
    if ip -4 addr show scope global 2>/dev/null | grep -q "inet "; then
        echo ""
    else
        echo "--listen-v6"
    fi
}

is_port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltn 2>/dev/null | awk -v p=":${port}$" '$4 ~ p {found = 1} END {exit !found}'
        return
    fi
    if command -v netstat >/dev/null 2>&1; then
        netstat -lnt 2>/dev/null | awk -v p=":${port}$" '$4 ~ p {found = 1} END {exit !found}'
        return
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1 && return 0
    fi
    return 1
}

normalize_panel_port() {
    local port="$1"
    [[ "$port" =~ ^[0-9]+$ ]] || return 1
    port="${port#"${port%%[!0]*}"}"
    [[ -n "$port" && ${#port} -le 5 ]] || return 1
    ((10#$port >= 1 && 10#$port <= 65535)) || return 1
    printf '%s\n' "$port"
}

select_panel_port() {
    local confirm="n" port="" attempt
    if [[ "$NONINTERACTIVE" == 1 ]]; then
        [[ -z "${FAP_PANEL_PORT+x}" ]] || confirm="y"
    else
        while true; do
            if ! tty_read "Would you like to customize the Panel Port settings? (If not, a random port will be applied) [y/N]: " confirm; then
                echo 'Cannot read the panel port confirmation; aborting.' >&2
                return 1
            fi
            confirm="${confirm#"${confirm%%[![:space:]]*}"}"
            confirm="${confirm%"${confirm##*[![:space:]]}"}"
            case "${confirm,,}" in
                y | yes) confirm="y"; break ;;
                n | no | '') confirm="n"; break ;;
                *) echo 'Please answer yes or no (Enter defaults to No).' >&2 ;;
            esac
        done
    fi

    if [[ "$confirm" == "n" ]]; then
        for ((attempt = 0; attempt < 100; attempt++)); do
            if ! port=$(shuf -i 1024-62000 -n 1); then
                echo 'Failed to generate a random panel port.' >&2
                return 1
            fi
            if port=$(normalize_panel_port "$port") && ! is_port_in_use "$port"; then
                echo "Generated random port: $port" >&2
                printf '%s\n' "$port"
                return 0
            fi
        done
        echo 'Could not find an available random panel port; aborting.' >&2
        return 1
    fi

    while true; do
        if [[ "$NONINTERACTIVE" == 1 ]]; then
            port="${FAP_PANEL_PORT}"
        elif ! tty_read 'Please set up the panel port: ' port; then
            echo 'Cannot read the custom panel port; aborting.' >&2
            return 1
        fi
        if ! port=$(normalize_panel_port "$port"); then
            echo 'Invalid port. Enter a number between 1 and 65535.' >&2
        elif is_port_in_use "$port"; then
            echo "Port $port is already in use. Choose another panel port." >&2
        else
            echo "Your Panel Port is: $port" >&2
            printf '%s\n' "$port"
            return 0
        fi
        [[ "$NONINTERACTIVE" != 1 ]] || return 1
    done
}

gen_random_string() {
    local length="$1" out
    out=$(LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | fold -w "$length" | head -n 1)
    printf '%s' "$out"
}

install_base() {
    case "${release}" in
        ubuntu | debian | armbian)
            apt-get update -y && apt-get install -y -q curl tar ca-certificates openssl socat iproute2
            ;;
        fedora | amzn | virtuozzo | rhel | almalinux | rocky | ol)
            dnf makecache -y && dnf install -y -q curl tar ca-certificates openssl socat iproute
            ;;
        centos)
            if [[ "${VERSION_ID:-}" =~ ^7 ]]; then
                yum makecache -y && yum install -y curl tar ca-certificates openssl socat iproute
            else
                dnf makecache -y && dnf install -y -q curl tar ca-certificates openssl socat iproute
            fi
            ;;
        arch | manjaro | parch)
            pacman -Sy --noconfirm curl tar ca-certificates openssl socat iproute2
            ;;
        opensuse-tumbleweed | opensuse-leap)
            zypper refresh && zypper -q install -y curl tar ca-certificates openssl socat iproute2
            ;;
        alpine)
            apk update && apk add curl tar ca-certificates openssl socat iproute2
            ;;
        *)
            apt-get update -y && apt-get install -y -q curl tar ca-certificates openssl socat iproute2
            ;;
    esac
}

resolve_latest_tag() {
    local url tag
    url=$(curl -sSLI -o /dev/null -w '%{url_effective}' --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 60 \
        "https://github.com/${REPO}/releases/latest" 2>/dev/null)
    tag="${url##*/tag/}"
    if [[ "$tag" != "$url" && -n "$tag" && "$tag" != "latest" ]]; then
        echo "$tag"
        return 0
    fi
    curl -Ls --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 60 \
        "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
}

verify_release_checksum() {
    local url="$1" file="$2" sums="$2.sha256" code expected actual
    rm -f "${sums}"
    code=$(curl -sL --retry 3 --retry-delay 3 --connect-timeout 15 --max-time 60 -o "${sums}" -w '%{http_code}' "${url}.sha256")
    if [[ "${code}" == "404" ]]; then
        rm -f "${sums}"
        echo -e "${yellow}No checksum published for this release, skipping verification${plain}"
        return 0
    fi
    if [[ "${code}" != "200" ]]; then
        rm -f "${sums}" "${file}"
        echo -e "${red}Failed to download checksum (HTTP ${code})${plain}"
        exit 1
    fi
    expected=$(awk 'NR == 1 {print $1}' "${sums}")
    actual=$(sha256sum "${file}" | awk '{print $1}')
    rm -f "${sums}"
    if [[ ! "${expected}" =~ ^[0-9a-f]{64}$ || "${expected}" != "${actual}" ]]; then
        rm -f "${file}"
        echo -e "${red}Checksum mismatch: expected ${expected:-<none>}, got ${actual}${plain}"
        exit 1
    fi
    echo -e "${green}Checksum verified: ${actual}${plain}"
}

detect_server_ip() {
    local urls=(
        "https://api4.ipify.org"
        "https://ipv4.icanhazip.com"
        "https://v4.api.ipinfo.io/ip"
        "https://ipv4.myexternalip.com/raw"
        "https://4.ident.me"
    )
    local u resp code ip
    for u in "${urls[@]}"; do
        resp=$(curl -s -w "\n%{http_code}" --max-time 3 "${u}" 2>/dev/null)
        code=$(echo "$resp" | tail -n1)
        ip=$(echo "$resp" | head -n-1 | tr -d '[:space:]"')
        if [[ "$code" == "200" ]] && is_ipv4 "$ip"; then
            echo "$ip"
            return 0
        fi
    done
    return 1
}

install_acme() {
    echo -e "${green}Installing acme.sh for SSL certificate management...${plain}"
    if curl -fsSL https://get.acme.sh | sh >/dev/null 2>&1; then
        echo -e "${green}acme.sh installed successfully${plain}"
        return 0
    fi
    echo -e "${red}Failed to install acme.sh${plain}"
    return 1
}

setup_domain_ssl() {
    local domain="$1"
    if [[ ! -x "${HOME}/.acme.sh/acme.sh" ]]; then
        install_acme || return 1
    fi

    local web_port="80"
    prompt_or_default web_port "Port to use for ACME HTTP-01 listener (default 80): " "80" FAP_ACME_HTTP_PORT
    web_port="${web_port:-80}"
    if ! [[ "$web_port" =~ ^[1-9][0-9]*$ ]] || ((web_port > 65535)); then
        web_port="80"
    fi

    if is_port_in_use "$web_port"; then
        echo -e "${red}Port ${web_port} is already in use.${plain}"
        return 1
    fi

    local cert_dir="/root/cert/${domain}"
    mkdir -p "$cert_dir"
    "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force >/dev/null 2>&1
    if ! "${HOME}/.acme.sh/acme.sh" --issue -d "$domain" $(acme_listen_flag) --standalone --httpport "$web_port" --force; then
        echo -e "${red}Failed to issue certificate for ${domain}${plain}"
        rm -rf "${HOME}/.acme.sh/${domain}" "${HOME}/.acme.sh/${domain}_ecc" "$cert_dir" 2>/dev/null || true
        return 1
    fi

    if ! "${HOME}/.acme.sh/acme.sh" --installcert --force -d "$domain" \
        --key-file "${cert_dir}/privkey.pem" \
        --fullchain-file "${cert_dir}/fullchain.pem" \
        --reloadcmd "systemctl restart ${fap_service_name}" >/dev/null 2>&1; then
        echo -e "${red}Failed to install certificate for ${domain}${plain}"
        return 1
    fi

    "${HOME}/.acme.sh/acme.sh" --upgrade --auto-upgrade >/dev/null 2>&1 || true
    chmod 600 "${cert_dir}/privkey.pem" 2>/dev/null || true
    chmod 644 "${cert_dir}/fullchain.pem" 2>/dev/null || true
    return 0
}

setup_ip_ssl() {
    local ipv4="$1"
    if ! is_ipv4 "$ipv4"; then
        echo -e "${red}Invalid IPv4 address: ${ipv4}${plain}"
        return 1
    fi
    if [[ ! -x "${HOME}/.acme.sh/acme.sh" ]]; then
        install_acme || return 1
    fi

    local web_port="80"
    prompt_or_default web_port "Port to use for ACME HTTP-01 listener (default 80): " "80" FAP_ACME_HTTP_PORT
    web_port="${web_port:-80}"
    if ! [[ "$web_port" =~ ^[1-9][0-9]*$ ]] || ((web_port > 65535)); then
        web_port="80"
    fi

    if is_port_in_use "$web_port"; then
        echo -e "${red}Port ${web_port} is already in use.${plain}"
        return 1
    fi

    local cert_dir="/root/cert/ip"
    mkdir -p "$cert_dir"
    "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force >/dev/null 2>&1
    if ! "${HOME}/.acme.sh/acme.sh" --issue -d "$ipv4" $(acme_listen_flag) --standalone \
        --server letsencrypt --certificate-profile shortlived --days 6 --httpport "$web_port" --force; then
        echo -e "${red}Failed to issue short-lived IP certificate for ${ipv4}${plain}"
        rm -rf "${HOME}/.acme.sh/${ipv4}" "${HOME}/.acme.sh/${ipv4}_ecc" "$cert_dir" 2>/dev/null || true
        return 1
    fi

    if ! "${HOME}/.acme.sh/acme.sh" --installcert --force -d "$ipv4" \
        --key-file "${cert_dir}/privkey.pem" \
        --fullchain-file "${cert_dir}/fullchain.pem" \
        --reloadcmd "systemctl restart ${fap_service_name}" >/dev/null 2>&1; then
        echo -e "${red}Failed to install short-lived IP certificate${plain}"
        return 1
    fi

    "${HOME}/.acme.sh/acme.sh" --upgrade --auto-upgrade >/dev/null 2>&1 || true
    chmod 600 "${cert_dir}/privkey.pem" 2>/dev/null || true
    chmod 644 "${cert_dir}/fullchain.pem" 2>/dev/null || true
    return 0
}

install_fastasiapanel() {
    local tag_version="${1:-}"
    if [[ -z "$tag_version" ]]; then
        tag_version=$(resolve_latest_tag)
        if [[ -z "$tag_version" ]]; then
            echo -e "${red}Failed to fetch ${APP_NAME} version from ${REPO}${plain}"
            exit 1
        fi
    fi

    echo -e "Got ${APP_NAME} version: ${tag_version}, beginning installation..."
    local tmp_dir archive_path download_url
    tmp_dir=$(mktemp -d) || exit 1
    trap 'rm -rf "$tmp_dir"' EXIT

    archive_path="${tmp_dir}/${APP_NAME}-linux-$(arch).tar.gz"
    download_url="https://github.com/${REPO}/releases/download/${tag_version}/${APP_NAME}-linux-$(arch).tar.gz"

    curl -fLR --retry 5 --retry-delay 3 --connect-timeout 15 -o "$archive_path" "$download_url" || {
        echo -e "${red}Downloading ${APP_NAME} failed: ${download_url}${plain}"
        exit 1
    }
    [[ -s "$archive_path" ]] || { echo -e "${red}Downloaded archive is empty${plain}"; exit 1; }
    verify_release_checksum "$download_url" "$archive_path"

    tar -xzf "$archive_path" -C "$tmp_dir" || {
        echo -e "${red}Failed to extract archive${plain}"
        exit 1
    }
    [[ -s "${tmp_dir}/${APP_NAME}/${APP_NAME}" ]] || {
        echo -e "${red}Archive is missing ${APP_NAME} binary${plain}"
        exit 1
    }

    systemctl stop "${fap_service_name}" >/dev/null 2>&1 || true

    local custom_bin_backup=""
    if [[ -d "${fap_folder}/bin" ]]; then
        custom_bin_backup="${tmp_dir}/bin-backup"
        mv "${fap_folder}/bin" "$custom_bin_backup" 2>/dev/null || true
    fi

    rm -rf "${fap_folder}"
    mv "${tmp_dir}/${APP_NAME}" "${fap_folder}" || exit 1
    chmod +x "${fap_folder}/${APP_NAME}"
    [[ -f "${fap_folder}/bin/xray-linux-$(arch)" ]] && chmod +x "${fap_folder}/bin/xray-linux-$(arch)"
    [[ -f "${fap_folder}/bin/xray" ]] && chmod +x "${fap_folder}/bin/xray"

    if [[ -n "$custom_bin_backup" && -d "$custom_bin_backup" ]]; then
        while IFS= read -r -d '' f; do
            local rel="${f#"${custom_bin_backup}"/}"
            if [[ ! -e "${fap_folder}/bin/${rel}" ]]; then
                mkdir -p "${fap_folder}/bin/$(dirname "${rel}")"
                cp -a "${f}" "${fap_folder}/bin/${rel}"
            fi
        done < <(find "${custom_bin_backup}" \( -type f -o -type l \) -print0)
    fi

    if [[ -f "${fap_folder}/scripts/fastasian.sh" ]]; then
        install -m 755 "${fap_folder}/scripts/fastasian.sh" "/usr/local/bin/${CLI_NAME}"
    fi
    ln -sf "${fap_folder}/${APP_NAME}" "/usr/local/bin/${APP_NAME}"
    mkdir -p "${fap_conf_folder}" "${fap_log_folder}"
    chmod 700 "${fap_conf_folder}"
}

write_service_and_env() {
    local listen_ip="$1" port="$2" base_path="$3" db_type="$4" db_dsn="$5" admin_user="$6" admin_pass="$7" tls_cert="$8" tls_key="$9"
    local env_file="${fap_conf_folder}/${APP_NAME}.env"
    local unit_file="${fap_service}/${fap_service_name}.service"

    local old_umask
    old_umask=$(umask)
    umask 077
    cat > "$env_file" <<EOF
LISTEN=${listen_ip}:${port}
PORT=${port}
BASE_PATH=/${base_path}
DB_TYPE=${db_type}
DB_DSN=${db_dsn}
ADMIN_USER=${admin_user}
ADMIN_PASSWORD=${admin_pass}
TLS_CERT=${tls_cert}
TLS_KEY=${tls_key}
EOF
    chmod 600 "$env_file"
    umask "$old_umask"

    cat > "$unit_file" <<EOF
[Unit]
Description=FastAsiaPanel Management Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${fap_folder}
EnvironmentFile=${env_file}
ExecStart=${fap_folder}/${APP_NAME}
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
EOF
    chmod 644 "$unit_file"
    systemctl daemon-reload
    systemctl enable "${fap_service_name}" >/dev/null 2>&1 || true
    systemctl restart "${fap_service_name}"
}

write_install_result() {
    local u="$1" p="$2" port="$3" wbp="$4" scheme="$5" host="$6" dbtype="$7"
    local result_file="${fap_conf_folder}/install-result.env"
    local old_umask
    old_umask=$(umask)
    umask 077
    {
        printf 'FAP_USERNAME=%q\n' "$u"
        printf 'FAP_PASSWORD=%q\n' "$p"
        printf 'FAP_PANEL_PORT=%q\n' "$port"
        printf 'FAP_WEB_BASE_PATH=%q\n' "$wbp"
        printf 'FAP_ACCESS_URL=%q\n' "${scheme}://${host}:${port}/${wbp}"
        printf 'FAP_DB_TYPE=%q\n' "$dbtype"
    } > "$result_file"
    chmod 600 "$result_file"
    umask "$old_umask"
}

config_after_install() {
    local env_file="${fap_conf_folder}/${APP_NAME}.env"
    local server_ip=""
    server_ip=$(detect_server_ip || true)
    if [[ -z "$server_ip" ]]; then
        if [[ "$NONINTERACTIVE" == "1" ]]; then
            server_ip="${FAP_SERVER_IP:-SERVER_IP}"
        else
            while [[ -z "$server_ip" ]]; do
                tty_read "Please enter your server's public IPv4 address: " server_ip
                server_ip="${server_ip// /}"
                is_ipv4 "$server_ip" || server_ip=""
            done
        fi
    fi

    if [[ -f "$env_file" ]]; then
        echo -e "${yellow}Existing configuration found at ${env_file}; preserving credentials and restarting service.${plain}"
        systemctl restart "${fap_service_name}"
        return 0
    fi

    local config_port config_base_path config_user config_pass db_type="sqlite" db_dsn="" listen_ip="0.0.0.0"
    config_port=$(select_panel_port) || exit 1
    config_base_path="${FAP_WEB_BASE_PATH:-$(gen_random_string 16)}"
    config_base_path="${config_base_path#/}"
    config_base_path="${config_base_path%/}"
    config_user="${FAP_USERNAME:-$(gen_random_string 10)}"
    config_pass="${FAP_PASSWORD:-$(gen_random_string 14)}"

    if [[ "$NONINTERACTIVE" != "1" ]]; then
        echo ""
        echo -e "${green}═══════════════════════════════════════════${plain}"
        echo -e "${green}     Database Selection                    ${plain}"
        echo -e "${green}═══════════════════════════════════════════${plain}"
        echo -e "  1) SQLite     (default)"
        echo -e "  2) PostgreSQL (enter DSN)"
        local db_choice=""
        tty_read "Choose [1]: " db_choice
        db_choice="${db_choice:-1}"
        if [[ "$db_choice" == "2" ]]; then
            db_type="postgres"
            while [[ -z "$db_dsn" ]]; do
                tty_read "Enter PostgreSQL DSN (postgres://user:pass@host:5432/dbname?sslmode=disable): " db_dsn
                db_dsn="${db_dsn// /}"
            done
        fi
    else
        db_type="${FAP_DB_TYPE:-sqlite}"
        db_dsn="${FAP_DB_DSN:-}"
    fi

    local ssl_scheme="http" ssl_host="$server_ip" tls_cert="" tls_key=""
    if [[ -n "${FAP_TLS_CERT:-}" && -n "${FAP_TLS_KEY:-}" ]]; then
        tls_cert="${FAP_TLS_CERT}"
        tls_key="${FAP_TLS_KEY}"
        ssl_scheme="https"
    elif [[ "$NONINTERACTIVE" != "1" ]]; then
        echo ""
        echo -e "${green}═══════════════════════════════════════════${plain}"
        echo -e "${green}     SSL Certificate Setup                 ${plain}"
        echo -e "${green}═══════════════════════════════════════════${plain}"
        echo -e "  1) Let's Encrypt for Domain"
        echo -e "  2) Let's Encrypt for IPv4 (short-lived)"
        echo -e "  3) Custom certificate paths"
        echo -e "  4) Skip SSL (HTTP / reverse proxy / SSH tunnel)"
        local ssl_opt=""
        tty_read "Choose [4]: " ssl_opt
        ssl_opt="${ssl_opt:-4}"
        case "$ssl_opt" in
            1)
                local dom=""
                while [[ -z "$dom" ]]; do
                    tty_read "Enter domain name: " dom
                    dom="${dom// /}"
                    is_domain "$dom" || { echo -e "${red}Invalid domain.${plain}"; dom=""; }
                done
                if setup_domain_ssl "$dom"; then
                    tls_cert="/root/cert/${dom}/fullchain.pem"
                    tls_key="/root/cert/${dom}/privkey.pem"
                    ssl_scheme="https"
                    ssl_host="$dom"
                fi
                ;;
            2)
                if setup_ip_ssl "$server_ip"; then
                    tls_cert="/root/cert/ip/fullchain.pem"
                    tls_key="/root/cert/ip/privkey.pem"
                    ssl_scheme="https"
                    ssl_host="$server_ip"
                fi
                ;;
            3)
                tty_read "Certificate path: " tls_cert
                tty_read "Private key path: " tls_key
                [[ -r "$tls_cert" && -r "$tls_key" ]] && ssl_scheme="https"
                ;;
            *) ;;
        esac
    fi

    write_service_and_env "$listen_ip" "$config_port" "$config_base_path" "$db_type" "$db_dsn" "$config_user" "$config_pass" "$tls_cert" "$tls_key"
    write_install_result "$config_user" "$config_pass" "$config_port" "$config_base_path" "$ssl_scheme" "$ssl_host" "$db_type"

    echo ""
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}     FastAsiaPanel Installation Complete!    ${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}Username:    ${config_user}${plain}"
    echo -e "${green}Password:    ${config_pass}${plain}"
    echo -e "${green}Port:        ${config_port}${plain}"
    echo -e "${green}WebBasePath: /${config_base_path}${plain}"
    echo -e "${green}Database:    ${db_type}${plain}"
    echo -e "${green}Access URL:  ${ssl_scheme}://${ssl_host}:${config_port}/${config_base_path}${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
}

echo -e "${green}Running...${plain}"
install_base
install_fastasiapanel "${1:-}"
config_after_install
