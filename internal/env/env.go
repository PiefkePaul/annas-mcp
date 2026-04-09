package env

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	DefaultAnnasBaseURL        = "annas-archive.gl"
	DefaultMaxInlineDownloadMB = 20
)

var DefaultAnnasBaseURLs = []string{
	"annas-archive.gl",
	"annas-archive.pk",
	"annas-archive.gd",
}

type BaseEnv struct {
	AnnasBaseURL  string   `json:"annas_base_url"`
	AnnasBaseURLs []string `json:"annas_base_urls"`
}

type Env struct {
	SecretKey     string   `json:"secret"`
	DownloadPath  string   `json:"download_path"`
	AnnasBaseURL  string   `json:"annas_base_url"`
	AnnasBaseURLs []string `json:"annas_base_urls"`
}

func GetBaseEnv() *BaseEnv {
	candidates := make([]string, 0, len(DefaultAnnasBaseURLs)+2)

	if host, ok := normalizeBaseHost(os.Getenv("ANNAS_BASE_URL")); ok {
		candidates = append(candidates, host)
	}

	for _, raw := range strings.Split(os.Getenv("ANNAS_BASE_URLS"), ",") {
		if host, ok := normalizeBaseHost(raw); ok {
			candidates = append(candidates, host)
		}
	}

	if len(candidates) == 0 {
		candidates = append(candidates, DefaultAnnasBaseURLs...)
	}

	candidates = uniqueStrings(candidates)

	return &BaseEnv{
		AnnasBaseURL:  candidates[0],
		AnnasBaseURLs: candidates,
	}
}

func GetEnv() (*Env, error) {
	return GetDownloadEnv(true)
}

func GetDownloadEnv(requireSecret bool) (*Env, error) {
	l := logger.GetLogger()
	secretKey := GetDefaultSecretKey()
	downloadPath := GetDefaultDownloadPath()
	baseEnv := GetBaseEnv()

	if downloadPath == "" || (requireSecret && secretKey == "") {
		err := errors.New("required Anna's Archive download environment variables are not set")

		l.Error("Download environment variables not set",
			zap.Bool("ANNAS_SECRET_KEY_set", secretKey != ""),
			zap.Bool("ANNAS_DOWNLOAD_PATH_set", downloadPath != ""),
			zap.String("ANNAS_BASE_URL", baseEnv.AnnasBaseURL),
			zap.Bool("secretRequired", requireSecret),
			zap.Error(err),
		)

		if requireSecret {
			return nil, errors.New("ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables must be set")
		}
		return nil, errors.New("ANNAS_DOWNLOAD_PATH environment variable must be set")
	}

	if !filepath.IsAbs(downloadPath) {
		return nil, fmt.Errorf("ANNAS_DOWNLOAD_PATH must be an absolute path, got: %s", downloadPath)
	}

	return &Env{
		SecretKey:     secretKey,
		DownloadPath:  downloadPath,
		AnnasBaseURL:  baseEnv.AnnasBaseURL,
		AnnasBaseURLs: baseEnv.AnnasBaseURLs,
	}, nil
}

func CanBookDownload() bool {
	return true
}

func CanArticleDownload() bool {
	return true
}

func GetDefaultSecretKey() string {
	return strings.TrimSpace(os.Getenv("ANNAS_SECRET_KEY"))
}

func HasDefaultSecretKey() bool {
	return GetDefaultSecretKey() != ""
}

func GetDefaultDownloadPath() string {
	return strings.TrimSpace(os.Getenv("ANNAS_DOWNLOAD_PATH"))
}

func HasDefaultDownloadPath() bool {
	return GetDefaultDownloadPath() != ""
}

func GetMaxInlineDownloadBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("ANNAS_MAX_INLINE_DOWNLOAD_MB"))
	if raw == "" {
		return int64(DefaultMaxInlineDownloadMB) * 1024 * 1024
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return int64(DefaultMaxInlineDownloadMB) * 1024 * 1024
	}

	return int64(value) * 1024 * 1024
}

func normalizeBaseHost(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}

	return strings.ToLower(parsed.Host), true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}
