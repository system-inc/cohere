// Package dotted calls net/http's names through a dot import, which names them by their package's path all the same.
package dotted

import . "net/http"

func found(headers Header, key string) {
	headers.Add(CanonicalHeaderKey(key), "value") // found
	_ = headers.Get((CanonicalHeaderKey)(key))    // found through the function's parentheses
}

func nearMisses(headers Header, key string) {
	canonicalKey := CanonicalHeaderKey
	_ = headers.Get(canonicalKey(key))               // a local holding the function
	_ = Header.Get(headers, CanonicalHeaderKey(key)) // a method expression
}
