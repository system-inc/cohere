// Package flagged holds declarations that state their type as any where this package chose it.
package flagged

import (
	"encoding/json"
	"fmt"

	"elsewhere"
)

// describe hands its value to a place that accepts anything, which fixes nothing.
func describe(value any) string { // want "as .any."
	return fmt.Sprint(value)
}

// parse returns whatever it decoded, and decodes into a variable that could hold anything.
func parse(data []byte) any { // want "as .any."
	var decoded any // want "as .any."
	_ = json.Unmarshal(data, &decoded)
	return decoded
}

// legacy spells the empty interface the old way.
func legacy(value interface{}) { // want "as .interface"
	fmt.Println(value)
}

// pair declares two parameters with one type, reported once where it is written.
func pair(first, second any) bool { // want "as .any."
	return first == second
}

// Envelope is a struct of this package, whose fields' types are ours to state, inside a larger type or not.
type Envelope struct {
	Kind    string
	Payload any                // want "as .any."
	Headers map[string]any     // want "as .any."
	Next    *any               // want "as .any."
	Queue   chan []interface{} // want "as .interface"
}

// Cache is an interface of this package, judged together with its implementations.
type Cache interface {
	Get(key string) any // want "as .any."
}

type memoryCache struct {
	entries map[string]any // want "as .any."
}

// Get implements this package's Cache, whose signature is ours, so both are reported.
func (cache *memoryCache) Get(key string) any { // want "as .any."
	return cache.entries[key]
}

// Save shares a name with a method of elsewhere.Store but not its signature, so the signature is this package's.
func (cache *memoryCache) Save(key string, value any) error { // want "as .any."
	cache.entries[key] = value
	return nil
}

// lastStore brings elsewhere.Store into the import graph, so Save is compared with its method and does not match.
var lastStore elsewhere.Store

// localRegistry's Lookup has the name and signature of elsewhere.Registry's, a concrete method, which fixes nothing of
// a concrete method of ours: only an interface fixes what implements it, so the signature is this package's.
type localRegistry struct {
	entries map[string]string
}

func (registry *localRegistry) Lookup(key string) any { // want "as .any."
	return registry.entries[key]
}

// cachedRegistry embeds elsewhere.Registry and declares a Lookup of its own, which shadows the embedded one. Go lets a
// shadowing method take any signature, so the embedded method fixes nothing of it, and with no interface asking for
// it the signature is this package's choice.
type cachedRegistry struct {
	elsewhere.Registry
	cache map[string]string
}

func (registry *cachedRegistry) Lookup(key string) any { // want "as .any."
	return registry.cache[key]
}

// lastRegistry and lastShelf bring elsewhere.Registry and elsewhere.Shelf into the import graph.
var (
	lastRegistry elsewhere.Registry
	lastShelf    elsewhere.Shelf[int]
)

// localShelf's Place has the shape of elsewhere.Shelf's, a generic concrete method, which no more fixes a concrete
// method of ours than a plain one does.
type localShelf struct{}

func (shelf localShelf) Place(item any) { // want "as .any."
	fmt.Println(item)
}

type pairHolder struct{}

// Hold takes two values where elsewhere.Holder's method takes one, so no instantiation of it is what this matches.
func (holder pairHolder) Hold(first any, second any) { // want "as .any." "as .any."
	fmt.Println(first, second)
}

// Box is a generic container of this package. Its constraint is no value's type.
type Box[Value any] struct {
	value Value
}

var shared Box[any] // want "as .any."

// boxed returns a type argument it chose, while the literal it returns spells its type inside an expression.
func boxed() Box[interface{}] { // want "as .interface"
	return Box[interface{}]{}
}

var lastValue any // want "as .any."

// collect builds a slice it hands on as one value, to a place that accepts anything.
func collect() {
	var scratch []any // want "as .any."
	scratch = append(scratch, 1)
	fmt.Println(scratch)
}

// Payload is this package's name for any, which hides the type at every use.
type Payload = any // want "as .any."

// Visitor is a function type of this package.
type Visitor func(node any) bool // want "as .any."

// each hands every value to visit. Its parameter's type is this package's and is reported here, while the literal
// handed to it below takes its type from it and is not reported again.
func each(values []string, visit func(value any)) { // want "as .any."
	for _, value := range values {
		visit(value)
	}
}

func useEach() {
	each([]string{"one"}, func(value any) {
		fmt.Println(value)
	})
}

// literals declares function literals called where they stand or through a variable, which no place fixes.
func literals() {
	go func(value any) { // want "as .any."
		fmt.Println(value)
	}(1)
	handler := func(value any) { // want "as .any."
		fmt.Println(value)
	}
	handler(2)
}

// count only counts its values, forwarding none of them.
func count(values ...any) int { // want "as .any."
	return len(values)
}

// show hands its values on as one value, without the ellipsis, to a place that accepts anything.
func show(values ...any) { // want "as .any."
	fmt.Println(values)
}

// copies hands first to second's declaration, which fixes first's type; second's type is this package's choice.
func copies() {
	var first []any
	var second []any = first // want "as .any."
	fmt.Println(second)
}

// fresh returns a value of its own choosing.
func fresh() any { // want "as .any."
	return 1
}

// copied takes its value from fresh, whose type is this package's, so the type is still ours to state.
func copied() {
	var held any = fresh() // want "as .any."
	fmt.Println(held)
}

// local declares a type inside a function, whose field's type is as much ours as one at package level.
func local() {
	type row struct {
		cells []any // want "as .any."
	}
	fmt.Println(row{})
}
