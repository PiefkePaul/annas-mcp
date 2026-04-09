package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "annas_mcp_session"
	csrfCookieName    = "annas_mcp_csrf"
)

var (
	ErrNotAuthenticated = errors.New("not authenticated")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrInvalidClient    = errors.New("invalid client")
	ErrInvalidGrant     = errors.New("invalid grant")
)

type Config struct {
	StorePath            string
	MasterKey            []byte
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	AuthorizationCodeTTL time.Duration
	SessionTTL           time.Duration
	MCPPath              string
}

type Manager struct {
	cfg Config

	mu   sync.Mutex
	data *storeData
}

type Identity struct {
	UserID    string
	Email     string
	SecretKey string
}

type contextKey string

const identityContextKey contextKey = "annas_mcp_identity"

type storeData struct {
	Users         map[string]*userRecord         `json:"users"`
	EmailIndex    map[string]string              `json:"email_index"`
	Sessions      map[string]*sessionRecord      `json:"sessions"`
	Clients       map[string]*clientRecord       `json:"clients"`
	AuthCodes     map[string]*authCodeRecord     `json:"auth_codes"`
	AccessTokens  map[string]*accessTokenRecord  `json:"access_tokens"`
	RefreshTokens map[string]*refreshTokenRecord `json:"refresh_tokens"`
}

type encryptedStoreFile struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type userRecord struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"password_hash"`
	SecretKey    string `json:"secret_key"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type sessionRecord struct {
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type clientRecord struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	CreatedAt               int64    `json:"created_at"`
}

type authCodeRecord struct {
	Code                string `json:"code"`
	ClientID            string `json:"client_id"`
	UserID              string `json:"user_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	ExpiresAt           int64  `json:"expires_at"`
}

type accessTokenRecord struct {
	Token     string `json:"token"`
	ClientID  string `json:"client_id"`
	UserID    string `json:"user_id"`
	Scope     string `json:"scope"`
	Resource  string `json:"resource"`
	ExpiresAt int64  `json:"expires_at"`
	IssuedAt  int64  `json:"issued_at"`
}

type refreshTokenRecord struct {
	Token     string `json:"token"`
	ClientID  string `json:"client_id"`
	UserID    string `json:"user_id"`
	Scope     string `json:"scope"`
	Resource  string `json:"resource"`
	ExpiresAt int64  `json:"expires_at"`
	IssuedAt  int64  `json:"issued_at"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type authorizeParams struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	State               string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
}

func NewManager(cfg Config) (*Manager, error) {
	m := &Manager{
		cfg: cfg,
		data: &storeData{
			Users:         make(map[string]*userRecord),
			EmailIndex:    make(map[string]string),
			Sessions:      make(map[string]*sessionRecord),
			Clients:       make(map[string]*clientRecord),
			AuthCodes:     make(map[string]*authCodeRecord),
			AccessTokens:  make(map[string]*accessTokenRecord),
			RefreshTokens: make(map[string]*refreshTokenRecord),
		},
	}

	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	payload, err := os.ReadFile(m.cfg.StorePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read auth store: %w", err)
	}

	var stored encryptedStoreFile
	if err := json.Unmarshal(payload, &stored); err != nil {
		return fmt.Errorf("failed to decode auth store metadata: %w", err)
	}

	nonce, err := base64.RawStdEncoding.DecodeString(stored.Nonce)
	if err != nil {
		return fmt.Errorf("failed to decode auth store nonce: %w", err)
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(stored.Ciphertext)
	if err != nil {
		return fmt.Errorf("failed to decode auth store ciphertext: %w", err)
	}

	plaintext, err := decrypt(m.cfg.MasterKey, nonce, ciphertext)
	if err != nil {
		return fmt.Errorf("failed to decrypt auth store: %w", err)
	}

	var data storeData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return fmt.Errorf("failed to decode auth store: %w", err)
	}

	if data.Users == nil {
		data.Users = make(map[string]*userRecord)
	}
	if data.EmailIndex == nil {
		data.EmailIndex = make(map[string]string)
	}
	if data.Sessions == nil {
		data.Sessions = make(map[string]*sessionRecord)
	}
	if data.Clients == nil {
		data.Clients = make(map[string]*clientRecord)
	}
	if data.AuthCodes == nil {
		data.AuthCodes = make(map[string]*authCodeRecord)
	}
	if data.AccessTokens == nil {
		data.AccessTokens = make(map[string]*accessTokenRecord)
	}
	if data.RefreshTokens == nil {
		data.RefreshTokens = make(map[string]*refreshTokenRecord)
	}

	m.data = &data
	m.cleanupLocked(time.Now())
	return nil
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.cfg.StorePath), 0o755); err != nil {
		return fmt.Errorf("failed to create auth store directory: %w", err)
	}

	plaintext, err := json.Marshal(m.data)
	if err != nil {
		return fmt.Errorf("failed to encode auth store: %w", err)
	}

	nonce, ciphertext, err := encrypt(m.cfg.MasterKey, plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt auth store: %w", err)
	}

	stored := encryptedStoreFile{
		Version:    1,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}

	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode auth store file: %w", err)
	}

	tempPath := m.cfg.StorePath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write auth store: %w", err)
	}
	if err := os.Rename(tempPath, m.cfg.StorePath); err != nil {
		return fmt.Errorf("failed to replace auth store: %w", err)
	}
	return nil
}

