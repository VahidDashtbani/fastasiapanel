package web

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

func fapAPIToken() string {
	for _, k := range []string{"FAP_API_TOKEN", "FASTASIAN_API_TOKEN", "API_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func fapExtractBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	if k := strings.TrimSpace(r.Header.Get("X-Api-Token")); k != "" {
		return k
	}
	return ""
}

func fapAuthorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	expected := fapAPIToken()
	if expected == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "FAP_API_TOKEN is not configured",
		})
		return false
	}
	provided := fapExtractBearerToken(r)
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "unauthorized",
		})
		return false
	}
	return true
}

func fapWriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fapForwardHandler(w http.ResponseWriter, r *http.Request, fn any) bool {
	switch h := fn.(type) {
	case http.HandlerFunc:
		h(w, r)
		return true
	case func(http.ResponseWriter, *http.Request):
		h(w, r)
		return true
	default:
		return false
	}
}

func fapHandleAPIV1Inbounds(w http.ResponseWriter, r *http.Request) {
	if !fapAuthorizeAPI(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		fapWriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"endpoint": "/api/v1/inbounds",
			"method":   r.Method,
		})
	default:
		fapWriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":    false,
			"error": "method not allowed",
		})
	}
}

func fapHandleAPIV1Clients(w http.ResponseWriter, r *http.Request) {
	if !fapAuthorizeAPI(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		fapWriteJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"endpoint": "/api/v1/clients",
			"method":   r.Method,
		})
	default:
		fapWriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":    false,
			"error": "method not allowed",
		})
	}
}
