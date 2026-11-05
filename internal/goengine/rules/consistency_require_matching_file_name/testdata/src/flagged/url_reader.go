package flagged

// A file named for a type that has no methods is named for nothing the rule counts, so the type it serves is judged.
type UrlReader struct {
	source string
}

type urlParser struct { // want "`urlParser` is the one type .* Name it `url_parser.go`.$"
	reader UrlReader
}

func (parser *urlParser) Parse() string {
	return parser.reader.source
}
