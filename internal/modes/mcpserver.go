package modes

import (
	"context"
	"fmt"
	"strings"

	"github.com/PiefkePaul/annas-mcp/internal/anna"
	"github.com/PiefkePaul/annas-mcp/internal/env"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"github.com/PiefkePaul/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const serverInstructions = "Use the Anna's Archive search tools first to find books or academic papers. Use download tools only when the user explicitly asks to save a file, because downloads write files into the server's configured download directory rather than returning file contents in chat. For article lookups, article_search also accepts a DOI directly when it starts with 10."

func BookSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookSearchParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()
	query := strings.TrimSpace(params.Arguments.Query)

	l.Info("Book search command called", zap.String("query", query))

	books, err := anna.FindBook(query)
	if err != nil {
		l.Error("Book search command failed",
			zap.String("query", query),
			zap.Error(err),
		)
		return nil, err
	}

	if len(books) == 0 {
		l.Info("Book search returned no results", zap.String("query", query))
		return textResult("No books found."), nil
	}

	l.Info("Book search command completed successfully",
		zap.String("query", query),
		zap.Int("resultsCount", len(books)),
	)

	return textResult(formatBookResults(books)), nil
}

func BookDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Download command called",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("title", params.Arguments.Title),
		zap.String("format", params.Arguments.Format),
	)

	downloadEnv, err := env.GetEnv()
	if err != nil {
		l.Error("Failed to get environment variables", zap.Error(err))
		return nil, err
	}

	book := &anna.Book{
		Hash:   params.Arguments.BookHash,
		Title:  params.Arguments.Title,
		Format: params.Arguments.Format,
	}

	if err := book.Download(downloadEnv.SecretKey, downloadEnv.DownloadPath); err != nil {
		l.Error("Download command failed",
			zap.String("bookHash", params.Arguments.BookHash),
			zap.String("downloadPath", downloadEnv.DownloadPath),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Download command completed successfully",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("downloadPath", downloadEnv.DownloadPath),
	)

	return textResult("Book downloaded successfully to path: " + downloadEnv.DownloadPath), nil
}

func ArticleSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleSearchParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()
	query := strings.TrimSpace(params.Arguments.Query)

	l.Info("Article search command called", zap.String("query", query))

	if strings.HasPrefix(query, "10.") {
		l.Info("Detected DOI format, performing DOI lookup", zap.String("doi", query))

		paper, err := anna.LookupDOI(query)
		if err != nil {
			l.Error("DOI lookup failed",
				zap.String("doi", query),
				zap.Error(err),
			)
			return textResult("No paper found for DOI: " + query), nil
		}

		l.Info("DOI lookup completed", zap.String("doi", query))
		return textResult(paper.String()), nil
	}

	papers, err := anna.FindArticle(query)
	if err != nil {
		l.Error("Article search failed",
			zap.String("query", query),
			zap.Error(err),
		)
		return nil, err
	}

	if len(papers) == 0 {
		l.Info("Article search returned no results", zap.String("query", query))
		return textResult("No articles found."), nil
	}

	l.Info("Article search command completed successfully",
		zap.String("query", query),
		zap.Int("resultsCount", len(papers)),
	)

	return textResult(formatPaperResults(papers)), nil
}

func ArticleDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()
	doi := strings.TrimSpace(params.Arguments.DOI)

	l.Info("Download paper command called", zap.String("doi", doi))

	downloadEnv, err := env.GetDownloadEnv(false)
	if err != nil {
		l.Error("Failed to get download environment variables", zap.Error(err))
		return nil, err
	}

	paper, err := anna.LookupDOI(doi)
	if err != nil {
		l.Error("DOI lookup failed for download",
			zap.String("doi", doi),
			zap.Error(err),
		)
		return nil, err
	}

	if paper.Hash != "" && downloadEnv.SecretKey != "" {
		book := &anna.Book{
			Hash:   paper.Hash,
			Title:  paper.Title,
			Format: "pdf",
		}
		if err := book.Download(downloadEnv.SecretKey, downloadEnv.DownloadPath); err != nil {
			l.Warn("Fast download failed, trying SciDB download",
				zap.String("doi", doi),
				zap.Error(err),
			)
		} else {
			l.Info("Paper downloaded via fast download",
				zap.String("doi", doi),
				zap.String("path", downloadEnv.DownloadPath),
			)
			return textResult("Paper downloaded successfully to path: " + downloadEnv.DownloadPath), nil
		}
	}

	if err := paper.Download(downloadEnv.DownloadPath); err != nil {
		l.Error("SciDB download failed",
			zap.String("doi", doi),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Paper downloaded via SciDB",
		zap.String("doi", doi),
		zap.String("path", downloadEnv.DownloadPath),
	)

	return textResult("Paper downloaded successfully to path: " + downloadEnv.DownloadPath), nil
}

