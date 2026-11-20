package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fastasiapanel/internal/auth"
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/web"
	"fastasiapanel/internal/xray"
	webui "fastasiapanel/web"
)

var version = "1.0.0"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config: ", err)
	}
	st, err := openStore(cfg)
	if err != nil {
		log.Fatal("database: ", err)
	}
	defer st.Close()
	au := auth.New(st, cfg.SecureCookies, cfg.BasePath)

	if len(args) > 0 && args[0] == "reset-admin" {
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: reset-admin <username> <password>")
			os.Exit(2)
		}
		if err := au.SetAdmin(args[1], args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("admin credentials updated")
		return
	}

	if !au.HasAdmin() {
		if err := cfg.ValidateBootstrap(); err != nil {
			log.Fatal(err)
		}
		if err := au.SetAdmin(cfg.AdminUser, cfg.AdminPassword); err != nil {
			log.Fatal("admin: ", err)
		}
		log.Printf("admin user %q created", cfg.AdminUser)
	}

	xr := xray.NewManager(cfg, st)
	if err := xr.Start(); err != nil {
		log.Printf("xray did not start: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go xr.Run(ctx)

	app := web.New(cfg, st, au, xr, webui.FS, version)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()

	log.Printf("%s v%s listening on %s path=%q db=%s", "FastAsiaPanel", version, cfg.Listen, cfg.BasePath, cfg.DBType)
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	xr.Stop()
}
