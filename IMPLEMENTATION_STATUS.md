# FastAsiaPanel 1.0 development status

## Implemented in the current source

- Go HTTP server, configurable listener and URL prefix
- Persian RTL dashboard and login
- Signed sessions, throttling, origin checks and security headers
- Atomic JSON persistence with restrictive permissions, fsync and rollback-safe updates
- Inbound CRUD and enable/disable with candidate Xray config validation
- Managed clients for VLESS, VMess, Trojan, Shadowsocks, SOCKS and HTTP
- Preservation of raw/manual client entries during config generation
- Client edit, enable/disable, reset and delete
- VLESS, VMess, Trojan and Shadowsocks subscription generation
- Xray API/StatsService config and CLI-based stats polling
- Per-user upload/download accounting, expiry/quota disable and config re-apply
- Xray process restart supervisor with backoff
- Admin password rotation
- ZIP backup and validated restore
- Linux installer and amd64/arm64/armv7 release workflow

## Still outstanding

1. Replace JSON persistence with SQLite migrations and add a PostgreSQL adapter if required.
2. Verify StatsService output and HandlerService behavior against a real Xray version and live traffic.
3. Add online-user tracking and protocol-specific accounting verification for SOCKS/HTTP.
4. Add WireGuard peer, MTProto account, Dokodemo-door and additional protocol adapters.
5. Add QR generation and profile-specific subscription formats.
6. Add Telegram bot settings, notifications, command authorization and retry queue.
7. Add audit logs, structured log streaming and scheduled backups.
8. Add multi-admin RBAC, role permissions and session revocation.
9. Add automated end-to-end tests on supported Linux distributions with a real Xray binary.

## Verification performed

- `go test -race ./...`
- `go vet ./...`
- `node --check web/app.js`
- `bash -n install.sh`
- Build and local HTTP/API smoke tests are run for release artifacts.

The Xray CLI stats test uses a mocked executable response. It does not prove live Xray protocol behavior; a real Xray integration test remains required.
