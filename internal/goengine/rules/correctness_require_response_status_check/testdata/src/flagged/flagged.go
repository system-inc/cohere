// Package flagged holds body reads of a response net/http returned, in functions that never read its status.
package flagged

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Every package function that returns the response whatever its status.
func packageFunctions(address string) {
	got, _ := http.Get(address)
	io.ReadAll(got.Body) // want "reads the body"
	posted, _ := http.Post(address, "text/plain", strings.NewReader("hello"))
	io.ReadAll(posted.Body) // want "reads the body"
	head, _ := http.Head(address)
	io.ReadAll(head.Body) // want "reads the body"
	form, _ := http.PostForm(address, url.Values{})
	io.ReadAll(form.Body) // want "reads the body"
}

// Every method of *http.Client that does, the default client's included.
func clientMethods(client *http.Client, request *http.Request, address string) {
	done, _ := client.Do(request)
	io.ReadAll(done.Body) // want "reads the body"
	got, _ := client.Get(address)
	io.ReadAll(got.Body) // want "reads the body"
	posted, _ := client.Post(address, "text/plain", nil)
	io.ReadAll(posted.Body) // want "reads the body"
	head, _ := client.Head(address)
	io.ReadAll(head.Body) // want "reads the body"
	form, _ := client.PostForm(address, url.Values{})
	io.ReadAll(form.Body) // want "reads the body"
	fallback, _ := http.DefaultClient.Get(address)
	io.ReadAll(fallback.Body) // want "reads the body"
}

// Each shape of read: a decoder, a reader built on it, a copy anywhere but io.Discard, a method, a method value, a
// goroutine, and parentheses around either half.
func shapesOfRead(address string, profile *map[string]string) error {
	response, requestError := http.Get(address)
	if requestError != nil {
		return requestError
	}
	defer response.Body.Close()
	if decodeError := json.NewDecoder(response.Body).Decode(profile); decodeError != nil { // want "reads the body"
		return decodeError
	}
	bufio.NewReader((response.Body))    // want "reads the body"
	io.Copy(os.Stdout, (response).Body) // want "reads the body"
	buffer := make([]byte, 16)
	response.Body.Read(buffer) // want "reads the body"
	read := response.Body.Read // want "reads the body"
	read(buffer)
	go io.ReadAll(response.Body) // want "reads the body"
	return nil
}

// Headers, the content length and a nil comparison say nothing about whether the request succeeded.
func neutralReads(address string) string {
	response, _ := http.Get(address)
	if response == nil || response.ContentLength == 0 {
		return response.Header.Get("Content-Type")
	}
	body, _ := io.ReadAll(response.Body) // want "reads the body"
	return string(body)
}

// A var with a value, a var with none then a producer, a := inside an if, and a retry that writes the response
// from a producer each time.
func declarations(client *http.Client, request *http.Request, address string) {
	var declared, _ = http.Get(address)
	io.ReadAll(declared.Body) // want "reads the body"

	var later *http.Response
	var requestError error
	later, requestError = client.Do(request)
	if requestError == nil {
		io.ReadAll(later.Body) // want "reads the body"
	}

	if scoped, scopedError := http.Get(address); scopedError == nil {
		io.ReadAll(scoped.Body) // want "reads the body"
	}

	retried, retryError := client.Do(request)
	for attempt := 0; retryError != nil && attempt < 3; attempt++ {
		retried, retryError = client.Do(request)
	}
	io.ReadAll(retried.Body) // want "reads the body"
}

// A read inside a closure is a read in the function, and a check nowhere covers it.
func closure(address string) func() []byte {
	response, _ := http.Get(address)
	return func() []byte {
		body, _ := io.ReadAll(response.Body) // want "reads the body"
		return body
	}
}

// The outer response is checked; the inner one is another variable of the same name, and is not.
func shadowed(address string) {
	response, _ := http.Get(address)
	if response.StatusCode != http.StatusOK {
		return
	}
	io.ReadAll(response.Body)
	func() {
		response, _ := http.Get(address)
		io.ReadAll(response.Body) // want "reads the body"
	}()
}

// A client embedded in a type of ours still runs net/http's method.
type api struct {
	*http.Client
}

func (service api) profile(address string) []byte {
	response, _ := service.Get(address)
	body, _ := io.ReadAll(response.Body) // want "reads the body"
	return body
}

// A generic function is read like any other.
func decoded[Value any](address string) (Value, error) {
	var value Value
	response, requestError := http.Get(address)
	if requestError != nil {
		return value, requestError
	}
	decodeError := json.NewDecoder(response.Body).Decode(&value) // want "reads the body"
	return value, decodeError
}

// A response alias names the same type.
type reply = http.Response

func aliased(address string) {
	var response *reply
	response, _ = http.Get(address)
	io.ReadAll(response.Body) // want "reads the body"
}
