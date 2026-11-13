#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="fastasiapanel"
APP_DIR="/opt/fastasiapanel"
BIN_PATH="${APP_DIR}/fastasiapanel"
SERVICE_PATH="/etc/systemd/system/fastasiapanel.service"
REPO="VahidDashtbani/fastasiapanel"

if [[ "${EUID}" -ne 0 ]]; then
  printf 'Run this installer as root.\n' >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  printf 'systemd is required.\n' >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  printf 'curl is required. Install it and run the installer again.\n' >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l) ARCH="armv7" ;;
  *) printf 'Unsupported architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

if [[ -r /etc/os-release ]]; then
  source /etc/os-release
fi
case "${ID:-}" in
  debian|ubuntu) if ! command -v unzip >/dev/null 2>&1; then apt-get update -q && apt-get install -y -q unzip; fi ;;
  fedora|rhel|rocky|almalinux) if ! command -v unzip >/dev/null 2>&1; then dnf install -y unzip; fi ;;
  alpine) if ! command -v unzip >/dev/null 2>&1; then apk add unzip; fi ;;
esac
mkdir -p "${APP_DIR}/data"
chmod 700 "${APP_DIR}" "${APP_DIR}/data"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
ASSET="https://github.com/${REPO}/releases/latest/download/fastasiapanel-linux-${ARCH}.tar.gz"
curl --fail --location --retry 3 --output "${TMP_DIR}/panel.tar.gz" "${ASSET}"
tar -xzf "${TMP_DIR}/panel.tar.gz" -C "${TMP_DIR}"
test -x "${TMP_DIR}/fastasiapanel"
install -m 0750 "${TMP_DIR}/fastasiapanel" "${BIN_PATH}"
if [[ -d "${TMP_DIR}/web" ]]; then
  install -d -m 0750 "${APP_DIR}/web"
  cp -a "${TMP_DIR}/web/." "${APP_DIR}/web/"
  find "${APP_DIR}/web" -type f -exec chmod 0640 {} \;
else
  printf 'Release archive does not contain web assets.\n' >&2
  exit 1
fi

case "${ARCH}" in
  amd64) XRAY_ARCH="64" ;;
  arm64) XRAY_ARCH="arm64-v8a" ;;
  armv7) XRAY_ARCH="arm32-v7a" ;;
esac
XRAY_URL="https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${XRAY_ARCH}.zip"
curl --fail --location --retry 3 --output "${TMP_DIR}/xray.zip" "${XRAY_URL}"
unzip -oq "${TMP_DIR}/xray.zip" -d "${TMP_DIR}/xray"
install -m 0750 "${TMP_DIR}/xray/xray" "${APP_DIR}/xray"
for asset in geoip.dat geosite.dat; do
  if [[ -f "${TMP_DIR}/xray/${asset}" ]]; then install -m 0640 "${TMP_DIR}/xray/${asset}" "${APP_DIR}/${asset}"; fi
done

if [[ ! -f "${APP_DIR}/panel.env" ]]; then
  PANEL_USER="$(openssl rand -hex 6)"
  PANEL_PASS="$(openssl rand -base64 24 | tr -d '\n')"
  COOKIE_SECRET="$(openssl rand -hex 32)"
  cat > "${APP_DIR}/panel.env" <<ENV
PANEL_ADDR=:2053
PANEL_USERNAME=${PANEL_USER}
PANEL_PASSWORD=${PANEL_PASS}
PANEL_COOKIE_SECRET=${COOKIE_SECRET}
PANEL_SECURE_COOKIE=false
DB_FILE=${APP_DIR}/data/fastasiapanel.json
XRAY_BIN=${APP_DIR}/xray
PANEL_PUBLIC_HOST=
XRAY_CONFIG=${APP_DIR}/data/xray.json
ENV
  chmod 600 "${APP_DIR}/panel.env"
else
  if grep -q '^XRAY_BIN=' "${APP_DIR}/panel.env"; then
    sed -i "s|^XRAY_BIN=.*|XRAY_BIN=${APP_DIR}/xray|" "${APP_DIR}/panel.env"
  else
    printf 'XRAY_BIN=%s/xray\n' "${APP_DIR}" >> "${APP_DIR}/panel.env"
  fi
fi

cat > "${SERVICE_PATH}" <<UNIT
[Unit]
Description=FastAsiaPanel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${APP_DIR}
EnvironmentFile=${APP_DIR}/panel.env
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

systemctl daemon-reload
systemctl enable --now "${APP_NAME}.service"
PANEL_USER="$(sed -n 's/^PANEL_USERNAME=//p' "${APP_DIR}/panel.env" | tail -n1)"
PANEL_PASS="$(sed -n 's/^PANEL_PASSWORD=//p' "${APP_DIR}/panel.env" | tail -n1)"
printf '\nFastAsiaPanel installed.\n'
printf 'Username: %s\n' "${PANEL_USER}"
printf 'Password: %s\n' "${PANEL_PASS}"
printf 'Credentials are also stored in %s/panel.env\n' "${APP_DIR}"
printf 'Service: systemctl status %s\n' "${APP_NAME}"
