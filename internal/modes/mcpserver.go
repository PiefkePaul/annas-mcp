package modes

import (
	"context"

	"github.com/iosifache/annas-mcp/internal/anna"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"github.com/iosifache/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func SearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Search command called",
		zap.String("searchTerm", params.Arguments.SearchTerm),
		zap.String("content", params.Arguments.Content),
	)

	books, err := anna.FindBook(params.Arguments.SearchTerm, params.Arguments.Content)
	if err != nil {
		l.Error("Search command failed",
			zap.String("searchTerm", params.Arguments.SearchTerm),
			zap.Error(err),
		)
		return nil, err
	}

	if len(books) == 0 {
		l.Info("Search returned no results",
			zap.String("searchTerm", params.Arguments.SearchTerm),
		)
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "No books found."}},
		}, nil
	}

	bookList := ""
	for _, book := range books {
		bookList += book.String() + "\n\n"
	}

	l.Info("Search command completed successfully",
		zap.String("searchTerm", params.Arguments.SearchTerm),
		zap.Int("resultsCount", len(books)),
	)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: bookList}},
	}, nil
}

func DownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[DownloadParams]) (*mcp.CallToolResultFor[any], error) {
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

func DOITool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[DOIParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("DOI lookup called", zap.String("doi", params.Arguments.DOI))

	paper, err := anna.LookupDOI(params.Arguments.DOI)
	if err != nil {
		l.Error("DOI lookup failed",
			zap.String("doi", params.Arguments.DOI),
			zap.Error(err),
		)
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "No paper found for DOI: " + params.Arguments.DOI}},
		}, nil
	}

	l.Info("DOI lookup completed", zap.String("doi", params.Arguments.DOI))

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: paper.String()}},
	}, nil
}

func DownloadPaperTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[DownloadPaperParams]) (*mcp.CallToolResultFor[any], error) {
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

	// Try fast_download API first if we have a hash and secret key
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

	// Fall back to SciDB download
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

func StartMCPServer() {
	l := logger.GetLogger()
	defer l.Sync()

	serverVersion := version.GetVersion()
	l.Info("Starting MCP server",
		zap.String("name", "annas-mcp"),
		zap.String("version", serverVersion),
	)

	server := mcp.NewServer("annas-mcp", serverVersion, nil)

	server.AddTools(
		mcp.NewServerTool("search", "Search Anna's Archive. Set content to 'book_any' to search books (default), or 'journal' to search journal articles and academic papers. When the user asks for papers or articles, use content=journal. To find a specific paper by DOI, use the doi tool instead.", SearchTool, mcp.Input(
			mcp.Property("term", mcp.Description("Search query (e.g. book title, author, topic, or paper keywords)")),
			mcp.Property("content", mcp.Description("Content type: 'book_any' for books (default), 'journal' for academic papers and articles")),
		)),
		mcp.NewServerTool("download", "Download a book by its MD5 hash. Requires ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables.", DownloadTool, mcp.Input(
			mcp.Property("hash", mcp.Description("MD5 hash of the book to download")),
			mcp.Property("title", mcp.Description("Book title, used for filename")),
			mcp.Property("format", mcp.Description("Book format, for example pdf or epub")),
		)),
		mcp.NewServerTool("doi", "Look up a specific journal article by its DOI via SciDB. Returns authors, journal, size, and download links. If you don't have a DOI and the user wants to find papers by topic or keyword, use the search tool with content=journal instead.", DOITool, mcp.Input(
			mcp.Property("doi", mcp.Description("DOI of the paper (e.g. 10.1038/nature12345)")),
		)),
		mcp.NewServerTool("download_paper", "Download a journal article/paper by its DOI. Looks up the paper, then downloads via fast download (if available) or SciDB. Requires ANNAS_DOWNLOAD_PATH environment variable.", DownloadPaperTool, mcp.Input(
			mcp.Property("doi", mcp.Description("DOI of the paper to download (e.g. 10.1038/nature12345)")),
		)),
	)

	l.Info("MCP server started successfully")

	if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
		l.Fatal("MCP server failed", zap.Error(err))
	}
}
