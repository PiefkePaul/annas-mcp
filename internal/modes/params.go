package modes

type BookSearchParams struct {
	Query string `json:"query" mcp:"Search query for books (e.g., title, author, topic)"`
}

type BookDownloadParams struct {
	BookHash  string `json:"hash" mcp:"MD5 hash of the book to download"`
	Title     string `json:"title" mcp:"Book title, used for filename"`
	Format    string `json:"format" mcp:"Book format, for example pdf or epub"`
	SecretKey string `json:"secret_key,omitempty" mcp:"Optional Anna's Archive fast-download secret key. Required if the server has no default ANNAS_SECRET_KEY configured."`
}

type ArticleSearchParams struct {
	Query string `json:"query" mcp:"DOI (e.g., '10.1038/nature12345') or search keywords for articles"`
}

type ArticleDownloadParams struct {
	DOI       string `json:"doi" mcp:"DOI of the article to download (e.g., '10.1038/nature12345')"`
	SecretKey string `json:"secret_key,omitempty" mcp:"Optional Anna's Archive fast-download secret key. Used before falling back to SciDB."`
}
