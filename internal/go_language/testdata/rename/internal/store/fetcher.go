package store

// Fetcher fetches a page.
type Fetcher interface {
	FetchURL() string
}
