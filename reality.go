package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateX25519Keys یک جفت کلید X25519 تولید می‌کند و هر دو را
// با base64.RawURLEncoding کد می‌کند (معادل xray x25519).
func GenerateX25519Keys() (privateKey, publicKey string, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	pub := priv.PublicKey()
	privateKey = base64.RawURLEncoding.EncodeToString(priv.Bytes())
	publicKey = base64.RawURLEncoding.EncodeToString(pub.Bytes())
	return privateKey, publicKey, nil
}

// GenShortID یک shortId هگز با طول زوج (حداکثر ۱۶ کاراکتر) تولید می‌کند.
func GenShortID() string {
	b := make([]byte, 4) // 8 hex chars
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// buildRealityStreamSettings ساختار streamSettings برای Reality می‌سازد.
// فقط در حالت network=tcp و security=reality معنی دارد.
func buildRealityStreamSettings(in Inbound) M {
	s := M{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": M{
			"show":        false,
			"dest":        in.RealityDest,
			"xver":        0,
			"serverNames": []string{in.RealityServerName},
			"privateKey":  in.RealityPrivateKey,
			"shortIds":    []string{in.RealityShortID},
		},
	}
	return s
}

// buildRealityLink لینک vless:// برای Reality + Vision می‌سازد.
// فقط publicKey به کلاینت داده می‌شود؛ privateKey روی سرور می‌ماند.
func buildRealityLink(in Inbound, c Client, host string) string {
	sni := in.RealityServerName
	if sni == "" {
		sni = in.RealityDest
	}
	return fmt.Sprintf(
		"vless://%s@%s:%d?type=tcp&security=reality&pbk=%s&fp=chrome&sni=%s&sid=%s&spx=%%2F&flow=xtls-rprx-vision#%s",
		c.UUID, host, in.Port,
		in.RealityPublicKey, sni, in.RealityShortID,
		c.Email,
	)
}