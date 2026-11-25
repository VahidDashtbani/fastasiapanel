package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Listen        string
	BasePath      string
	DBType        string
	DataFile      string
	PostgresDSN   string
	XrayBinary    string
	XrayConfig    string
	PublicHost    string
	SecureCookies bool
	TLSCert       string
	TLSKey        string
	AdminUser     string
	AdminPassword string
}

func Load() (Config, error) {
	base := strings.TrimRight(env("FAP_BASE_PATH", ""), "/")
	if base != "" && !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	secure, _ := strconv.ParseBool(env("FAP_SECURE_COOKIES", "false"))
	return Config{
		Listen:        env("FAP_LISTEN", "0.0.0.0:2053"),
		BasePath:      base,
		DBType:        env("FAP_DB_TYPE", "json"),
		DataFile:      env("FAP_DB_PATH", "data/fastasiapanel.json"),
		PostgresDSN:   env("FAP_DB_DSN", ""),
		XrayBinary:    env("FAP_XRAY_BIN", "xray"),
		XrayConfig:    env("FAP_XRAY_CONFIG", "data/xray.json"),
		PublicHost:    env("FAP_PUBLIC_HOST", ""),
		SecureCookies: secure,
		TLSCert:       env("FAP_TLS_CERT", ""),
		TLSKey:        env("FAP_TLS_KEY", ""),
		AdminUser:     env("FAP_ADMIN_USER", "admin"),
		AdminPassword: env("FAP_ADMIN_PASSWORD", "admin1234"),
	}, nil
}

func (c Config) ValidateBootstrap() error {
	if strings.TrimSpace(c.AdminUser) == "" {
		return errors.New("admin username is required for initial setup")
	}
	if len(c.AdminPassword) < 8 {
		return errors.New("admin password must be at least 8 characters")
	}
	return nil
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
