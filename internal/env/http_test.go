package env

import "testing"

func TestNormalizeHTTPPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty becomes default", in: "", want: DefaultHTTPPath},
		{name: "adds leading slash", in: "mcp", want: "/mcp"},
		{name: "trims trailing slash", in: "/mcp/", want: "/mcp"},
		{name: "root stays root", in: "/", want: "/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeHTTPPath(tc.in); got != tc.want {
				t.Fatalf("normalizeHTTPPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGetHTTPEnvDefaults(t *testing.T) {
	t.Setenv("ANNAS_HTTP_ADDR", "")
	t.Setenv("ANNAS_HTTP_PATH", "")
	t.Setenv("ANNAS_HTTP_AUTH_MODE", "")
	t.Setenv("ANNAS_HTTP_BEARER_TOKEN", "")
	t.Setenv("ANNAS_PUBLIC_BASE_URL", "")

	got, err := GetHTTPEnv()
	if err != nil {
		t.Fatalf("GetHTTPEnv returned error: %v", err)
	}

	if got.Addr != DefaultHTTPAddr {
		t.Fatalf("Addr = %q, want %q", got.Addr, DefaultHTTPAddr)
	}
	if got.Path != DefaultHTTPPath {
		t.Fatalf("Path = %q, want %q", got.Path, DefaultHTTPPath)
	}
	if got.AuthMode != HTTPAuthModeNone {
		t.Fatalf("AuthMode = %q, want %q", got.AuthMode, HTTPAuthModeNone)
	}
}

func TestGetHTTPEnvRequiresBearerToken(t *testing.T) {
	t.Setenv("ANNAS_HTTP_AUTH_MODE", "bearer")
	t.Setenv("ANNAS_HTTP_BEARER_TOKEN", "")
	t.Setenv("ANNAS_PUBLIC_BASE_URL", "")

	if _, err := GetHTTPEnv(); err == nil {
		t.Fatal("expected error when bearer auth is configured without a token")
	}
}

func TestGetHTTPEnvNormalizesPublicBaseURL(t *testing.T) {
	t.Setenv("ANNAS_HTTP_ADDR", "")
	t.Setenv("ANNAS_HTTP_PATH", "")
	t.Setenv("ANNAS_HTTP_AUTH_MODE", "")
	t.Setenv("ANNAS_HTTP_BEARER_TOKEN", "")
	t.Setenv("ANNAS_PUBLIC_BASE_URL", "https://example.com/")

	got, err := GetHTTPEnv()
	if err != nil {
		t.Fatalf("GetHTTPEnv returned error: %v", err)
	}

	if got.PublicBaseURL != "https://example.com" {
		t.Fatalf("PublicBaseURL = %q, want %q", got.PublicBaseURL, "https://example.com")
	}
}

func TestGetHTTPEnvRejectsPublicBaseURLPath(t *testing.T) {
	t.Setenv("ANNAS_PUBLIC_BASE_URL", "https://example.com/base")

	if _, err := GetHTTPEnv(); err == nil {
		t.Fatal("expected error when ANNAS_PUBLIC_BASE_URL contains a path")
	}
}
