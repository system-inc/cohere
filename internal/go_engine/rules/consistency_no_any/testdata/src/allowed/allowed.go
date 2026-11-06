// Package allowed holds declarations whose any another package fixes, so none of them is reported.
package allowed

import (
	"context"
	"fmt"
	"slices"

	"elsewhere"
)

// memoryStore implements elsewhere.Store, whose methods fix its signatures.
type memoryStore struct {
	entries map[string]string
}

var _ elsewhere.Store = (*memoryStore)(nil)

func (store *memoryStore) Load(key string) (any, bool) {
	value, found := store.entries[key]
	return value, found
}

func (store *memoryStore) Save(key string, value any) {
	store.entries[key] = fmt.Sprint(value)
}

// printer is written to accept elsewhere's *Printer, so its method copies that type's.
type printer interface {
	Printf(format string, arguments ...any)
}

var _ printer = &elsewhere.Printer{}

// anyHolder has the shape of elsewhere.Holder's method, which an instantiation, Holder[any], may be what it meets.
type anyHolder struct{}

func (holder anyHolder) Hold(value any) {
	fmt.Println(value)
}

// valueContext implements context.Context, whose Value method takes and returns any.
type valueContext struct {
	context.Context
}

func (valued valueContext) Value(key any) any {
	return valued.Context.Value(key)
}

// keepGoing is handed to elsewhere.Visit as a value, whose parameter fixes its signature.
func keepGoing(value any) bool {
	return value != nil
}

// keepLooking is handed on through a variable a short declaration defines, which stands for it.
func keepLooking(value any) bool {
	return value == nil
}

type filter struct{}

// accept is handed to elsewhere.Visit as a method value.
func (filter filter) accept(value any) bool {
	return value != nil
}

// runPlugin is given to a field of elsewhere's struct, whose type fixes its signature.
func runPlugin(options any) error {
	return fmt.Errorf("running %v", options)
}

func visits(chosen filter) {
	elsewhere.Visit(elsewhere.Arguments(), func(value any) bool {
		return value != nil
	})
	elsewhere.Visit(nil, keepGoing)
	callback := keepLooking
	elsewhere.Visit(nil, callback)
	elsewhere.Visit(nil, chosen.accept)
	visitor := elsewhere.VisitFunc(func(value any) bool {
		return value == nil
	})
	fmt.Println(visitor(1))
	found := slices.ContainsFunc(elsewhere.Arguments(), func(value any) bool {
		return value == "wanted"
	})
	fmt.Println(found)
}

func plugins() []elsewhere.Plugin {
	plugin := elsewhere.Plugin{Run: runPlugin}
	plugin.Run = func(options any) error {
		return fmt.Errorf("replacing %v", options)
	}
	return []elsewhere.Plugin{plugin, {Run: func(options any) error {
		return fmt.Errorf("listing %v", options)
	}}}
}

// logf forwards its arguments unchanged to fmt.Printf, whose variadic parameter fixes their type.
func logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

// debugf forwards to logf, whose parameter fixes its type, and logf is judged where it is declared.
func debugf(format string, arguments ...any) {
	logf("debug: "+format, arguments...)
}

// queryArguments returns a slice its caller forwards to elsewhere.Query, which fixes the result's type.
func queryArguments(identifier int) []any {
	return []any{identifier}
}

func queries() {
	var arguments []any
	arguments = append(arguments, 1)
	elsewhere.Query("select", arguments...)
	elsewhere.Query("select", queryArguments(2)...)
}

// cacheEntry holds what elsewhere.Fetch returns, so its field's type is that package's.
type cacheEntry struct {
	value any
}

// fetch returns what elsewhere.Fetch returns, so its result's type is that package's.
func fetch() any {
	return elsewhere.Fetch("key")
}

// keep is handed a value elsewhere.Fetch returned, so its parameter holds that package's type.
func keep(value any) {
	fmt.Println(value)
}

// reportPanic is handed what recover returns, whose type the language fixes.
func reportPanic(cause any) {
	fmt.Println("recovered:", cause)
}

func guarded() {
	defer func() {
		reportPanic(recover())
	}()
}

func sources(store elsewhere.Store) {
	var fetched any = elsewhere.Fetch("key")
	var loaded any
	loaded, _ = store.Load("key")
	var again any = fetch()
	entry := cacheEntry{}
	entry.value = elsewhere.Fetch("entry")
	keep(elsewhere.Fetch("kept"))
	fmt.Println(fetched, loaded, again, entry)
}
