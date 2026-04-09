package anna

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PiefkePaul/annas-mcp/internal/env"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	colly "github.com/gocolly/colly/v2"
	"go.uber.org/zap"
)

const (
	AnnasSearchEndpointFormat   = "https://%s/search?q=%s&content=%s"
	AnnasSciDBEndpointFormat    = "https://%s/scidb/%s"
	AnnasDownloadEndpointFormat = "https://%s/dyn/api/fast_download.json?md5=%s&key=%s"
	HTTPTimeout                 = 30 * time.Second
	BrowserUserAgent            = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	DefaultBinaryExtension      = ".bin"
	DefaultBinaryMIMEType       = "application/octet-stream"
)

var (
	ErrInlineDownloadTooLarge = errors.New("download exceeds inline size limit")

	// Regex to sanitize filenames - removes dangerous characters.
	unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
)

func extractMetaInformation(meta string) (language, format, size string) {
	parts := strings.Split(meta, " · ")
	if len(parts) < 3 {
		return "", "", ""
	}

	languagePart := strings.TrimSpace(parts[0])
	if idx := strings.Index(languagePart, "["); idx > 0 {
		language = strings.TrimSpace(languagePart[:idx])
		language = strings.TrimPrefix(language, "✅")
		language = strings.TrimSpace(language)
	}

	formatRegex := regexp.MustCompile(`(?i)\b(EPUB|PDF|MOBI|AZW3|AZW|DJVU|CBZ|CBR|FB2|DOCX?|TXT)\b`)
	sizeRegex := regexp.MustCompile(`\d+\.?\d*\s*(MB|KB|GB|TB)`)

	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])

		if size == "" && sizeRegex.MatchString(part) {
			size = part
		}

		if format == "" && formatRegex.MatchString(part) {
			matches := formatRegex.FindStringSubmatch(part)
			if len(matches) > 0 {
				format = strings.ToUpper(matches[1])
			}
		}

		if format != "" && size != "" {
			break
		}
	}

	return language, format, size
}

