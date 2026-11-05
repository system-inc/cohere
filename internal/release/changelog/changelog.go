// Package changelog writes a release's section of CHANGELOG.md from what changed in the rule sets cohere
// carries, and says the smallest version that change allows.
//
// The versioning policy Kirk approved (#ed2dp27), from 0.1.0 by his ruling of 2026-11-03: cohere is
// semver. From 1.0.0, turning a rule on in a set is a minor version, because a project that passed before
// can fail after it, which is a change a person opts into by taking a minor, and a major version is for
// breaking the configuration format or the command line. Below 1.0.0, semver moves both of those into the
// minor, so a patch only relaxes or fixes and anything that can fail a project or break a configuration
// takes the next minor. The sets are the part of a release a diff can read exactly, so their entries are generated;
// what changed in cohere itself is written by a person under the same heading.
package changelog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// SetDirectory is where the sets live in the module, and what their names are relative to.
const SetDirectory = "internal/lint/configuration/sets"

// Bump is how much a version must move.
type Bump int

const (
	// Patch is a release whose sets changed nothing that can fail a project that passed.
	Patch Bump = iota
	// Minor is a release that can fail a project that passed: a rule turned on, made stricter, or given
	// other options.
	Minor
	// Major is a release that breaks a configuration: a set removed, so an `extends` naming it no longer
	// loads.
	Major
)

func (bump Bump) String() string {
	return [...]string{"patch", "minor", "major"}[bump]
}

// SetChanges is what changed in one set between two releases.
type SetChanges struct {
	// Added is a set that did not exist before, Removed one that no longer exists.
	Added   bool
	Removed bool

	// TurnedOn are rules that now report, with their severity; TurnedOff are rules that no longer do.
	TurnedOn  []string
	TurnedOff []string
	// Severity are rules that report at another severity, as "rule: before -> after".
	Severity []string
	// Options are rules that report at the same severity with other options.
	Options []string
	// Settings are the set's other keys that changed: format, ignorePatterns, overrides and the rest.
	Settings []string

	// Bump is the smallest version these changes allow.
	Bump Bump
}

// Empty reports a set that did not change.
func (changes SetChanges) Empty() bool {
	return !changes.Added && !changes.Removed && len(changes.TurnedOn)+len(changes.TurnedOff)+len(changes.Severity)+
		len(changes.Options)+len(changes.Settings) == 0
}

// severity ranks a rule entry's severity: 0 off, 1 warn, 2 error, the way ESLint does, from a string, a
// number, or the first element of a list.
func severity(entry json.RawMessage) (int, error) {
	severityJson := entry
	if kindOf(entry) == '[' {
		var list []json.RawMessage
		if decodeError := json.Unmarshal(entry, &list); decodeError != nil {
			return 0, decodeError
		}
		if len(list) == 0 {
			return 0, fmt.Errorf("an empty rule entry")
		}
		severityJson = list[0]
	}
	switch kindOf(severityJson) {
	case '"':
		var name string
		if decodeError := json.Unmarshal(severityJson, &name); decodeError != nil {
			return 0, decodeError
		}
		switch name {
		case "off":
			return 0, nil
		case "warn":
			return 1, nil
		case "error":
			return 2, nil
		}
	case '0':
		var level float64
		if decodeError := json.Unmarshal(severityJson, &level); decodeError != nil {
			return 0, decodeError
		}
		if level == 0 || level == 1 || level == 2 {
			return int(level), nil
		}
	}
	return 0, fmt.Errorf("%s is not a severity", bytes.TrimSpace(severityJson))
}

// options is a rule entry's options, everything after its severity.
func options(entry json.RawMessage) ([]json.RawMessage, error) {
	if kindOf(entry) != '[' {
		return nil, nil
	}
	var list []json.RawMessage
	if decodeError := json.Unmarshal(entry, &list); decodeError != nil {
		return nil, decodeError
	}
	if len(list) > 1 {
		return list[1:], nil
	}
	return nil, nil
}

// sameOptions is whether two rule entries carry the same options.
func sameOptions(before json.RawMessage, after json.RawMessage) (bool, error) {
	optionsBefore, beforeError := options(before)
	if beforeError != nil {
		return false, beforeError
	}
	optionsAfter, afterError := options(after)
	if afterError != nil {
		return false, afterError
	}
	return sameElements(optionsBefore, optionsAfter)
}

