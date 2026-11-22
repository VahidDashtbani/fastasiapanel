#!/usr/bin/env bash
set -uo pipefail

REPO_OWNER_SLASH="VahidDashtbani/fastasiapanel"
REPO_URL="https://github.com/${REPO_OWNER_SLASH}.git"
API_COMMIT_URL="https://api.github.com/repos/${REPO_OWNER_SLASH}/commits/main"
API_LATEST_REL="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/latest"
API_TAG_V116="https://api.github.com/repos/${REPO_OWNER_SLASH}/releases/tags/v1.1.6"

APP_NAME="fastasiapanel"
CLI_NAME="fastasian"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/${APP_NAME}"
ENV_FILE="${CONFIG_DIR}/${APP_NAME}.env"
STATE_FILE="${CONFIG_DIR}/update_state.env"
SERVICE_NAME="${APP_NAME}.service"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}"

UPDATE_RUN_ID="${FAP_UPDATE_RUN_ID:-0}"
[[ "${UPDATE_RUN_ID}" =~ ^[0-9]+$ ]] || UPDATE_RUN_ID="0"
UPDATE_STATUS_FILE="${FAP_UPDATE_STATUS_FILE:-${CONFIG_DIR}/update-status.json}"

log_info() { echo "[INF] $*"; }
log_ok()   { echo "[OK]  $*"; }
log_warn() { echo "[WRN] $*" >&2; }
log_err()  { echo "[ERR] $*" >&2; }

_write_status() {
  local state="$1"
  local exit_code="$2"
  local dir
  dir="$(dirname "${UPDATE_STATUS_FILE}")"
  mkdir -p "${dir}" >/dev/null 2>&1 || true
  local tmp_file="${UPDATE_STATUS_FILE}.tmp.$$"
  printf '{"runId":"%s","state":"%s","exitCode":%s,"finishedAt":%s}\n' \
    "${UPDATE_RUN_ID}" "${state}" "${exit_code}" "$(date +%s)" > "${tmp_file}" 2>/dev/null || return 0
  mv -f "${tmp_file}" "${UPDATE_STATUS_FILE}" >/dev/null 2>&1 || true
}

_on_exit() {
  local code=$?
  if [[ "${code}" -eq 0 ]]; then
    _write_status "success" "0"
  else
    _write_status "failed" "${code}"
  fi
}

trap '_on_exit' EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

if [[ "${EUID}" -ne 0 ]]; then
  log_err "Please run as root."
  exit 1
fi

if ! command -v curl >/dev/null 2>&1 || ! command -v tar >/dev/null 2>&1; then
  log_err "Required commands 'curl' and 'tar' are not available."
  exit 1
fi

# ۱. بررسی قابلیت نوشتن در مسیرهای نصب و کانفیگ پیش از شروع تغییرات
require_writable_paths() {
  local d probe
  for d in "${INSTALL_DIR}" "${CONFIG_DIR}"; do
    mkdir -p "${d}" 2>/dev/null || true
    probe="${d}/.fap-write-test.$$"
    if ! : > "${probe}" 2>/dev/null; then
      log_err "Directory '${d}' is not writable (read-only mount or sandbox)."
      return 1
    fi
    rm -f "${probe}"
  done
  return 0
}

if ! require_writable_paths; then
  exit 1
fi

# ۲. خواندن تنظیمات فعلی دیتابیس و محیط برای حفظ کامل پس از آپدیت
PREV_DB_TYPE=""
PREV_DB_DSN=""