func sanitizeFilename(filename string) string {
	safe := unsafeFilenameChars.ReplaceAllString(filename, "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = filepath.Base(safe)

	if len(safe) > 200 {
		safe = safe[:200]
	}

	return safe
}

func FindBook(query string) ([]*Book, error) {
	return withMirrorFallback("book search", func(host string) ([]*Book, error) {
		return findBooksOnHost(host, query)
	})
}

func findBooksOnHost(host string, query string) ([]*Book, error) {
	l := logger.GetLogger()

	var (
		bookListMutex sync.Mutex
		bookList      = make([]*colly.HTMLElement, 0)
		requestErr    error
	)

	c := colly.NewCollector(
		colly.Async(true),
		colly.UserAgent(BrowserUserAgent),
	)

	c.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if e.Attr("class") == "custom-a block mr-2 sm:mr-4 hover:opacity-80" {
			bookListMutex.Lock()
			bookList = append(bookList, e)
			bookListMutex.Unlock()
		}
	})

	c.OnRequest(func(r *colly.Request) {
		l.Info("Visiting Anna's Archive search URL",
			zap.String("mirror", host),
			zap.String("url", r.URL.String()),
		)
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if requestErr == nil {
			requestErr = fmt.Errorf("mirror %s search failed with status %d: %w", host, status, err)
		}
		l.Error("Search request failed",
			zap.String("mirror", host),
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, host, url.QueryEscape(query), "book_any")

	if err := c.Visit(fullURL); err != nil {
		l.Error("Failed to visit search URL",
			zap.String("mirror", host),
			zap.String("url", fullURL),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to visit search URL on %s: %w", host, err)
	}
	c.Wait()

	if requestErr != nil {
		return nil, requestErr
	}

	bookListParsed := make([]*Book, 0, len(bookList))
	for _, e := range bookList {
		parent := e.DOM.Parent()
		if parent.Length() == 0 {
			l.Warn("Skipping book: no parent element found", zap.String("mirror", host))
			continue
		}

		bookInfoDiv := parent.Find("div.max-w-full")
		if bookInfoDiv.Length() == 0 {
			l.Warn("Skipping book: book info container not found", zap.String("mirror", host))
			continue
		}

		titleElement := bookInfoDiv.Find("a[href^='/md5/']")
		title := strings.TrimSpace(titleElement.Text())
		if title == "" {
			l.Warn("Skipping book: title is empty", zap.String("mirror", host))
			continue
		}

		authorsRaw := bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().Text()
		authors := strings.TrimSpace(authorsRaw)

		publisherRaw := bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().Text()
		publisher := strings.TrimSpace(publisherRaw)

		meta := bookInfoDiv.Find("div.text-gray-800").Text()
		language, format, size := extractMetaInformation(meta)

		link := e.Attr("href")
		if link == "" {
			l.Warn("Skipping book: no link found",
				zap.String("mirror", host),
				zap.String("title", title),
			)
			continue
		}
		hash := strings.TrimPrefix(link, "/md5/")
		if hash == "" {
			l.Warn("Skipping book: no hash found",
				zap.String("mirror", host),
				zap.String("title", title),
			)
			continue
		}

		bookListParsed = append(bookListParsed, &Book{
			Language:  language,
			Format:    format,
			Size:      size,
			Title:     title,
			Publisher: publisher,
			Authors:   authors,
			URL:       e.Request.AbsoluteURL(link),
			Hash:      hash,
		})
	}

	l.Info("Book search completed",
		zap.String("mirror", host),
		zap.Int("totalElements", len(bookList)),
		zap.Int("validBooks", len(bookListParsed)),
	)

	return bookListParsed, nil
}

func FindArticle(query string) ([]*Paper, error) {
	return withMirrorFallback("article search", func(host string) ([]*Paper, error) {
		return findArticlesOnHost(host, query)
	})
}

func findArticlesOnHost(host string, query string) ([]*Paper, error) {
	l := logger.GetLogger()

	var (
		paperListMutex sync.Mutex
		paperList      = make([]*colly.HTMLElement, 0)
		requestErr     error
	)

	c := colly.NewCollector(
		colly.Async(true),
		colly.UserAgent(BrowserUserAgent),
	)

	c.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if e.Attr("class") == "custom-a block mr-2 sm:mr-4 hover:opacity-80" {
			paperListMutex.Lock()
			paperList = append(paperList, e)
			paperListMutex.Unlock()
		}
	})

	c.OnRequest(func(r *colly.Request) {
		l.Info("Visiting Anna's Archive article search URL",
			zap.String("mirror", host),
			zap.String("url", r.URL.String()),
		)
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if requestErr == nil {
			requestErr = fmt.Errorf("mirror %s article search failed with status %d: %w", host, status, err)
		}
		l.Error("Article search request failed",
			zap.String("mirror", host),
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, host, url.QueryEscape(query), "journal")

	if err := c.Visit(fullURL); err != nil {
		l.Error("Failed to visit article search URL",
			zap.String("mirror", host),
			zap.String("url", fullURL),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to visit article search URL on %s: %w", host, err)
	}
	c.Wait()

	if requestErr != nil {
		return nil, requestErr
	}

	paperListParsed := make([]*Paper, 0, len(paperList))
	for _, e := range paperList {
		parent := e.DOM.Parent()
		if parent.Length() == 0 {
			l.Warn("Skipping article: no parent element found", zap.String("mirror", host))
			continue
		}

		paperInfoDiv := parent.Find("div.max-w-full")
		if paperInfoDiv.Length() == 0 {
			l.Warn("Skipping article: info container not found", zap.String("mirror", host))
			continue
		}

		titleElement := paperInfoDiv.Find("a[href^='/md5/']")
		title := strings.TrimSpace(titleElement.Text())
		if title == "" {
			l.Warn("Skipping article: title is empty", zap.String("mirror", host))
			continue
		}

		authorsRaw := paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().Text()
		authors := strings.TrimSpace(authorsRaw)

		journalRaw := paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().Text()
		journal := strings.TrimSpace(journalRaw)

		meta := paperInfoDiv.Find("div.text-gray-800").Text()
		_, _, size := extractMetaInformation(meta)

		link := e.Attr("href")
		if link == "" {
			l.Warn("Skipping article: no link found",
				zap.String("mirror", host),
				zap.String("title", title),
			)
			continue
		}
		hash := strings.TrimPrefix(link, "/md5/")
		if hash == "" {
			l.Warn("Skipping article: no hash found",
				zap.String("mirror", host),
				zap.String("title", title),
			)
			continue
		}

		paperListParsed = append(paperListParsed, &Paper{
			Title:   title,
			Authors: authors,
			Journal: journal,
			Size:    size,
			Hash:    hash,
			PageURL: e.Request.AbsoluteURL(link),
		})
	}

	l.Info("Article search completed",
		zap.String("mirror", host),
		zap.Int("totalElements", len(paperList)),
		zap.Int("validPapers", len(paperListParsed)),
	)

	return paperListParsed, nil
}

func (b *Book) Download(secretKey, folderPath string) error {
	l := logger.GetLogger()
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return errors.New("secret key is required for book downloads")
	}
	if strings.TrimSpace(folderPath) == "" {
		return errors.New("download path is required for book downloads")
	}

	baseEnv := env.GetBaseEnv()
	client := &http.Client{Timeout: HTTPTimeout}
	errorsByMirror := make([]string, 0, len(baseEnv.AnnasBaseURLs))

	for _, host := range baseEnv.AnnasBaseURLs {
		downloadURL, err := fetchFastDownloadURL(client, host, b.Hash, secretKey)
		if err != nil {
			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Fast download URL retrieval failed",
				zap.String("mirror", host),
				zap.String("hash", b.Hash),
				zap.Error(err),
			)
			continue
		}

		filePath, err := downloadFromURLToDisk(client, downloadURL, folderPath, preferredBookFilename(b))
		if err != nil {
			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Book download failed on mirror",
				zap.String("mirror", host),
				zap.String("hash", b.Hash),
				zap.Error(err),
			)
			continue
		}

		l.Info("Book download completed successfully",
			zap.String("mirror", host),
			zap.String("hash", b.Hash),
			zap.String("path", filePath),
		)
		return nil
	}

	return fmt.Errorf("book download failed on all configured Anna's Archive mirrors: %s", strings.Join(errorsByMirror, " | "))
}

