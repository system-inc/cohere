// Package parity holds the shapes the slog port must judge as the original does: every entry of the table of slog's
// functions and methods, as a call that misplaces a key, one that drops a final value, and one that pairs, then
// method expressions, method values, embedded and aliased loggers, spread calls, keys that are constants, variables,
// Attr values and interfaces, the unknown position and its recovery, generics, closures and messages quoting long
// arguments. Nothing here is a want: the test compares the two analyzers, so a comment says what each line holds.
package parity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	renamed "log/slog"

	"mimic"
)

var (
	ctx    = context.Background()
	level  = slog.LevelInfo
	logger = slog.Default()
	record slog.Record
	number = 7
)

// Package-level functions, the "" row: each entry misplaces a key, drops a final value, pairs, and passes no pairs.
func PackageFunctions() {
	slog.Debug("m", 1, 2)
	slog.Debug("m", "k")
	slog.Debug("m", "k", 1)
	slog.Debug("m")

	slog.Info("m", 1, 2)
	slog.Info("m", "k")
	slog.Info("m", "k", 1)
	slog.Info("m")

	slog.Warn("m", 1, 2)
	slog.Warn("m", "k")
	slog.Warn("m", "k", 1)
	slog.Warn("m")

	slog.Error("m", 1, 2)
	slog.Error("m", "k")
	slog.Error("m", "k", 1)
	slog.Error("m")

	slog.DebugContext(ctx, "m", 1, 2)
	slog.DebugContext(ctx, "m", "k")
	slog.DebugContext(ctx, "m", "k", 1)
	slog.DebugContext(ctx, "m")

	slog.InfoContext(ctx, "m", 1, 2)
	slog.InfoContext(ctx, "m", "k")
	slog.InfoContext(ctx, "m", "k", 1)
	slog.InfoContext(ctx, "m")

	slog.WarnContext(ctx, "m", 1, 2)
	slog.WarnContext(ctx, "m", "k")
	slog.WarnContext(ctx, "m", "k", 1)
	slog.WarnContext(ctx, "m")

	slog.ErrorContext(ctx, "m", 1, 2)
	slog.ErrorContext(ctx, "m", "k")
	slog.ErrorContext(ctx, "m", "k", 1)
	slog.ErrorContext(ctx, "m")

	slog.Log(ctx, level, "m", 1, 2)
	slog.Log(ctx, level, "m", "k")
	slog.Log(ctx, level, "m", "k", 1)
	slog.Log(ctx, level, "m")

	_ = slog.Group("g", 1, 2)
	_ = slog.Group("g", "k")
	_ = slog.Group("g", "k", 1)
	_ = slog.Group("g")
}

// Logger's methods, the "Logger" row, called on a pointer.
func LoggerMethods() {
	logger.Debug("m", 1, 2)
	logger.Debug("m", "k")
	logger.Debug("m", "k", 1)
	logger.Debug("m")

	logger.Info("m", 1, 2)
	logger.Info("m", "k")
	logger.Info("m", "k", 1)
	logger.Info("m")

	logger.Warn("m", 1, 2)
	logger.Warn("m", "k")
	logger.Warn("m", "k", 1)
	logger.Warn("m")

	logger.Error("m", 1, 2)
	logger.Error("m", "k")
	logger.Error("m", "k", 1)
	logger.Error("m")

	logger.DebugContext(ctx, "m", 1, 2)
	logger.DebugContext(ctx, "m", "k")
	logger.DebugContext(ctx, "m", "k", 1)
	logger.DebugContext(ctx, "m")

	logger.InfoContext(ctx, "m", 1, 2)
	logger.InfoContext(ctx, "m", "k")
	logger.InfoContext(ctx, "m", "k", 1)
	logger.InfoContext(ctx, "m")

	logger.WarnContext(ctx, "m", 1, 2)
	logger.WarnContext(ctx, "m", "k")
	logger.WarnContext(ctx, "m", "k", 1)
	logger.WarnContext(ctx, "m")

	logger.ErrorContext(ctx, "m", 1, 2)
	logger.ErrorContext(ctx, "m", "k")
	logger.ErrorContext(ctx, "m", "k", 1)
	logger.ErrorContext(ctx, "m")

	logger.Log(ctx, level, "m", 1, 2)
	logger.Log(ctx, level, "m", "k")
	logger.Log(ctx, level, "m", "k", 1)
	logger.Log(ctx, level, "m")

	_ = logger.With(1, 2)
	_ = logger.With("k")
	_ = logger.With("k", 1)
	_ = logger.With()
}

