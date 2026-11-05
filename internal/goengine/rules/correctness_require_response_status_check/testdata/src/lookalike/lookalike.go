// Package lookalike holds body reads that look like an unchecked net/http response and are not one the rule tracks.
package lookalike

import (
	"io"
	"net/http"
	"strings"
)

// A Response of our own, from a Get of our own, has a Body that is no net/http response's.
type Response struct {
	Body io.Reader
}

// Get is named like net/http's.
func Get(address string) (*Response, error) {
	return &Response{Body: strings.NewReader(address)}, nil
}

func ownResponse(address string) {
	response, _ := Get(address)
	io.ReadAll(response.Body)
}

// A client of ours with a Do of its own may check the status before it returns, as a wrapper does.
type client struct {
	inner *http.Client
}

func (wrapper client) Do(request *http.Request) (*http.Response, error) {
	return wrapper.inner.Do(request)
}

func wrapped(wrapper client, request *http.Request) {
	response, _ := wrapper.Do(request)
	io.ReadAll(response.Body)
}

// An interface's Do is whatever implements it.
type doer interface {
	Do(request *http.Request) (*http.Response, error)
}

func throughInterface(implementation doer, request *http.Request) {
	response, _ := implementation.Do(request)
	io.ReadAll(response.Body)
}

// A function value is not resolved to the function it holds.
func functionValue(address string) {
	get := http.Get
	response, _ := get(address)
	io.ReadAll(response.Body)
}

// A parameter and a named result come from somewhere the rule cannot see.
func parameter(response *http.Response) {
	io.ReadAll(response.Body)
}

func namedResult(address string) (response *http.Response, body []byte) {
	response, _ = http.Get(address)
	body, _ = io.ReadAll(response.Body)
	return
}

// A package variable is not local.
var shared, _ = http.Get("https://example.com")

func packageVariable() {
	io.ReadAll(shared.Body)
}

// A variable written by a producer and by something else too may hold either.
func mixedWrites(address string, cached *http.Response) {
	response, _ := http.Get(address)
	if cached != nil {
		response = cached
	}
	io.ReadAll(response.Body)
}

func rangeWritten(responses []*http.Response) {
	var response *http.Response
	response, _ = http.Get("https://example.com")
	for _, response = range responses {
	}
	io.ReadAll(response.Body)
}

// A RoundTripper is outside the calls the rule tracks.
func roundTrip(transport http.RoundTripper, request *http.Request) {
	response, _ := transport.RoundTrip(request)
	io.ReadAll(response.Body)
}

// A body converted, returned or compared is not seen being read.
func convertedOrReturned(address string) io.ReadCloser {
	response, _ := http.Get(address)
	_ = io.Reader(response.Body)
	if response.Body == nil {
		return nil
	}
	return response.Body
}