read_db_from_file() {
  local f="$1"
  [[ -f "$f" ]] || { return 0; }
  local t d
  t=$(grep -E '^(FAP_DB_TYPE|DB_TYPE)=' "$f" 2>/dev/null | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'" || true)
  d=$(grep -E '^(FAP_DB_DSN|DB_DSN)=' "$f" 2>/dev/null | head -n1 | cut -d= -f2- | tr -d '"' | tr -d "'" || true)
  [[ -n "$t" && -z "$PREV_DB_TYPE" ]] && PREV_DB_TYPE="$t"
  [[ -n "$d" && -z "$PREV_DB_DSN" ]] && PREV_DB_DSN="$d"
  return 0
}

read_db_from_file "${ENV_FILE}"
read_db_from_file "${SERVICE_FILE}"

if [[ "${PREV_DB_DSN}" == postgres://* || "${PREV_DB_DSN}" == postgresql://* ]]; then
  PREV_DB_TYPE="postgres"
elif [[ -z "${PREV_DB_TYPE}" ]] && command -v psql >/dev/null 2>&1 && runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='fastasiapanel'" 2>/dev/null | grep -q 1; then
  PREV_DB_TYPE="postgres"
fi

# ۳. بکاپ امن از فایل env، دیتابیس محلی و باینری فعلی
BACKUP_DIR="/var/backups/${APP_NAME}/pre_update_$(date +%Y%m%d_%H%M%S)"
mkdir -p "${BACKUP_DIR}"
[[ -f "${ENV_FILE}" ]] && cp -af "${ENV_FILE}" "${BACKUP_DIR}/"
[[ -f "${CONFIG_DIR}/${APP_NAME}.db" ]] && cp -af "${CONFIG_DIR}/${APP_NAME}.db" "${BACKUP_DIR}/"
[[ -f "${INSTALL_DIR}/${APP_NAME}" ]] && cp -af "${INSTALL_DIR}/${APP_NAME}" "${BACKUP_DIR}/${APP_NAME}.bin.bak"

# ۴. تشخیص معماری سیستم
ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    log_err "Unsupported architecture: ${ARCH_RAW}"
    exit 1
    ;;
esac

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"; _on_exit' EXIT

# ۵. دریافت اطلاعات آخرین کامیت و ریلیز از GitHub
REL_JSON="$(curl -fsSL --connect-timeout 15 --max-time 30 -H "Accept: application/vnd.github+json" "${API_LATEST_REL}" 2>/dev/null || true)"
if [[ -z "${REL_JSON}" ]]; then
  REL_JSON="$(curl -fsSL --connect-timeout 15 --max-time 30 -H "Accept: application/vnd.github+json" "${API_TAG_V116}" 2>/dev/null || true)"
fi
REL_TAG="$(printf '%s' "${REL_JSON}" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
REMOTE_SHA="$(curl -fsSL --connect-timeout 15 --max-time 30 -H "Accept: application/vnd.github+json" "${API_COMMIT_URL}" 2>/dev/null \
  | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -n1 || true)"

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

# ۶. دانلود امن آرشیو و بررسی sha256 در صورت وجود
verify_sha256_if_present() {
  local archive_path="$1"
  local asset_url="$2"
  local sha_url="${asset_url}.sha256"
  local sha_file="${WORK_DIR}/release.sha256"
  local http_code

  rm -f "${sha_file}"
  http_code="$(curl -sL --retry 2 --connect-timeout 10 --max-time 30 -o "${sha_file}" -w '%{http_code}' "${sha_url}" 2>/dev/null || echo "000")"
  if [[ "${http_code}" == "200" && -s "${sha_file}" ]]; then
    local expected actual
    expected="$(awk 'NR==1 {print $1}' "${sha_file}" | tr '[:upper:]' '[:lower:]')"
    actual="$(sha256sum "${archive_path}" 2>/dev/null | awk '{print $1}' | tr '[:upper:]' '[:lower:]')"
    if [[ ! "${expected}" =~ ^[0-9a-f]{64}$ || "${expected}" != "${actual}" ]]; then
      log_err "SHA256 mismatch for $(basename "${archive_path}"): expected ${expected:-none}, got ${actual:-none}"
      return 1
    fi
    log_ok "SHA256 verified: ${actual}"
  else
    log_info "No .sha256 asset found (HTTP ${http_code}); proceeding with archive extraction."
  fi
  return 0
}

NEW_BIN_READY=0
for url in "${ASSET_URLS[@]}"; do
  archive_file="${WORK_DIR}/release.tar.gz"
  extract_dir="${WORK_DIR}/unpack"
  rm -rf "${archive_file}" "${extract_dir}"
  mkdir -p "${extract_dir}"

  log_info "Downloading release archive: ${url}"
  if ! curl -fSL --retry 2 --connect-timeout 15 --max-time 120 "${url}" -o "${archive_file}" 2>/dev/null; then
    log_warn "Download failed from ${url}"
    continue
  fi
  if [[ ! -s "${archive_file}" ]]; then
    log_warn "Downloaded archive is empty from ${url}"
    continue
  fi
  if ! verify_sha256_if_present "${archive_file}" "${url}"; then
    continue
  fi
  if ! tar -xzf "${archive_file}" -C "${extract_dir}" 2>/dev/null; then
    log_warn "Failed to extract ${archive_file}"
    continue
  fi

  found_bin="$(find "${extract_dir}" -type f -name "${APP_NAME}" 2>/dev/null | head -n1 || true)"
  if [[ -n "${found_bin}" && -s "${found_bin}" ]]; then
    cp -f "${found_bin}" "${WORK_DIR}/${APP_NAME}.new"
    chmod 755 "${WORK_DIR}/${APP_NAME}.new"
    NEW_BIN_READY=1
    break
  fi
done

# ۷. فالبک به بیلد از سورس فقط در صورت عدم موفقیت دانلود باینری آماده
if [[ "${NEW_BIN_READY}" -ne 1 ]]; then
  log_warn "Release asset unavailable; trying Go source build..."
  if command -v go >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    if git clone --depth 1 --branch main "${REPO_URL}" "${WORK_DIR}/repo" >/dev/null 2>&1; then
      if (cd "${WORK_DIR}/repo" && GOTOOLCHAIN=local go build --buildvcs=false -o "${WORK_DIR}/${APP_NAME}.new" ./cmd/panel 2>/dev/null); then
        chmod 755 "${WORK_DIR}/${APP_NAME}.new"
        NEW_BIN_READY=1
      fi
    fi
  fi
fi

if [[ "${NEW_BIN_READY}" -ne 1 || ! -s "${WORK_DIR}/${APP_NAME}.new" ]]; then
  log_err "Update failed: could not obtain a valid ${APP_NAME} binary."
  exit 1
fi

# ۸. جایگزینی اتمی باینری، به‌روزرسانی CLI و حفظ کامل دیتابیس و تنظیمات
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true

staged_bin="${INSTALL_DIR}/.${APP_NAME}.new.$$"
cp -f "${WORK_DIR}/${APP_NAME}.new" "${staged_bin}"
chmod 755 "${staged_bin}"
mv -f "${staged_bin}" "${INSTALL_DIR}/${APP_NAME}"

staged_cli="${INSTALL_DIR}/.${CLI_NAME}.new.$$"
if curl -fsSL --connect-timeout 15 --max-time 30 \
     "https://raw.githubusercontent.com/${REPO_OWNER_SLASH}/main/scripts/fastasian.sh?v=$(date +%s)" \
     -o "${staged_cli}" 2>/dev/null && [[ -s "${staged_cli}" ]]; then
  chmod 755 "${staged_cli}"
  mv -f "${staged_cli}" "${INSTALL_DIR}/${CLI_NAME}"
else
  rm -f "${staged_cli}"
fi

if [[ -n "${PREV_DB_TYPE}" ]]; then
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
  log_info "Preserved database settings (${PREV_DB_TYPE})."
fi

cat <<STATEEOF > "${STATE_FILE}"
INSTALLED_COMMIT="${REMOTE_SHA}"
INSTALLED_TAG="${REL_TAG:-v1.1.6}"
UPDATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STATEEOF

systemctl daemon-reload >/dev/null 2>&1 || true
systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
systemctl start "${SERVICE_NAME}"

if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
  log_err "Service ${SERVICE_NAME} failed to start; restoring backup binary."
  if [[ -s "${BACKUP_DIR}/${APP_NAME}.bin.bak" ]]; then
    install -m 755 "${BACKUP_DIR}/${APP_NAME}.bin.bak" "${INSTALL_DIR}/${APP_NAME}"
    systemctl start "${SERVICE_NAME}" || true
  fi
  exit 1
fi

log_ok "FastAsiaPanel updated successfully (${REL_TAG:-v1.1.6}, commit ${REMOTE_SHA:0:7})."
