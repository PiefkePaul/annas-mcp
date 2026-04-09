package env

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	DefaultAuthStorePath        = "./annas-auth-store.enc"
	DefaultAccessTokenTTL       = time.Hour
	DefaultRefreshTokenTTL      = 30 * 24 * time.Hour
	DefaultAuthorizationCodeTTL = 10 * time.Minute
	DefaultSessionTTL           = 30 * 24 * time.Hour
)

type AuthEnv struct {
	StorePath            string
	MasterKey            []byte
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	AuthorizationCodeTTL time.Duration
	SessionTTL           time.Duration
}

func GetAuthEnv() (*AuthEnv, error) {
	masterKey, err := parseAuthMasterKey(strings.TrimSpace(os.Getenv("ANNAS_AUTH_MASTER_KEY")))
	if err != nil {
		return nil, err
	}

	return &AuthEnv{
		StorePath:            getEnvOrDefault("ANNAS_AUTH_STORE_PATH", DefaultAuthStorePath),
		MasterKey:            masterKey,
		AccessTokenTTL:       getDurationEnv("ANNAS_AUTH_ACCESS_TOKEN_TTL", DefaultAccessTokenTTL),
		RefreshTokenTTL:      getDurationEnv("ANNAS_AUTH_REFRESH_TOKEN_TTL", DefaultRefreshTokenTTL),
		AuthorizationCodeTTL: getDurationEnv("ANNAS_AUTH_CODE_TTL", DefaultAuthorizationCodeTTL),
		SessionTTL:           getDurationEnv("ANNAS_AUTH_SESSION_TTL", DefaultSessionTTL),
	}, nil
}

func parseAuthMasterKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("ANNAS_AUTH_MASTER_KEY must be set when ANNAS_HTTP_AUTH_MODE=oauth")
	}

	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}

	return nil, fmt.Errorf("ANNAS_AUTH_MASTER_KEY must decode to exactly 32 bytes (base64, base64url, or hex)")
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}
