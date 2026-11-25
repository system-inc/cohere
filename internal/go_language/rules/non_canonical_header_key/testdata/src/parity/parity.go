package parity

import (
	"net/http"

	"parity/keys"
)

const packageKey = "x-request-id"

const canonicalKey = "X-Request-Id"

type aliased = http.Header

type aliasOfAlias = aliased

type ownHeader http.Header

type holder struct {
	header http.Header
}

func reads(h http.Header, r *http.Request, variable string) {
	const localKey = "content-type"
	_ = h["foo"]
	_ = h["Foo"]
	_ = h[`etag`]
	_ = h[localKey]
	_ = h[packageKey]
	_ = h[canonicalKey]
	_ = h[keys.Lower]
	_ = h[keys.Canonical]
	_ = h[("foo")]
	_ = h["x-"+"foo"]
	_ = h[variable]
	_ = h[""]
	_ = h["foo bar"]
	_ = h["X-FOO"]
	_ = h["x_foo"]
	_ = r.Header["accept"]
	_ = (h)["foo"]
	_ = (h["foo"])
}

func definitions(h http.Header) {
	value := h["foo"]
	_ = value
	other, found := h["bar"]
	_, _ = other, found
	var declared = h["baz"]
	_ = declared
	for range h["range"] {
	}
	if len(h["cond"]) > 0 {
	}
	println(h["argument"])
}

func writes(h http.Header, other http.Header, m map[string][]string) {
	h["foo"] = nil
	h["foo"], m["bar"] = nil, nil
	m["bar"], h["foo"] = nil, nil
	h["write"] = other["unjudged"]
	h["write"] = append(h["unjudged"], "value")
	h["write"] = func() []string { return other["unjudged"] }()
	m["plain"] = h["judged"]
	(h["parenthesized"]) = nil
	var discard []string
	discard = h["judged"]
	_ = discard
	h2 := h
	h2["foo"] = h["unjudged"]
}

func types(a aliased, b aliasOfAlias, own ownHeader, pointer *http.Header, held holder, slice []http.Header) {
	_ = a["foo"]
	_ = b["foo"]
	_ = own["foo"]
	_ = (*pointer)["foo"]
	_ = held.header["foo"]
	_ = slice[0]["foo"]
	_ = http.Header{"foo": nil}
	_ = map[string][]string(own)["foo"]
	_ = http.Header(own)["foo"]
}

func nested(h http.Header) {
	_ = h[h["inner"][0]]
	_ = h[h["Inner"][0]]
}

func generic[H http.Header, M ~map[string][]string](h H, m M) {
	_ = h["foo"]
	_ = m["foo"]
}

func multiline(h http.Header) {
	_ = h["multi-line"]
	_ = h[ /* before */ "commented" /* after */]
	_ = h[localIdentifier()]
}

func localIdentifier() string { return "foo" }
