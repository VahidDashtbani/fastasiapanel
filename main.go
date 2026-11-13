package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

//go:embed templates/*
var templateFS embed.FS

const (
	panelName    = "FastAsiaPanel"
	panelVersion = "0.3.0"
)

func main() {
	log.Printf("Starting %s v%s ...", panelName, panelVersion)
	if err := initDB(); err != nil {
		log.Fatal("DB init failed: ", err)
	}
	if err := startXray(); err != nil {
		log.Println("Warning: Xray start failed:", err)
	}
	go statsLoop()

	mux := http.NewServeMux()
	a := requireAuth
	mux.HandleFunc("GET /{$}", indexHandler)
	mux.HandleFunc("GET /login", loginPage)
	mux.HandleFunc("GET /api/info", infoHandler)
	mux.HandleFunc("POST /api/login", loginHandler)
	mux.HandleFunc("POST /api/logout", a(logoutHandler))
	mux.HandleFunc("POST /api/password", a(passwordHandler))
	mux.HandleFunc("GET /api/status", a(statusHandler))

	// Reality key generator
	mux.HandleFunc("GET /api/reality/keys", a(realityKeysHandler))

	mux.HandleFunc("GET /api/inbounds", a(listInbounds))
	mux.HandleFunc("POST /api/inbounds", a(createInbound))
	mux.HandleFunc("PUT /api/inbounds/{id}", a(updateInbound))
	mux.HandleFunc("POST /api/inbounds/{id}/toggle", a(toggleInbound))
	mux.HandleFunc("DELETE /api/inbounds/{id}", a(deleteInbound))

	mux.HandleFunc("GET /api/clients", a(listClients))
	mux.HandleFunc("POST /api/clients", a(createClient))
	mux.HandleFunc("PUT /api/clients/{id}", a(updateClient))
	mux.HandleFunc("DELETE /api/clients/{id}", a(deleteClient))
	mux.HandleFunc("POST /api/clients/{id}/reset", a(resetClientTraffic))

	mux.HandleFunc("POST /api/xray/restart", a(restartHandler))
	mux.HandleFunc("GET /api/backup", a(backupHandler))

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		stopXray()
		os.Exit(0)
	}()

	addr := os.Getenv("PANEL_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("%s running on %s", panelName, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}