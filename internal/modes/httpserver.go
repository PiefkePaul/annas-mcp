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

	httpEnv := env.GetHTTPEnv()
	serverVersion := version.GetVersion()
	server := newMCPServer(serverVersion)

	mux := http.NewServeMux()
	mux.Handle(httpEnv.Path, withBearerAuth(httpEnv.BearerToken, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
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

			writeJSON(w, http.StatusOK, map[string]any{
				"name":                "annas-mcp",
				"version":             serverVersion,
				"transport":           "streamable-http",
				"mcp_endpoint":        httpEnv.Path,
				"health_endpoint":     "/healthz",
				"bearer_auth_enabled": httpEnv.BearerToken != "",
			})
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
		zap.Bool("bearerAuthEnabled", httpEnv.BearerToken != ""),
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}