// kindOf is the kind of JSON value raw holds, by its first byte: '{', '[', '"', 't', 'f' or 'n', and '0' for every
// number. An absent value reads as null, as a missing key reads from a decoded map.
func kindOf(raw json.RawMessage) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 'n'
	}
	if trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9') {
		return '0'
	}
	return trimmed[0]
}

// sameJson is whether left and right decode to the same value, as reflect.DeepEqual compares decoded JSON: objects
// by their members in any order, arrays element by element, strings by their text, numbers by their value, and an
// absent value as null.
func sameJson(left json.RawMessage, right json.RawMessage) (bool, error) {
	kind := kindOf(left)
	if kind != kindOf(right) {
		return false, nil
	}
	switch kind {
	case '{':
		leftMembers, rightMembers, decodeError := decodeBoth[map[string]json.RawMessage](left, right)
		if decodeError != nil || len(leftMembers) != len(rightMembers) {
			return false, decodeError
		}
		for key, leftMember := range leftMembers {
			rightMember, present := rightMembers[key]
			if !present {
				return false, nil
			}
			if same, sameError := sameJson(leftMember, rightMember); sameError != nil || !same {
				return false, sameError
			}
		}
		return true, nil
	case '[':
		leftElements, rightElements, decodeError := decodeBoth[[]json.RawMessage](left, right)
		if decodeError != nil {
			return false, decodeError
		}
		return sameElements(leftElements, rightElements)
	case '"':
		leftText, rightText, decodeError := decodeBoth[string](left, right)
		return decodeError == nil && leftText == rightText, decodeError
	case '0':
		leftNumber, rightNumber, decodeError := decodeBoth[float64](left, right)
		return decodeError == nil && leftNumber == rightNumber, decodeError
	}
	// true, false and null are each the only value of their kind.
	return true, nil
}

// sameElements is whether two lists hold the same values in the same order.
func sameElements(left []json.RawMessage, right []json.RawMessage) (bool, error) {
	if len(left) != len(right) {
		return false, nil
	}
	for index := range left {
		if same, sameError := sameJson(left[index], right[index]); sameError != nil || !same {
			return false, sameError
		}
	}
	return true, nil
}

// decodeBoth decodes two values of one kind into the type that kind decodes to.
func decodeBoth[Value any](left json.RawMessage, right json.RawMessage) (Value, Value, error) {
	var leftValue Value
	var rightValue Value
	if decodeError := json.Unmarshal(left, &leftValue); decodeError != nil {
		return leftValue, rightValue, decodeError
	}
	decodeError := json.Unmarshal(right, &rightValue)
	return leftValue, rightValue, decodeError
}

// membersOf is a JSON object's members, or none when raw holds another kind, as a type assertion to a map reads
// a decoded value that is no object.
func membersOf(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if kindOf(raw) != '{' {
		return nil, nil
	}
	members := map[string]json.RawMessage{}
	if decodeError := json.Unmarshal(raw, &members); decodeError != nil {
		return nil, decodeError
	}
	return members, nil
}

var severityNames = []string{"off", "warn", "error"}

// DiffSet compares one set's text before and after. Either may be nil, for a set added or removed.
func DiffSet(before []byte, after []byte) (SetChanges, error) {
	changes := SetChanges{Added: before == nil && after != nil, Removed: before != nil && after == nil}
	old, decodeSetError := decodeSet(before)
	if decodeSetError != nil {
		return SetChanges{}, fmt.Errorf("reading the set before: %w", decodeSetError)
	}
	updated, decodeSetError := decodeSet(after)
	if decodeSetError != nil {
		return SetChanges{}, fmt.Errorf("reading the set after: %w", decodeSetError)
	}

	oldRules, oldRulesError := membersOf(old["rules"])
	if oldRulesError != nil {
		return SetChanges{}, fmt.Errorf("reading the rules before: %w", oldRulesError)
	}
	newRules, newRulesError := membersOf(updated["rules"])
	if newRulesError != nil {
		return SetChanges{}, fmt.Errorf("reading the rules after: %w", newRulesError)
	}
	for _, name := range slices.Sorted(maps.Keys(unionKeys(oldRules, newRules))) {
		was, wasError := severityOf(oldRules, name)
		now, nowError := severityOf(newRules, name)
		if wasError != nil || nowError != nil {
			return SetChanges{}, fmt.Errorf("rule %s: %w", name, firstError(wasError, nowError))
		}
		switch {
		case was == 0 && now > 0:
			changes.TurnedOn = append(changes.TurnedOn, fmt.Sprintf("%s (%s)", name, severityNames[now]))
		case was > 0 && now == 0:
			changes.TurnedOff = append(changes.TurnedOff, name)
		case was != now:
			changes.Severity = append(
				changes.Severity,
				fmt.Sprintf("%s: %s -> %s", name, severityNames[was], severityNames[now]),
			)
			if now > was {
				changes.Bump = Minor
			}
		case now > 0:
			same, sameError := sameOptions(oldRules[name], newRules[name])
			if sameError != nil {
				return SetChanges{}, fmt.Errorf("rule %s: %w", name, sameError)
			}
			if !same {
				changes.Options = append(changes.Options, name)
			}
		}
	}
	if len(changes.TurnedOn) > 0 || len(changes.Options) > 0 {
		changes.Bump = Minor
	}

	delete(old, "rules")
	delete(updated, "rules")
	for _, key := range slices.Sorted(maps.Keys(unionKeys(old, updated))) {
		same, sameError := sameJson(old[key], updated[key])
		if sameError != nil {
			return SetChanges{}, fmt.Errorf("setting %s: %w", key, sameError)
		}
		if !same {
			changes.Settings = append(changes.Settings, key)
		}
	}
	// A setting cannot be judged from its name: an ignore pattern removed or an override added can each
	// fail a project that passed. So any change to one counts as one that can.
	if len(changes.Settings) > 0 || changes.Added {
		changes.Bump = max(changes.Bump, Minor)
	}
	if changes.Removed {
		changes.Bump = Major
	}
	return changes, nil
}