func newMCPServer(serverVersion string) *mcp.Server {
	server := mcp.NewServer("annas-mcp", serverVersion, &mcp.ServerOptions{Instructions: serverInstructions})

	bookSearchTool := mcp.NewServerTool(
		"book_search",
		"Search Anna's Archive for books by title, author, or topic. Use this before any book download so you can inspect the metadata and MD5 hash first.",
		BookSearchTool,
		mcp.Input(
			mcp.Property("query", mcp.Description("Book search query, such as a title, author, ISBN, or topic.")),
		),
	)
	decorateReadOnlyTool(bookSearchTool, "Search books in Anna's Archive")
	server.AddTools(bookSearchTool)

	articleSearchTool := mcp.NewServerTool(
		"article_search",
		"Search Anna's Archive for academic papers by DOI or keywords. Use this to inspect article metadata before deciding whether to download.",
		ArticleSearchTool,
		mcp.Input(
			mcp.Property("query", mcp.Description("A DOI like 10.1038/nature12345 or free-text article keywords.")),
		),
	)
	decorateReadOnlyTool(articleSearchTool, "Search academic papers in Anna's Archive")
	server.AddTools(articleSearchTool)

	if env.CanBookDownload() {
		bookDownloadTool := mcp.NewServerTool(
			"book_download",
			"Download a book file by MD5 hash into the server's configured download directory. Use this only after the user explicitly asks to save a file.",
			BookDownloadTool,
			mcp.Input(
				mcp.Property("hash", mcp.Description("The MD5 hash returned by book_search.")),
				mcp.Property("title", mcp.Description("Book title used to create the local filename.")),
				mcp.Property("format", mcp.Description("Book file format, for example pdf or epub.")),
			),
		)
		decorateWriteTool(bookDownloadTool, "Download a book file from Anna's Archive")
		server.AddTools(bookDownloadTool)
	}

	if env.CanArticleDownload() {
		articleDownloadTool := mcp.NewServerTool(
			"article_download",
			"Download an academic paper by DOI into the server's configured download directory. Use this only after the user explicitly asks to save a file.",
			ArticleDownloadTool,
			mcp.Input(
				mcp.Property("doi", mcp.Description("The DOI of the paper to download, for example 10.1038/nature12345.")),
			),
		)
		decorateWriteTool(articleDownloadTool, "Download an academic paper by DOI")
		server.AddTools(articleDownloadTool)
	}

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

func textResult(text string) *mcp.CallToolResultFor[any] {
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func formatBookResults(books []*anna.Book) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Found %d books.\n\n", len(books))
	for i, book := range books {
		fmt.Fprintf(&builder, "Book %d\n%s", i+1, book.String())
		if i < len(books)-1 {
			builder.WriteString("\n\n")
		}
	}
	return builder.String()
}

func formatPaperResults(papers []*anna.Paper) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Found %d articles.\n\n", len(papers))
	for i, paper := range papers {
		fmt.Fprintf(&builder, "Article %d\n%s", i+1, paper.String())
		if i < len(papers)-1 {
			builder.WriteString("\n\n")
		}
	}
	return builder.String()
}

func decorateReadOnlyTool(tool *mcp.ServerTool, title string) {
	tool.Tool.Title = title
	tool.Tool.Annotations = &mcp.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  boolPtr(true),
	}
}

func decorateWriteTool(tool *mcp.ServerTool, title string) {
	tool.Tool.Title = title
	tool.Tool.Annotations = &mcp.ToolAnnotations{
		Title:         title,
		OpenWorldHint: boolPtr(true),
	}
}

func boolPtr(value bool) *bool {
	return &value
}
