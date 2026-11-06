// Package allowed holds responses whose status is read, or which are handed somewhere the rule cannot follow, or
// whose body is never read.
package allowed

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
)

// The check first, the usual guard.
func checkedFirst(address string) ([]byte, error) {
	response, requestError := http.Get(address)
	if requestError != nil {
		return nil, requestError
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getting %s: %s", address, response.Status)
	}
	return io.ReadAll(response.Body)
}

// The body first, to put the server's words in the error, then the check: Go's usual shape, and the twins accept a
// check after the read too.
func checkedAfter(client *http.Client, request *http.Request, profile *map[string]string) error {
	response, requestError := client.Do(request)
	if requestError != nil {
		return requestError
	}
	defer response.Body.Close()
	body, readError := io.ReadAll(response.Body)
	if readError != nil {
		return readError
	}
	if response.StatusCode >= 400 {
		return fmt.Errorf("server said %d: %s", response.StatusCode, body)
	}
	return json.Unmarshal(body, profile)
}

// Status, the text, read only in a log line, still asks.
func statusLogged(address string) []byte {
	response, _ := http.Get(address)
	log.Println(response.Status)
	body, _ := io.ReadAll(response.Body)
	return body
}

// A check inside a closure is a check in the function.
func checkedInClosure(address string) []byte {
	response, _ := http.Get(address)
	succeeded := func() bool { return response.StatusCode < 300 }
	if !succeeded() {
		return nil
	}
	body, _ := io.ReadAll(response.Body)
	return body
}

// Handed to a helper, which may read the status.
func handedOn(address string) ([]byte, error) {
	response, requestError := http.Get(address)
	if requestError != nil {
		return nil, requestError
	}
	if validateError := validate(response); validateError != nil {
		return nil, validateError
	}
	return io.ReadAll(response.Body)
}

func validate(response *http.Response) error {
	if response.StatusCode != http.StatusOK {
		return errors.New(response.Status)
	}
	return nil
}

// Returned alongside the body: the caller holds the response, status and all.
func returned(address string) (*http.Response, []byte) {
	response, _ := http.Get(address)
	body, _ := io.ReadAll(response.Body)
	return response, body
}

// Stored, assigned, put in a literal, its address taken, and a method called on it: each hands the response on.
type envelope struct {
	response *http.Response
}

func stored(address string) []byte {
	response, _ := http.Get(address)
	kept := envelope{response: response}
	_ = kept
	body, _ := io.ReadAll(response.Body)
	return body
}

func assigned(address string) []byte {
	response, _ := http.Get(address)
	var other *http.Response
	other = response
	_ = other
	body, _ := io.ReadAll(response.Body)
	return body
}

func addressTaken(address string, responses chan<- **http.Response) []byte {
	response, _ := http.Get(address)
	responses <- &response
	body, _ := io.ReadAll(response.Body)
	return body
}

func methodCalled(address string, destination io.Writer) {
	response, _ := http.Get(address)
	response.Write(destination)
	io.ReadAll(response.Body)
}

// A field written changes what a later read finds, so the rule stops judging.
func fieldWritten(address string, replacement io.ReadCloser) {
	response, _ := http.Get(address)
	response.Body = replacement
	io.ReadAll(response.Body)
}

// Only closed, never read.
func onlyClosed(address string) error {
	response, requestError := http.Get(address)
	if requestError != nil {
		return requestError
	}
	return response.Body.Close()
}

// Drained into io.Discard, or io/ioutil's, so the connection can be reused: read only to be thrown away.
func drained(address string) {
	response, _ := http.Get(address)
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	io.CopyN(ioutil.Discard, response.Body, 512)
}

// A deferred call is cleanup on the way out.
func deferredCleanup(address string) {
	response, _ := http.Get(address)
	defer drainAndClose(response.Body)
	defer response.Body.Read(nil)
}

func drainAndClose(body io.ReadCloser) {
	io.Copy(io.Discard, body)
	body.Close()
}

// Error checked and nothing else read: no body, no finding.
func headersOnly(address string) string {
	response, requestError := http.Head(address)
	if requestError != nil {
		return ""
	}
	defer response.Body.Close()
	return response.Header.Get("ETag")
}
