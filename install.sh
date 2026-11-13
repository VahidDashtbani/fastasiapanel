  rm -rf "${APP_DIR}"
  rm -f /usr/local/bin/fastpanel

  printf 'FastAsiaPanel uninstalled.\n'
  exit 0
}

pause_menu() {
  printf '\n'
  read -rp "Press Enter to continue..."
}

run_menu() {
  local choice

  while true; do
    clear
    printf '%b' "${CYAN}"
    printf '========================================\n'
    printf '             FastAsiaPanel\n'
    printf '           Management Console\n'
    printf '========================================\n'
    printf '%b' "${PLAIN}"

    printf '  1. Panel status\n'
    printf '  2. Start panel\n'
    printf '  3. Stop panel\n'
    printf '  4. Restart panel\n'
    printf '  5. Change panel port\n'
    printf '  6. Panel information\n'
    printf '  7. Restart panel and Xray\n'
    printf '  8. View logs\n'
    printf '  9. Uninstall panel\n'
    printf '  0. Exit\n\n'

    read -rp "Select an option [0-9]: " choice

    case "${choice}" in
      1) show_status; pause_menu ;;
      2) systemctl start "${SERVICE}"; printf 'Panel started.\n'; pause_menu ;;
      3) systemctl stop "${SERVICE}"; printf 'Panel stopped.\n'; pause_menu ;;
      4) restart_panel; pause_menu ;;
      5) change_port; pause_menu ;;
      6) show_info; pause_menu ;;
      7) restart_panel; pause_menu ;;
      8) journalctl -u "${SERVICE}" -f ;;
      9) uninstall_panel ;;
      0) exit 0 ;;
      *) printf '%bInvalid option.%b\n' "${RED}" "${PLAIN}"; sleep 1 ;;
    esac
  done
}

case "${1:-}" in
  "") run_menu ;;
  start) systemctl start "${SERVICE}" ;;
  stop) systemctl stop "${SERVICE}" ;;
  restart) restart_panel ;;
  status) show_status ;;
  info) show_info ;;
  port) change_port ;;
  log|logs) journalctl -u "${SERVICE}" -f ;;
  xray-restart) restart_panel ;;
  uninstall) uninstall_panel ;;
  *)
    printf 'Usage: fastpanel [start|stop|restart|status|info|port|log|xray-restart|uninstall]\n'
    exit 2
    ;;
esac
CLIEOF

  chmod 0750 /usr/local/bin/fastpanel
}

create_cli

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
PANEL_PORT="$(sed -n 's/^PANEL_ADDR=//p' "${APP_DIR}/panel.env" | tail -n1)"
PANEL_PORT="${PANEL_PORT##*:}"

printf '\nFastAsiaPanel installed.\n'
printf 'Username: %s\n' "${PANEL_USER}"
printf 'Password: %s\n' "${PANEL_PASS}"
printf 'Port: %s\n' "${PANEL_PORT}"
printf 'Credentials are also stored in %s/panel.env\n' "${APP_DIR}"
printf 'Service: systemctl status %s\n' "${APP_NAME}"
printf 'Management: fastpanel\n'