func (b *Book) DownloadInline(secretKey string, maxBytes int64) (*DownloadedFile, error) {
	l := logger.GetLogger()
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, errors.New("secret key is required for book downloads")
	}

	baseEnv := env.GetBaseEnv()
	client := &http.Client{Timeout: HTTPTimeout}
	errorsByMirror := make([]string, 0, len(baseEnv.AnnasBaseURLs))

	for _, host := range baseEnv.AnnasBaseURLs {
		downloadURL, err := fetchFastDownloadURL(client, host, b.Hash, secretKey)
		if err != nil {
			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Fast download URL retrieval failed",
				zap.String("mirror", host),
				zap.String("hash", b.Hash),
				zap.Error(err),
			)
			continue
		}

		file, err := downloadFromURLToMemory(client, downloadURL, preferredBookFilename(b), host, maxBytes)
		if err != nil {
			if errors.Is(err, ErrInlineDownloadTooLarge) {
				return nil, err
			}

			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Inline book download failed on mirror",
				zap.String("mirror", host),
				zap.String("hash", b.Hash),
				zap.Error(err),
			)
			continue
		}

		l.Info("Inline book download completed successfully",
			zap.String("mirror", host),
			zap.String("hash", b.Hash),
			zap.String("filename", file.Filename),
			zap.Int64("bytes", file.Size),
		)
		return file, nil
	}

	return nil, fmt.Errorf("book download failed on all configured Anna's Archive mirrors: %s", strings.Join(errorsByMirror, " | "))
}

func LookupDOI(doi string) (*Paper, error) {
	return withMirrorFallback("DOI lookup", func(host string) (*Paper, error) {
		return lookupDOIOnHost(host, doi)
	})
}

