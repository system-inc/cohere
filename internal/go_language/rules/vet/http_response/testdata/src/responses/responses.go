// Package responses returns net/http's responses from functions of its own, for the parity fixtures: imported under
// the name http, its functions are what the original takes for net/http's, and through its client a package that
// never imports net/http calls net/http's methods.
package responses

import "net/http"

// Client is net/http's client, reached from a package that does not import net/http.
var Client = &http.Client{}

// Last is a response a package that dot-imports this one assigns.
var Last *http.Response

// Get returns a response and an error as net/http's Get does.
func Get(url string) (*http.Response, error) {
	return http.Get(url)
}

// Fetch is a generic function returning a response, called through an instantiation.
func Fetch[T any](url string) (*http.Response, error) {
	return http.Get(url)
}
