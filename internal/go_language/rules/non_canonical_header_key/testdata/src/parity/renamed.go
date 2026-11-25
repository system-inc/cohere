package parity

import nethttp "net/http"

func renamed(h nethttp.Header) {
	const key = "renamed"
	_ = h[key]
	_ = h["renamed"]
}
