package regexp

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/needs"
	"github.com/system-inc/cohere/unicode_properties"
)

type inventoryRange struct{ Lo, Hi rune }
type propertyProbe struct {
	Name   string
	Points []rune
}
type propertyAnswer struct {
	Name     string
	Accepted bool
	Bits     []string
}

const propertyOracle = `
const fs = require('fs');
if (process.versions.unicode !== '17.0') throw Error('Node Unicode: '+process.versions.unicode+'; want 17.0');
const out = JSON.parse(fs.readFileSync(0,'utf8')).map(({Name,Points}) => {
 try {new RegExp('\\p{'+Name+'}', 'u')} catch {return {Name,Accepted:false}}
 const Bits=[];
 for(const flags of ['u','iu']) for(const escape of ['p','P']) for(const shape of [0,1,2]) {
 const atom='\\'+escape+'{'+Name+'}';
 const source='^(?:'+(shape===0?atom:shape===1?'['+atom+']':'[^'+atom+']')+')$';
 const re=new RegExp(source,flags);
 Bits.push(Points.map(c=>re.test(String.fromCodePoint(c))?'1':'0').join(''));
 }
 return {Name,Accepted:true,Bits};
});
process.stdout.write(JSON.stringify(out));`

func propertyPoints(rs []inventoryRange) []rune {
	points := []rune{0, 0x10ffff, 0xD7ff, 0xD800, 0xDfff, 0xE000, 0xB5, 0x3BC}
	for _, inventoryRange := range rs {
		for _, character := range []rune{
			inventoryRange.Lo - 1,
			inventoryRange.Lo,
			inventoryRange.Hi,
			inventoryRange.Hi + 1,
		} {
			if character >= 0 && character <= 0x10ffff {
				points = append(points, character)
			}
		}
	}
	// Every fold member is probed as well; a fold can cross an interval boundary.
	for _, group := range CaseEquivalenceGroups(true) {
		points = append(points, group...)
	}
	slices.Sort(points)
	return slices.Compact(points)
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
func oracleProperties(test *testing.T, probes []propertyProbe) []propertyAnswer {
	test.Helper()
	needs.Tool(test, "node", "node's RegExp is the oracle each property's members are held to")
	input, encodeError := json.Marshal(probes)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	command := exec.Command("node", "-e", propertyOracle)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, outputError := command.Output()
	if outputError != nil {
		test.Fatalf("Node oracle: %v: %s", outputError, stderr.String())
	}
	var answers []propertyAnswer
	if decodeError := json.Unmarshal(output, &answers); decodeError != nil {
		test.Fatal(decodeError)
	}
	return answers
}
func compareProperty(name string, points []rune, bits []string) (int, string) {
	count, index := 0, 0
	for _, flags := range []string{"u", "iu"} {
		for _, escape := range []string{"p", "P"} {
			for shape := range 3 {
				source := propertySource(name, escape, shape)
				re, compileError := Compile(source, flags)
				if compileError != nil {
					return count, fmt.Sprintf("%s /%s: %v", source, flags, compileError)
				}
				for pointIndex, point := range points {
					match, matchRunesError := re.Unwrap().MatchRunes([]rune{point})
					count++
					if matchRunesError != nil {
						return count, matchRunesError.Error()
					}
					if match != (bits[index][pointIndex] == '1') {
						return count, fmt.Sprintf(
							"%s /%s U+%04X: Go=%t Node=%t",
							source,
							flags,
							point,
							match,
							bits[index][pointIndex] == '1',
						)
					}
				}
				index++
			}
		}
	}
	return count, ""
}

// The accepted-name fixture was recorded during the old-code inventory, independently of
// the generator. Iterating only the generated names would never catch a dropped alias.
//
//go:embed testdata/unicode-property-names.txt
var acceptedPropertyNames string

func TestUnicodePropertiesNode(test *testing.T) {
	test.Parallel()
	if unicode.Version != unicode_properties.Version {
		test.Fatalf("Go Unicode %s; property tables %s", unicode.Version, unicode_properties.Version)
	}
	names := strings.Fields(acceptedPropertyNames)
	if !slices.Equal(names, unicode_properties.Names()) {
		test.Fatal("generated names differ from the pinned Node-accepted inventory")
	}

	probes := []propertyProbe{}
	for _, name := range names {
		property, _ := unicode_properties.Lookup(name)
		rs := make([]inventoryRange, len(property.Ranges))
		for index, span := range property.Ranges {
			rs[index] = inventoryRange{span.Lo, span.Hi}
		}
		probes = append(probes, propertyProbe{name, propertyPoints(rs)})
	}
	answers := oracleProperties(test, probes)
	checks, tested := 0, 0
	for index, answer := range answers {
		// Not parallel: each subtest updates counts that the parent logs after the loop.
		test.Run(answer.Name, func(test *testing.T) {
			tested++
			if !answer.Accepted {
				test.Fatal("table admits a property Node rejects")
			}
			checked, why := compareProperty(answer.Name, probes[index].Points, answer.Bits)
			checks += checked
			if why != "" {
				test.Fatal(why)
			}
		})
	}
	test.Logf(
		"table spellings=%d tested spellings=%d patterns=%d membership checks=%d",
		len(names),
		tested,
		tested*12,
		checks,
	)
}

func TestUnicodePropertiesFresh(test *testing.T) {
	test.Parallel()
	build_inputs.Record(test, ".", "../../../../unicode_properties/internal/generate")
	command := exec.Command(
		"go",
		"run",
		"../../../../unicode_properties/internal/generate",
		"-check",
		"-output",
		"../../../../unicode_properties/tables_generated.go",
	)
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("freshness: %v: %s", outputError, output)
	}
}

