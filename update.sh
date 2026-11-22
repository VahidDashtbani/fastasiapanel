#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER_SLASH="VahidDashtbani/fastasiapanel"
REPO_URL="https://github.com/${REPO_OWNER_SLASH}.git"
API_COMMIT_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/commits/main"
RELEASES_API_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/latest"

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

# حفظ تنظیمات فعلی (یوزرنیم، پسورد، پورت، base path و دیتابیس)
PREV_DB_TYPE=""
PREV_DB_DSN=""

read_db_from_file() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  local t d
  t=$(grep -E '^(FAP_DB_TYPE|DB_TYPE)=' "$f" 2>/dev/null | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'" || true)
  d=$(grep -E '^(FAP_DB_DSN|DB_DSN)=' "$f" 2>/dev/null | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'" || true)
  [[ -n "$t" && -z "$PREV_DB_TYPE" ]] && PREV_DB_TYPE="$t"
  [[ -n "$d" && -z "$PREV_DB_DSN" ]] && PREV_DB_DSN="$d"
}

read_db_from_file "${ENV_FILE}"
read_db_from_file "/etc/systemd/system/${SERVICE_NAME}"

if [[ "${PREV_DB_DSN}" == postgres://* || "${PREV_DB_DSN}" == postgresql://* ]]; then
  PREV_DB_TYPE="postgres"
elif [[ -z "${PREV_DB_TYPE}" ]] && command -v psql >/dev/null 2>&1 && runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='fastasiapanel'" 2>/dev/null | grep -q 1; then
  PREV_DB_TYPE="postgres"
fi

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

BACKUP_DIR="/var/backups/${APP_NAME}/pre_update_$(date +%Y%m%d_%H%M%S)"
mkdir -p "${BACKUP_DIR}"
[[ -f "${ENV_FILE}" ]] && cp -af "${ENV_FILE}" "${BACKUP_DIR}/"
[[ -f "${CONFIG_DIR}/${APP_NAME}.db" ]] && cp -af "${CONFIG_DIR}/${APP_NAME}.db" "${BACKUP_DIR}/"

ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) ARCH="amd64" ;;
esac

SRC_DIR="$(mktemp -d)"
trap 'rm -rf "${SRC_DIR}"' EXIT

INSTALLED_OK=0

# اولویت ۱: دانلود مستقیم باینری/پکیج آماده از Release گیت‌هاب (بدون نیاز به کامپایل Go)
RELEASE_TAG="$(curl -fsSL -H "Accept: application/vnd.github+json" "${RELEASES_API_URL}" 2>/dev/null \
  | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"

CANDIDATE_URLS=()
if [[ -n "${RELEASE_TAG}" ]]; then
  CANDIDATE_URLS+=(
    "https://github.com/${REPO_OWNER_SLASH}/releases/download/${RELEASE_TAG}/${APP_NAME}-linux-${ARCH}.tar.gz"
    "https://github.com/${REPO_OWNER_SLASH}/releases/download/${RELEASE_TAG}/${APP_NAME}-linux-${ARCH}"
  )
fi
CANDIDATE_URLS+=(
  "https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-${ARCH}.tar.gz"
  "https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-${ARCH}"
)

for url in "${CANDIDATE_URLS[@]}"; do
  log_info "Downloading prebuilt panel from ${url}..."
  if [[ "${url}" == *.tar.gz ]]; then
    if curl -fsSL "${url}" -o "${SRC_DIR}/panel.tar.gz" && tar -xzf "${SRC_DIR}/panel.tar.gz" -C "${SRC_DIR}"; then
      found_bin="$(find "${SRC_DIR}" -maxdepth 2 -type f -name "${APP_NAME}" | head -n1 || true)"
      if [[ -n "${found_bin}" ]]; then
        cp -f "${found_bin}" "${SRC_DIR}/${APP_NAME}"
        INSTALLED_OK=1
        break
      fi
    fi
  else
    if curl -fsSL "${url}" -o "${SRC_DIR}/${APP_NAME}" && [[ -s "${SRC_DIR}/${APP_NAME}" ]]; then
      INSTALLED_OK=1
      break
    fi
  fi
done

# اولویت ۲ (فالبک): اگر پکیج آماده در Release پیدا نشد، فقط در صورت وجود Go باینری بساز
if [[ "${INSTALLED_OK}" -ne 1 ]]; then
  log_warn "Prebuilt release asset not found; trying source build fallback..."
  if command -v go >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    git clone --depth 1 --branch main "${REPO_URL}" "${SRC_DIR}/repo" >/dev/null 2>&1 || true
    if [[ -d "${SRC_DIR}/repo" ]]; then
      if (cd "${SRC_DIR}/repo" && GOTOOLCHAIN=local go build -o "${SRC_DIR}/${APP_NAME}" . 2>/dev/null); then
        INSTALLED_OK=1
      elif (cd "${SRC_DIR}/repo" && go build -o "${SRC_DIR}/${APP_NAME}" . 2>/dev/null); then
        INSTALLED_OK=1
      fi
    fi
  fi
fi

if [[ "${INSTALLED_OK}" -ne 1 || ! -s "${SRC_DIR}/${APP_NAME}" ]]; then
  log_warn "Panel update failed: could not download prebuilt binary or build from source."
  exit 1
fi

systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
install -m 755 "${SRC_DIR}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"

# به‌روزرسانی اسکریپت fastasian از main در صورت دسترسی
curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER_SLASH}/main/scripts/fastasian.sh?v=$(date +%s)" -o "${INSTALL_DIR}/fastasian" 2>/dev/null && chmod 755 "${INSTALL_DIR}/fastasian" || true

# حفظ قطعی تنظیمات دیتابیس قبلی
if [[ -n "${PREV_DB_TYPE}" ]]; then
  mkdir -p "${CONFIG_DIR}"
  touch "${ENV_FILE}"
  python3 - "$ENV_FILE" "$PREV_DB_TYPE" "$PREV_DB_DSN" <<'PYEOF'
import sys
from pathlib import Path

env_path = Path(sys.argv[1])
db_type = sys.argv[2]
db_dsn = sys.argv[3]

lines = env_path.read_text(encoding="utf-8").splitlines() if env_path.exists() else []
keys_to_replace = {"DB_TYPE", "FAP_DB_TYPE", "DB_DSN", "FAP_DB_DSN"}
filtered = [ln for ln in lines if ln.split("=", 1)[0].strip() not in keys_to_replace]

filtered.append(f"DB_TYPE={db_type}")
filtered.append(f"FAP_DB_TYPE={db_type}")
if db_dsn:
    filtered.append(f"DB_DSN={db_dsn}")
    filtered.append(f"FAP_DB_DSN={db_dsn}")

env_path.write_text("\n".join(filtered).rstrip() + "\n", encoding="utf-8")
PYEOF
  log_info "Preserved database configuration: ${PREV_DB_TYPE}"
fi

cat <<STATEEOF > "${STATE_FILE}"
INSTALLED_COMMIT="${REMOTE_SHA}"
UPDATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STATEEOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
systemctl start "${SERVICE_NAME}"

log_ok "FastAsiaPanel updated successfully (${REMOTE_SHA:0:7})."
