// Package parity calls both functions with addresses of each kind, valid and not, in the shapes staticcheck's
// representation builds an address or a call in more than one way.
package parity

import (
	"net/http"
	"strconv"
)

type address string

const (
	broken  = "localhost:65536"
	joined  = "localhost:" + "8080"
	typed   = address("host:")
	invalid = "[::1]"
)

var served = http.ListenAndServe("localhost:99999", nil)

func eachFunction() {
	http.ListenAndServe("localhost:8080/", nil)
	http.ListenAndServe("localhost:8080", nil)
	http.ListenAndServeTLS("localhost", "certificate", "key", nil)
	http.ListenAndServeTLS(":https", "certificate", "key", nil)
	http.ListenAndServe(":0", nil)
	http.ListenAndServe(":65535", nil)
	http.ListenAndServe(":-1", nil)
	http.ListenAndServe(":", nil)
	http.ListenAndServe("[::1]:80", nil)
	http.ListenAndServe("::1:80", nil)
	http.ListenAndServe("0.0.0.0:http", nil)
	http.ListenAndServe(":+80", nil)
	http.ListenAndServe(":0x50", nil)
	http.ListenAndServe(":080", nil)
	http.ListenAndServe(":8080 ", nil)
	http.Get("localhost")
	_ = (&http.Server{Addr: "localhost"}).ListenAndServe()
}

func serviceNames() {
	http.ListenAndServe(":http-alt", nil)
	http.ListenAndServe(":h--p", nil)
	http.ListenAndServe(":-http", nil)
	http.ListenAndServe(":http-", nil)
	http.ListenAndServe(":abcdefghijklmno", nil)
	http.ListenAndServe(":abcdefghijklmnop", nil)
	http.ListenAndServe(":a1", nil)
	http.ListenAndServe(":12a", nil)
	http.ListenAndServe(":h_p", nil)
	http.ListenAndServe(":é", nil)
}

func addressShapes(port int, given string, condition bool) {
	http.ListenAndServe(broken, nil)
	http.ListenAndServe(joined, nil)
	http.ListenAndServe(string(typed), nil)
	http.ListenAndServe(invalid, nil)
	http.ListenAndServe(("localhost"), nil)
	local := "localhost"
	http.ListenAndServe(local, nil)
	http.ListenAndServe(given, nil)
	http.ListenAndServe(":"+strconv.Itoa(port), nil)
	differing := "localhost"
	if condition {
		differing = ":80"
	}
	http.ListenAndServe(differing, nil)
}

func callShapes() {
	defer http.ListenAndServe("localhost", nil)
	go http.ListenAndServe("localhost", nil)
	func() {
		http.ListenAndServe("localhost", nil)
	}()
	listen := http.ListenAndServe
	listen("localhost", nil)
	http.ListenAndServe(
		"localhost",
		nil,
	)
	if err := http.ListenAndServe("localhost", nil); err != nil {
		return
	}
	_ = served
}

func generic[T any](value T) T {
	http.ListenAndServe("localhost", nil)
	return value
}
