package rule

import "fmt"

// Registration is one rule plus everything the tool needs to know about it.
//
// A rule's options decoder lives here rather than on Rule itself, because Rule is the type a rule
// author writes and a decoder is the config layer's business. Keeping them in one Registration
// means a rule is declared in exactly one place while neither package has to import the other: the
// rule package builds the decoder with a generic helper and hands it over as a function.
//
// Options is the rule's own, so the decoder's result and the value Run reads are one type, checked by the
// compiler where the two are paired: `Registration[Options]{Rule: subject, Decode: decoder}` does not compile
// when subject is a Rule of another type than decoder returns (#ymag4q6).
type Registration[Options any] struct {
	Rule Rule[Options]
	// Decode turns this rule's single option element into its own options struct. Nil for a rule that
	// takes no options, which is the common case. A config giving a rule with Decode more than one
	// option element is refused by name, never truncated.
	Decode func(raw []byte) (Options, error)
	// DecodeOptionList is Decode for a rule whose upstream schema takes more than one option
	// element: `eqeqeq`'s `["error", "always", {"null": "ignore"}]`, or `consistent-this`'s
	// variadic `["error", "self", "vm"]`. It is handed every element after the severity as one JSON
	// array, which is upstream's `context.options`, or nil for a bare severity. Set at most one of
	// Decode and DecodeOptionList; which one is set is how the config layer learns the rule's arity.
	DecodeOptionList func(list []byte) (Options, error)
	// DecodeAt is Decode for a rule whose options name a place on disk, so decoding needs to know
	// where the config was written and which project is being checked. It takes one option element,
	// as Decode does, and is handed nil for a bare severity so it can fill in a default from the base.
	// Set at most one of Decode, DecodeOptionList and DecodeAt.
	//
	// It exists because a path in a committed config is read on every machine and in every worktree.
	// An absolute one is right on exactly one of them, and a rule whose root matches none of the files
	// it is handed declines them all, which is the inert-rule defect with a path in front of it.
	DecodeAt func(raw []byte, base OptionsBase) (Options, error)
	// RequiresOptions is whether the rule declines every file without its options.
	//
	// The load-bearing field, and the whole lesson of the inert-rule defect.
	// `boundary-no-project-import` was enabled and inert for months, declining every file because
	// its configured directory was empty, which is correct behavior for a misconfigured guard and
	// indistinguishable from a rule with nothing to report. Marking it required turns that silence
	// into a failure.
	RequiresOptions bool
}

// entry is the registration as the framework holds it: the rule's traits, its arity, and its options
// decoders closed over its own type, so nothing past this point carries the options untyped. It refuses a
// registration that sets more than one decoder, since the config layer reads which one is set as the arity.
func (registration Registration[Options]) entry() (Entry, error) {
	subject := registration.Rule
	entry := Entry{
		Traits:          traitsOf(subject),
		RequiresOptions: registration.RequiresOptions,
		Unconfigured:    Bind(subject, Given[Options]{}),
		RunPackage:      runPackage(subject.Run),
	}
	decoders := 0
	if registration.Decode != nil {
		decoders++
		entry.Arity = OptionArityOne
		entry.Bind = func(raw []byte, _ OptionsBase) (Bound, error) {
			return bindDecoded(subject, raw, registration.Decode)
		}
	}
	if registration.DecodeOptionList != nil {
		decoders++
		entry.Arity = OptionArityList
		entry.Bind = func(list []byte, _ OptionsBase) (Bound, error) {
			return bindDecoded(subject, list, registration.DecodeOptionList)
		}
	}
	if registration.DecodeAt != nil {
		decoders++
		entry.Arity = OptionArityAt
		entry.Bind = func(raw []byte, base OptionsBase) (Bound, error) {
			decoded, decodeError := registration.DecodeAt(raw, base)
			if decodeError != nil {
				return Bound{}, decodeError
			}
			return Bind(subject, Configured(decoded)), nil
		}
	}
	if decoders > 1 {
		return Entry{}, fmt.Errorf("%q sets more than one of Decode, DecodeOptionList and DecodeAt; the config "+
			"layer reads which one is set as the rule's option arity, so two is a contradiction", subject.Name)
	}
	return entry, nil
}

// bindDecoded decodes raw with decode and binds the rule to the result.
func bindDecoded[Options any](subject Rule[Options], raw []byte, decode func([]byte) (Options, error)) (Bound, error) {
	decoded, decodeError := decode(raw)
	if decodeError != nil {
		return Bound{}, decodeError
	}
	return Bind(subject, Configured(decoded)), nil
}