func lookupDOIOnHost(host, doi string) (*Paper, error) {
	l := logger.GetLogger()
	paper := &Paper{DOI: doi}
	var searchErr error

	searchCollector := colly.NewCollector(
		colly.UserAgent(BrowserUserAgent),
	)

	searchCollector.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if paper.Hash != "" {
			return
		}
		href := e.Attr("href")
		hash := strings.TrimPrefix(href, "/md5/")
		if hash != "" {
			paper.Hash = hash
		}
	})

	searchCollector.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if searchErr == nil {
			searchErr = fmt.Errorf("mirror %s SciDB lookup failed with status %d: %w", host, status, err)
		}
		l.Error("SciDB search failed",
			zap.String("mirror", host),
			zap.String("doi", doi),
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	scidbURL := fmt.Sprintf(AnnasSciDBEndpointFormat, host, doi)
	paper.PageURL = scidbURL

	l.Info("Looking up DOI",
		zap.String("mirror", host),
		zap.String("url", scidbURL),
	)

	if err := searchCollector.Visit(scidbURL); err != nil {
		return nil, fmt.Errorf("failed to lookup DOI on %s: %w", host, err)
	}
	if searchErr != nil {
		return nil, searchErr
	}

	if paper.Hash == "" {
		return nil, fmt.Errorf("no paper found for DOI %s on mirror %s", doi, host)
	}

	detailCollector := colly.NewCollector(
		colly.UserAgent(BrowserUserAgent),
	)

	detailCollector.OnHTML("title", func(e *colly.HTMLElement) {
		title := e.Text
		if idx := strings.Index(title, " - Anna"); idx > 0 {
			paper.Title = strings.TrimSpace(title[:idx])
		}
	})

	detailCollector.OnHTML("meta[name=description]", func(e *colly.HTMLElement) {
		desc := e.Attr("content")
		parts := strings.Split(desc, "\n\n")
		if len(parts) >= 3 {
			paper.Journal = strings.TrimSpace(parts[2])
		} else if len(parts) >= 2 {
			paper.Journal = strings.TrimSpace(parts[1])
		} else {
			paper.Journal = strings.TrimSpace(desc)
		}
	})

	detailCollector.OnHTML("a[href^='/search']", func(e *colly.HTMLElement) {
		if paper.Authors != "" {
			return
		}
		if e.DOM.Find("span.icon-\\[mdi--user-edit\\]").Length() > 0 {
			paper.Authors = strings.TrimSpace(e.Text)
		}
	})

	detailCollector.OnHTML("div.text-gray-500", func(e *colly.HTMLElement) {
		text := e.Text
		if strings.Contains(text, "MB") || strings.Contains(text, "KB") {
			paper.Size = strings.TrimSpace(text)
		}
	})

	detailCollector.OnError(func(r *colly.Response, err error) {
		l.Warn("Failed to fetch paper details",
			zap.String("mirror", host),
			zap.String("hash", paper.Hash),
			zap.Error(err),
		)
	})

	md5URL := fmt.Sprintf("https://%s/md5/%s", host, paper.Hash)
	l.Info("Fetching paper details",
		zap.String("mirror", host),
		zap.String("url", md5URL),
	)

	if err := detailCollector.Visit(md5URL); err != nil {
		l.Warn("Failed to visit paper detail page",
			zap.String("mirror", host),
			zap.Error(err),
		)
	}

	paper.DownloadURL = fmt.Sprintf("/scidb?doi=%s", url.QueryEscape(doi))

	return paper, nil
}

func (p *Paper) Download(folderPath string) error {
	l := logger.GetLogger()
	if strings.TrimSpace(folderPath) == "" {
		return errors.New("download path is required for article downloads")
	}
	if p.DownloadURL == "" {
		return errors.New("no download URL available for this paper")
	}

	baseEnv := env.GetBaseEnv()
	client := &http.Client{Timeout: 2 * HTTPTimeout}
	errorsByMirror := make([]string, 0, len(baseEnv.AnnasBaseURLs))

	for _, host := range baseEnv.AnnasBaseURLs {
		downloadURL := relativeOrAbsoluteDownloadURL(host, p.DownloadURL)
		filePath, err := downloadFromURLToDisk(client, downloadURL, folderPath, preferredPaperFilename(p))
		if err != nil {
			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Paper download failed on mirror",
				zap.String("mirror", host),
				zap.String("doi", p.DOI),
				zap.Error(err),
			)
			continue
		}

		l.Info("Paper download completed successfully",
			zap.String("mirror", host),
			zap.String("doi", p.DOI),
			zap.String("path", filePath),
		)
		return nil
	}

	return fmt.Errorf("paper download failed on all configured Anna's Archive mirrors: %s", strings.Join(errorsByMirror, " | "))
}