func (m *Manager) cleanupLocked(now time.Time) {
	nowUnix := now.Unix()

	for token, session := range m.data.Sessions {
		if session.ExpiresAt <= nowUnix {
			delete(m.data.Sessions, token)
		}
	}
	for code, record := range m.data.AuthCodes {
		if record.ExpiresAt <= nowUnix {
			delete(m.data.AuthCodes, code)
		}
	}
	for token, record := range m.data.AccessTokens {
		if record.ExpiresAt <= nowUnix {
			delete(m.data.AccessTokens, token)
		}
	}
	for token, record := range m.data.RefreshTokens {
		if record.ExpiresAt <= nowUnix {
			delete(m.data.RefreshTokens, token)
		}
	}
}

func (m *Manager) ChallengeHeader(r *http.Request) string {
	prmURL := baseURLFromRequest(r) + "/.well-known/oauth-protected-resource"
	return fmt.Sprintf(`Bearer realm="annas-mcp", resource_metadata="%s"`, prmURL)
}

func (m *Manager) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}

	baseURL := baseURLFromRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":              baseURL + m.cfg.MCPPath,
		"authorization_servers": []string{baseURL},
		"scopes_supported":      []string{"mcp"},
	})
}

func (m *Manager) HandleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}

	baseURL := baseURLFromRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/authorize",
		"token_endpoint":                        baseURL + "/token",
		"registration_endpoint":                 baseURL + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"mcp"},
	})
}

func (m *Manager) HandleOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	m.HandleAuthorizationServerMetadata(w, r)
}

func (m *Manager) HandleClientRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON payload")
		return
	}

	client, err := m.registerClient(payload.ClientName, payload.RedirectURIs, payload.GrantTypes, payload.ResponseTypes, payload.TokenEndpointAuthMethod)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"client_id_issued_at":        client.CreatedAt,
	})
}

