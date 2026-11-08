// Package parity holds the shapes the pattern
// `(CallExpr (SelectorExpr (CallExpr (SelectorExpr recv (Ident "Query")) []) (Ident meth)) _)` accepts and refuses,
// with the receiver types the original then asks about and the methods it names. Each function's comment says what
// the original makes of it; the parity test holds only that the port makes the same.
package parity

import "net/url"

type link = url.URL

type ownURL url.URL

func (ownURL) Query() url.Values { return nil }

type embedding struct {
	*url.URL
}

type holder struct {
	address *url.URL
}

type querier struct{}

func (querier) Query(key string) url.Values { return nil }

func use(values ...any) {}

func parse() *url.URL { return nil }

// Found thrice: each method the original names.
func each(address *url.URL) {
	address.Query().Add("a", "b")
	address.Query().Set("a", "b")
	address.Query().Del("a")
}

// Not found: methods that only read the copy.
func reading(address *url.URL) {
	use(address.Query().Get("a"), address.Query().Has("a"), address.Query().Encode())
}

// Found four times: parentheses around the receiver, the Query call, the Query selector and the method's selector.
func parentheses(address *url.URL) {
	(address).Query().Add("a", "b")
	(address.Query()).Add("a", "b")
	(address.Query)().Add("a", "b")
	(address.Query().Add)("a", "b")
}

// Found: a field, a call's result, an address taken, an alias.
func receivers(container holder, value url.URL, aliased *link) {
	container.address.Query().Set("a", "b")
	parse().Query().Set("a", "b")
	(&value).Query().Set("a", "b")
	aliased.Query().Set("a", "b")
}

// Not found: a URL value, not a pointer, though its Query is the same copy.
func byValue(value url.URL) {
	value.Query().Set("a", "b")
}

// Not found: the pointer is embedded, so the receiver's type is the struct's.
func embedded(container embedding) {
	container.Query().Set("a", "b")
}

// Not found: a defined type with a Query of its own, and a Query that takes an argument.
func otherQueries(own *ownURL, other querier) {
	own.Query().Set("a", "b")
	other.Query("a").Set("a", "b")
}

// Found: deferred and in a goroutine, each a call.
func deferredAndSpawned(address *url.URL) {
	defer address.Query().Del("a")
	go address.Query().Del("a")
}

// Not found: the copy kept in a variable, and a method value never called here.
func kept(address *url.URL) {
	values := address.Query()
	values.Set("a", "b")
	add := address.Query().Add
	use(add)
}

// Not found: a type parameter whose core type is a pointer to URL is no pointer type itself.
func generic[Pointer interface {
	*url.URL
	Query() url.Values
}](address Pointer) {
	address.Query().Set("a", "b")
}

// Found: converted to the pointer it stands for, it is one.
func converted[Pointer interface{ *url.URL }](address Pointer) {
	(*url.URL)(address).Query().Set("a", "b")
}