func (p *Paper) DownloadInline(maxBytes int64) (*DownloadedFile, error) {
	l := logger.GetLogger()
	if p.DownloadURL == "" {
		return nil, errors.New("no download URL available for this paper")
	}

	baseEnv := env.GetBaseEnv()
	client := &http.Client{Timeout: 2 * HTTPTimeout}
	errorsByMirror := make([]string, 0, len(baseEnv.AnnasBaseURLs))

	for _, host := range baseEnv.AnnasBaseURLs {
		downloadURL := relativeOrAbsoluteDownloadURL(host, p.DownloadURL)
		file, err := downloadFromURLToMemory(client, downloadURL, preferredPaperFilename(p), host, maxBytes)
		if err != nil {
			if errors.Is(err, ErrInlineDownloadTooLarge) {
				return nil, err
			}

			errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
			l.Warn("Inline paper download failed on mirror",
				zap.String("mirror", host),
				zap.String("doi", p.DOI),
				zap.Error(err),
			)
			continue
		}

		l.Info("Inline paper download completed successfully",
			zap.String("mirror", host),
			zap.String("doi", p.DOI),
			zap.String("filename", file.Filename),
			zap.Int64("bytes", file.Size),
		)
		return file, nil
	}

	return nil, fmt.Errorf("paper download failed on all configured Anna's Archive mirrors: %s", strings.Join(errorsByMirror, " | "))
}

func (p *Paper) DownloadInlineFromLibgen(maxBytes int64) (*DownloadedFile, error) {
	l := logger.GetLogger()
	if strings.TrimSpace(p.Hash) == "" {
		return nil, errors.New("no Anna's Archive hash is available for Libgen lookup")
	}

	entryURL, err := findLibgenEntryURL(p.Hash)
	if err != nil {
		return nil, err
	}

	adsURL, err := findLibgenAdsURL(entryURL)
	if err != nil {
		return nil, err
	}

	downloadURL, err := findLibgenDirectDownloadURL(adsURL)
	if err != nil {
		return nil, err
	}

	file, err := downloadFromURLToMemory(&http.Client{Timeout: 2 * HTTPTimeout}, downloadURL, preferredPaperFilename(p), "libgen.li", maxBytes)
	if err != nil {
		return nil, err
	}

	l.Info("Inline paper download completed successfully via Libgen",
		zap.String("doi", p.DOI),
		zap.String("hash", p.Hash),
		zap.String("filename", file.Filename),
		zap.Int64("bytes", file.Size),
	)

	return file, nil
}

func (b *Book) String() string {
	return fmt.Sprintf("Title: %s\nAuthors: %s\nPublisher: %s\nLanguage: %s\nFormat: %s\nSize: %s\nURL: %s\nHash: %s",
		b.Title, b.Authors, b.Publisher, b.Language, b.Format, b.Size, b.URL, b.Hash)
}

func (b *Book) ToJSON() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func withMirrorFallback[T any](operation string, fn func(host string) (T, error)) (T, error) {
	l := logger.GetLogger()
	baseEnv := env.GetBaseEnv()

	var zero T
	if len(baseEnv.AnnasBaseURLs) == 0 {
		return zero, fmt.Errorf("%s failed: no Anna's Archive mirrors configured", operation)
	}

	errorsByMirror := make([]string, 0, len(baseEnv.AnnasBaseURLs))
	for _, host := range baseEnv.AnnasBaseURLs {
		value, err := fn(host)
		if err == nil {
			return value, nil
		}

		errorsByMirror = append(errorsByMirror, fmt.Sprintf("%s: %v", host, err))
		l.Warn("Anna's Archive mirror attempt failed",
			zap.String("operation", operation),
			zap.String("mirror", host),
			zap.Error(err),
		)
	}

	return zero, fmt.Errorf("%s failed on all configured Anna's Archive mirrors: %s", operation, strings.Join(errorsByMirror, " | "))
}

func findLibgenEntryURL(hash string) (string, error) {
	return withMirrorFallback("Libgen entry lookup", func(host string) (string, error) {
		return findLibgenEntryURLOnHost(host, hash)
	})
}

