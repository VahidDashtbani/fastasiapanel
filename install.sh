#!/usr/bin/env bash
set -euo pipefail

red='\033[0;31m'
green='\033[0;32m'
blue='\033[0;34m'
yellow='\033[0;33m'
plain='\033[0m'

REPO_URL="https://github.com/VahidDashtbani/fastasiapanel.git"
REPO_BRANCH="main"

fap_folder="${FAP_MAIN_FOLDER:-/opt/fastasiapanel}"
fap_src_folder="${FAP_SRC_FOLDER:-/usr/local/src/fastasiapanel}"
fap_data_folder="${FAP_DATA_FOLDER:-/var/lib/fastasiapanel}"
fap_conf_folder="${FAP_CONF_FOLDER:-/etc/fastasiapanel}"
fap_env_file="${fap_conf_folder}/panel.env"
fap_bin_path="${FAP_BIN_PATH:-/usr/local/bin/fastasiapanel}"
fap_cli_path="${FAP_CLI_PATH:-/usr/local/bin/fastasian}"
fap_service_name="fastasiapanel"
fap_service="${FAP_SERVICE:-/etc/systemd/system}"
fap_service_file="${fap_service}/${fap_service_name}.service"

# Check root
[[ $EUID -ne 0 ]] && echo -e "${red}Fatal error:${plain} Please run this script with root privilege" && exit 1

# Fix unknown terminal types like xterm-kitty
if ! command -v infocmp >/dev/null 2>&1 || ! infocmp "${TERM:-}" >/dev/null 2>&1; then
    export TERM=xterm-256color
fi

# Check OS and set release variable
if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    source /etc/os-release
    release="${ID}"
elif [[ -f /usr/lib/os-release ]]; then
    # shellcheck disable=SC1091
    source /usr/lib/os-release
    release="${ID}"
else
    echo "Failed to check the system OS, please contact the author!" >&2
    exit 1
fi
echo "The OS release is: ${release}"

arch() {
    case "$(uname -m)" in
        x86_64 | x64 | amd64) echo 'amd64' ;;
        i*86 | x86) echo '386' ;;
        armv8* | armv8 | arm64 | aarch64) echo 'arm64' ;;
        armv7* | armv7 | arm) echo 'armv7' ;;
        armv6* | armv6) echo 'armv6' ;;
        armv5* | armv5) echo 'armv5' ;;
        s390x) echo 's390x' ;;
        *) echo -e "${red}Unsupported CPU architecture!${plain}" && exit 1 ;;
    esac
}

echo "Arch: $(arch)"

# Non-interactive mode when FAP_NONINTERACTIVE=1 or no TTY available
if [[ "${FAP_NONINTERACTIVE:-0}" == "1" ]] || [[ ! -t 0 && ! -r /dev/tty ]]; then
    NONINTERACTIVE=1
else
    NONINTERACTIVE=0
fi
export NONINTERACTIVE

tty_read() {
    local prompt="$1" outvar="$2"
    if [[ -r /dev/tty ]]; then
        IFS= read -rp "${prompt}" "${outvar}" </dev/tty
    else
        IFS= read -rp "${prompt}" "${outvar}"
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
    local length="$1"
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 $((length * 2)) \
            | tr -dc 'a-zA-Z0-9' \
            | head -c "$length"
    else
        tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "$length"
    fi
}

prompt_or_default() {
    local __var="$1" __prompt="$2" __default="$3" __env="${4:-$1}"
    if [[ "$NONINTERACTIVE" == "1" ]]; then
        printf -v "$__var" '%s' "${!__env:-$__default}"
    else
        tty_read "$__prompt" "$__var"
        if [[ -z "${!__var}" && -n "$__default" ]]; then
            printf -v "$__var" '%s' "$__default"
        fi
    fi
}

