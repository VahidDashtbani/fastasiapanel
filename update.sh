#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER_SLASH="VahidDashtbani/fastasiapanel"
REPO_URL="https://github.com/${REPO_OWNER_SLASH}.git"
API_COMMIT_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/commits/main"
API_LATEST_REL="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/latest"
API_TAG_V116="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/tags/v1.1.6"

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

# ۱. حفظ تنظیمات دیتابیس، پورت، base path و کلیدهای پنل
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

# ۲. بکاپ امن قبل از جایگزینی باینری
BACKUP_DIR="/var/backups/${APP_NAME}/pre_update_$(date +%Y%m%d_%H%M%S)"
mkdir -p "${BACKUP_DIR}"
[[ -f "${ENV_FILE}" ]] && cp -af "${ENV_FILE}" "${BACKUP_DIR}/"
[[ -f "${CONFIG_DIR}/${APP_NAME}.db" ]] && cp -af "${CONFIG_DIR}/${APP_NAME}.db" "${BACKUP_DIR}/"
[[ -f "${INSTALL_DIR}/${APP_NAME}" ]] && cp -af "${INSTALL_DIR}/${APP_NAME}" "${BACKUP_DIR}/${APP_NAME}.bin.bak"

ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) ARCH="amd64" ;;
esac

SRC_DIR="$(mktemp -d)"
trap 'rm -rf "${SRC_DIR}"' EXIT

# ۳. دریافت اطلاعات ریلیز و آخرین کامیت main
REL_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "${API_LATEST_REL}" 2>/dev/null || true)"
if [[ -z "${REL_JSON}" ]]; then
  REL_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "${API_TAG_V116}" 2>/dev/null || true)"
fi
REL_TAG="$(printf '%s' "${REL_JSON}" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
REMOTE_SHA="$(curl -fsSL -H "Accept: application/vnd.github+json" "${API_COMMIT_URL}" 2>/dev/null \
  | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -n1 || true)"

# ۴. جمع‌آوری URL های دانلود باینری آماده برای معماری سرور
ASSET_URLS=()
if [[ -n "${REL_JSON}" ]]; then
  while IFS= read -r u; do
    [[ -n "$u" ]] && ASSET_URLS+=("$u")
  done < <(printf '%s' "${REL_JSON}" \
    | grep -oE 'https://github\.com/[^"]+/releases/download/[^"]+' \
    | grep -i "linux-${ARCH}\.tar\.gz" \
    | sort -u || true)
fi

if [[ -n "${REL_TAG}" ]]; then
  ASSET_URLS+=("https://github.com/${REPO_OWNER_SLASH}/releases/download/${REL_TAG}/${APP_NAME}-linux-${ARCH}.tar.gz")
fi
ASSET_URLS+=(
  "https://github.com/${REPO_OWNER_SLASH}/releases/download/v1.1.6/${APP_NAME}-linux-${ARCH}.tar.gz"
  "https://github.com/${REPO_OWNER_SLASH}/releases/latest/download/${APP_NAME}-linux-${ARCH}.tar.gz"
)

INSTALLED_OK=0
for url in "${ASSET_URLS[@]}"; do
  log_info "Downloading release package: ${url}"
  rm -rf "${SRC_DIR}/unpack" && mkdir -p "${SRC_DIR}/unpack"
  if curl -fsSL "${url}" -o "${SRC_DIR}/release.tar.gz" 2>/dev/null \
     && tar -xzf "${SRC_DIR}/release.tar.gz" -C "${SRC_DIR}/unpack" 2>/dev/null; then
    found_bin="$(find "${SRC_DIR}/unpack" -type f -name "${APP_NAME}" | head -n1 || true)"
    if [[ -n "${found_bin}" && -s "${found_bin}" ]]; then
      cp -f "${found_bin}" "${SRC_DIR}/${APP_NAME}"
      INSTALLED_OK=1
      break
    fi
  fi
done

# ۵. فالبک به بیلد سورس فقط اگر دانلود بسته آماده ممکن نشد
if [[ "${INSTALLED_OK}" -ne 1 ]]; then
  log_warn "Release package download failed; trying local Go source build..."
  if command -v go >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    if git clone --depth 1 --branch main "${REPO_URL}" "${SRC_DIR}/repo" >/dev/null 2>&1; then
      if (cd "${SRC_DIR}/repo" && GOTOOLCHAIN=local go build --buildvcs=false -o "${SRC_DIR}/${APP_NAME}" ./cmd/panel 2>/dev/null); then
        INSTALLED_OK=1
      fi
    fi
  fi
fi

if [[ "${INSTALLED_OK}" -ne 1 || ! -s "${SRC_DIR}/${APP_NAME}" ]]; then
  log_warn "Update failed: could not download or build ${APP_NAME}."
  exit 1
fi

# ۶. توقف سرویس، نصب باینری جدید، آپدیت CLI و حفظ دیتابیس
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
install -m 755 "${SRC_DIR}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"

curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER_SLASH}/main/scripts/fastasian.sh?v=$(date +%s)" \
  -o "${INSTALL_DIR}/fastasian" 2>/dev/null && chmod 755 "${INSTALL_DIR}/fastasian" || true

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
INSTALLED_TAG="${REL_TAG:-v1.1.6}"
UPDATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STATEEOF

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
systemctl start "${SERVICE_NAME}"

log_ok "FastAsiaPanel updated to ${REL_TAG:-v1.1.6} (${REMOTE_SHA:0:7})."
