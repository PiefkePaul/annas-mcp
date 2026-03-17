package env

import (
	"os"
	"strings"
)

const (
	DefaultHTTPAddr = ":8080"
	DefaultHTTPPath = "/mcp"
)

type HTTPEnv struct {
	Addr        string `json:"addr"`
	Path        string `json:"path"`
	BearerToken string `json:"bearer_token"`
}

func GetHTTPEnv() *HTTPEnv {
	addr := strings.TrimSpace(os.Getenv("ANNAS_HTTP_ADDR"))
	if addr == "" {
		addr = DefaultHTTPAddr
	}

	path := normalizeHTTPPath(os.Getenv("ANNAS_HTTP_PATH"))

	return &HTTPEnv{
		Addr:        addr,
		Path:        path,
		BearerToken: strings.TrimSpace(os.Getenv("ANNAS_HTTP_BEARER_TOKEN")),
	}
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