func findLibgenEntryURLOnHost(host, hash string) (string, error) {
	l := logger.GetLogger()
	var (
		entryURL   string
		requestErr error
	)

	c := colly.NewCollector(
		colly.UserAgent(BrowserUserAgent),
	)

	c.OnHTML("a[href*='libgen.li/file.php?id=']", func(e *colly.HTMLElement) {
		if entryURL != "" {
			return
		}

		href := strings.TrimSpace(e.Attr("href"))
		if strings.Contains(href, "libgen.li/file.php?id=") {
			entryURL = href
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if requestErr == nil {
			requestErr = fmt.Errorf("mirror %s md5 lookup failed with status %d: %w", host, status, err)
		}
	})

	md5URL := fmt.Sprintf("https://%s/md5/%s", host, hash)
	l.Info("Looking up Libgen mirror entry on Anna's Archive",
		zap.String("mirror", host),
		zap.String("hash", hash),
		zap.String("url", md5URL),
	)

	if err := c.Visit(md5URL); err != nil {
		return "", fmt.Errorf("failed to visit Anna's Archive md5 page on %s: %w", host, err)
	}
	if requestErr != nil {
		return "", requestErr
	}
	if entryURL == "" {
		return "", fmt.Errorf("no Libgen.li entry link found for hash %s on mirror %s", hash, host)
	}

	return entryURL, nil
}

func findLibgenAdsURL(entryURL string) (string, error) {
	l := logger.GetLogger()
	var (
		adsURL     string
		requestErr error
	)

	c := colly.NewCollector(
		colly.UserAgent(BrowserUserAgent),
	)

	c.OnHTML("a[href^='/ads.php?md5='], a[href^='ads.php?md5=']", func(e *colly.HTMLElement) {
		if adsURL != "" {
			return
		}
		adsURL = normalizeDiscoveredURL(entryURL, strings.TrimSpace(e.Attr("href")))
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if requestErr == nil {
			requestErr = fmt.Errorf("Libgen entry page failed with status %d: %w", status, err)
		}
	})

	l.Info("Fetching Libgen entry page",
		zap.String("url", entryURL),
	)

	if err := c.Visit(entryURL); err != nil {
		return "", fmt.Errorf("failed to visit Libgen entry page: %w", err)
	}
	if requestErr != nil {
		return "", requestErr
	}
	if adsURL == "" {
		return "", fmt.Errorf("no Libgen ads/download page found at %s", entryURL)
	}

	return adsURL, nil
}

func findLibgenDirectDownloadURL(adsURL string) (string, error) {
	l := logger.GetLogger()
	var (
		downloadURL string
		requestErr  error
	)

	c := colly.NewCollector(
		colly.UserAgent(BrowserUserAgent),
	)

	c.OnHTML("a[href^='get.php?md5='], a[href^='/get.php?md5=']", func(e *colly.HTMLElement) {
		if downloadURL != "" {
			return
		}
		downloadURL = normalizeDiscoveredURL(adsURL, strings.TrimSpace(e.Attr("href")))
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		if requestErr == nil {
			requestErr = fmt.Errorf("Libgen ads page failed with status %d: %w", status, err)
		}
	})

	l.Info("Fetching Libgen ads page",
		zap.String("url", adsURL),
	)

	if err := c.Visit(adsURL); err != nil {
		return "", fmt.Errorf("failed to visit Libgen ads page: %w", err)
	}
	if requestErr != nil {
		return "", requestErr
	}
	if downloadURL == "" {
		return "", fmt.Errorf("no direct Libgen download link found at %s", adsURL)
	}

	return downloadURL, nil
}

func normalizeDiscoveredURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return raw
	}
	parsedRaw, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	return parsedBase.ResolveReference(parsedRaw).String()
}

func fetchFastDownloadURL(client *http.Client, host, hash, secretKey string) (string, error) {
	apiURL := fmt.Sprintf(AnnasDownloadEndpointFormat, host, hash, secretKey)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create fast download request: %w", err)
	}
	req.Header.Set("User-Agent", BrowserUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch download URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, resp.Status)
		}
		return "", fmt.Errorf("API request failed with status %d: %s (body: %s)", resp.StatusCode, resp.Status, string(body))
	}

	var apiResp fastDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("failed to decode API response: %w", err)
	}

	if apiResp.DownloadURL == "" {
		if apiResp.Error != "" {
			return "", fmt.Errorf("API error: %s", apiResp.Error)
		}
		return "", errors.New("API returned empty download URL")
	}

	return apiResp.DownloadURL, nil
}

func downloadFromURLToDisk(client *http.Client, downloadURL, folderPath, filenameHint string) (string, error) {
	resp, err := issueDownloadRequest(client, downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", readDownloadError(resp)
	}

	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		return "", fmt.Errorf("failed to create download directory: %w", err)
	}

	filename := resolveDownloadFilename(resp, filenameHint)
	filePath := filepath.Join(folderPath, filename)

	out, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}

	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(filePath)
		}
	}()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write file (wrote %d bytes): %w", written, err)
	}

	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync file to disk: %w", err)
	}

	success = true
	return filePath, nil
}