install_base() {
    case "${release}" in
        ubuntu | debian | armbian)
            apt-get update && apt-get install -y -q git curl tar tzdata socat ca-certificates openssl build-essential
            ;;
        fedora | amzn | virtuozzo | rhel | almalinux | rocky | ol)
            dnf makecache -y && dnf install -y -q git curl tar tzdata socat ca-certificates openssl gcc make
            ;;
        centos)
            if [[ "${VERSION_ID:-}" =~ ^7 ]]; then
                yum makecache -y && yum install -y git curl tar tzdata socat ca-certificates openssl gcc make
            else
                dnf makecache -y && dnf install -y -q git curl tar tzdata socat ca-certificates openssl gcc make
            fi
            ;;
        arch | manjaro | parch)
            pacman -Sy --noconfirm git curl tar tzdata socat ca-certificates openssl base-devel
            ;;
        opensuse-tumbleweed | opensuse-leap)
            zypper refresh && zypper -q install -y git curl tar timezone socat ca-certificates openssl gcc make
            ;;
        alpine)
            apk update && apk add git curl tar tzdata socat ca-certificates openssl build-base
            ;;
        *)
            apt-get update && apt-get install -y -q git curl tar tzdata socat ca-certificates openssl build-essential
            ;;
    esac

    if ! command -v go >/dev/null 2>&1; then
        local go_ver="1.23.4"
        local go_arch
        go_arch=$(arch)
        case "${go_arch}" in
            amd64 | arm64 | 386) ;;
            *) echo -e "${red}No official Go tarball mapping for ${go_arch}${plain}" && exit 1 ;;
        esac
        echo -e "${green}Installing Go ${go_ver} (${go_arch})...${plain}"
        curl -fLR --retry 3 --connect-timeout 15 "https://go.dev/dl/go${go_ver}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tgz
        rm -f /tmp/go.tgz
    fi
    export PATH="/usr/local/go/bin:${PATH}"
}

install_acme() {
    echo -e "${green}Installing acme.sh for SSL certificate management...${plain}"
    curl -fsSL https://get.acme.sh | sh >/dev/null 2>&1
    if [[ $? -ne 0 ]]; then
        echo -e "${red}Failed to install acme.sh${plain}"
        return 1
    fi
    echo -e "${green}acme.sh installed successfully${plain}"
    return 0
}

setup_ssl_certificate() {
    local domain="$1"
    local web_port="${2:-80}"

    if [[ ! -x "${HOME}/.acme.sh/acme.sh" ]]; then
        install_acme || return 1
    fi

    if is_port_in_use "${web_port}"; then
        echo -e "${red}Port ${web_port} is already in use.${plain}"
        return 1
    fi

    local cert_dir="/root/cert/${domain}"
    mkdir -p "${cert_dir}"
    "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force >/dev/null 2>&1
    if ! "${HOME}/.acme.sh/acme.sh" --issue -d "${domain}" $(acme_listen_flag) --standalone --httpport "${web_port}" --force; then
        echo -e "${red}Failed to issue certificate for ${domain}${plain}"
        rm -rf "${HOME}/.acme.sh/${domain}" "${HOME}/.acme.sh/${domain}_ecc" "${cert_dir}" 2>/dev/null || true
        return 1
    fi

    if ! "${HOME}/.acme.sh/acme.sh" --installcert --force -d "${domain}" \
        --key-file "${cert_dir}/privkey.pem" \
        --fullchain-file "${cert_dir}/fullchain.pem" \
        --reloadcmd "systemctl restart ${fap_service_name}" >/dev/null 2>&1; then
        echo -e "${red}Failed to install certificate for ${domain}${plain}"
        return 1
    fi

    "${HOME}/.acme.sh/acme.sh" --upgrade --auto-upgrade >/dev/null 2>&1 || true
    chmod 600 "${cert_dir}/privkey.pem" 2>/dev/null || true
    chmod 644 "${cert_dir}/fullchain.pem" 2>/dev/null || true
    echo -e "${green}SSL certificate installed at ${cert_dir}/${plain}"
    return 0
}