// decodeSet reads a set's keys, each value kept as written for the comparisons that read it.
func decodeSet(text []byte) (map[string]json.RawMessage, error) {
	decoded := map[string]json.RawMessage{}
	if text == nil {
		return decoded, nil
	}
	if decodeError := json.Unmarshal(text, &decoded); decodeError != nil {
		return nil, decodeError
	}
	return decoded, nil
}

func severityOf(rules map[string]json.RawMessage, name string) (int, error) {
	entry, present := rules[name]
	if !present {
		return 0, nil
	}
	return severity(entry)
}

func unionKeys[Value any](left map[string]Value, right map[string]Value) map[string]bool {
	keys := map[string]bool{}
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	return keys
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

// Diff compares every set between two trees, each as set name to its text, and returns the sets that
// changed and the smallest version the whole change allows.
func Diff(before map[string][]byte, after map[string][]byte) (map[string]SetChanges, Bump, error) {
	changed := map[string]SetChanges{}
	bump := Patch
	names := map[string]bool{}
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	for name := range names {
		changes, diffSetError := DiffSet(before[name], after[name])
		if diffSetError != nil {
			return nil, Patch, fmt.Errorf("cohere:%s: %w", name, diffSetError)
		}
		if changes.Empty() {
			continue
		}
		changed[name] = changes
		bump = max(bump, changes.Bump)
	}
	return changed, bump, nil
}

// SetsAt reads every set as it is in a commit of the repository at moduleDirectory, as name to text. An
// empty ref reads the working tree.
func SetsAt(moduleDirectory string, reference string) (map[string][]byte, error) {
	sets := map[string][]byte{}
	if reference == "" {
		root := filepath.Join(moduleDirectory, SetDirectory)
		walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, pathError error) error {
			if pathError != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
				return pathError
			}
			contents, readError := os.ReadFile(path)
			if readError != nil {
				return readError
			}
			relative, relativeError := filepath.Rel(root, path)
			if relativeError != nil {
				return relativeError
			}
			sets[strings.TrimSuffix(filepath.ToSlash(relative), ".json")] = contents
			return nil
		})
		return sets, walkError
	}

	listing, gitError := git(moduleDirectory, "ls-tree", "-r", "--name-only", reference, "--", SetDirectory)
	if gitError != nil {
		return nil, gitError
	}
	for _, path := range strings.Fields(string(listing)) {
		if filepath.Ext(path) != ".json" {
			continue
		}
		contents, gitError := git(moduleDirectory, "show", reference+":"+path)
		if gitError != nil {
			return nil, gitError
		}
		sets[strings.TrimSuffix(strings.TrimPrefix(path, SetDirectory+"/"), ".json")] = contents
	}
	return sets, nil
}

func git(directory string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, outputError := command.Output()
	if outputError != nil {
		return nil, fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(arguments, " "),
			outputError,
			strings.TrimSpace(standardError.String()),
		)
	}
	return output, nil
}