func downloadFromURLToMemory(client *http.Client, downloadURL, filenameHint, sourceMirror string, maxBytes int64) (*DownloadedFile, error) {
	resp, err := issueDownloadRequest(client, downloadURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readDownloadError(resp)
	}

	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds configured limit of %d bytes", ErrInlineDownloadTooLarge, resp.ContentLength, maxBytes)
	}

	data, err := readBodyWithLimit(resp.Body, maxBytes)
	if err != nil {
		return nil, err
	}

	filename := resolveDownloadFilename(resp, filenameHint)
	mimeType := resolveMIMEType(resp, filename, data)

	return &DownloadedFile{
		Filename:     filename,
		MIMEType:     mimeType,
		Size:         int64(len(data)),
		Data:         data,
		SourceURL:    downloadURL,
		SourceMirror: sourceMirror,
	}, nil
}

func issueDownloadRequest(client *http.Client, downloadURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	return client.Do(req)
}

func readDownloadError(resp *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	if readErr != nil {
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, resp.Status)
	}
	return fmt.Errorf("download failed with status %d: %s (body: %s)", resp.StatusCode, resp.Status, string(body))
}

func readBodyWithLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}

	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: response body exceeds configured limit of %d bytes", ErrInlineDownloadTooLarge, maxBytes)
	}

	return data, nil
}

func resolveDownloadFilename(resp *http.Response, filenameHint string) string {
	ext := inferResponseExtension(resp, filepath.Ext(filenameHint))
	base := strings.TrimSuffix(filepath.Base(filenameHint), filepath.Ext(filenameHint))
	base = sanitizeFilename(base)
	if base == "" {
		base = "download"
	}
	return base + ext
}

func inferResponseExtension(resp *http.Response, fallbackExt string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := params["filename"]; filename != "" {
				if ext := filepath.Ext(filename); ext != "" {
					return normalizeExtension(ext, fallbackExt)
				}
			}
		}
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if mediaType, _, err := mime.ParseMediaType(ct); err == nil {
			exts, _ := mime.ExtensionsByType(mediaType)
			if len(exts) > 0 {
				return normalizeExtension(exts[0], fallbackExt)
			}
		}
	}

	return normalizeExtension(fallbackExt, DefaultBinaryExtension)
}

func normalizeExtension(ext, fallback string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		ext = strings.TrimSpace(fallback)
	}
	if ext == "" {
		ext = DefaultBinaryExtension
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.ToLower(ext)
}

func resolveMIMEType(resp *http.Response, filename string, data []byte) string {
	if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
		if mediaType, _, err := mime.ParseMediaType(ct); err == nil && mediaType != "" {
			if !isGenericBinaryMIMEType(mediaType) {
				return mediaType
			}
		} else {
			mediaType = strings.TrimSpace(strings.Split(ct, ";")[0])
			if mediaType != "" && !isGenericBinaryMIMEType(mediaType) {
				return mediaType
			}
		}
	}

	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return strings.TrimSpace(strings.Split(mimeType, ";")[0])
		}
	}

	if len(data) > 0 {
		return http.DetectContentType(data)
	}

	return DefaultBinaryMIMEType
}

func isGenericBinaryMIMEType(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "", DefaultBinaryMIMEType, "binary/octet-stream":
		return true
	default:
		return false
	}
}

func preferredBookFilename(b *Book) string {
	name := strings.TrimSpace(b.Title)
	if name == "" {
		name = strings.TrimSpace(b.Hash)
	}
	if name == "" {
		name = "book"
	}

	ext := strings.TrimSpace(strings.ToLower(b.Format))
	if ext == "" {
		ext = strings.TrimPrefix(DefaultBinaryExtension, ".")
	}

	return sanitizeFilename(name) + "." + ext
}

func preferredPaperFilename(p *Paper) string {
	name := strings.TrimSpace(p.Title)
	if name == "" {
		name = strings.TrimSpace(p.DOI)
	}
	if name == "" {
		name = "paper"
	}

	return sanitizeFilename(name) + ".pdf"
}

func relativeOrAbsoluteDownloadURL(host, raw string) string {
	if strings.HasPrefix(strings.TrimSpace(raw), "http://") || strings.HasPrefix(strings.TrimSpace(raw), "https://") {
		return raw
	}
	return fmt.Sprintf("https://%s%s", host, raw)
}
