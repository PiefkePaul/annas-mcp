package env

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultHTTPAddr = ":8080"
	DefaultHTTPPath = "/mcp"
)

type HTTPAuthMode string

const (
	HTTPAuthModeNone   HTTPAuthMode = "none"
	HTTPAuthModeBearer HTTPAuthMode = "bearer"
	HTTPAuthModeOAuth  HTTPAuthMode = "oauth"
)

type HTTPEnv struct {
	Addr          string       `json:"addr"`
	Path          string       `json:"path"`
	AuthMode      HTTPAuthMode `json:"auth_mode"`
	BearerToken   string       `json:"bearer_token"`
	PublicBaseURL string       `json:"public_base_url"`
}

func GetHTTPEnv() (*HTTPEnv, error) {
	addr := getEnvOrDefault("ANNAS_HTTP_ADDR", DefaultHTTPAddr)
	path := normalizeHTTPPath(getEnvOrDefault("ANNAS_HTTP_PATH", DefaultHTTPPath))
	bearerToken := strings.TrimSpace(getEnvOrDefault("ANNAS_HTTP_BEARER_TOKEN", ""))
	publicBaseURL, err := normalizePublicBaseURL(getEnvOrDefault("ANNAS_PUBLIC_BASE_URL", ""))
	if err != nil {
		return nil, err
	}

	authMode := normalizeHTTPAuthMode(getEnvOrDefault("ANNAS_HTTP_AUTH_MODE", ""), bearerToken)
	if authMode == "" {
		return nil, fmt.Errorf("ANNAS_HTTP_AUTH_MODE must be one of: %s, %s, %s", HTTPAuthModeNone, HTTPAuthModeBearer, HTTPAuthModeOAuth)
	}
	if authMode == HTTPAuthModeBearer && bearerToken == "" {
		return nil, fmt.Errorf("ANNAS_HTTP_BEARER_TOKEN must be set when ANNAS_HTTP_AUTH_MODE=%s", HTTPAuthModeBearer)
	}

	return &HTTPEnv{
		Addr:          addr,
		Path:          path,
		AuthMode:      authMode,
		BearerToken:   bearerToken,
		PublicBaseURL: publicBaseURL,
	}, nil
}

func (e *HTTPEnv) ChatGPTCompatibleAuth() bool {
	return e.AuthMode == HTTPAuthModeNone || e.AuthMode == HTTPAuthModeOAuth
}

func (e *HTTPEnv) ConnectorURL() string {
	if e.PublicBaseURL == "" {
		return ""
	}
	return e.PublicBaseURL + e.Path
}

func normalizeHTTPPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return DefaultHTTPPath
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}

	return path
}

func normalizeHTTPAuthMode(raw string, bearerToken string) HTTPAuthMode {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		if bearerToken != "" {
			return HTTPAuthModeBearer
		}
		return HTTPAuthModeNone
	}

	switch HTTPAuthMode(raw) {
	case HTTPAuthModeNone, HTTPAuthModeBearer, HTTPAuthModeOAuth:
		return HTTPAuthMode(raw)
	default:
		return ""
	}
}

func normalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ANNAS_PUBLIC_BASE_URL is invalid: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ANNAS_PUBLIC_BASE_URL must include scheme and host")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("ANNAS_PUBLIC_BASE_URL must use http or https")
	}

	return strings.TrimRight(raw, "/"), nil
}

func getEnvOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