// Render writes a release's section: its heading, then one subsection per set that changed, in name
// order, with every change named. The section for the first release, which has nothing to compare
// with, lists each set and how many rules it turns on, and refuses a set it cannot read, since nothing
// read those sets before.
func Render(version string, date string, changed map[string]SetChanges, first map[string][]byte) (string, error) {
	var section strings.Builder
	fmt.Fprintf(&section, "## %s (%s)\n\n", version, date)
	if first != nil {
		section.WriteString("The first release. It carries these rule sets:\n\n")
		for _, name := range slices.Sorted(maps.Keys(first)) {
			decoded, decodeError := decodeSet(first[name])
			if decodeError != nil {
				return "", fmt.Errorf("cohere:%s: %w", name, decodeError)
			}
			rules, rulesError := membersOf(decoded["rules"])
			if rulesError != nil {
				return "", fmt.Errorf("cohere:%s: reading the rules: %w", name, rulesError)
			}
			on := 0
			for ruleName := range rules {
				if level, severityOfError := severityOf(rules, ruleName); severityOfError == nil && level > 0 {
					on++
				}
			}
			fmt.Fprintf(&section, "- `cohere:%s`, %d rules on\n", name, on)
		}
		return section.String(), nil
	}
	if len(changed) == 0 {
		section.WriteString("No rule set changed.\n")
		return section.String(), nil
	}
	for _, name := range slices.Sorted(maps.Keys(changed)) {
		changes := changed[name]
		fmt.Fprintf(&section, "### `cohere:%s`\n\n", name)
		switch {
		case changes.Added:
			section.WriteString("New in this release.\n\n")
		case changes.Removed:
			section.WriteString("Removed in this release.\n\n")
		}
		list := func(label string, items []string) {
			for _, item := range items {
				fmt.Fprintf(&section, "- %s `%s`\n", label, item)
			}
		}
		list("On:", changes.TurnedOn)
		list("Off:", changes.TurnedOff)
		list("Severity:", changes.Severity)
		list("Options changed:", changes.Options)
		list("Setting changed:", changes.Settings)
		section.WriteString("\n")
	}
	return strings.TrimRight(section.String(), "\n") + "\n", nil
}

// RequireBump refuses a version that moves less than the change allows. Versions are compared as plain
// major.minor.patch. A prerelease, which release.Build allows, has no section of its own: the section is
// written for the release it leads to.
//
// Below 1.0.0 the minor is semver's breaking slot, so a Major change moves the minor rather than the
// major: 0.3.0 may remove a set that 0.2.x carried. Leaving 0.x for 1.0.0 is always allowed.
func RequireBump(previous string, next string, bump Bump) error {
	from, parseVersionError := parseVersion(previous)
	if parseVersionError != nil {
		return parseVersionError
	}
	to, parseVersionError := parseVersion(next)
	if parseVersionError != nil {
		return parseVersionError
	}
	switch {
	case to[0] != from[0]:
		if to[0] < from[0] {
			return fmt.Errorf("%s comes before %s", next, previous)
		}
		return nil
	case to[0] == 0 && to[1] != from[1]:
		if to[1] < from[1] {
			return fmt.Errorf("%s comes before %s", next, previous)
		}
		return nil
	case to[0] == 0 && bump != Patch:
		return fmt.Errorf(
			"%s is a patch after %s, and a rule set changed in a way that can fail a project that passed or break a configuration, which below 1.0.0 needs the next minor version",
			next,
			previous,
		)
	case bump == Major:
		return fmt.Errorf(
			"%s keeps the major version of %s, and a rule set was removed, so a configuration that extends it no longer loads, which needs a major version",
			next,
			previous,
		)
	case to[1] != from[1]:
		if to[1] < from[1] {
			return fmt.Errorf("%s comes before %s", next, previous)
		}
		return nil
	case to[2] <= from[2]:
		return fmt.Errorf("%s does not come after %s", next, previous)
	case bump == Minor:
		return fmt.Errorf(
			"%s is a patch after %s, and a rule set changed in a way that can fail a project that passed, which needs at least a minor version",
			next,
			previous,
		)
	}
	return nil
}

func parseVersion(version string) ([3]int, error) {
	var parsed [3]int
	if _, scanError := fmt.Sscanf(version, "%d.%d.%d", &parsed[0], &parsed[1], &parsed[2]); scanError != nil ||
		fmt.Sprintf("%d.%d.%d", parsed[0], parsed[1], parsed[2]) != version {
		return parsed, fmt.Errorf("%q is not a major.minor.patch version", version)
	}
	return parsed, nil
}
