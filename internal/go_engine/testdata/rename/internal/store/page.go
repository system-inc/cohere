package store

// page is the one Fetcher.
type page struct{}

// FetchURL fetches an empty page.
func (page) FetchURL() string {
	return ""
}
