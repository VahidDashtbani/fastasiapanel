package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Listen        string
	BasePath      string
	DataFile      string
	XrayBinary    string
	XrayConfig    string
	CookieSecret  string
	SecureCookies bool
	AdminUser     string
	AdminPassword string
	PublicHost    string
}

func Load() (Config, error) {
	c := Config{Listen: env("PANEL_ADDR", ":8080"), BasePath: cleanPath(os.Getenv("PANEL_PATH")), DataFile: env("DB_FILE", "./data/fastasiapanel.json"), XrayBinary: env("XRAY_BIN", "xray"), XrayConfig: env("XRAY_CONFIG", "./data/xray.json"), CookieSecret: os.Getenv("PANEL_COOKIE_SECRET"), SecureCookies: os.Getenv("PANEL_SECURE_COOKIE") == "true", AdminUser: os.Getenv("PANEL_USERNAME"), AdminPassword: os.Getenv("PANEL_PASSWORD"), PublicHost: os.Getenv("PANEL_PUBLIC_HOST")}
	if c.CookieSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return c, err
		}
		c.CookieSecret = hex.EncodeToString(b)
	}
	if c.AdminUser == "" {
		c.AdminUser = "admin"
	}
	if c.AdminPassword == "" {
		return c, errors.New("PANEL_PASSWORD must be set before first start")
	}
	if len(c.AdminPassword) < 12 {
		return c, errors.New("PANEL_PASSWORD must contain at least 12 characters")
	}
	if err := os.MkdirAll(filepath.Dir(c.DataFile), 0700); err != nil {
		return c, err
	}
	if err := os.MkdirAll(filepath.Dir(c.XrayConfig), 0700); err != nil {
		return c, err
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func cleanPath(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, "/")
	if v == "" {
		return ""
	}
	return "/" + v
}
