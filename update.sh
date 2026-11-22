#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER_SLASH="VahidDashtbani/fastasiapanel"
REPO_URL="https://github.com/${REPO_OWNER_SLASH}.git"
API_COMMIT_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/commits/main"

APP_NAME="fastasiapanel"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/${APP_NAME}"
ENV_FILE="${CONFIG_DIR}/${APP_NAME}.env"
STATE_FILE="${CONFIG_DIR}/update_state.env"
SERVICE_NAME="${APP_NAME}.service"

log_info() { echo "[INF] $*"; }
log_ok()   { echo "[OK]  $*"; }
log_warn() { echo "[WRN] $*" >&2; }

if [[ "${EUID}" -ne 0 ]]; then
  log_warn "Please run as root."
  exit 1
fi

mkdir -p "${CONFIG_DIR}"

get_remote_commit() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -H "Accept: application/vnd.github+json" "${API_COMMIT_URL}" 2>/dev/null \
      | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -n1
  elif command -v git >/dev/null 2>&1; then
    git ls-remote "${REPO_URL}" refs/heads/main 2>/dev/null | awk '{print $1}' | head -n1
  fi
}

get_local_commit() {
  if [[ -f "${STATE_FILE}" ]]; then
    # shellcheck disable=SC1090
    source "${STATE_FILE}"
    if [[ -n "${INSTALLED_COMMIT:-}" ]]; then
      echo "${INSTALLED_COMMIT}"
      return 0
    fi
  fi
  if command -v git >/dev/null 2>&1 && [[ -d ".git" ]]; then
    git rev-parse HEAD 2>/dev/null || true
  fi
}

REMOTE_SHA="$(get_remote_commit || true)"
LOCAL_SHA="$(get_local_commit || true)"

if [[ -n "${REMOTE_SHA}" && -n "${LOCAL_SHA}" && "${REMOTE_SHA}" == "${LOCAL_SHA}" ]]; then
  log_ok "All up to date (${REMOTE_SHA:0:7})."
  exit 0
fi

# حفظ تنظیمات فعلی (یوزرنیم، پسورد، پورت، base path، دیتابیس)
BACKUP_DIR="/var/backups/${APP_NAME}/pre_update_$(date +%Y%m%d_%H%M%S)"
mkdir -p "${BACKUP_DIR}"
[[ -f "${ENV_FILE}" ]] && cp -af "${ENV_FILE}" "${BACKUP_DIR}/"
[[ -f "${CONFIG_DIR}/${APP_NAME}.db" ]] && cp -af "${CONFIG_DIR}/${APP_NAME}.db" "${BACKUP_DIR}/"

# اطمینان از وجود Go برای بیلد مستقیم از آخرین main
if ! command -v go >/dev/null 2>&1; then
  log_info "Go not found, installing golang..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -y >/dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y golang-go >/dev/null 2>&1
  fi
fi

SRC_DIR="$(mktemp -d)"
trap 'rm -rf "${SRC_DIR}"' EXIT

log_info "Fetching latest source from main..."
git clone --depth 1 --branch main "${REPO_URL}" "${SRC_DIR}/repo" >/dev/null 2>&1

if [[ -z "${REMOTE_SHA}" ]]; then
  REMOTE_SHA="$(git -C "${SRC_DIR}/repo" rev-parse HEAD 2>/dev/null || true)"
fi

log_info "Building ${APP_NAME}..."
BUILT_OK=0
if (
  cd "${SRC_DIR}/repo" && GOTOOLCHAIN=local go build -o "${SRC_DIR}/${APP_NAME}" .
); then
  BUILT_OK=1
elif (
  cd "${SRC_DIR}/repo" && GOPROXY=https://goproxy.cn,https://goproxy.io,direct go build -o "${SRC_DIR}/${APP_NAME}" .
); then
  BUILT_OK=1
fi

if [[ "${BUILT_OK}" -ne 1 ]]; then
  log_warn "Local go build failed (likely toolchain 403). Falling back to GitHub release asset..."
  ASSET_URL="https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-amd64.tar.gz"
  if curl -fsSL "${ASSET_URL}" -o "${SRC_DIR}/panel.tar.gz" && tar -xzf "${SRC_DIR}/panel.tar.gz" -C "${SRC_DIR}"; then
    found_bin="$(find "${SRC_DIR}" -maxdepth 2 -type f -name "${APP_NAME}" | head -n1 || true)"
    if [[ -n "${found_bin}" && "${found_bin}" != "${SRC_DIR}/${APP_NAME}" ]]; then
      cp -f "${found_bin}" "${SRC_DIR}/${APP_NAME}"
    fi
    [[ -f "${SRC_DIR}/${APP_NAME}" ]] && BUILT_OK=1
  fi
fi

if [[ "${BUILT_OK}" -ne 1 ]]; then
  log_warn "Failed to build or fetch ${APP_NAME} binary."
  exit 1
fi

systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
install -m 755 "${SRC_DIR}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"

if [[ -f "${SRC_DIR}/repo/scripts/fastasian.sh" ]]; then
  install -m 755 "${SRC_DIR}/repo/scripts/fastasian.sh" "${INSTALL_DIR}/fastasian"
fi

cat <<STATEEOF > "${STATE_FILE}"
INSTALLED_COMMIT="${REMOTE_SHA}"
UPDATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STATEEOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
systemctl start "${SERVICE_NAME}"

log_ok "Updated ${APP_NAME} to latest main (${REMOTE_SHA:0:7})."
