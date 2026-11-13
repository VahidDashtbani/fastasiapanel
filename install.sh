pause_menu() {
  printf '\n'
  read -r -p "Press Enter to continue..."
}

run_menu() {
  local choice
  while true; do
    clear 2>/dev/null || true
    printf '%b========================================%b\n' "${CYAN}" "${PLAIN}"
    printf '             FastAsiaPanel\n'
    printf '           Management Console\n'
    printf '%b========================================%b\n\n' "${CYAN}" "${PLAIN}"
    printf '  1. Panel status\n'
    printf '  2. Start panel\n'
    printf '  3. Stop panel\n'
    printf '  4. Restart panel and Xray supervisor\n'
    printf '  5. Change panel port\n'
    printf '  6. Panel information\n'
    printf '  7. View logs\n'
    printf '  8. Uninstall panel\n'
    printf '  0. Exit\n\n'
    read -r -p "Select an option [0-8]: " choice || exit 0
    case "${choice}" in
      1) show_status; pause_menu ;;
      2) systemctl start "${SERVICE}"; printf 'Start command sent.\n'; pause_menu ;;
      3) systemctl stop "${SERVICE}"; printf 'Stop command sent.\n'; pause_menu ;;
      4) restart_panel; pause_menu ;;
      5) change_port; pause_menu ;;
      6) show_info; pause_menu ;;
      7) journalctl -u "${SERVICE}" -f ;;
      8) uninstall_panel ;;
      0) exit 0 ;;
      *) printf 'Invalid option.\n'; sleep 1 ;;
    esac
  done
}

require_root
case "${1:-}" in
  "") run_menu ;;
  start) systemctl start "${SERVICE}" ;;
  stop) systemctl stop "${SERVICE}" ;;
  restart) restart_panel ;;
  status) show_status ;;
  info) show_info ;;
  port) change_port ;;
  log|logs) journalctl -u "${SERVICE}" -f ;;
  uninstall) uninstall_panel ;;
  *)
    printf 'Usage: fastpanel [start|stop|restart|status|info|port|log|uninstall]\n'
    exit 2
    ;;
esac
CLIEOF
  chmod 0750 "${CLI_PATH}"
}

create_service() {
  cat > "${SERVICE_PATH}" <<UNIT
[Unit]
Description=FastAsiaPanel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${APP_DIR}
EnvironmentFile=${ENV_FILE}
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
  chmod 0644 "${SERVICE_PATH}"
  systemctl daemon-reload
  systemctl enable "${APP_NAME}.service"
  systemctl restart "${APP_NAME}.service"
  sleep 2
  systemctl is-active --quiet "${APP_NAME}.service" || {
    journalctl -u "${APP_NAME}.service" -n 60 --no-pager >&2 || true
    die "FastAsiaPanel service did not become active."
  }
}

main() {
  require_root
  command -v systemctl >/dev/null 2>&1 || die "systemd is required on this Linux host."
  install_dependencies
  detect_arch
  TMP_DIR="$(mktemp -d)"
  install -d -m 0755 "${APP_DIR}"
  ensure_env
  download_panel
  install_xray_if_needed
  create_cli
  create_service

  local panel_user panel_pass panel_addr panel_port
  panel_user="$(read_env_value PANEL_USERNAME)"
  panel_pass="$(read_env_value PANEL_PASSWORD)"
  panel_addr="$(read_env_value PANEL_ADDR)"
  panel_port="${panel_addr##*:}"

  printf '\n%bFastAsiaPanel installed.%b\n' "${GREEN}" "${PLAIN}"
  printf 'Username: %s\n' "${panel_user}"
  printf 'Password: %s\n' "${panel_pass}"
  printf 'Port: %s\n' "${panel_port}"
  printf 'Credentials: %s\n' "${ENV_FILE}"
  printf 'Service: systemctl status %s\n' "${APP_NAME}"
  printf 'Management menu: fastpanel\n'
  printf 'Open: http://SERVER-IP:%s/\n' "${panel_port}"
  warn "The panel currently uses HTTP. Put it behind HTTPS before exposing it publicly."
}

main "$@"