func (m *Manager) registerClient(name string, redirectURIs, grantTypes, responseTypes []string, authMethod string) (*clientRecord, error) {
	if strings.TrimSpace(name) == "" {
		name = "MCP Client"
	}
	if len(redirectURIs) == 0 {
		return nil, fmt.Errorf("redirect_uris must not be empty")
	}
	for _, redirectURI := range redirectURIs {
		if err := validateRedirectURI(redirectURI); err != nil {
			return nil, err
		}
	}

	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		return nil, fmt.Errorf("only token_endpoint_auth_method=none is supported")
	}

	now := time.Now()
	client := &clientRecord{
		ID:                      randomToken(24),
		Name:                    name,
		RedirectURIs:            dedupeStrings(redirectURIs),
		GrantTypes:              dedupeStrings(grantTypes),
		ResponseTypes:           dedupeStrings(responseTypes),
		TokenEndpointAuthMethod: authMethod,
		CreatedAt:               now.Unix(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	m.data.Clients[client.ID] = client
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return client, nil
}

func (m *Manager) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.handleAuthorizeGet(w, r)
	case http.MethodPost:
		m.handleAuthorizePost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) handleAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	params, client, err := m.parseAuthorizeRequest(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	identity, _ := m.identityFromSession(r)
	if identity == nil {
		http.Redirect(w, r, "/account/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}

	csrfToken := ensureCSRFCookie(w, r)
	renderHTML(w, authorizeTemplate, map[string]any{
		"ClientName":          client.Name,
		"Resource":            params.Resource,
		"Scope":               params.Scope,
		"State":               params.State,
		"CSRFToken":           csrfToken,
		"ClientID":            params.ClientID,
		"RedirectURI":         params.RedirectURI,
		"ResponseType":        params.ResponseType,
		"CodeChallenge":       params.CodeChallenge,
		"CodeChallengeMethod": params.CodeChallengeMethod,
		"ResourceValue":       params.Resource,
		"ScopeValue":          params.Scope,
		"Email":               identity.Email,
	})
}

func (m *Manager) handleAuthorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !validateCSRFCookie(r) {
		http.Error(w, "invalid CSRF token", http.StatusBadRequest)
		return
	}

	identity, err := m.identityFromSession(r)
	if err != nil {
		http.Redirect(w, r, "/account/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}

	values := url.Values{}
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "state", "scope", "resource", "code_challenge", "code_challenge_method"} {
		values.Set(key, r.FormValue(key))
	}

	params, client, err := m.parseAuthorizeRequest(values)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if r.FormValue("action") != "approve" {
		target, buildErr := buildOAuthErrorRedirectURL(params.RedirectURI, "access_denied", params.State)
		if buildErr != nil {
			http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
			return
		}
		logger.GetLogger().Info("Denied OAuth authorization request",
			zap.String("clientID", client.ID),
			zap.String("userID", identity.UserID),
			zap.String("redirectURI", params.RedirectURI),
			zap.String("redirectTarget", target),
		)
		completeBrowserRedirect(w, r, target, "Access denied")
		return
	}

	code, err := m.createAuthorizationCode(identity.UserID, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger.GetLogger().Info("Issued OAuth authorization code",
		zap.String("clientID", client.ID),
		zap.String("userID", identity.UserID),
		zap.String("resource", params.Resource),
		zap.String("redirectURI", params.RedirectURI),
	)

	target, err := buildOAuthCodeRedirectURL(params.RedirectURI, code, params.State)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	logger.GetLogger().Info("Redirecting OAuth authorization response",
		zap.String("clientID", client.ID),
		zap.String("userID", identity.UserID),
		zap.String("redirectTarget", target),
	)
	completeBrowserRedirect(w, r, target, "Authorization complete")
}

func (m *Manager) parseAuthorizeRequest(values url.Values) (*authorizeParams, *clientRecord, error) {
	params := &authorizeParams{
		ClientID:            strings.TrimSpace(values.Get("client_id")),
		RedirectURI:         strings.TrimSpace(values.Get("redirect_uri")),
		ResponseType:        strings.TrimSpace(values.Get("response_type")),
		State:               strings.TrimSpace(values.Get("state")),
		Scope:               strings.TrimSpace(values.Get("scope")),
		Resource:            strings.TrimSpace(values.Get("resource")),
		CodeChallenge:       strings.TrimSpace(values.Get("code_challenge")),
		CodeChallengeMethod: strings.TrimSpace(values.Get("code_challenge_method")),
	}

	if params.ClientID == "" || params.RedirectURI == "" {
		return nil, nil, fmt.Errorf("client_id and redirect_uri are required")
	}
	if params.ResponseType != "code" {
		return nil, nil, fmt.Errorf("response_type must be code")
	}
	if params.Resource == "" {
		return nil, nil, fmt.Errorf("resource is required")
	}
	if params.CodeChallenge == "" || params.CodeChallengeMethod != "S256" {
		return nil, nil, fmt.Errorf("PKCE with code_challenge_method=S256 is required")
	}

	client, err := m.lookupClient(params.ClientID)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(client.RedirectURIs, params.RedirectURI) {
		return nil, nil, fmt.Errorf("redirect_uri is not registered for this client")
	}
	if err := validateAbsoluteResource(params.Resource); err != nil {
		return nil, nil, err
	}

	if params.Scope == "" {
		params.Scope = "mcp"
	}

	return params, client, nil
}

func (m *Manager) lookupClient(clientID string) (*clientRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())

	client, ok := m.data.Clients[clientID]
	if !ok {
		return nil, ErrInvalidClient
	}
	return client, nil
}

func (m *Manager) createAuthorizationCode(userID string, params *authorizeParams) (string, error) {
	now := time.Now()
	record := &authCodeRecord{
		Code:                randomToken(32),
		ClientID:            params.ClientID,
		UserID:              userID,
		RedirectURI:         params.RedirectURI,
		Scope:               params.Scope,
		Resource:            params.Resource,
		CodeChallenge:       params.CodeChallenge,
		CodeChallengeMethod: params.CodeChallengeMethod,
		ExpiresAt:           now.Add(m.cfg.AuthorizationCodeTTL).Unix(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	m.data.AuthCodes[record.Code] = record
	return record.Code, m.saveLocked()
}

func (m *Manager) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}

	setNoStoreHeaders(w)

	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}

	switch r.FormValue("grant_type") {
	case "authorization_code":
		m.handleAuthorizationCodeExchange(w, r)
	case "refresh_token":
		m.handleRefreshTokenExchange(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

func (m *Manager) handleAuthorizationCodeExchange(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	code := strings.TrimSpace(r.FormValue("code"))
	redirectURI := strings.TrimSpace(r.FormValue("redirect_uri"))
	codeVerifier := strings.TrimSpace(r.FormValue("code_verifier"))
	resource := strings.TrimSpace(r.FormValue("resource"))

	if clientID == "" || code == "" || redirectURI == "" || codeVerifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id, code, redirect_uri and code_verifier are required")
		return
	}

	response, err := m.exchangeAuthorizationCode(clientID, code, redirectURI, codeVerifier, resource)
	if err != nil {
		logger.GetLogger().Warn("OAuth authorization code exchange failed",
			zap.String("clientID", clientID),
			zap.String("redirectURI", redirectURI),
			zap.String("resource", resource),
			zap.Error(err),
		)
		switch {
		case errors.Is(err, ErrInvalidClient):
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		case errors.Is(err, ErrInvalidGrant), errors.Is(err, ErrTokenExpired):
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		default:
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return
	}

	logger.GetLogger().Info("OAuth authorization code exchanged successfully",
		zap.String("clientID", clientID),
		zap.String("redirectURI", redirectURI),
		zap.String("resource", resource),
	)
	writeJSON(w, http.StatusOK, response)
}

func (m *Manager) exchangeAuthorizationCode(clientID, code, redirectURI, codeVerifier, resource string) (*tokenResponse, error) {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	client, ok := m.data.Clients[clientID]
	if !ok {
		return nil, ErrInvalidClient
	}
	record, ok := m.data.AuthCodes[code]
	if !ok {
		return nil, ErrInvalidGrant
	}
	if record.ExpiresAt <= now.Unix() {
		delete(m.data.AuthCodes, code)
		_ = m.saveLocked()
		return nil, ErrTokenExpired
	}
	if record.ClientID != client.ID || record.RedirectURI != redirectURI {
		return nil, ErrInvalidGrant
	}
	if resource != "" && normalizeResource(resource) != normalizeResource(record.Resource) {
		return nil, ErrInvalidGrant
	}
	if !validatePKCE(record.CodeChallenge, codeVerifier) {
		return nil, ErrInvalidGrant
	}

	delete(m.data.AuthCodes, code)
	tokenSet := m.issueTokenSetLocked(now, record.UserID, client.ID, record.Scope, record.Resource)
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return tokenSet, nil
}

func (m *Manager) handleRefreshTokenExchange(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	refreshToken := strings.TrimSpace(r.FormValue("refresh_token"))
	resource := strings.TrimSpace(r.FormValue("resource"))

	if clientID == "" || refreshToken == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id and refresh_token are required")
		return
	}

	response, err := m.refreshToken(clientID, refreshToken, resource)
	if err != nil {
		logger.GetLogger().Warn("OAuth refresh token exchange failed",
			zap.String("clientID", clientID),
			zap.String("resource", resource),
			zap.Error(err),
		)
		switch {
		case errors.Is(err, ErrInvalidClient):
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		case errors.Is(err, ErrInvalidGrant), errors.Is(err, ErrTokenExpired):
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		default:
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return
	}

	logger.GetLogger().Info("OAuth refresh token exchange succeeded",
		zap.String("clientID", clientID),
		zap.String("resource", resource),
	)
	writeJSON(w, http.StatusOK, response)
}

func (m *Manager) refreshToken(clientID, refreshToken, resource string) (*tokenResponse, error) {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	client, ok := m.data.Clients[clientID]
	if !ok {
		return nil, ErrInvalidClient
	}
	record, ok := m.data.RefreshTokens[refreshToken]
	if !ok {
		return nil, ErrInvalidGrant
	}
	if record.ExpiresAt <= now.Unix() {
		delete(m.data.RefreshTokens, refreshToken)
		_ = m.saveLocked()
		return nil, ErrTokenExpired
	}
	if record.ClientID != client.ID {
		return nil, ErrInvalidGrant
	}
	if resource != "" && normalizeResource(resource) != normalizeResource(record.Resource) {
		return nil, ErrInvalidGrant
	}

	delete(m.data.RefreshTokens, refreshToken)
	tokenSet := m.issueTokenSetLocked(now, record.UserID, client.ID, record.Scope, record.Resource)
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return tokenSet, nil
}

func (m *Manager) issueTokenSetLocked(now time.Time, userID, clientID, scope, resource string) *tokenResponse {
	accessToken := randomToken(32)
	refreshToken := randomToken(48)

	m.data.AccessTokens[accessToken] = &accessTokenRecord{
		Token:     accessToken,
		ClientID:  clientID,
		UserID:    userID,
		Scope:     scope,
		Resource:  resource,
		ExpiresAt: now.Add(m.cfg.AccessTokenTTL).Unix(),
		IssuedAt:  now.Unix(),
	}
	m.data.RefreshTokens[refreshToken] = &refreshTokenRecord{
		Token:     refreshToken,
		ClientID:  clientID,
		UserID:    userID,
		Scope:     scope,
		Resource:  resource,
		ExpiresAt: now.Add(m.cfg.RefreshTokenTTL).Unix(),
		IssuedAt:  now.Unix(),
	}

	return &tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(m.cfg.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	}
}

func (m *Manager) ValidateAccessToken(token, resource string) (*Identity, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	record, ok := m.data.AccessTokens[token]
	if !ok {
		return nil, ErrInvalidToken
	}
	if record.ExpiresAt <= now.Unix() {
		delete(m.data.AccessTokens, token)
		_ = m.saveLocked()
		return nil, ErrTokenExpired
	}
	if normalizeResource(record.Resource) != normalizeResource(resource) {
		return nil, ErrInvalidToken
	}

	user, ok := m.data.Users[record.UserID]
	if !ok {
		return nil, ErrInvalidToken
	}

	return &Identity{
		UserID:    user.ID,
		Email:     user.Email,
		SecretKey: user.SecretKey,
	}, nil
}

func (m *Manager) HandleAccountRegister(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		csrfToken := ensureCSRFCookie(w, r)
		renderHTML(w, registerTemplate, map[string]any{
			"CSRFToken": csrfToken,
			"Next":      r.URL.Query().Get("next"),
		})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if !validateCSRFCookie(r) {
			http.Error(w, "invalid CSRF token", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		secret := r.FormValue("secret_key")
		next := sanitizeNext(r.FormValue("next"))

		user, err := m.registerUser(email, password, secret)
		if err != nil {
			renderHTML(w, registerTemplate, map[string]any{
				"CSRFToken": ensureCSRFCookie(w, r),
				"Next":      next,
				"Error":     err.Error(),
				"Email":     email,
			})
			return
		}
		if err := m.startSession(w, r, user.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if next == "" {
			next = "/account"
		}
		http.Redirect(w, r, next, http.StatusFound)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) HandleAccountLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderHTML(w, loginTemplate, map[string]any{
			"CSRFToken": ensureCSRFCookie(w, r),
			"Next":      r.URL.Query().Get("next"),
		})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if !validateCSRFCookie(r) {
			http.Error(w, "invalid CSRF token", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")
		next := sanitizeNext(r.FormValue("next"))

		user, err := m.authenticateUser(email, password)
		if err != nil {
			renderHTML(w, loginTemplate, map[string]any{
				"CSRFToken": ensureCSRFCookie(w, r),
				"Next":      next,
				"Error":     "Invalid email or password",
				"Email":     email,
			})
			return
		}
		if err := m.startSession(w, r, user.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if next == "" {
			next = "/account"
		}
		http.Redirect(w, r, next, http.StatusFound)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) HandleAccountLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}
	if !validateCSRFCookie(r) {
		http.Error(w, "invalid CSRF token", http.StatusBadRequest)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = m.deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/account/login", http.StatusFound)
}

func (m *Manager) HandleAccount(w http.ResponseWriter, r *http.Request) {
	identity, err := m.identityFromSession(r)
	if err != nil {
		http.Redirect(w, r, "/account/login?next="+url.QueryEscape("/account"), http.StatusFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		renderHTML(w, accountTemplate, map[string]any{
			"CSRFToken":        ensureCSRFCookie(w, r),
			"Email":            identity.Email,
			"SecretConfigured": strings.TrimSpace(identity.SecretKey) != "",
		})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if !validateCSRFCookie(r) {
			http.Error(w, "invalid CSRF token", http.StatusBadRequest)
			return
		}
		if err := m.updateUserSecret(identity.UserID, strings.TrimSpace(r.FormValue("secret_key"))); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderHTML(w, accountTemplate, map[string]any{
			"CSRFToken":        ensureCSRFCookie(w, r),
			"Email":            identity.Email,
			"SecretConfigured": strings.TrimSpace(r.FormValue("secret_key")) != "",
			"Success":          "Secret saved successfully.",
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) registerUser(email, password, secret string) (*userRecord, error) {
	email = normalizeEmail(email)
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("please provide a valid email address")
	}
	if len(password) < 10 {
		return nil, fmt.Errorf("password must be at least 10 characters long")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now()
	user := &userRecord{
		ID:           randomToken(18),
		Email:        email,
		PasswordHash: string(hash),
		SecretKey:    strings.TrimSpace(secret),
		CreatedAt:    now.Unix(),
		UpdatedAt:    now.Unix(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	if _, exists := m.data.EmailIndex[email]; exists {
		return nil, fmt.Errorf("a user with this email already exists")
	}
	m.data.Users[user.ID] = user
	m.data.EmailIndex[email] = user.ID
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return user, nil
}

func (m *Manager) authenticateUser(email, password string) (*userRecord, error) {
	email = normalizeEmail(email)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())

	userID, ok := m.data.EmailIndex[email]
	if !ok {
		return nil, ErrNotAuthenticated
	}
	user, ok := m.data.Users[userID]
	if !ok {
		return nil, ErrNotAuthenticated
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrNotAuthenticated
	}
	return user, nil
}

func (m *Manager) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	now := time.Now()
	session := &sessionRecord{
		Token:     randomToken(32),
		UserID:    userID,
		ExpiresAt: now.Add(m.cfg.SessionTTL).Unix(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	m.data.Sessions[session.Token] = session
	if err := m.saveLocked(); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(session.ExpiresAt, 0),
	})
	return nil
}

func (m *Manager) deleteSession(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data.Sessions, token)
	return m.saveLocked()
}

func (m *Manager) identityFromSession(r *http.Request) (*Identity, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, ErrNotAuthenticated
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)

	session, ok := m.data.Sessions[cookie.Value]
	if !ok {
		return nil, ErrNotAuthenticated
	}
	if session.ExpiresAt <= now.Unix() {
		delete(m.data.Sessions, cookie.Value)
		_ = m.saveLocked()
		return nil, ErrNotAuthenticated
	}
	user, ok := m.data.Users[session.UserID]
	if !ok {
		return nil, ErrNotAuthenticated
	}
	return &Identity{
		UserID:    user.ID,
		Email:     user.Email,
		SecretKey: user.SecretKey,
	}, nil
}

func (m *Manager) updateUserSecret(userID, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.data.Users[userID]
	if !ok {
		return ErrNotAuthenticated
	}
	user.SecretKey = strings.TrimSpace(secret)
	user.UpdatedAt = time.Now().Unix()
	return m.saveLocked()
}

func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, identity)
}

func IdentityFromContext(ctx context.Context) *Identity {
	value := ctx.Value(identityContextKey)
	identity, _ := value.(*Identity)
	return identity
}

func ResourceURLForRequest(r *http.Request, mcpPath string) string {
	return baseURLFromRequest(r) + mcpPath
}

func baseURLFromRequest(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}

	return scheme + "://" + host
}

func requestIsSecure(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") || r.TLS != nil
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid redirect_uri: %s", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {
		return nil
	}
	return fmt.Errorf("redirect_uri must use https or localhost http")
}

func validateAbsoluteResource(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("resource must be an absolute URL")
	}
	return nil
}

func normalizeResource(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return strings.TrimRight(u.String(), "/")
}

func validatePKCE(codeChallenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	calculated := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtleConstantTimeEqual(calculated, codeChallenge)
}

func subtleConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func encrypt(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}

	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}

func decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func randomToken(numBytes int) string {
	buf := make([]byte, numBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}

	token := randomToken(18)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func validateCSRFCookie(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	formToken := strings.TrimSpace(r.FormValue("csrf_token"))
	return subtleConstantTimeEqual(cookie.Value, formToken)
}

func sanitizeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "http://") || strings.HasPrefix(next, "https://") {
		return ""
	}
	if !strings.HasPrefix(next, "/") {
		return ""
	}
	return next
}

func buildOAuthCodeRedirectURL(redirectURI, code, state string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func buildOAuthErrorRedirectURL(redirectURI, errCode, state string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("error", errCode)
	if state != "" {
		query.Set("state", state)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func completeBrowserRedirect(w http.ResponseWriter, r *http.Request, targetURL, title string) {
	renderRedirectHTML(w, title, targetURL)
}

func writeOAuthError(w http.ResponseWriter, status int, errCode, description string) {
	writeJSON(w, status, map[string]any{
		"error":             errCode,
		"error_description": description,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func renderHTML(w http.ResponseWriter, tmpl string, data map[string]any) {
	t := template.Must(template.New("page").Parse(layoutTemplate + tmpl))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; img-src 'self' data:; frame-ancestors 'none'")
	_ = t.Execute(w, data)
}

func renderRedirectHTML(w http.ResponseWriter, title, targetURL string) {
	t := template.Must(template.New("redirect").Parse(redirectTemplate))
	w.Header().Set("Location", targetURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	_ = t.Execute(w, map[string]any{
		"Title":     title,
		"TargetURL": targetURL,
	})
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

const layoutTemplate = `
{{define "layout"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Anna's MCP Auth</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; background: #f7f7f5; color: #1f2937; margin: 0; }
    main { max-width: 720px; margin: 48px auto; background: white; padding: 32px; border-radius: 18px; box-shadow: 0 18px 40px rgba(0,0,0,.08); }
    h1 { margin-top: 0; }
    label { display: block; margin: 16px 0 6px; font-weight: 600; }
    input { width: 100%; box-sizing: border-box; padding: 12px 14px; border-radius: 12px; border: 1px solid #d1d5db; font: inherit; }
    button { margin-top: 20px; padding: 12px 18px; border: 0; border-radius: 12px; background: #111827; color: white; font: inherit; cursor: pointer; }
    .secondary { background: #e5e7eb; color: #111827; }
    .row { display: flex; gap: 12px; flex-wrap: wrap; }
    .notice { background: #eef6ff; color: #1d4ed8; padding: 12px 14px; border-radius: 12px; margin: 16px 0; }
    .error { background: #fef2f2; color: #b91c1c; padding: 12px 14px; border-radius: 12px; margin: 16px 0; }
    .muted { color: #6b7280; }
    code { background: #f3f4f6; padding: 2px 6px; border-radius: 6px; }
    a { color: #1d4ed8; }
  </style>
</head>
<body>
  <main>
    {{template "content" .}}
  </main>
</body>
</html>
{{end}}
`

const registerTemplate = `
{{define "content"}}
<h1>Create account</h1>
<p class="muted">Register once, save your Anna's Archive secret securely, and then sign in with OAuth from ChatGPT or Claude.</p>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<form method="post">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="next" value="{{.Next}}">
  <label for="email">Email</label>
  <input id="email" name="email" type="email" required value="{{.Email}}">
  <label for="password">Password</label>
  <input id="password" name="password" type="password" required minlength="10">
  <label for="secret_key">Anna's Archive secret key</label>
  <input id="secret_key" name="secret_key" type="password" autocomplete="off">
  <button type="submit">Create account</button>
</form>
<p class="muted">Already registered? <a href="/account/login{{if .Next}}?next={{.Next}}{{end}}">Sign in</a></p>
{{end}}
{{template "layout" .}}
`

const loginTemplate = `
{{define "content"}}
<h1>Sign in</h1>
<p class="muted">Use your account to authorize ChatGPT or Claude and attach your saved Anna's Archive secret automatically.</p>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<form method="post">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="next" value="{{.Next}}">
  <label for="email">Email</label>
  <input id="email" name="email" type="email" required value="{{.Email}}">
  <label for="password">Password</label>
  <input id="password" name="password" type="password" required>
  <button type="submit">Sign in</button>
</form>
<p class="muted">No account yet? <a href="/account/register{{if .Next}}?next={{.Next}}{{end}}">Register here</a></p>
{{end}}
{{template "layout" .}}
`

const accountTemplate = `
{{define "content"}}
<h1>Account</h1>
<p>Signed in as <strong>{{.Email}}</strong></p>
{{if .Success}}<div class="notice">{{.Success}}</div>{{end}}
<div class="notice">Stored Anna secret: {{if .SecretConfigured}}configured{{else}}not set{{end}}</div>
<form method="post">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <label for="secret_key">Anna's Archive secret key</label>
  <input id="secret_key" name="secret_key" type="password" autocomplete="off" placeholder="Paste or replace your secret">
  <button type="submit">Save secret</button>
</form>
<form method="post" action="/account/logout">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <button type="submit" class="secondary">Sign out</button>
</form>
{{end}}
{{template "layout" .}}
`

const authorizeTemplate = `
{{define "content"}}
<h1>Authorize access</h1>
<p><strong>{{.ClientName}}</strong> wants to connect to your Anna's MCP account as <strong>{{.Email}}</strong>.</p>
<div class="notice">
  Resource: <code>{{.Resource}}</code><br>
  Scope: <code>{{.Scope}}</code>
</div>
<p class="muted">If you approve, the client will get OAuth tokens for this MCP server and your saved Anna's Archive secret can be used server-side without appearing in the chat.</p>
<form method="post">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="response_type" value="{{.ResponseType}}">
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="scope" value="{{.ScopeValue}}">
  <input type="hidden" name="resource" value="{{.ResourceValue}}">
  <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
  <div class="row">
    <button type="submit" name="action" value="approve">Approve</button>
    <button type="submit" name="action" value="deny" class="secondary">Deny</button>
  </div>
</form>
{{end}}
{{template "layout" .}}
`

const redirectTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <meta http-equiv="refresh" content="0;url={{.TargetURL}}">
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; background: #f7f7f5; color: #1f2937; margin: 0; }
    main { max-width: 720px; margin: 48px auto; background: white; padding: 32px; border-radius: 18px; box-shadow: 0 18px 40px rgba(0,0,0,.08); }
    a { color: #1d4ed8; }
    code { background: #f3f4f6; padding: 2px 6px; border-radius: 6px; word-break: break-all; }
    .muted { color: #6b7280; }
  </style>
  <script>
    (function () {
      var target = {{printf "%q" .TargetURL}};
      try { window.location.replace(target); } catch (e) {}
      try {
        if (window.top && window.top !== window) {
          window.top.location.href = target;
          return;
        }
      } catch (e) {}
      setTimeout(function () {
        try { window.location.href = target; } catch (e) {}
      }, 50);
    }());
  </script>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    <p class="muted">If the app does not continue automatically, use the link below.</p>
    <p><a href="{{.TargetURL}}">Continue back to the app</a></p>
    <p><code>{{.TargetURL}}</code></p>
  </main>
</body>
</html>
`
