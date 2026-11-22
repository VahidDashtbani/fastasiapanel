#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER_SLASH="VahidDashtbani/fastasiapanel"
REPO_URL="https://github.com/${REPO_OWNER_SLASH}.git"
API_COMMIT_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/commits/main"
API_LATEST_REL="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/latest"

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

# ۱. حفظ تنظیمات دیتابیس فعلی (خصوصاً PostgreSQL)
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

REMOTE_SHA="$(curl -fsSL -H "Accept: application/vnd.github+json" "${API_COMMIT_URL}" 2>/dev/null \
  | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -n1 || true)"

REL_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "${API_LATEST_REL}" 2>/dev/null || true)"
REL_TAG="$(printf '%s' "${REL_JSON}" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"

LOCAL_SHA=""
LOCAL_TAG=""
if [[ -f "${STATE_FILE}" ]]; then
  # shellcheck disable=SC1090
  source "${STATE_FILE}"
  LOCAL_SHA="${INSTALLED_COMMIT:-}"
  LOCAL_TAG="${INSTALLED_TAG:-}"
fi

# اگر هم SHA آخرین کامیت و هم Tag ریلیز دقیقاً مثل قبل باشند، All up to date بزن
if [[ -n "${REMOTE_SHA}" && -n "${LOCAL_SHA}" && "${REMOTE_SHA}" == "${LOCAL_SHA}" ]]; then
  if [[ -z "${REL_TAG}" || "${REL_TAG}" == "${LOCAL_TAG}" ]]; then
    log_ok "All up to date (${REL_TAG:-${REMOTE_SHA:0:7}})."
    exit 0
  fi
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

# ۲. کشف خودکار asset های باینری/آرشیو از ریلیز گیت‌هاب
ASSET_URLS=()
if [[ -n "${REL_JSON}" ]]; then
  while IFS= read -r u; do
    [[ -n "$u" ]] && ASSET_URLS+=("$u")
  done < <(printf '%s' "${REL_JSON}" \
    | grep -oE 'https://github\.com/[^"]+/releases/download/[^"]+' \
    | grep -iE "(linux.*(${ARCH}|x86_64|aarch64)|${APP_NAME}-${ARCH}|${APP_NAME}-linux)" \
    | sort -u || true)
fi

if [[ -n "${REL_TAG}" ]]; then
  ASSET_URLS+=(
    "https://github.com/${REPO_OWNER_SLASH}/releases/download/${REL_TAG}/${APP_NAME}-linux-${ARCH}.tar.gz"
    "https://github.com/${REPO_OWNER_SLASH}/releases/download/${REL_TAG}/${APP_NAME}-linux-${ARCH}"
  )
fi
ASSET_URLS+=(
  "https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-${ARCH}.tar.gz"
  "https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-${ARCH}"
)

for url in "${ASSET_URLS[@]}"; do
  log_info "Trying release binary: ${url}"
  if [[ "${url}" == *.tar.gz ]]; then
    if curl -fsSL "${url}" -o "${SRC_DIR}/rel.tar.gz" 2>/dev/null && tar -xzf "${SRC_DIR}/rel.tar.gz" -C "${SRC_DIR}" 2>/dev/null; then
      found_bin="$(find "${SRC_DIR}" -maxdepth 3 -type f -name "${APP_NAME}" | head -n1 || true)"
      if [[ -n "${found_bin}" && -s "${found_bin}" ]]; then
        cp -f "${found_bin}" "${SRC_DIR}/${APP_NAME}"
        INSTALLED_OK=1
        break
      fi
    fi
  else
    if curl -fsSL "${url}" -o "${SRC_DIR}/${APP_NAME}" 2>/dev/null && [[ -s "${SRC_DIR}/${APP_NAME}" ]]; then
      INSTALLED_OK=1
      break
    fi
  fi
done

# ۳. فالبک به بیلد از سورس فقط اگر باینری آماده در Release نبود
if [[ "${INSTALLED_OK}" -ne 1 ]]; then
  log_warn "No prebuilt binary found in GitHub Release; trying source build..."
  if command -v go >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    if git clone --depth 1 --branch main "${REPO_URL}" "${SRC_DIR}/repo" >/dev/null 2>&1; then
      if (cd "${SRC_DIR}/repo" && GOTOOLCHAIN=local go build --buildvcs=false -o "${SRC_DIR}/${APP_NAME}" . 2>/dev/null); then
        INSTALLED_OK=1
      fi
    fi
  fi
fi

if [[ "${INSTALLED_OK}" -ne 1 || ! -s "${SRC_DIR}/${APP_NAME}" ]]; then
  log_warn "Update failed: no release binary available and local Go version could not build."
  log_warn "Please attach fastasiapanel-linux-${ARCH}.tar.gz to the GitHub Release (${REL_TAG:-latest})."
  exit 1
fi

# ۴. توقف سرویس، جایگزینی باینری و اسکریپت CLI، حفظ DB و استارت مجدد
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
install -m 755 "${SRC_DIR}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"

curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER_SLASH}/main/scripts/fastasian.sh?v=$(date +%s)" -o "${INSTALL_DIR}/fastasian" 2>/dev/null && chmod 755 "${INSTALL_DIR}/fastasian" || true

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
INSTALLED_TAG="${REL_TAG}"
UPDATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STATEEOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
systemctl start "${SERVICE_NAME}"

log_ok "FastAsiaPanel updated to ${REL_TAG:-latest} (${REMOTE_SHA:0:7})."