// Record's method, the "Record" row, on an addressable value and through a pointer.
func RecordMethods() {
	record.Add(1, 2)
	record.Add("k")
	record.Add("k", 1)
	record.Add()

	pointer := &record
	pointer.Add(1, 2)
	pointer.Add("k")
	pointer.Add("k", 1)
}

// Methods of slog the table leaves out take Attrs or no pairs, and are never judged.
func Untabled() {
	logger.LogAttrs(ctx, level, "m", slog.Int("k", 1))
	slog.LogAttrs(ctx, level, "m", slog.Int("k", 1))
	record.AddAttrs(slog.Int("k", 1))
	_ = logger.WithGroup("g")
	_ = slog.String("k", "v")
	_ = slog.Any("k", 1)
	_ = slog.GroupValue(slog.Int("k", 1))
	_ = slog.NewRecord
}

// A method expression passes the receiver first, so its pairs begin one argument later.
func MethodExpressions() {
	(*slog.Logger).Info(logger, "m", 1, 2)
	(*slog.Logger).Info(logger, "m", "k")
	(*slog.Logger).Info(logger, "m", "k", 1)
	(*slog.Logger).Info(logger, "m")
	(*slog.Logger).Log(logger, ctx, level, "m", 1, 2)
	(*slog.Logger).Log(logger, ctx, level, "m", "k")
	(*slog.Logger).Log(logger, ctx, level, "m")
	(*slog.Logger).ErrorContext(logger, ctx, "m", "k")
	_ = (*slog.Logger).With(logger, 1, 2)
	_ = (*slog.Logger).With(logger, "k")
	_ = (*slog.Logger).With(logger)
	(*slog.Record).Add(&record, 1, 2)
	(*slog.Record).Add(&record, "k")
	(*slog.Record).Add(&record)

	// A parenthesized method expression, whose findings the original reads one argument early, is in the
	// differences fixture.

	// A parenthesized function or method, not a method expression, is still the same static callee.
	(slog.Info)("m", 1, 2)
	(logger.Warn)("m", "k")
}

// A method value called through a variable names no static function, so neither judges it, while one called where it
// is selected does.
func MethodValues() {
	info := logger.Info
	info("m", 1, 2)
	add := record.Add
	add("k")
	debug := slog.Debug
	debug("m", 1, 2)
	logger.Info("m", 1, 2)
}

// embedding promotes Logger's and Record's methods, which are still slog's.
type embedding struct {
	*slog.Logger
	slog.Record
}

// LoggerAlias is Logger under another name, whose methods are still slog's.
type LoggerAlias = slog.Logger

// DefinedLogger is a new type whose underlying type is Logger's: it has none of Logger's methods.
type DefinedLogger slog.Logger

// Logs has methods under Logger's names, on a type of this package, and is never judged.
type Logs struct{}

func (Logs) Info(message string, arguments ...any) {}

func (Logs) With(arguments ...any) Logs { return Logs{} }

func Receivers() {
	var embedded embedding
	embedded.Info("m", 1, 2)
	embedded.Warn("m", "k")
	embedded.Add(1, 2)
	embedded.Add("k", 1)
	_ = embedded.With("k")

	var aliased *LoggerAlias
	aliased.Info("m", 1, 2)
	aliased.Error("m", "k")

	var defined *DefinedLogger
	(*slog.Logger)(defined).Info("m", 1, 2)

	Logs{}.Info("m", 1, 2)
	_ = Logs{}.With("k")
	mimic.Info("m", 1, 2)
	mimic.Log("k")

	// A renamed import is still log/slog.
	renamed.Info("m", 1, 2)
	renamed.Warn("m", "k")

	// Chained calls, each judged, inner first in the syntax.
	slog.Default().With("k").Info("m", 1, 2)
	logger.With("a", 1).Info("m", "k")
	slog.New(slog.Default().Handler()).Debug("m", 1, 2)
}

// A spread slice's pairs are not in the syntax, so no call with one is judged.
func Spreads() {
	var arguments []any
	slog.Info("m", arguments...)
	slog.Log(ctx, level, "m", arguments...)
	logger.Info("m", arguments...)
	_ = logger.With(arguments...)
	record.Add(arguments...)
	_ = slog.Group("g", arguments...)
	(*slog.Logger).Info(logger, "m", arguments...)
	pairs := []any{1, 2, "k"}
	slog.Warn("m", pairs...)
}

// MyString is not the predeclared string, so a key of it is reported.
type MyString string

// StringAlias is string under another name.
type StringAlias = string

