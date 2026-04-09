package modes

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/PiefkePaul/annas-mcp/internal/env"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"github.com/PiefkePaul/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func StartHTTPServer() {
	l := logger.GetLogger()
	defer l.Sync()

	httpEnv, err := env.GetHTTPEnv()
	if err != nil {
		l.Fatal("Invalid HTTP environment", zap.Error(err))
	}

	serverVersion := version.GetVersion()
	server := newMCPServer(serverVersion)
	availableTools := exposedToolNames()
	baseEnv := env.GetBaseEnv()

	if !httpEnv.ChatGPTCompatibleAuth() {
		l.Warn("Bearer auth is enabled. ChatGPT MCP connectors currently expect no auth or OAuth rather than a custom bearer token.",
			zap.String("authMode", string(httpEnv.AuthMode)),
		)
	}

	mux := http.NewServeMux()
	mux.Handle(httpEnv.Path, withConfiguredAuth(httpEnv, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"name":      "annas-mcp",
			"version":   serverVersion,
			"transport": "streamable-http",
		})
	})

	if httpEnv.Path != "/" {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
				return
			}

			payload := map[string]any{
				"name":                             "annas-mcp",
				"version":                          serverVersion,
				"transport":                        "streamable-http",
				"mcp_endpoint":                     httpEnv.Path,
				"health_endpoint":                  "/healthz",
				"auth_mode":                        httpEnv.AuthMode,
				"chatgpt_auth_compatible":          httpEnv.ChatGPTCompatibleAuth(),
				"book_download_enabled":            true,
				"article_download_enabled":         true,
				"available_tools":                  availableTools,
				"annas_mirrors":                    baseEnv.AnnasBaseURLs,
				"primary_annas_mirror":             baseEnv.AnnasBaseURL,
				"default_secret_configured":        env.HasDefaultSecretKey(),
				"default_download_path_configured": env.HasDefaultDownloadPath(),
				"inline_download_max_bytes":        env.GetMaxInlineDownloadBytes(),
			}

			if connectorURL := httpEnv.ConnectorURL(); connectorURL != "" {
				payload["chatgpt_connector_url"] = connectorURL
			}
			if httpEnv.PublicBaseURL != "" {
				payload["public_base_url"] = httpEnv.PublicBaseURL
			}
			if !httpEnv.ChatGPTCompatibleAuth() {
				payload["chatgpt_auth_note"] = "Set ANNAS_HTTP_AUTH_MODE=none for direct ChatGPT MCP use, or put an OAuth-capable gateway in front of this server."
			}

			writeJSON(w, http.StatusOK, payload)
		})
	}

	srv := &http.Server{
		Addr:              httpEnv.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	l.Info("Starting MCP HTTP server",
		zap.String("name", "annas-mcp"),
		zap.String("version", serverVersion),
		zap.String("transport", "streamable-http"),
		zap.String("addr", httpEnv.Addr),
		zap.String("path", httpEnv.Path),
		zap.String("authMode", string(httpEnv.AuthMode)),
		zap.Bool("chatgptAuthCompatible", httpEnv.ChatGPTCompatibleAuth()),
		zap.Bool("bookDownloadEnabled", true),
		zap.Bool("articleDownloadEnabled", true),
		zap.Bool("defaultSecretConfigured", env.HasDefaultSecretKey()),
		zap.Bool("defaultDownloadPathConfigured", env.HasDefaultDownloadPath()),
		zap.Strings("annasMirrors", baseEnv.AnnasBaseURLs),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.Error("Failed to shut down MCP HTTP server cleanly", zap.Error(err))
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		l.Fatal("MCP HTTP server failed", zap.Error(err))
	}

	l.Info("MCP HTTP server stopped")
}

func withConfiguredAuth(httpEnv *env.HTTPEnv, next http.Handler) http.Handler {
	switch httpEnv.AuthMode {
	case env.HTTPAuthModeBearer:
		return withBearerAuth(httpEnv.BearerToken, next)
	default:
		return next
	}
}

func withBearerAuth(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="annas-mcp"`)
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		gotToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(gotToken), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="annas-mcp"`)
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func exposedToolNames() []string {
	return []string{"book_search", "article_search", "book_download", "article_download"}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