setup_ip_certificate() {
    local ipv4="$1"
    local web_port="${2:-80}"

    if ! is_ipv4 "${ipv4}"; then
        echo -e "${red}Invalid IPv4 address: ${ipv4}${plain}"
        return 1
    fi

    if [[ ! -x "${HOME}/.acme.sh/acme.sh" ]]; then
        install_acme || return 1
    fi

    if is_port_in_use "${web_port}"; then
        echo -e "${red}Port ${web_port} is already in use.${plain}"
        return 1
    fi

    local cert_dir="/root/cert/ip"
    mkdir -p "${cert_dir}"
    "${HOME}/.acme.sh/acme.sh" --set-default-ca --server letsencrypt --force >/dev/null 2>&1

    if ! "${HOME}/.acme.sh/acme.sh" --issue -d "${ipv4}" $(acme_listen_flag) --standalone \
        --server letsencrypt --certificate-profile shortlived --days 6 --httpport "${web_port}" --force; then
        echo -e "${red}Failed to issue short-lived IP certificate for ${ipv4}${plain}"
        rm -rf "${HOME}/.acme.sh/${ipv4}" "${HOME}/.acme.sh/${ipv4}_ecc" "${cert_dir}" 2>/dev/null || true
        return 1
    fi

    if ! "${HOME}/.acme.sh/acme.sh" --installcert --force -d "${ipv4}" \
        --key-file "${cert_dir}/privkey.pem" \
        --fullchain-file "${cert_dir}/fullchain.pem" \
        --reloadcmd "systemctl restart ${fap_service_name}" >/dev/null 2>&1; then
        echo -e "${red}Failed to install short-lived IP certificate${plain}"
        return 1
    fi

    "${HOME}/.acme.sh/acme.sh" --upgrade --auto-upgrade >/dev/null 2>&1 || true
    chmod 600 "${cert_dir}/privkey.pem" 2>/dev/null || true
    chmod 644 "${cert_dir}/fullchain.pem" 2>/dev/null || true
    echo -e "${green}IP SSL certificate installed at ${cert_dir}/${plain}"
    return 0
}

detect_server_ip() {
    local urls=(
        "https://api4.ipify.org"
        "https://ipv4.icanhazip.com"
        "https://v4.api.ipinfo.io/ip"
        "https://ipv4.myexternalip.com/raw"
        "https://4.ident.me"
    )
    local u ip
    for u in "${urls[@]}"; do
        ip=$(curl -4 -fsSL --max-time 4 "${u}" 2>/dev/null | tr -d '[:space:]')
        if is_ipv4 "${ip}"; then
            echo "${ip}"
            return 0
        fi
    done
    hostname -I 2>/dev/null | awk '{print $1}'
}

sync_and_build_panel() {
    mkdir -p "${fap_folder}" "${fap_data_folder}" "${fap_conf_folder}" "$(dirname "${fap_src_folder}")"

    if [[ -d "${fap_src_folder}/.git" ]]; then
        echo -e "${green}Updating source repository in ${fap_src_folder}...${plain}"
        git -C "${fap_src_folder}" fetch --all --prune
        git -C "${fap_src_folder}" checkout -B "${REPO_BRANCH}" "origin/${REPO_BRANCH}"
        git -C "${fap_src_folder}" reset --hard "origin/${REPO_BRANCH}"
    else
        echo -e "${green}Cloning ${REPO_URL} into ${fap_src_folder}...${plain}"
        rm -rf "${fap_src_folder}"
        git clone --depth 1 -b "${REPO_BRANCH}" "${REPO_URL}" "${fap_src_folder}"
    fi

    echo -e "${green}Building FastAsiaPanel binary...${plain}"
    cd "${fap_src_folder}"
    go mod tidy >/dev/null 2>&1 || true
    go build -o "${fap_bin_path}" .
    chmod 755 "${fap_bin_path}"

    if [[ -f "${fap_src_folder}/scripts/fastasian.sh" ]]; then
        install -m 755 "${fap_src_folder}/scripts/fastasian.sh" "${fap_cli_path}"
        echo -e "${green}Installed CLI manager to ${fap_cli_path}${plain}"
    else
        echo -e "${red}Missing ${fap_src_folder}/scripts/fastasian.sh in repository!${plain}"
        exit 1
    fi
}

