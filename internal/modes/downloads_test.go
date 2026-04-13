package modes

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PiefkePaul/annas-mcp/internal/anna"
	"github.com/PiefkePaul/annas-mcp/internal/auth"
	"github.com/PiefkePaul/annas-mcp/internal/env"
)

func TestCreateDownloadURLNilStoreReturnsEmpty(t *testing.T) {
	var store *ephemeralDownloadStore

	got := store.CreateDownloadURL("https://example.com", &anna.DownloadedFile{
		Filename: "book.pdf",
		Data:     []byte("data"),
	})

	if got != "" {
		t.Fatalf("CreateDownloadURL returned %q, want empty string", got)
	}
}

func TestCreateDownloadURLStoresFile(t *testing.T) {
	store := newEphemeralDownloadStore(time.Minute)
	file := &anna.DownloadedFile{
		Filename:     "book.pdf",
		MIMEType:     "application/pdf",
		Size:         4,
		Data:         []byte("data"),
		SourceMirror: "annas-archive.gl",
	}

	downloadURL := store.CreateDownloadURL("https://example.com/", file)
	if !strings.HasPrefix(downloadURL, "https://example.com/downloads/") {
		t.Fatalf("CreateDownloadURL returned %q", downloadURL)
	}

	parsed, err := url.Parse(downloadURL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}

	token := extractDownloadToken(parsed.Path)
	got, ok := store.lookup(token)
	if !ok {
		t.Fatal("expected stored download to be retrievable")
	}
	if got.Filename != file.Filename {
		t.Fatalf("stored filename = %q, want %q", got.Filename, file.Filename)
	}
}

func TestExtractDownloadToken(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/downloads/token/file.pdf", want: "token"},
		{path: "/downloads/token", want: "token"},
		{path: "/downloads/", want: ""},
		{path: "/other/token/file.pdf", want: ""},
	}

	for _, tc := range tests {
		if got := extractDownloadToken(tc.path); got != tc.want {
			t.Fatalf("extractDownloadToken(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestResolveSecretKeyPrefersProvidedValue(t *testing.T) {
	t.Setenv("ANNAS_SECRET_KEY", "env-secret")

	if got := resolveSecretKey("provided-secret", &auth.Identity{SecretKey: "identity-secret"}); got != "provided-secret" {
		t.Fatalf("resolveSecretKey preferred %q, want %q", got, "provided-secret")
	}

	if got := resolveSecretKey("", &auth.Identity{SecretKey: "identity-secret"}); got != "identity-secret" {
		t.Fatalf("resolveSecretKey preferred %q, want %q", got, "identity-secret")
	}

	if got := resolveSecretKey("", nil); got != "env-secret" {
		t.Fatalf("resolveSecretKey preferred %q, want %q", got, "env-secret")
	}
}

func TestEnsureAuthorizedDownloadAccess(t *testing.T) {
	policy := &env.UsagePolicyEnv{OperatorAttestsAuthorizedAccess: true}

	if err := ensureAuthorizedDownloadAccess(nil, nil); err != nil {
		t.Fatalf("ensureAuthorizedDownloadAccess(nil, nil) returned %v", err)
	}

	if err := ensureAuthorizedDownloadAccess(nil, policy); err == nil {
		t.Fatal("expected error for missing identity when policy is enforced")
	}

	if err := ensureAuthorizedDownloadAccess(&auth.Identity{}, policy); err == nil {
		t.Fatal("expected error for missing authorized access confirmation")
	}

	if err := ensureAuthorizedDownloadAccess(&auth.Identity{AuthorizedAccessConfirmedAt: time.Now().Unix()}, policy); err != nil {
		t.Fatalf("expected confirmed identity to pass, got %v", err)
	}
}
