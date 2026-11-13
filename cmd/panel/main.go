package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fastasiapanel/internal/auth"
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
	"fastasiapanel/internal/traffic"
	"fastasiapanel/internal/web"
	"fastasiapanel/internal/xray"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(cfg.DataFile)
	if err != nil {
		log.Fatal(err)
	}
	manager := xray.New(cfg.XrayBinary, cfg.XrayConfig)
	if err := manager.Apply(db.Snapshot()); err != nil {
		log.Printf("Xray is not ready: %v", err)
	}
	authn := auth.New(db, cfg.CookieSecret, cfg.BasePath, cfg.SecureCookies)
	app := web.New(db, manager, authn, cfg)
	accounting := traffic.New(db, manager, manager, 10*time.Second)
	mux := http.NewServeMux()
	app.Register(mux)
	server := &http.Server{Addr: cfg.Listen, Handler: securityHeaders(mux), ReadHeaderTimeout: 8 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go accounting.Run(ctx.Done())
	go manager.Supervise(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		_ = manager.Stop()
	}()
	log.Printf("FastAsiaPanel listening on %s%s", cfg.Listen, cfg.BasePath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