func TestUnicodePropertySyntaxNode(test *testing.T) {
	test.Parallel()
	type fixture struct{ Source, Flags string }
	fixtures := []fixture{
		{`^[\p{Script=Greek}-]$`, "u"},
		{`^[\p{Script=Greek}-A]$`, "u"},
		{`^[A-\p{Script=Greek}]$`, "u"},
		{`^[\P{ASCII}A]$`, "u"},
		{`^[^\P{ASCII}A]$`, "iu"},
		{`^[\P{Lt}\d\D\s\S\w\W]$`, "iu"},
		{`^[\p{Script=Greek}\s\d_]$`, "iu"},
		{`^(?i:\P{Script=Greek})$`, "u"},
		{`^(?i:\p{Script=Greek})(?-i:a)$`, "u"},
		{`^\P{Any}*$`, "u"},
		{`^[^\P{Any}]$`, "u"},
		{`^\p{Any}{2}$`, "u"},
		{`^\p{IsGreek}$`, "u"},
		{`^\p{script=Greek}$`, "u"},
		{`^\p{Script=greek}$`, "u"},
		{`^\p{Script=Hrkt}$`, "u"},
		{`^\p{Script=Katakana_Or_Hiragana}$`, "u"},
		{`^\p{Greek}$`, "u"},
		{`^\p{gc = Lu}$`, "u"},
		{`^\p{lowercase_letter}$`, "u"},
		{`^\p{Letter=yes}$`, "u"},
		{`^\p{Basic_Emoji}$`, "u"},
		{`^\p{Script=Greek}$`, ""},
		{`^\\p\{Script=Greek\}$`, "u"},
	}
	subjects := []string{
		"",
		"A",
		"a",
		"α",
		"Α",
		"µ",
		"μ",
		"-",
		"_",
		"0",
		" ",
		"\u0085",
		"\ufeff",
		"AA",
		"αa",
		"αA",
		"p{Script=Greek}",
		`\p{Script=Greek}`,
	}
	needs.Tool(test, "node", "node's RegExp is the oracle each property escape's syntax is held to")
	input, encodeError := json.Marshal(struct {
		Fixtures []fixture
		Subjects []string
	}{fixtures, subjects})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	command := exec.Command(
		"node",
		"-e",
		`const fs=require('fs');if(process.versions.unicode!=='17.0')throw Error('Unicode version mismatch');const {Fixtures,Subjects}=JSON.parse(fs.readFileSync(0,'utf8'));process.stdout.write(JSON.stringify(Fixtures.map(({Source,Flags})=>{try{const r=new RegExp(Source,Flags);return {Accepted:true,Bits:Subjects.map(s=>r.test(s))}}catch{return {Accepted:false}}})))`,
	)
	command.Stdin = bytes.NewReader(input)
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("Node: %v: %s", outputError, output)
	}
	var answers []struct {
		Accepted bool
		Bits     []bool
	}
	if decodeError := json.Unmarshal(output, &answers); decodeError != nil {
		test.Fatal(decodeError)
	}
	checks := 0
	for fixtureIndex, fixture := range fixtures {
		// Not parallel: each subtest updates the membership count that the parent logs after the loop.
		test.Run(fmt.Sprintf("%d", fixtureIndex), func(test *testing.T) {
			re, compileError := Compile(fixture.Source, fixture.Flags)
			if (compileError == nil) != answers[fixtureIndex].Accepted {
				test.Fatalf(
					"/%s/%s compile=%v Node accepted=%t",
					fixture.Source,
					fixture.Flags,
					compileError,
					answers[fixtureIndex].Accepted,
				)
			}
			if compileError != nil {
				return
			}
			for subjectIndex, subject := range subjects {
				got := re.Test(subject)
				checks++
				if got != answers[fixtureIndex].Bits[subjectIndex] {
					test.Fatalf(
						"/%s/%s vs %q: Go=%t Node=%t",
						fixture.Source,
						fixture.Flags,
						subject,
						got,
						answers[fixtureIndex].Bits[subjectIndex],
					)
				}
			}
		})
	}
	test.Logf("syntax fixtures=%d subjects=%d membership checks=%d", len(fixtures), len(subjects), checks)
}
