package modes

import (
	"context"
	"strings"

	"github.com/PiefkePaul/annas-mcp/internal/anna"
	"github.com/PiefkePaul/annas-mcp/internal/env"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"github.com/PiefkePaul/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func BookSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookSearchParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Book search command called",
		zap.String("query", params.Arguments.Query),
	)

	books, err := anna.FindBook(params.Arguments.Query)
	if err != nil {
		l.Error("Book search command failed",
			zap.String("query", params.Arguments.Query),
			zap.Error(err),
		)
		return nil, err
	}

	if len(books) == 0 {
		l.Info("Book search returned no results",
			zap.String("query", params.Arguments.Query),
		)
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "No books found."}},
		}, nil
	}

	bookList := ""
	for _, book := range books {
		bookList += book.String() + "\n\n"
	}

	l.Info("Book search command completed successfully",
		zap.String("query", params.Arguments.Query),
		zap.Int("resultsCount", len(books)),
	)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: bookList}},
	}, nil
}

func BookDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Download command called",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("title", params.Arguments.Title),
		zap.String("format", params.Arguments.Format),
	)

	env, err := env.GetEnv()
	if err != nil {
		l.Error("Failed to get environment variables", zap.Error(err))
		return nil, err
	}
	secretKey := env.SecretKey
	downloadPath := env.DownloadPath

	title := params.Arguments.Title
	format := params.Arguments.Format
	book := &anna.Book{
		Hash:   params.Arguments.BookHash,
		Title:  title,
		Format: format,
	}

	err = book.Download(secretKey, downloadPath)
	if err != nil {
		l.Error("Download command failed",
			zap.String("bookHash", params.Arguments.BookHash),
			zap.String("downloadPath", downloadPath),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Download command completed successfully",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("downloadPath", downloadPath),
	)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{
			Text: "Book downloaded successfully to path: " + downloadPath,
		}},
	}, nil
}

func ArticleSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleSearchParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()
	query := params.Arguments.Query

	l.Info("Article search command called", zap.String("query", query))

	if strings.HasPrefix(strings.TrimSpace(query), "10.") {
		l.Info("Detected DOI format, performing DOI lookup", zap.String("doi", query))

		paper, err := anna.LookupDOI(query)
		if err != nil {
			l.Error("DOI lookup failed",
				zap.String("doi", query),
				zap.Error(err),
			)
			return &mcp.CallToolResultFor[any]{
				Content: []mcp.Content{&mcp.TextContent{Text: "No paper found for DOI: " + query}},
			}, nil
		}

		l.Info("DOI lookup completed", zap.String("doi", query))

		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: paper.String()}},
		}, nil
	}

	l.Info("Performing article keyword search", zap.String("query", query))

	papers, err := anna.FindArticle(query)
	if err != nil {
		l.Error("Article search failed",
			zap.String("query", query),
			zap.Error(err),
		)
		return nil, err
	}

	if len(papers) == 0 {
		l.Info("Article search returned no results",
			zap.String("query", query),
		)
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "No articles found."}},
		}, nil
	}

	paperList := ""
	for _, paper := range papers {
		paperList += paper.String() + "\n\n"
	}

	l.Info("Article search command completed successfully",
		zap.String("query", query),
		zap.Int("resultsCount", len(papers)),
	)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: paperList}},
	}, nil
}

func ArticleDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Download paper command called", zap.String("doi", params.Arguments.DOI))

	env, err := env.GetEnv()
	if err != nil {
		l.Error("Failed to get environment variables", zap.Error(err))
		return nil, err
	}

	paper, err := anna.LookupDOI(params.Arguments.DOI)
	if err != nil {
		l.Error("DOI lookup failed for download",
			zap.String("doi", params.Arguments.DOI),
			zap.Error(err),
		)
		return nil, err
	}

	if paper.Hash != "" && env.SecretKey != "" {
		book := &anna.Book{
			Hash:   paper.Hash,
			Title:  paper.Title,
			Format: "pdf",
		}
		if err := book.Download(env.SecretKey, env.DownloadPath); err != nil {
			l.Warn("Fast download failed, trying SciDB download",
				zap.String("doi", params.Arguments.DOI),
				zap.Error(err),
			)
		} else {
			l.Info("Paper downloaded via fast download",
				zap.String("doi", params.Arguments.DOI),
				zap.String("path", env.DownloadPath),
			)
			return &mcp.CallToolResultFor[any]{
				Content: []mcp.Content{&mcp.TextContent{
					Text: "Paper downloaded successfully to path: " + env.DownloadPath,
				}},
			}, nil
		}
	}

	if err := paper.Download(env.DownloadPath); err != nil {
		l.Error("SciDB download failed",
			zap.String("doi", params.Arguments.DOI),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Paper downloaded via SciDB",
		zap.String("doi", params.Arguments.DOI),
		zap.String("path", env.DownloadPath),
	)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{
			Text: "Paper downloaded successfully to path: " + env.DownloadPath,
		}},
	}, nil
}

func newMCPServer(serverVersion string) *mcp.Server {
	server := mcp.NewServer("annas-mcp", serverVersion, nil)

	server.AddTools(
		mcp.NewServerTool("book_search", "Search Anna's Archive for books by title, author, or topic. Returns book metadata including MD5 hash for downloading.", BookSearchTool, mcp.Input(
			mcp.Property("query", mcp.Description("Search query for books (e.g., title, author, topic)")),
		)),
		mcp.NewServerTool("book_download", "Download a book by its MD5 hash from search results. Requires ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables.", BookDownloadTool, mcp.Input(
			mcp.Property("hash", mcp.Description("MD5 hash of the book to download")),
			mcp.Property("title", mcp.Description("Book title, used for filename")),
			mcp.Property("format", mcp.Description("Book format, for example pdf or epub")),
		)),
		mcp.NewServerTool("article_search", "Search for academic articles/papers by DOI or keywords. Auto-detects if input is a DOI (starts with '10.') or a search term. Returns article metadata including DOI and hash.", ArticleSearchTool, mcp.Input(
			mcp.Property("query", mcp.Description("DOI (e.g., '10.1038/nature12345') or search keywords for articles")),
		)),
		mcp.NewServerTool("article_download", "Download an academic article/paper by its DOI. Looks up the paper, then downloads via fast download (if available) or SciDB. Requires ANNAS_DOWNLOAD_PATH environment variable.", ArticleDownloadTool, mcp.Input(
			mcp.Property("doi", mcp.Description("DOI of the article to download (e.g., '10.1038/nature12345')")),
		)),
	)

	return server
}

func StartMCPServer() {
	l := logger.GetLogger()
	defer l.Sync()

	serverVersion := version.GetVersion()
	l.Info("Starting MCP server",
		zap.String("name", "annas-mcp"),
		zap.String("version", serverVersion),
		zap.String("transport", "stdio"),
	)

	server := newMCPServer(serverVersion)

	l.Info("MCP server started successfully")

	if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
		l.Fatal("MCP server failed", zap.Error(err))
	}
}
