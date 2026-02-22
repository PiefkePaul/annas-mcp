package env

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const DefaultAnnasBaseURL = "annas-archive.li"

type Env struct {
	SecretKey    string `json:"secret"`
	DownloadPath string `json:"download_path"`
	AnnasBaseURL string `json:"annas_base_url"`
}

func GetEnv() (*Env, error) {
	l := logger.GetLogger()

	secretKey := os.Getenv("ANNAS_SECRET_KEY")
	downloadPath := os.Getenv("ANNAS_DOWNLOAD_PATH")
	annasBaseURL := os.Getenv("ANNAS_BASE_URL")
	if secretKey == "" || downloadPath == "" {
		err := errors.New("ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables must be set")

		// Never log secret keys - use boolean flags instead
		l.Error("Environment variables not set",
			zap.Bool("ANNAS_SECRET_KEY_set", secretKey != ""),
			zap.String("ANNAS_DOWNLOAD_PATH", downloadPath),
			zap.String("ANNAS_BASE_URL", annasBaseURL),
			zap.Error(err),
		)

		return nil, err
	}

	if !filepath.IsAbs(downloadPath) {
		return nil, fmt.Errorf("ANNAS_DOWNLOAD_PATH must be an absolute path, got: %s", downloadPath)
	}

	if annasBaseURL == "" {
		annasBaseURL = DefaultAnnasBaseURL
	}

	return &Env{
		SecretKey:    secretKey,
		DownloadPath: downloadPath,
		AnnasBaseURL: annasBaseURL,
	}, nil
}
