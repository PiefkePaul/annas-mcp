package env

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const DefaultAnnasBaseURL = "annas-archive.li"

type BaseEnv struct {
	AnnasBaseURL string `json:"annas_base_url"`
}

type Env struct {
	SecretKey    string `json:"secret"`
	DownloadPath string `json:"download_path"`
	AnnasBaseURL string `json:"annas_base_url"`
}

func GetBaseEnv() *BaseEnv {
	annasBaseURL := strings.TrimSpace(os.Getenv("ANNAS_BASE_URL"))
	if annasBaseURL == "" {
		annasBaseURL = DefaultAnnasBaseURL
	}

	return &BaseEnv{AnnasBaseURL: annasBaseURL}
}

func GetEnv() (*Env, error) {
	return GetDownloadEnv(true)
}

func GetDownloadEnv(requireSecret bool) (*Env, error) {
	l := logger.GetLogger()
	secretKey := strings.TrimSpace(os.Getenv("ANNAS_SECRET_KEY"))
	downloadPath := strings.TrimSpace(os.Getenv("ANNAS_DOWNLOAD_PATH"))
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
		SecretKey:    secretKey,
		DownloadPath: downloadPath,
		AnnasBaseURL: baseEnv.AnnasBaseURL,
	}, nil
}

func CanBookDownload() bool {
	return strings.TrimSpace(os.Getenv("ANNAS_SECRET_KEY")) != "" && strings.TrimSpace(os.Getenv("ANNAS_DOWNLOAD_PATH")) != ""
}

func CanArticleDownload() bool {
	return strings.TrimSpace(os.Getenv("ANNAS_DOWNLOAD_PATH")) != ""
}
