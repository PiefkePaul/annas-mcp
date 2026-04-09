package modes

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PiefkePaul/annas-mcp/internal/anna"
	"github.com/PiefkePaul/annas-mcp/internal/auth"
	"github.com/PiefkePaul/annas-mcp/internal/env"
	"github.com/PiefkePaul/annas-mcp/internal/logger"
	"github.com/PiefkePaul/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const serverInstructions = "Use the Anna's Archive search tools first to find books or academic papers. Download tools can return files as embedded MCP resources when the payload fits within the configured inline size limit. For book downloads, ask for the user's Anna's Archive fast-download secret key when it has not already been provided, unless the user has authenticated through OAuth and already registered one with the server. Article downloads can fall back to SciDB when no secret key is available. The server automatically retries official Anna's Archive mirrors when one is unavailable."

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

func bookDownloadToolWithIdentity(identity *auth.Identity, ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()

	l.Info("Download command called",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("title", params.Arguments.Title),
		zap.String("format", params.Arguments.Format),
	)

	secretKey := resolveSecretKey(params.Arguments.SecretKey, identity)
	if secretKey == "" {
		return nil, errors.New("book_download requires a registered user secret, a secret_key argument, or a server-side ANNAS_SECRET_KEY default")
	}

	book := &anna.Book{
		Hash:   params.Arguments.BookHash,
		Title:  params.Arguments.Title,
		Format: params.Arguments.Format,
	}

	file, err := book.DownloadInline(secretKey, env.GetMaxInlineDownloadBytes())
	if err != nil {
		if errors.Is(err, anna.ErrInlineDownloadTooLarge) {
			return textResult(inlineLimitMessage("book")), nil
		}

		l.Error("Download command failed",
			zap.String("bookHash", params.Arguments.BookHash),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Download command completed successfully",
		zap.String("bookHash", params.Arguments.BookHash),
		zap.String("filename", file.Filename),
		zap.Int64("bytes", file.Size),
	)

	return inlineFileResult(file, "Book downloaded successfully and returned as an embedded file."), nil
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

func articleDownloadToolWithIdentity(identity *auth.Identity, ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	l := logger.GetLogger()
	doi := strings.TrimSpace(params.Arguments.DOI)

	l.Info("Download paper command called", zap.String("doi", doi))

	paper, err := anna.LookupDOI(doi)
	if err != nil {
		l.Error("DOI lookup failed for download",
			zap.String("doi", doi),
			zap.Error(err),
		)
		return nil, err
	}

	secretKey := resolveSecretKey(params.Arguments.SecretKey, identity)
	if paper.Hash != "" && secretKey != "" {
		book := &anna.Book{
			Hash:   paper.Hash,
			Title:  paper.Title,
			Format: "pdf",
		}

		file, err := book.DownloadInline(secretKey, env.GetMaxInlineDownloadBytes())
		if err != nil {
			if errors.Is(err, anna.ErrInlineDownloadTooLarge) {
				return textResult(inlineLimitMessage("article")), nil
			}

			l.Warn("Fast download failed, trying SciDB download",
				zap.String("doi", doi),
				zap.Error(err),
			)
		} else {
			l.Info("Paper downloaded via fast download",
				zap.String("doi", doi),
				zap.String("filename", file.Filename),
				zap.Int64("bytes", file.Size),
			)
			return inlineFileResult(file, "Article downloaded successfully via fast download and returned as an embedded file."), nil
		}
	}

	file, err := paper.DownloadInline(env.GetMaxInlineDownloadBytes())
	if err != nil {
		if errors.Is(err, anna.ErrInlineDownloadTooLarge) {
			return textResult(inlineLimitMessage("article")), nil
		}

		l.Error("SciDB download failed",
			zap.String("doi", doi),
			zap.Error(err),
		)
		return nil, err
	}

	l.Info("Paper downloaded via SciDB",
		zap.String("doi", doi),
		zap.String("filename", file.Filename),
		zap.Int64("bytes", file.Size),
	)

	return inlineFileResult(file, "Article downloaded successfully via SciDB and returned as an embedded file."), nil
}

func newMCPServer(serverVersion string, identity *auth.Identity) *mcp.Server {
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

	bookDownloadTool := mcp.NewServerTool(
		"book_download",
		"Download a book file by MD5 hash. The file is returned as an embedded MCP resource when it fits within the configured inline size limit. Requires an Anna's Archive fast-download secret key from the signed-in user account, the tool's secret_key argument, or a server-side default.",
		func(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookDownloadParams]) (*mcp.CallToolResultFor[any], error) {
			return bookDownloadToolWithIdentity(identity, ctx, cc, params)
		},
		mcp.Input(
			mcp.Property("hash", mcp.Description("The MD5 hash returned by book_search.")),
			mcp.Property("title", mcp.Description("Book title used to create the returned filename.")),
			mcp.Property("format", mcp.Description("Book file format, for example pdf or epub.")),
			mcp.Property("secret_key", mcp.Description("Optional Anna's Archive fast-download secret key. Usually not needed when the user authenticated through OAuth and stored one in the account portal.")),
		),
	)
	decorateWriteTool(bookDownloadTool, "Download a book file from Anna's Archive")
	server.AddTools(bookDownloadTool)

	articleDownloadTool := mcp.NewServerTool(
		"article_download",
		"Download an academic paper by DOI. The file is returned as an embedded MCP resource when it fits within the configured inline size limit. Optional secret_key enables Anna's fast-download path before falling back to SciDB.",
		func(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
			return articleDownloadToolWithIdentity(identity, ctx, cc, params)
		},
		mcp.Input(
			mcp.Property("doi", mcp.Description("The DOI of the paper to download, for example 10.1038/nature12345.")),
			mcp.Property("secret_key", mcp.Description("Optional Anna's Archive fast-download secret key. Usually not needed when the user authenticated through OAuth and stored one in the account portal.")),
		),
	)
	decorateWriteTool(articleDownloadTool, "Download an academic paper by DOI")
	server.AddTools(articleDownloadTool)

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

	server := newMCPServer(serverVersion, nil)

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

func inlineFileResult(file *anna.DownloadedFile, message string) *mcp.CallToolResultFor[any] {
	resourceURI := "annas://download/" + url.PathEscape(file.Filename)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("%s\nFilename: %s\nMIME type: %s\nSize: %d bytes\nMirror: %s",
					message,
					file.Filename,
					file.MIMEType,
					file.Size,
					file.SourceMirror,
				),
			},
			&mcp.EmbeddedResource{
				Resource: &mcp.ResourceContents{
					URI:      resourceURI,
					MIMEType: file.MIMEType,
					Blob:     file.Data,
				},
			},
		},
	}
}

func resolveSecretKey(provided string, identity *auth.Identity) string {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		return provided
	}
	if identity != nil && strings.TrimSpace(identity.SecretKey) != "" {
		return strings.TrimSpace(identity.SecretKey)
	}
	return env.GetDefaultSecretKey()
}

func inlineLimitMessage(kind string) string {
	limitMB := env.GetMaxInlineDownloadBytes() / (1024 * 1024)
	return fmt.Sprintf("The %s download is larger than the current inline return limit of %d MB, so it could not be attached to the MCP response.", kind, limitMB)
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
