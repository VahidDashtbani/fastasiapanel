# FastAsiaPanel

FastAsiaPanel is an independent Go-based Xray management panel. This is an active 1.0 development build based on the supplied FastAsiaPanel 0.3.1 source; it is not yet a verified feature-for-feature replacement for 3x-ui.

## Implemented

- Persian RTL dark responsive dashboard and login
- Signed HTTP-only SameSite sessions, login throttling, origin checks and security headers
- Atomic JSON persistence with file mode 0600, fsync and rollback-safe in-memory updates
- Inbound create, edit, enable/disable and delete operations
- Raw Xray inbound JSON support with candidate validation through `xray run -test`
- Managed account create, edit, enable/disable, reset and delete for VLESS, VMess, Trojan, Shadowsocks, SOCKS and HTTP
- Preservation of manually configured clients/accounts when managed users are injected
- VLESS, VMess, Trojan and Shadowsocks subscription output
- Internal Xray API configuration with StatsService and per-user traffic counter polling through the Xray CLI
- Traffic upload/download accumulation, expiry enforcement and quota enforcement for Xray users whose inbound protocol emits per-user stats
- Xray process supervisor with restart backoff
- Admin password rotation with salted PBKDF2 password hash
- ZIP backup download and validated backup restore
- Linux installer, systemd unit generation and GitHub release workflow for amd64, arm64 and armv7

## Known limitations

- Persistence is atomic JSON, not SQLite/PostgreSQL; large deployments and concurrent multi-process access are not supported.
- Actual Xray StatsService output and protocol behavior still need end-to-end verification against a real Xray binary and server. Automated tests validate the panel logic and mocked CLI response only.
- SOCKS/HTTP account structures are managed, but per-account traffic statistics may not be exposed by Xray in the same way as VLESS/VMess/Trojan/Shadowsocks users; do not rely on per-user quotas for those until verified on the target Xray version.
- WireGuard peer management, MTProto account management, Dokodemo-door peer workflows, online-user tracking, Telegram bot integration, QR generation, audit trail/log streaming, scheduled backups, multi-admin RBAC and a dedicated settings/operations console are not implemented.
- Subscriptions are supported for VLESS, VMess, Trojan and Shadowsocks. SOCKS and HTTP do not currently have generated subscription URIs.
- Xray API is bound to 127.0.0.1:10085. This port is reserved and must not be used by a public inbound.

## Requirements

- Go 1.23+ to build
- Xray-core installed on the host
- Linux is the intended deployment target

## Development

```bash
export PANEL_USERNAME=admin
export PANEL_PASSWORD='use-a-long-random-password'
export PANEL_COOKIE_SECRET="$(openssl rand -hex 32)"
export XRAY_BIN=/usr/local/bin/xray
export DB_FILE=./data/fastasiapanel.json
export XRAY_CONFIG=./data/xray.json
go run ./cmd/panel
```

Open `http://127.0.0.1:8080/`.

## Inbound configuration

Create an inbound using the complete JSON object accepted by Xray. The top-level `protocol`, `port`, and `settings` must match the form. The panel validates the candidate configuration with the installed Xray binary before replacing the active configuration. API port 10085 is reserved for local Xray management.

Managed account injection is available for VLESS, VMess, Trojan, Shadowsocks, SOCKS and HTTP. Other protocols can be configured with raw Xray JSON, but their peer/account management is not provided by this build.

## Deployment notes

Put the panel behind HTTPS, set `PANEL_SECURE_COOKIE=true`, restrict the management port with a firewall, and use a unique high-entropy `PANEL_COOKIE_SECRET`. Keep the data directory readable only by the panel service account. Never expose the Xray API port publicly.
