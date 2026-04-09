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

	"github.com/PiefkePaul/annas-mcp/internal/auth"
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

	var authManager *auth.Manager
	if httpEnv.AuthMode == env.HTTPAuthModeOAuth {
		authEnv, err := env.GetAuthEnv()
		if err != nil {
			l.Fatal("Invalid OAuth/auth environment", zap.Error(err))
		}

		authManager, err = auth.NewManager(auth.Config{
			StorePath:            authEnv.StorePath,
			MasterKey:            authEnv.MasterKey,
			AccessTokenTTL:       authEnv.AccessTokenTTL,
			RefreshTokenTTL:      authEnv.RefreshTokenTTL,
			AuthorizationCodeTTL: authEnv.AuthorizationCodeTTL,
			SessionTTL:           authEnv.SessionTTL,
			MCPPath:              httpEnv.Path,
		})
		if err != nil {
			l.Fatal("Failed to initialize OAuth/auth manager", zap.Error(err))
		}
	}

	serverVersion := version.GetVersion()
	availableTools := exposedToolNames()
	baseEnv := env.GetBaseEnv()
	downloadStore := newEphemeralDownloadStore(defaultEphemeralDownloadTTL)

	if !httpEnv.ChatGPTCompatibleAuth() {
		l.Warn("Bearer auth is enabled. ChatGPT MCP connectors currently expect no auth or OAuth rather than a custom bearer token.",
			zap.String("authMode", string(httpEnv.AuthMode)),
		)
	}

	mux := http.NewServeMux()
	mux.Handle(httpEnv.Path, withConfiguredAuth(httpEnv, authManager, mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return newMCPServer(
			serverVersion,
			auth.IdentityFromContext(r.Context()),
			downloadStore,
			effectiveBaseURL(httpEnv, r),
			false,
		)
	}, nil)))
	mux.HandleFunc("/downloads/", downloadStore.HandleDownload)

	if authManager != nil {
		mux.HandleFunc("/.well-known/oauth-protected-resource", authManager.HandleProtectedResourceMetadata)
		mux.HandleFunc("/.well-known/oauth-authorization-server", authManager.HandleAuthorizationServerMetadata)
		mux.HandleFunc("/.well-known/openid-configuration", authManager.HandleOpenIDConfiguration)
		mux.HandleFunc("/register", authManager.HandleClientRegistration)
		mux.HandleFunc("/authorize", authManager.HandleAuthorize)
		mux.HandleFunc("/token", authManager.HandleToken)
		mux.HandleFunc("/account/register", authManager.HandleAccountRegister)
		mux.HandleFunc("/account/login", authManager.HandleAccountLogin)
		mux.HandleFunc("/account/logout", authManager.HandleAccountLogout)
		mux.HandleFunc("/account", authManager.HandleAccount)
	}

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
				"oauth_enabled":                    authManager != nil,
				"temporary_download_links_enabled": true,
				"embedded_downloads_enabled":       false,
			}

			if connectorURL := httpEnv.ConnectorURL(); connectorURL != "" {
				payload["chatgpt_connector_url"] = connectorURL
			}
			if httpEnv.PublicBaseURL != "" {
				payload["public_base_url"] = httpEnv.PublicBaseURL
			}
			if httpEnv.AuthMode == env.HTTPAuthModeBearer {
				payload["chatgpt_auth_note"] = "Bearer mode is mainly for simple shared-token clients. Use ANNAS_HTTP_AUTH_MODE=oauth for per-user sign-in from ChatGPT or Claude."
			}
			if httpEnv.AuthMode == env.HTTPAuthModeOAuth {
				payload["chatgpt_auth_note"] = "OAuth mode is enabled. Users should create an account in /account, save their Anna's Archive secret there, and then connect the MCP server from the client."
			}
			if authManager != nil {
				payload["account_portal"] = "/account"
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
		zap.Bool("oauthEnabled", authManager != nil),
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

func withConfiguredAuth(httpEnv *env.HTTPEnv, authManager *auth.Manager, next http.Handler) http.Handler {
	switch httpEnv.AuthMode {
	case env.HTTPAuthModeBearer:
		return withBearerAuth(httpEnv.BearerToken, next)
	case env.HTTPAuthModeOAuth:
		return withOAuthBearerAuth(authManager, httpEnv.Path, next)
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

func withOAuthBearerAuth(authManager *auth.Manager, mcpPath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authManager == nil {
			http.Error(w, "oauth auth is not configured", http.StatusInternalServerError)
			return
		}

		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", authManager.ChallengeHeader(r))
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		resource := auth.ResourceURLForRequest(r, mcpPath)
		identity, err := authManager.ValidateAccessToken(token, resource)
		if err != nil {
			w.Header().Set("WWW-Authenticate", authManager.ChallengeHeader(r))
			http.Error(w, "invalid or expired access token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
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

func effectiveBaseURL(httpEnv *env.HTTPEnv, r *http.Request) string {
	if httpEnv != nil && strings.TrimSpace(httpEnv.PublicBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(httpEnv.PublicBaseURL), "/")
	}
	return auth.BaseURLFromRequest(r)
}
