package modes

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/PiefkePaul/annas-mcp/internal/anna"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const defaultEphemeralDownloadTTL = 30 * time.Minute

type ephemeralDownloadStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	files map[string]*ephemeralDownload
}

type ephemeralDownload struct {
	file      *anna.DownloadedFile
	expiresAt time.Time
}

func newEphemeralDownloadStore(ttl time.Duration) *ephemeralDownloadStore {
	if ttl <= 0 {
		ttl = defaultEphemeralDownloadTTL
	}

	return &ephemeralDownloadStore{
		ttl:   ttl,
		files: make(map[string]*ephemeralDownload),
	}
}

func (s *ephemeralDownloadStore) CreateDownloadURL(baseURL string, file *anna.DownloadedFile) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if s == nil || baseURL == "" || file == nil || len(file.Data) == 0 {
		return ""
	}

	now := time.Now()
	token := randomURLToken(24)
	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		filename = "download"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked(now)
	s.files[token] = &ephemeralDownload{
		file:      file,
		expiresAt: now.Add(s.ttl),
	}

	return fmt.Sprintf("%s/downloads/%s/%s", baseURL, token, url.PathEscape(filename))
}

func (s *ephemeralDownloadStore) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		return
	}

	token := extractDownloadToken(r.URL.Path)
	if token == "" {
		http.NotFound(w, r)
		return
	}

	file, ok := s.lookup(token)
	if !ok || file == nil {
		http.NotFound(w, r)
		return
	}

	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		filename = "download"
	}

	contentType := strings.TrimSpace(file.MIMEType)
	if contentType == "" {
		contentType = anna.DefaultBinaryMIMEType
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = fmt.Sprintf("attachment; filename=%q", filename)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(file.Data)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodHead {
		return
	}

	if _, err := w.Write(file.Data); err != nil {
		logger.GetLogger().Warn("Failed to write ephemeral download response",
			zap.String("filename", filename),
			zap.Error(err),
		)
	}
}

func (s *ephemeralDownloadStore) lookup(token string) (*anna.DownloadedFile, bool) {
	if s == nil {
		return nil, false
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked(now)
	entry, ok := s.files[token]
	if !ok || entry == nil || entry.file == nil {
		return nil, false
	}

	return entry.file, true
}

func (s *ephemeralDownloadStore) cleanupLocked(now time.Time) {
	for token, entry := range s.files {
		if entry == nil || !entry.expiresAt.After(now) {
			delete(s.files, token)
		}
	}
}

func extractDownloadToken(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return ""
	}

	trimmed := strings.TrimPrefix(rawPath, "/downloads/")
	if trimmed == rawPath || trimmed == "" {
		return ""
	}

	firstSegment := trimmed
	if slash := strings.Index(firstSegment, "/"); slash >= 0 {
		firstSegment = firstSegment[:slash]
	}

	firstSegment = path.Clean("/" + firstSegment)
	firstSegment = strings.TrimPrefix(firstSegment, "/")
	if firstSegment == "." || firstSegment == "" {
		return ""
	}

	return firstSegment
}

func randomURLToken(numBytes int) string {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
