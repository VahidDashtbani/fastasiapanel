#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/VahidDashtbani/fastasiapanel.git"
SRC_DIR="${SRC_DIR:-}"
for d in "/opt/fastasiapanel" "/root/fastasiapanel" "$HOME/fastasiapanel" "/home/vahidnet/Downloads/fastasiapanel"; do
  if [ -d "$d/.git" ]; then
    SRC_DIR="$d"
    break
  fi
done
[ -n "$SRC_DIR" ] || SRC_DIR="/opt/fastasiapanel"

BIN_TARGET="${BIN_TARGET:-/usr/local/bin/fastasiapanel}"
ENV_FILE="${ENV_FILE:-/etc/fastasiapanel/fastasiapanel.env}"
BACKUP_DIR="/var/backups/fastasiapanel/$(date +%Y%m%d_%H%M%S)"
TARGET_REF="${1:-main}"

echo "[1/5] Backing up config & database to $BACKUP_DIR ..."
mkdir -p "$BACKUP_DIR"
[ -f "$ENV_FILE" ] && cp -f "$ENV_FILE" "$BACKUP_DIR/fastasiapanel.env"
[ -f "/etc/fastasiapanel/fastasiapanel.db" ] && cp -f "/etc/fastasiapanel/fastasiapanel.db" "$BACKUP_DIR/fastasiapanel.db"

echo "[2/5] Fetching latest updates ($TARGET_REF) ..."
if [ -d "$SRC_DIR/.git" ]; then
  cd "$SRC_DIR"
  git fetch --all --tags --prune
  git checkout "$TARGET_REF"
  if git show-ref --verify --quiet "refs/remotes/origin/$TARGET_REF"; then
    git reset --hard "origin/$TARGET_REF"
  fi
else
  git clone --branch "$TARGET_REF" "$REPO_URL" "$SRC_DIR"
  cd "$SRC_DIR"
fi

echo "[3/5] Running checks & building binary (preserving existing credentials/env) ..."
go vet ./...
go test ./...
if [ -d "./cmd/panel" ]; then
  go build -trimpath -o "$BIN_TARGET" ./cmd/panel
else
  go build -trimpath -o "$BIN_TARGET" .
fi
chmod +x "$BIN_TARGET"

echo "[4/5] Ensuring existing panel credentials & DB config stay intact ..."
if [ -f "$BACKUP_DIR/fastasiapanel.env" ]; then
  mkdir -p "$(dirname "$ENV_FILE")"
  cp -f "$BACKUP_DIR/fastasiapanel.env" "$ENV_FILE"
fi

echo "[5/5] Restarting fastasiapanel service ..."
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files | grep -q "^fastasiapanel\.service"; then
  systemctl daemon-reload
  systemctl restart fastasiapanel
  systemctl status fastasiapanel --no-pager || true
elif command -v service >/dev/null 2>&1 && service --status-all 2>&1 | grep -q "fastasiapanel"; then
  service fastasiapanel restart
else
  pkill -f "$BIN_TARGET" || true
  nohup "$BIN_TARGET" >/var/log/fastasiapanel.log 2>&1 &
fi

echo "Update completed cleanly on ref: $TARGET_REF (panel credentials preserved)."
