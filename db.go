package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const dbPath = "fastasiapanel.db"

var DB *gorm.DB

type User struct {
	ID       uint   `gorm:"primaryKey"`
	Username string `gorm:"uniqueIndex"`
	Password string
}

type Session struct {
	Token     string    `gorm:"primaryKey"`
	ExpiresAt time.Time `gorm:"index"`
}

type Inbound struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Remark   string `json:"remark"`
	Enable   bool   `json:"enable"`
	Port     int    `gorm:"uniqueIndex" json:"port"`
	Protocol string `json:"protocol"` // vless | vmess | trojan | shadowsocks
	Network  string `json:"network"`  // tcp | ws | grpc
	Path     string `json:"path"`     // ws path / grpc serviceName
	Security string `json:"security"` // none | tls | reality
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	SSMethod string `json:"ss_method"`

	// Reality fields
	RealityDest       string `json:"reality_dest"`        // e.g. www.microsoft.com:443
	RealityServerName string `json:"reality_server_name"` // e.g. www.microsoft.com
	RealityPrivateKey string `json:"reality_private_key"` // base64 RawURL, server-only
	RealityPublicKey  string `json:"reality_public_key"`  // base64 RawURL, given to clients
	RealityShortID    string `json:"reality_short_id"`    // hex, even length, <=16

	Up   int64 `json:"up"`
	Down int64 `json:"down"`

	Clients []Client `gorm:"-" json:"clients"`
}

type Client struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	InboundID  uint   `gorm:"index" json:"inbound_id"`
	Email      string `gorm:"uniqueIndex" json:"email"`
	UUID       string `json:"uuid"` // uuid for vless/vmess, password for trojan/ss
	Flow       string `json:"flow"` // "" or "xtls-rprx-vision"
	Enable     bool   `json:"enable"`
	TotalBytes int64  `json:"total_bytes"` // 0 = unlimited
	ExpiryTime int64  `json:"expiry_time"` // unix ms, 0 = never
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Link       string `gorm:"-" json:"link,omitempty"`
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func initDB() error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return err
	}
	if err = DB.AutoMigrate(&Inbound{}, &Client{}, &User{}, &Session{}); err != nil {
		return err
	}
	var n int64
	DB.Model(&User{}).Count(&n)
	if n == 0 {
		pass := randHex(6)
		h, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		DB.Create(&User{Username: "admin", Password: string(h)})
		log.Printf("==== first run: username=admin password=%s (change it after login) ====", pass)
	}
	DB.Where("expires_at < ?", time.Now()).Delete(&Session{})
	return nil
}