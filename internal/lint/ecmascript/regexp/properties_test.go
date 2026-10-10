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

	"github.com/system-inc/cohere/unicodeproperties"
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
	for _, r := range rs {
		for _, c := range []rune{r.Lo - 1, r.Lo, r.Hi, r.Hi + 1} {
			if c >= 0 && c <= 0x10ffff {
				points = append(points, c)
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
func oracleProperties(t *testing.T, probes []propertyProbe) []propertyAnswer {
	t.Helper()
	input, err := json.Marshal(probes)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "-e", propertyOracle)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Node oracle: %v: %s", err, stderr.String())
	}
	var answers []propertyAnswer
	if err = json.Unmarshal(output, &answers); err != nil {
		t.Fatal(err)
	}
	return answers
}
func compareProperty(name string, points []rune, bits []string) (int, string) {
	count, index := 0, 0
	for _, flags := range []string{"u", "iu"} {
		for _, escape := range []string{"p", "P"} {
			for shape := range 3 {
				source := propertySource(name, escape, shape)
				re, err := Compile(source, flags)
				if err != nil {
					return count, fmt.Sprintf("%s /%s: %v", source, flags, err)
				}
				for j, c := range points {
					match, err := re.Unwrap().MatchRunes([]rune{c})
					count++
					if err != nil {
						return count, err.Error()
					}
					if match != (bits[index][j] == '1') {
						return count, fmt.Sprintf("%s /%s U+%04X: Go=%t Node=%t", source, flags, c, match, bits[index][j] == '1')
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

func TestUnicodePropertiesNode(t *testing.T) {
	t.Parallel()
	if unicode.Version != unicodeproperties.Version {
		t.Fatalf("Go Unicode %s; property tables %s", unicode.Version, unicodeproperties.Version)
	}
	names := strings.Fields(acceptedPropertyNames)
	if !slices.Equal(names, unicodeproperties.Names()) {
		t.Fatal("generated names differ from the pinned Node-accepted inventory")
	}

	probes := []propertyProbe{}
	for _, name := range names {
		p, _ := unicodeproperties.Lookup(name)
		rs := make([]inventoryRange, len(p.Ranges))
		for i, r := range p.Ranges {
			rs[i] = inventoryRange{r.Lo, r.Hi}
		}
		probes = append(probes, propertyProbe{name, propertyPoints(rs)})
	}
	answers := oracleProperties(t, probes)
	checks, tested := 0, 0
	for i, a := range answers {
		// Not parallel: each subtest updates counts that the parent logs after the loop.
		t.Run(a.Name, func(t *testing.T) {
			tested++
			if !a.Accepted {
				t.Fatal("table admits a property Node rejects")
			}
			n, why := compareProperty(a.Name, probes[i].Points, a.Bits)
			checks += n
			if why != "" {
				t.Fatal(why)
			}
		})
	}
	t.Logf("table spellings=%d tested spellings=%d patterns=%d membership checks=%d", len(names), tested, tested*12, checks)
}

func TestUnicodePropertiesFresh(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", "../../../../unicodeproperties/internal/generate", "-check", "-output", "../../../../unicodeproperties/tables_generated.go")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("freshness: %v: %s", err, output)
	}
}

func TestUnicodePropertySyntaxNode(t *testing.T) {
	t.Parallel()
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
	subjects := []string{"", "A", "a", "α", "Α", "µ", "μ", "-", "_", "0", " ", "\u0085", "\ufeff", "AA", "αa", "αA", "p{Script=Greek}", `\p{Script=Greek}`}
	input, err := json.Marshal(struct {
		Fixtures []fixture
		Subjects []string
	}{fixtures, subjects})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "-e", `const fs=require('fs');if(process.versions.unicode!=='17.0')throw Error('Unicode version mismatch');const {Fixtures,Subjects}=JSON.parse(fs.readFileSync(0,'utf8'));process.stdout.write(JSON.stringify(Fixtures.map(({Source,Flags})=>{try{const r=new RegExp(Source,Flags);return {Accepted:true,Bits:Subjects.map(s=>r.test(s))}}catch{return {Accepted:false}}})))`)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Node: %v: %s", err, output)
	}
	var answers []struct {
		Accepted bool
		Bits     []bool
	}
	if err = json.Unmarshal(output, &answers); err != nil {
		t.Fatal(err)
	}
	checks := 0
	for i, f := range fixtures {
		// Not parallel: each subtest updates the membership count that the parent logs after the loop.
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			re, err := Compile(f.Source, f.Flags)
			if (err == nil) != answers[i].Accepted {
				t.Fatalf("/%s/%s compile=%v Node accepted=%t", f.Source, f.Flags, err, answers[i].Accepted)
			}
			if err != nil {
				return
			}
			for j, subject := range subjects {
				got := re.Test(subject)
				checks++
				if got != answers[i].Bits[j] {
					t.Fatalf("/%s/%s vs %q: Go=%t Node=%t", f.Source, f.Flags, subject, got, answers[i].Bits[j])
				}
			}
		})
	}
	t.Logf("syntax fixtures=%d subjects=%d membership checks=%d", len(fixtures), len(subjects), checks)
}