// MyAttr has Attr's underlying type, and is not slog's Attr.
type MyAttr slog.Attr

// AttrAlias is Attr under another name, and still an Attr.
type AttrAlias = slog.Attr

const keyConstant = "k"

const typedKeyConstant string = "k"

const myStringConstant MyString = "k"

func Keys() {
	// Constants, untyped, typed and of a named string type.
	slog.Info("m", keyConstant, 1)
	slog.Info("m", keyConstant)
	slog.Info("m", typedKeyConstant, 1, typedKeyConstant)
	slog.Info("m", myStringConstant, 1)
	slog.Info("m", "a"+keyConstant, 1)
	slog.Info("m", 'k', 1)
	slog.Info("m", 1.5, 1)
	slog.Info("m", true, 1)

	// Variables.
	key := "k"
	slog.Info("m", key, 1)
	slog.Info("m", key, 1, key)
	var myKey MyString = "k"
	slog.Info("m", myKey, 1)
	// A key of an alias of string, which the original calls neither a string nor an Attr, is in the differences
	// fixture. In a value's place both pass it.
	var aliasValue StringAlias = "v"
	slog.Info("m", "k", aliasValue)
	slog.Info("m", "k", aliasValue, "j")
	slog.Info("m", string(myKey), 1)
	keys := []string{"k"}
	slog.Info("m", keys[0], 1)
	slog.Info("m", keys, 1)
	pointerKey := &key
	slog.Info("m", pointerKey, 1)
	slog.Info("m", *pointerKey, 1)
	slog.Info("m", number, 1)

	// Attr values, called, held and aliased, alone and between pairs.
	attr := slog.Int("k", 1)
	slog.Info("m", attr)
	slog.Info("m", attr, attr, "k", 1)
	slog.Info("m", attr, "k")
	slog.Info("m", slog.String("k", "v"), slog.Bool("b", true), slog.Group("g", "k", 1))
	slog.Info("m", slog.Group("g", "k"))
	var aliasAttr AttrAlias = attr
	slog.Info("m", aliasAttr, "k", 1)
	slog.Info("m", MyAttr(attr), "k", 1)
	pointerAttr := &attr
	slog.Info("m", pointerAttr, "k", 1)
	slog.Info("m", *pointerAttr, "k", 1)
	slog.Info("m", slog.Attr{Key: "k", Value: slog.IntValue(1)}, "k")

	// A local type named Attr is not slog's.
	type Attr struct{}
	slog.Info("m", Attr{}, "k", 1)

	// The untyped nil is no key.
	slog.Info("m", nil, 1)
	_ = logger.With(nil)
}

// stringerAndEqualer is an interface Attr satisfies and string does not.
type stringerAndEqualer interface {
	String() string
	Equal(slog.Attr) bool
}

// Interfaces in a key position: one a string fits leaves the position unknown, one only an Attr fits is taken for an
// Attr, and one neither fits is reported.
func Interfaces() {
	var anything any = "k"
	var stringer fmt.Stringer
	var equaler stringerAndEqualer
	var reader io.Reader
	var failure error = errors.New("e")
	var empty interface{}

	slog.Info("m", stringer, "k", 1)
	slog.Info("m", stringer, 1)
	slog.Info("m", stringer, "k")
	slog.Info("m", equaler, "k", 1)
	slog.Info("m", equaler, 1)
	slog.Info("m", reader, 1)
	slog.Info("m", failure, 1)
	slog.Info("m", "k", failure, failure)
	slog.Info("m", empty, 1, "k", 2)
	slog.Info("m", empty, 1, "k")

	// The unknown position: after an argument of an empty interface, an argument that cannot be a key settles it as
	// a value, and anything else keeps it unknown.
	slog.Info("m", anything)
	slog.Info("m", anything, 1)
	slog.Info("m", anything, 1, "k")
	slog.Info("m", anything, 1, "k", 2)
	slog.Info("m", anything, 1, 2)
	slog.Info("m", anything, 1, 2, 3)
	slog.Info("m", anything, "k")
	slog.Info("m", anything, "k", 1)
	slog.Info("m", anything, "k", 1, 2)
	slog.Info("m", anything, "k", 1, 2, 3)
	slog.Info("m", anything, anything)
	slog.Info("m", anything, anything, 1)
	slog.Info("m", anything, anything, 1, 2)
	slog.Info("m", anything, slog.Int("k", 1), 1)
	slog.Info("m", anything, slog.Int("k", 1), "k")
	slog.Info("m", anything, stringer, 1, 2)
	slog.Info("m", anything, failure, 1, 2)
	slog.Info("m", anything, failure, "k")
	slog.Info("m", anything, 1, failure)
	slog.Info("m", anything, 1, failure, 1)
	slog.Info("m", anything, 1, stringer, 1)
	slog.Info("m", anything, 1, anything, 1, 2)
	_ = logger.With(anything, 1, 2)
	record.Add(anything, 1, "k")
	slog.Log(ctx, level, "m", anything, 1, 2, 3)
	_ = slog.Group("g", anything, 1, 2)
}

