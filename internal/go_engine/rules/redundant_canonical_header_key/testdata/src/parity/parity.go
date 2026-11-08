// Package parity holds every shape S1035's query branches on, and the near miss of each.
package parity

import (
	"net/http"
	nethttp "net/http"
	"strings"
)

type wrapper struct {
	http.Header
}

type pointerWrapper struct {
	*http.Header
}

type headerAlias = http.Header

// header is a type of the package's own whose methods are named as Header's are.
type header map[string][]string

func (header) Add(key string, value string) {}

func (header) Get(key string) string { return key }

// CanonicalHeaderKey is a function of the package's own named as net/http's is.
func CanonicalHeaderKey(key string) string { return key }

type adder interface {
	Add(key string, value string)
}

var canonicalize = http.CanonicalHeaderKey

func found(headers http.Header, key string) {
	headers.Add(http.CanonicalHeaderKey(key), "value")                          // found
	headers.Del(http.CanonicalHeaderKey(key))                                   // found
	_ = headers.Get(http.CanonicalHeaderKey(key))                               // found
	headers.Set(http.CanonicalHeaderKey(key), "value")                          // found
	headers.Add(nethttp.CanonicalHeaderKey(key), "value")                       // found through a renamed import
	headers.Add((http.CanonicalHeaderKey(key)), "value")                        // found inside the argument's parentheses
	(headers.Add)(http.CanonicalHeaderKey(key), "value")                        // found through the method's parentheses
	headers.Add((http.CanonicalHeaderKey)(key), "value")                        // found through the inner function's parentheses
	headers.Set(http.CanonicalHeaderKey("x-"+key), "value")                     // found, an expression as the key
	headers.Set(http.CanonicalHeaderKey(strings.ToLower(key)), "value")         // found, a call as the key
	headers.Set(http.CanonicalHeaderKey(http.CanonicalHeaderKey(key)), "value") // found once, the outer
	headers.Set(http.CanonicalHeaderKey(
		key,
	), "value") // found across lines
	headers.Set(http.CanonicalHeaderKey( /* the key */ key), "value")    // found, a comment the fix drops
	http.Header{}.Add(http.CanonicalHeaderKey(key), "value")             // found on a composite literal
	http.Header(map[string][]string{}).Del(http.CanonicalHeaderKey(key)) // found on a conversion
	var aliased headerAlias
	aliased.Del(http.CanonicalHeaderKey(key)) // found through an alias
	var wrapped wrapper
	wrapped.Get(http.CanonicalHeaderKey(key)) // found through an embedding
	pointer := pointerWrapper{Header: &headers}
	pointer.Set(http.CanonicalHeaderKey(key), "value") // found through an embedded pointer
	request := &http.Request{Header: http.Header{}}
	_ = request.Header.Get(http.CanonicalHeaderKey(key)) // found on a field
	_ = func() string {
		return headers.Get(http.CanonicalHeaderKey(key)) // found inside a function literal
	}()
}

func nearMisses(headers http.Header, key string, own header, values adder) {
	headers.Add(key, "value")                                        // no canonicalization
	headers.Add(key, http.CanonicalHeaderKey(key))                   // the value, not the key
	_ = headers.Values(http.CanonicalHeaderKey(key))                 // another method of Header
	_ = headers.Get(http.CanonicalHeaderKey(key) + "-x")             // a concatenation around the call
	_ = headers.Get(strings.TrimSpace(http.CanonicalHeaderKey(key))) // a call around the call
	_ = headers.Get(CanonicalHeaderKey(key))                         // the package's own function named alike
	_ = headers.Get(canonicalize(key))                               // a variable holding the function
	own.Add(http.CanonicalHeaderKey(key), "value")                   // another type's method named alike
	_ = own.Get(http.CanonicalHeaderKey(key))                        // another type's method named alike
	values.Add(http.CanonicalHeaderKey(key), "value")                // an interface's method
	values = headers
	values.Add(http.CanonicalHeaderKey(key), "value") // an interface holding a Header
	add := headers.Add
	add(http.CanonicalHeaderKey(key), "value")                          // a method value
	http.Header.Add(headers, http.CanonicalHeaderKey(key), "value")     // a method expression, the key second
	(*http.Header).Set(&headers, http.CanonicalHeaderKey(key), "value") // a method expression on the pointer
	_ = http.CanonicalHeaderKey(key)                                    // the call alone
}