config_after_install() {
    local server_ip
    server_ip=$(detect_server_ip || true)

    local config_port config_base_path config_db config_user config_pass config_pg_dsn
    config_port=$(select_panel_port) || exit 1
    config_base_path="/$(gen_random_string 10)"
    config_db="sqlite"
    config_user="admin"
    config_pass="$(gen_random_string 14)"
    config_pg_dsn=""

    if [[ "$NONINTERACTIVE" != "1" ]]; then
        local input_base="" input_db="" input_user="" input_pass=""
        tty_read "Web Base Path [default ${config_base_path}]: " input_base
        if [[ -n "${input_base}" ]]; then
            [[ "${input_base:0:1}" != "/" ]] && input_base="/${input_base}"
            config_base_path="${input_base%/}"
        fi

        tty_read "Database type (sqlite / postgres) [default sqlite]: " input_db
        if [[ "${input_db,,}" == "postgres" || "${input_db,,}" == "postgresql" ]]; then
            config_db="postgres"
            tty_read "PostgreSQL DSN (postgres://user:pass@host:5432/dbname?sslmode=disable): " config_pg_dsn
        fi

        tty_read "Admin username [default ${config_user}]: " input_user
        [[ -n "${input_user}" ]] && config_user="${input_user}"

        tty_read "Admin password [default random]: " input_pass
        [[ -n "${input_pass}" ]] && config_pass="${input_pass}"
    else
        config_port="${FAP_PANEL_PORT:-${config_port}}"
        config_base_path="${FAP_WEB_BASE_PATH:-${config_base_path}}"
        config_db="${FAP_DB_TYPE:-sqlite}"
        config_user="${FAP_USERNAME:-${config_user}}"
        config_pass="${FAP_PASSWORD:-${config_pass}}"
        config_pg_dsn="${FAP_DB_DSN:-}"
    fi

    local prev_umask
    prev_umask=$(umask)
    umask 077
    cat > "${fap_env_file}" <<EOF !="1" " "" "$NONINTERACTIVE" "${dom}" "${fap_env_file}" "${green}SSL "${prev_umask}" "${server_ip}" "${ssl_opt}" "${web_p}"; "Choose "Enter "Standalone && (HTTP (short-lived)" *) -e / 1) 2) 3) 3]: 600 80]: ;; Certificate Domain" EOF Encrypt FAP_ADDR="0.0.0.0:${config_port}" FAP_ADMIN_PASS="${config_pass}" FAP_ADMIN_USER="${config_user}" FAP_BASE_PATH="${config_base_path}" FAP_DATA="${fap_data_folder}/fastasiapanel.db" FAP_DB="${config_db}" FAP_LISTEN="0.0.0.0:${config_port}" FAP_PG_DSN="${config_pg_dsn}" FAP_PORT="${config_port}" HTTP IPv4 Let's SSH SSL Setup:${plain}" Skip [[ [default ]]; an case cat chmod dom domain echo esac fi for if in is_domain is_ipv4 local name: option port proxy reverse setup_ip_certificate setup_ssl_certificate ssl_host="${server_ip}" ssl_opt ssl_scheme="http" then tty_read tunnel)" umask web_p="${web_p:-80}"> "${fap_service_file}" <<EOF "${fap_service_file}" After="network.target" Description="FastAsiaPanel" EOF EnvironmentFile="-${fap_env_file}" ExecStart="${fap_bin_path}" LimitNOFILE="1048576" Management Restart="always" RestartSec="3" Service Type="simple" WantedBy="multi-user.target" WorkingDirectory="${fap_data_folder}" Xray [Install] [Service] [Unit] chown root:root>/dev/null 2>&1 || true
    chmod 644 "${fap_service_file}" >/dev/null 2>&1 || true
    systemctl daemon-reload
    systemctl enable "${fap_service_name}" >/dev/null 2>&1 || true
    systemctl restart "${fap_service_name}"

    local result_file="${fap_conf_folder}/install-result.env"
    prev_umask=$(umask)
    umask 077
    cat > "${result_file}" <<EOF "${config_base_path}" "${config_db}" "${config_pass}" "${config_port}" "${config_user}" "${ssl_scheme}://${ssl_host:-SERVER_IP_UNKNOWN}:${config_port}${config_base_path}/" 'FAP_USERNAME="%q\nFAP_PASSWORD=%q\nFAP_PANEL_PORT=%q\nFAP_WEB_BASE_PATH=%q\nFAP_ACCESS_URL=%q\nFAP_DB_TYPE=%q\n'" EOF FAP_USERNAME="%q\n" \ printf> "${result_file}"
    chmod 600 "${result_file}"
    umask "${prev_umask}"

    echo ""
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}     FastAsiaPanel Installation Complete!    ${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
    echo -e "${green}Username:    ${config_user}${plain}"
    echo -e "${green}Password:    ${config_pass}${plain}"
    echo -e "${green}Port:        ${config_port}${plain}"
    echo -e "${green}WebBasePath: ${config_base_path}${plain}"
    echo -e "${green}Database:    ${config_db}${plain}"
    [[ -n "${ssl_host}" ]] && echo -e "${green}Access URL:  ${ssl_scheme}://${ssl_host}:${config_port}${config_base_path}/${plain}"
    echo -e "${green}CLI Command: fastasian${plain}"
    echo -e "${green}═══════════════════════════════════════════${plain}"
}

install_fastasiapanel() {
    install_base
    sync_and_build_panel
    config_after_install
}

echo -e "${green}Running...${plain}"
install_fastasiapanel