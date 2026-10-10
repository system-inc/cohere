// Package ucd builds ECMAScript character properties from the pinned Unicode Character Database.
package ucd

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

const Version = "17.0.0"

//go:embed 17.0.0.zip
var source []byte

type Range struct{ Lo, Hi rune }
type Property struct {
	Family string
	Ranges []Range
}
type Tables struct {
	Properties map[string]Property
	Aliases    map[string]string
}

// ECMAScript's table of binary Unicode properties, not every binary UCD property.
// https://tc39.es/ecma262/#table-binary-unicode-properties
const binaryNames = `ASCII ASCII_Hex_Digit Alphabetic Any Assigned Bidi_Control Bidi_Mirrored Case_Ignorable Cased Changes_When_Casefolded Changes_When_Casemapped Changes_When_Lowercased Changes_When_NFKC_Casefolded Changes_When_Titlecased Changes_When_Uppercased Dash Default_Ignorable_Code_Point Deprecated Diacritic Emoji Emoji_Component Emoji_Modifier Emoji_Modifier_Base Emoji_Presentation Extended_Pictographic Extender Grapheme_Base Grapheme_Extend Hex_Digit ID_Continue ID_Start Ideographic IDS_Binary_Operator IDS_Trinary_Operator Join_Control Logical_Order_Exception Lowercase Math Noncharacter_Code_Point Pattern_Syntax Pattern_White_Space Quotation_Mark Radical Regional_Indicator Sentence_Terminal Soft_Dotted Terminal_Punctuation Unified_Ideograph Uppercase Variation_Selector White_Space XID_Continue XID_Start`

func fields(data []byte) [][]string {
	var rows [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		row := strings.Split(line, ";")
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		rows = append(rows, row)
	}
	return rows
}
func span(s string) Range {
	lo, hi, ok := strings.Cut(s, "..")
	if !ok {
		hi = lo
	}
	a, e := strconv.ParseInt(lo, 16, 32)
	if e != nil {
		panic(e)
	}
	b, e := strconv.ParseInt(hi, 16, 32)
	if e != nil {
		panic(e)
	}
	return Range{rune(a), rune(b)}
}
func merge(rs []Range) []Range {
	slices.SortFunc(rs, func(a, b Range) int { return int(a.Lo - b.Lo) })
	out := []Range{}
	for _, r := range rs {
		if len(out) > 0 && r.Lo <= out[len(out)-1].Hi+1 {
			out[len(out)-1].Hi = max(out[len(out)-1].Hi, r.Hi)
		} else {
			out = append(out, r)
		}
	}
	return out
}
func Complement(rs []Range) []Range {
	var out []Range
	next := rune(0)
	for _, r := range merge(slices.Clone(rs)) {
		if next < r.Lo {
			out = append(out, Range{next, r.Lo - 1})
		}
		next = r.Hi + 1
	}
	if next <= 0x10ffff {
		out = append(out, Range{next, 0x10ffff})
	}
	return out
}

func Build() (Tables, error) {
	z, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return Tables{}, err
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, e := f.Open()
		if e != nil {
			return Tables{}, e
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			return Tables{}, e
		}
		files[f.Name] = b
	}
	t := Tables{map[string]Property{}, map[string]string{}}
	add := func(id, family string, rs []Range) { t.Properties[id] = Property{family, merge(rs)} }
	gc := map[string][]Range{}
	for _, row := range fields(files["extracted/DerivedGeneralCategory.txt"]) {
		gc[row[1]] = append(gc[row[1]], span(row[0]))
	}
	for name, rs := range gc {
		if len(name) == 2 {
			gc[name[:1]] = append(gc[name[:1]], rs...)
		}
	}
	gc["LC"] = append(append(slices.Clone(gc["Lu"]), gc["Ll"]...), gc["Lt"]...)
	scriptAlias := map[string]string{}
	for _, row := range fields(files["PropertyValueAliases.txt"]) {
		switch row[0] {
		case "gc":
			id := "gc=" + row[1]
			add(id, "General_Category", gc[row[1]])
			for _, alias := range row[1:] {
				for _, prefix := range []string{"", "gc=", "General_Category="} {
					t.Aliases[prefix+alias] = id
				}
			}
		case "sc":
			for _, alias := range row[1:] {
				scriptAlias[alias] = row[2]
			}
		}
	}
	scripts := map[string][]Range{}
	var assignedScript []Range
	for _, row := range fields(files["Scripts.txt"]) {
		r := span(row[0])
		scripts[row[1]] = append(scripts[row[1]], r)
		assignedScript = append(assignedScript, r)
	}
	scripts["Unknown"] = Complement(assignedScript)
	// Script_Extensions defaults to Script, except for explicit overrides.
	overrides := []Range{}
	extensions := map[string][]Range{}
	for _, row := range fields(files["ScriptExtensions.txt"]) {
		r := span(row[0])
		overrides = append(overrides, r)
		for _, alias := range strings.Fields(row[1]) {
			name := scriptAlias[alias]
			extensions[name] = append(extensions[name], r)
		}
	}
	overrides = merge(overrides)
	for name, rs := range scripts {
		for _, r := range rs {
			next := r.Lo
			for _, o := range overrides {
				if o.Hi < next {
					continue
				}
				if o.Lo > r.Hi {
					break
				}
				if next < o.Lo {
					extensions[name] = append(extensions[name], Range{next, o.Lo - 1})
				}
				next = max(next, o.Hi+1)
			}
			if next <= r.Hi {
				extensions[name] = append(extensions[name], Range{next, r.Hi})
			}
		}
	}
	for _, row := range fields(files["PropertyValueAliases.txt"]) {
		// Hrkt is a UCD aggregate alias, excluded by ECMAScript's Script/Script_Extensions table.
		if row[0] != "sc" || row[1] == "Hrkt" {
			continue
		}
		name := row[2]
		for _, family := range []string{"sc", "scx"} {
			rs := scripts[name]
			long := "Script"
			if family == "scx" {
				rs = extensions[name]
				long = "Script_Extensions"
			}
			id := family + "=" + name
			add(id, long, rs)
			for _, alias := range row[1:] {
				t.Aliases[family+"="+alias] = id
				t.Aliases[long+"="+alias] = id
			}
		}
	}
	binaries := map[string][]Range{}
	for _, f := range []string{"PropList.txt", "DerivedCoreProperties.txt", "extracted/DerivedBinaryProperties.txt", "emoji/emoji-data.txt", "DerivedNormalizationProps.txt"} {
		for _, row := range fields(files[f]) {
			binaries[row[1]] = append(binaries[row[1]], span(row[0]))
		}
	}
	binaries["ASCII"] = []Range{{0, 127}}
	binaries["Any"] = []Range{{0, 0x10ffff}}
	binaries["Assigned"] = Complement(gc["Cn"])
	for _, name := range strings.Fields(binaryNames) {
		rs, ok := binaries[name]
		if !ok {
			return Tables{}, fmt.Errorf("missing binary property %s", name)
		}
		add(name, "Binary", rs)
		t.Aliases[name] = name
		for _, row := range fields(files["PropertyAliases.txt"]) {
			if row[1] == name {
				for _, alias := range row {
					t.Aliases[alias] = name
				}
			}
		}
	}
	return t, nil
}
