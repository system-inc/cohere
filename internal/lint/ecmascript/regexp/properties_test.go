package regexp

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/system-inc/cohere/internal/testing/build_inputs"
	"github.com/system-inc/cohere/modules/unicode_properties"
)

// propertyAnswer is Node's recorded answer for one property spelling, from testdata/node_property_members.txt:
// whether `\p{spelling}` compiles under `u`, and for each code point asked about, a digit adding 1 when \p matches
// it under u, 2 when \p matches it under iu, and 4 when \P matches it under iu.
type propertyAnswer struct {
	Name     string
	Accepted bool
	Points   []rune
	Digits   []byte
}

// nodePropertyMembers is Node's RegExp on a sample of code points per property spelling, recorded by
// tools/record_node_answers, whose sample says how the code points were chosen. The test reads it and never starts
// Node.
//
//go:embed testdata/node_property_members.txt
var nodePropertyMembers string

// readPropertyAnswers parses nodePropertyMembers, returning the Unicode version its header names.
func readPropertyAnswers(test *testing.T) (string, []propertyAnswer) {
	test.Helper()
	recordedUnicode := ""
	var answers []propertyAnswer
	for line := range strings.Lines(nodePropertyMembers) {
		line = strings.TrimSuffix(line, "\n")
		if version, found := strings.CutPrefix(line, "# unicode: "); found {
			recordedUnicode = version
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 2 && fields[1] == "refused" {
			answers = append(answers, propertyAnswer{Name: fields[0]})
			continue
		}
		answer := propertyAnswer{Name: fields[0], Accepted: true}
		for _, field := range fields[1:] {
			hexadecimal, digit, found := strings.Cut(field, ":")
			point, parseError := strconv.ParseInt(hexadecimal, 16, 32)
			if !found || parseError != nil || len(digit) != 1 || digit[0] < '0' || digit[0] > '7' {
				test.Fatalf("%s: %q is not <hex>:<digit>", answer.Name, field)
			}
			answer.Points = append(answer.Points, rune(point))
			answer.Digits = append(answer.Digits, digit[0]-'0')
		}
		answers = append(answers, answer)
	}
	return recordedUnicode, answers
}

func propertySource(name, escape string, shape int) string {
	atom := `\` + escape + `{` + name + `}`
	if shape == 1 {
		atom = "[" + atom + "]"
	}
	if shape == 2 {
		atom = "[^" + atom + "]"
	}
	return "^(?:" + atom + ")$"
}

// propertyWant is what Node answers for one profile from the three answers recorded. In ECMAScript an escape inside
// a class matches what it matches bare, a negated class matches what the class does not, and without i, \P matches
// what \p does not. The recorder checked Node's own answers for every profile agree with this before writing.
func propertyWant(digit byte, ignoreCase bool, escape string, shape int) bool {
	var matched bool
	switch {
	case !ignoreCase && escape == "p":
		matched = digit&1 != 0
	case !ignoreCase:
		matched = digit&1 == 0
	case escape == "p":
		matched = digit&2 != 0
	default:
		matched = digit&4 != 0
	}
	if shape == 2 {
		return !matched
	}
	return matched
}

// compareProperty holds the tables, then the compiled escape in each of the twelve profiles, to Node's answers.
func compareProperty(answer propertyAnswer) string {
	property, found := unicode_properties.Lookup(answer.Name)
	if !found {
		return "the tables have no such spelling"
	}
	for index, point := range answer.Points {
		if inTable, node := propertyRangesCover(property.Ranges, point), answer.Digits[index]&1 != 0; inTable != node {
			return fmt.Sprintf("table U+%04X: tables=%t Node=%t", point, inTable, node)
		}
	}
	for _, flags := range []string{"u", "iu"} {
		for _, escape := range []string{"p", "P"} {
			for shape := range 3 {
				source := propertySource(answer.Name, escape, shape)
				re, compileError := Compile(source, flags)
				if compileError != nil {
					return fmt.Sprintf("%s /%s: %v", source, flags, compileError)
				}
				for index, point := range answer.Points {
					match, matchRunesError := re.Unwrap().MatchRunes([]rune{point})
					if matchRunesError != nil {
						return matchRunesError.Error()
					}
					if node := propertyWant(answer.Digits[index], flags == "iu", escape, shape); match != node {
						return fmt.Sprintf("%s /%s U+%04X: Go=%t Node=%t", source, flags, point, match, node)
					}
				}
			}
		}
	}
	return ""
}

// The accepted-name fixture was recorded during the old-code inventory, independently of
// the generator. Iterating only the generated names would never catch a dropped alias.
//
//go:embed testdata/unicode-property-names.txt
var acceptedPropertyNames string

func TestUnicodePropertiesMatchNode(test *testing.T) {
	test.Parallel()
	recordedUnicode, answers := readPropertyAnswers(test)
	if unicode.Version != unicode_properties.Version {
		test.Fatalf("Go Unicode %s; property tables %s", unicode.Version, unicode_properties.Version)
	}
	if !strings.HasPrefix(unicode_properties.Version, recordedUnicode+".") {
		test.Fatalf(
			"Node's answers were recorded on Unicode %q; property tables %s: run tools/record_node_answers",
			recordedUnicode,
			unicode_properties.Version,
		)
	}
	names := strings.Fields(acceptedPropertyNames)
	if !slices.Equal(names, unicode_properties.Names()) {
		test.Fatal("generated names differ from the pinned Node-accepted inventory")
	}
	recordedNames := make([]string, len(answers))
	for index, answer := range answers {
		recordedNames[index] = answer.Name
	}
	if !slices.Equal(recordedNames, names) {
		test.Fatal("Node's recorded answers name other spellings than the tables: run tools/record_node_answers")
	}
	for _, answer := range answers {
		test.Run(answer.Name, func(test *testing.T) {
			test.Parallel()
			if !answer.Accepted {
				test.Fatal("table admits a property Node rejects")
			}
			if why := compareProperty(answer); why != "" {
				test.Fatal(why)
			}
		})
	}
}

func TestUnicodePropertiesFresh(test *testing.T) {
	test.Parallel()
	build_inputs.Record(test, ".", "../../../../modules/unicode_properties/internal/generate")
	command := exec.Command(
		"go",
		"run",
		"../../../../modules/unicode_properties/internal/generate",
		"-check",
		"-output",
		"../../../../modules/unicode_properties/tables_generated.go",
	)
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("freshness: %v: %s", outputError, output)
	}
}

// nodePropertySyntax is whether Node accepts each property escape fixture and what it matches, recorded by
// tools/record_node_answers, which holds the fixtures. The test reads it and never starts Node.
//
//go:embed testdata/node_property_syntax.json
var nodePropertySyntax []byte

func TestUnicodePropertySyntaxMatchesNode(test *testing.T) {
	test.Parallel()
	var recorded struct {
		Unicode  string   `json:"unicode"`
		Subjects []string `json:"subjects"`
		Fixtures []struct {
			Source   string `json:"source"`
			Flags    string `json:"flags"`
			Accepted bool   `json:"accepted"`
			Matches  string `json:"matches"`
		} `json:"fixtures"`
	}
	if decodeError := json.Unmarshal(nodePropertySyntax, &recorded); decodeError != nil {
		test.Fatal(decodeError)
	}
	if !strings.HasPrefix(unicode_properties.Version, recorded.Unicode+".") {
		test.Fatalf(
			"Node's answers were recorded on Unicode %q; property tables %s: run tools/record_node_answers",
			recorded.Unicode,
			unicode_properties.Version,
		)
	}
	if len(recorded.Fixtures) == 0 {
		test.Fatal("no fixtures recorded")
	}
	for fixtureIndex, fixture := range recorded.Fixtures {
		test.Run(strconv.Itoa(fixtureIndex), func(test *testing.T) {
			test.Parallel()
			re, compileError := Compile(fixture.Source, fixture.Flags)
			if (compileError == nil) != fixture.Accepted {
				test.Fatalf(
					"/%s/%s compile=%v Node accepted=%t",
					fixture.Source,
					fixture.Flags,
					compileError,
					fixture.Accepted,
				)
			}
			if compileError != nil {
				return
			}
			if len(fixture.Matches) != len(recorded.Subjects) {
				test.Fatalf("%d answers recorded for %d subjects", len(fixture.Matches), len(recorded.Subjects))
			}
			for subjectIndex, subject := range recorded.Subjects {
				if got, node := re.Test(subject, nil), fixture.Matches[subjectIndex] == '1'; got != node {
					test.Fatalf("/%s/%s vs %q: Go=%t Node=%t", fixture.Source, fixture.Flags, subject, got, node)
				}
			}
		})
	}
}
