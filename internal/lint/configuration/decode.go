package configuration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Decoder turns a rule's raw JSON options into the rule bound to them, ready to run.
//
// A rule declares its own options struct, and only the rule's own package knows that type. The
// config layer holds bytes. This function type is the seam: the registry pairs a rule with a
// decoder that decodes its struct and closes the rule over it, so what comes back is a rule.Bound and
// the options' type never crosses into this package.
//
// It returns an error rather than a rule bound to a zero value, because a rule that silently receives a
// zero options struct is the exact defect this exists to fix. `boundary-no-project-import` was enabled
// and inert for months under the previous gate, declining every file because its LibraryDirectory
// was empty, and a liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that
// ever noticed.
type Decoder func(raw json.RawMessage) (rule.Bound, error)

// RuleOptions pairs a rule name with how to decode its options and whether it can run without them.
//
// At most one of Decode and DecodeList is set, and which one is the rule's arity; neither is a rule
// that takes no options. Decode serves a rule that takes one option element and is handed that element
// alone. DecodeList serves a rule whose upstream schema takes more than one, and is handed every element
// as a JSON array, which is upstream's own `context.options` with the severity removed. The arity has
// to live here, beside the decoder, because this is the only layer that sees both the elements and the
// rule.
//
// Required is the load-bearing field, and it is the whole lesson of the inert-rule defect. A rule
// that needs an option and does not get one must fail loudly rather than decline quietly: declining
// looks identical to a rule with nothing to report, and that ambiguity hid a dead rule for months.
// A rule whose options only tune it, rather than enable it, sets Required false and runs on
// defaults.
//
// Unconfigured is the rule as it runs handed no options, which is what Decode returns for a bare
// severity on a rule that takes none or whose options only tune it.
type RuleOptions struct {
	Decode       Decoder
	DecodeList   Decoder
	Required     bool
	Unconfigured rule.Bound
}

// refuseNulls refuses a JSON null anywhere in a rule's option elements, naming the element and the
// path to it.
//
// No ported rule's upstream schema admits null, and ESLint's validator refuses it: measured on
// 2026-10-05 across the 185 option-taking rules from core, typescript-eslint, react, react-hooks,
// next and eslint-comments, plus better-tailwindcss, and none of ESLint 10.8.1's 6,641 corpus option
// rows writes one. Go's decoder reads it differently, as "leave this field alone", so
// `{"allowConstructorFlags": null}` loaded as the default with no error. That is a shape no ESLint
// version accepts, quietly accepted, which is what #pd2chkx exists to end. Refusing it here, the
// one layer every rule's elements pass through, covers every decoder at once, including the ones
// that read their element with json.Unmarshal rather than rule.UnmarshalOptions.
func refuseNulls(elements []json.RawMessage) error {
	for index, element := range elements {
		value, parseError := json_value.Parse(element)
		if parseError != nil {
			// Malformed JSON is the decoder's to report, in its own words.
			continue
		}
		if path, found := nullPath(value, ""); found {
			at := fmt.Sprintf("element %d", index+1)
			if path != "" {
				at += " at " + path
			}
			return fmt.Errorf("%s is null, which no ESLint version accepts in a rule's options. "+
				"Remove it to take the default", at)
		}
	}
	return nil
}

// nullPath finds the first null in a decoded JSON value, walking object keys in sorted order so the
// same config always names the same path, and returns its dotted path.
func nullPath(value json_value.Value, path string) (string, bool) {
	switch typed := value.(type) {
	case json_value.Null:
		return path, true
	case json_value.Array:
		for index, member := range typed {
			if found, isNull := nullPath(member, joinPath(path, strconv.Itoa(index))); isNull {
				return found, true
			}
		}
	case json_value.Object:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if found, isNull := nullPath(typed[key], joinPath(path, key)); isNull {
				return found, true
			}
		}
	}
	return "", false
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// describeElements names the option elements from index `from` on, numbered the way a config
// author counts them (the first element after the severity is element 1), so an error says which
// element it means and what it held.
func describeElements(elements []json.RawMessage, from int) string {
	described := make([]string, 0, len(elements)-from)
	for index := from; index < len(elements); index++ {
		described = append(described, fmt.Sprintf("element %d %s", index+1, truncate(string(elements[index]))))
	}
	return strings.Join(described, ", ")
}