// Generics: a key of a type parameter is an interface neither a string nor an Attr is assignable to, so it is reported
// whatever its constraint, and slog's calls inside a generic function are judged as any others.
func Generic[Key ~string, Anything any, Attribute interface{ ~struct{} | slog.Attr }](
	key Key, anything Anything, attribute Attribute, value int,
) {
	slog.Info("m", key, value)
	slog.Info("m", anything, value)
	slog.Info("m", attribute, value)
	slog.Info("m", string(key), value)
	slog.Info("m", any(key), value, "k")
	slog.Info("m", "k", key, "k")
	logger.Info("m", value, value)
}

// The constraints below admit a type that is not string, so a key of a type parameter they constrain is reported by
// both: a tilde admits a named string type, which slog logs as !BADKEY, as do a union with another type and a method
// no string has. A key of any is reported as well, and only in a key's place.
func NotStringOnly[
	Tilde interface{ ~string },
	Mixed ~string | ~int,
	Stringer interface {
		~string
		String() string
	},
	Nested interface{ interface{ ~string } },
	Anything any,
	Comparable comparable,
](tilde Tilde, mixed Mixed, stringer Stringer, nested Nested, anything Anything, comparableKey Comparable) {
	slog.Info("m", tilde, 1)
	slog.Info("m", mixed, 1)
	slog.Info("m", stringer, 1)
	slog.Info("m", nested, 1)
	slog.Info("m", anything, 1)
	slog.Info("m", comparableKey, 1)
	slog.Info("m", "k", anything)
	slog.Info("m", "k", anything, "j", tilde)
}

// GenericLogger holds a logger in a generic type.
type GenericLogger[Element any] struct {
	*slog.Logger
	element Element
}

func (generic GenericLogger[Element]) Log() {
	generic.Info("m", generic.element, 1)
	generic.Info("m", "k", generic.element, "k")
}

func Instantiations() {
	Generic[string, int, slog.Attr]("k", 1, slog.Attr{}, 1)
	GenericLogger[int]{Logger: logger}.Log()
	GenericLogger[string]{Logger: logger, element: "k"}.Info("m", 1, 2)
}

// Calls in closures, deferred and in goroutines, and in a package-level initializer, are judged.
var initialized = slog.Group("g", 1, 2)

var initializedClosure = func() { slog.Info("m", "k") }

func Closures() {
	func() {
		slog.Info("m", 1, 2)
	}()
	defer slog.Warn("m", "k")
	go logger.Error("m", 1, 2)
	report := func(arguments ...any) {
		slog.Info("m", 1, arguments)
	}
	report()
}

// Messages quote the misplaced argument as gofmt prints it, so long and nested arguments print the same in each.
func Formatting() {
	values := []int{1, 2}
	table := map[string]int{"k": 1}
	slog.Info("m", values[0]+number*2, 1)
	slog.Info("m", table["k"], 1)
	slog.Info("m", len(values), 1)
	slog.Info("m", []int{1, 2}, 1)
	slog.Info("m", struct{ Name string }{Name: "n"}, 1)
	slog.Info("m", func() int {
		return 1
	}(), 1)
	slog.Info("m", func(first int,
		second int) int {
		return first + second
	}(1, 2), 1)
	slog.Info("m", (number), 1)
	slog.Info("m", "k", `raw
string`, "\"quoted\"")
	slog.Info("m", anyOf(1), 1, `raw "quoted"`, 1, 2)

	// A call spanning lines, reported across its whole range.
	slog.Info(
		"m",
		"k", 1,
		"j",
	)
	slog.Info(
		"m",
		"k",
		1,
		2,
		3,
	)
}

func anyOf(value any) any { return value }

// Nested calls: an argument that is itself a slog call is judged on its own, and the outer call sees its result.
func Nested() {
	slog.Info("m", slog.Group("g", 1, 2), "k")
	slog.Info("m", slog.Group("g", slog.Group("h", "k")), slog.Group("i", "k", 1))
	logger.With(slog.Group("g", "k")).Info("m", 1, 2)
}

var _ = initialized

var _ = initializedClosure
