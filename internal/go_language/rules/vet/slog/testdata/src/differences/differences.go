// Package differences holds the calls where the port and the original disagree, each adjudicated in the port's
// favour and recorded on its line: a finding only the port makes, one both make in words that differ, or one only the
// original makes, wrong for the reason on the line above it.
package differences

import "log/slog"

var (
	logger = slog.Default()
	record slog.Record
)

// StringAlias is string under another name: `type StringAlias = string` declares no new type.
type StringAlias = string

// OtherAlias is an alias of the alias, still string.
type OtherAlias = StringAlias

// A key whose type is an alias of string is a string, which slog takes as a key.
func Aliases() {
	var aliasKey StringAlias = "k"
	var otherKey OtherAlias = "k"
	var anything any = "k"

	// aliasKey is a string key, and 1 its value: slog logs k=1, so the call is right.
	slog.Info("m", aliasKey, 1) // port drops: slog.Info arg "aliasKey" should be a string or a slog.Attr (possible missing key or value)
	// The same through two aliases.
	slog.Info("m", otherKey, 1) // port drops: slog.Info arg "otherKey" should be a string or a slog.Attr (possible missing key or value)
	// A conversion to the alias is a string as well.
	slog.Info("m", StringAlias("k"), 1) // port drops: slog.Info arg "StringAlias(\"k\")" should be a string or a slog.Attr (possible missing key or value)
	// The second key is a string too, after a right pair.
	logger.Info("m", "k", 1, aliasKey, 2) // port drops: slog.Logger.Info arg "aliasKey" should be a string or a slog.Attr (possible missing key or value)
	// An alias key in Record.Add, whose pairs begin at once.
	record.Add(aliasKey, 1) // port drops: slog.Record.Add arg "aliasKey" should be a string or a slog.Attr (possible missing key or value)

	// The key is right and its value is missing, which the port names; the original calls the key misplaced.
	slog.Info("m", aliasKey) // port renames: slog.Info arg "aliasKey" should be a string or a slog.Attr (possible missing key or value) => call to slog.Info missing a final value

	// After an argument that may be a key or an Attr, a string can be either a value or a key, so the position stays
	// unknown and the call may be right, as with a plain "k" there, which neither reports. The original says aliasKey
	// cannot be a key, and it can.
	slog.Info("m", anything, aliasKey, 1) // port drops: slog.Info arg "1" should probably be a string or a slog.Attr (previous arg "aliasKey" cannot be a key)
	// Here the call is wrong whichever anything is, a key then 1 in a key's place, or an Attr then "j" with no value,
	// and the port says what it can be sure of, as it does with a plain "k" there.
	slog.Info("m", anything, aliasKey, 1, "j") // port renames: slog.Info arg "1" should probably be a string or a slog.Attr (previous arg "aliasKey" cannot be a key) => call to slog.Info has a missing or misplaced value
}

// A parenthesized method expression still passes its receiver first, so the pairs begin after the message.
func ParenthesizedMethodExpressions() {
	// The pairs are 1 and 2, so the first misplaced key is 1, where the original, reading "m" as a key, names 2.
	((*slog.Logger).Debug)(logger, "m", 1, 2) // port renames: slog.Logger.Debug arg "2" should be a string or a slog.Attr (possible missing key or value) => slog.Logger.Debug arg "1" should be a string or a slog.Attr (possible missing key or value)
	// The one pair is "k" and nothing: the final value is missing, which the original, reading "m" and "k" as a pair,
	// misses.
	((*slog.Logger).Debug)(logger, "m", "k") // port finds: call to slog.Logger.Debug missing a final value
	// The pair is "k" and 1, a right call, where the original reads 1 as a key.
	((*slog.Logger).Debug)(logger, "m", "k", 1) // port drops: slog.Logger.Debug arg "1" should be a string or a slog.Attr (possible missing key or value)
	// With's pairs begin at once after the receiver, which the original reads as the first key.
	_ = ((*slog.Logger).With)(logger, "k", 1) // port drops: slog.Logger.With arg "logger" should be a string or a slog.Attr (possible missing key or value)
	// Record.Add's one pair has no value, and the original calls the receiver a misplaced key.
	((*slog.Record).Add)(&record, "k") // port renames: slog.Record.Add arg "&record" should be a string or a slog.Attr (possible missing key or value) => call to slog.Record.Add missing a final value
}

// StringOnly is a constraint whose one term is exactly string.
type StringOnly interface{ string }

// AliasOnly spells its one exact string term through an alias.
type AliasOnly interface{ StringAlias }

// EmbedsStringOnly embeds a string-only constraint and adds nothing.
type EmbedsStringOnly interface{ StringOnly }

// A type parameter every term of whose constraint is exactly string is string under every instantiation, so a key of
// it is a string to slog, which the original calls neither a string nor an Attr.
func StringOnlyTypeParameters[
	Exact string,
	Literal interface{ string },
	Named StringOnly,
	Aliased AliasOnly,
	Embedding EmbedsStringOnly,
](exact Exact, literal Literal, named Named, aliased Aliased, embedding Embedding, value int) {
	// Exact can only be string, so exact is a key and value its value.
	slog.Info("m", exact, value) // port drops: slog.Info arg "exact" should be a string or a slog.Attr (possible missing key or value)
	// The same constraint written as an interface literal.
	slog.Info("m", literal, value) // port drops: slog.Info arg "literal" should be a string or a slog.Attr (possible missing key or value)
	// The same constraint by name.
	slog.Info("m", named, value) // port drops: slog.Info arg "named" should be a string or a slog.Attr (possible missing key or value)
	// The one term spelled through an alias of string is still exactly string.
	slog.Info("m", aliased, value) // port drops: slog.Info arg "aliased" should be a string or a slog.Attr (possible missing key or value)
	// A constraint embedding a string-only one is string-only.
	slog.Info("m", embedding, value) // port drops: slog.Info arg "embedding" should be a string or a slog.Attr (possible missing key or value)
	// A string key after a right pair, in Logger.With.
	_ = logger.With("k", value, exact, value) // port drops: slog.Logger.With arg "exact" should be a string or a slog.Attr (possible missing key or value)
	// The key is right and its value is missing, which the port names; the original calls the key misplaced.
	slog.Info("m", exact) // port renames: slog.Info arg "exact" should be a string or a slog.Attr (possible missing key or value) => call to slog.Info missing a final value
}
