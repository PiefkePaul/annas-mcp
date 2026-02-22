package modes

type SearchParams struct {
	SearchTerm string `json:"term" mcp:"Term to search for"`
	Content    string `json:"content" mcp:"Content type filter: book_any (default) for books, journal for papers/articles"`
}

type DownloadParams struct {
	BookHash string `json:"hash" mcp:"MD5 hash of the book to download"`
	Title    string `json:"title" mcp:"Book title, used for filename"`
	Format   string `json:"format" mcp:"Book format, for example pdf or epub"`
}

type DOIParams struct {
	DOI string `json:"doi" mcp:"DOI of the paper to look up (e.g. 10.1038/nature12345)"`
}

type DownloadPaperParams struct {
	DOI string `json:"doi" mcp:"DOI of the paper to download"`
}
