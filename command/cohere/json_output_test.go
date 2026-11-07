package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestJsonLines(test *testing.T) {
	test.Parallel()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	finding := runFinding{
		Path:      "/repo/a.ts",
		Line:      3,
		Column:    7,
		Severity:  "error",
		Rule:      "prefer-const",
		MessageId: "useConst",
		Message:   "Use const.",
	}
	writeJsonLine(stream, findingAsJson(finding))
	for _, line := range changedFilesAsJson([]changedFile{
		{Path: "b.ts", Fixed: true, Formatted: true, FixedBy: map[string]int{"prefer-const": 2, "object-shorthand": 1}},
	}) {
		writeJsonLine(stream, line)
	}
	flushStream(test, stream)
	want := `{"kind":"finding","path":"/repo/a.ts","line":3,"column":7,"severity":"error","rule":"prefer-const","messageId":"useConst","message":"Use const."}
{"kind":"fixed","path":"b.ts","rules":["object-shorthand","prefer-const"]}
{"kind":"formatted","path":"b.ts"}
`
	if out.String() != want {
		test.Errorf("lines:\n got\n%s want\n%s", out.String(), want)
	}
}

func TestJsonSummary(test *testing.T) {
	test.Parallel()
	summary := cleanSummary()
	warmSummary(&summary)
	summary.Findings = 2
	summary.Changed = []changedFile{{Path: "b.ts", Fixed: true, Formatted: true}, {Path: "c.ts", Formatted: true}}
	summary.Gaps.CrashedFiles = 1

	encoded, encodeError := json.Marshal(summaryAsJson(summary))
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	// The line is read back under the names a reader reads it by, spelled here apart from summaryJSON's tags, so
	// a field written under another name reads as zero and fails the comparison, and one written as another kind
	// fails the decode.
	type phaseLine struct {
		Name string `json:"name"`
	}
	type gapsLine struct {
		CrashedFiles int `json:"crashedFiles"`
	}
	type summaryLine struct {
		Kind              string      `json:"kind"`
		SchemaVersion     int         `json:"schemaVersion"`
		Verdict           string      `json:"verdict"`
		Seconds           float64     `json:"seconds"`
		Findings          int         `json:"findings"`
		FilesFixed        int         `json:"filesFixed"`
		FilesFormatted    int         `json:"filesFormatted"`
		Cohered           int         `json:"cohered"`
		Checked           int         `json:"checked"`
		Cached            int         `json:"cached"`
		FilesInScope      int         `json:"filesInScope"`
		Rules             int         `json:"rules"`
		FormattingSeconds float64     `json:"formattingSeconds"`
		Gaps              gapsLine    `json:"gaps"`
		Phases            []phaseLine `json:"phases"`
	}
	var decoded summaryLine
	if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
		test.Fatal(decodeError)
	}
	want := summaryLine{
		Kind: "summary", SchemaVersion: outputSchemaVersion, Verdict: "fail", Seconds: 0.7,
		Findings: 2, FilesFixed: 1, FilesFormatted: 2,
		Cohered: 2, Checked: 3, Cached: 3923, FilesInScope: 3926, Rules: 480, FormattingSeconds: 0.02,
		Gaps:   gapsLine{CrashedFiles: 1},
		Phases: []phaseLine{{Name: "fix"}, {Name: "types"}, {Name: "lint"}},
	}
	if !reflect.DeepEqual(decoded, want) {
		test.Errorf("the summary line:\n got %+v\nwant %+v", decoded, want)
	}
	if replay := summaryAsJson(
		runSummary{Total: 50 * time.Millisecond, Cache: cacheUse{Replayed: true}},
	); !replay.Cache.Replayed || replay.Verdict != "pass" {
		test.Errorf("a replay's summary: %+v", replay)
	}
}

// TestOutputSchemaMatchesTheTypes holds schema/CohereOutput.schema.json to the types `--json` writes:
// every field a type writes is a property in the schema, every property is a field, and the required
// list is exactly the fields that are never omitted. A field added to a type without the schema, or a
// property the code stopped writing, fails here rather than in a consumer.
func TestOutputSchemaMatchesTheTypes(test *testing.T) {
	test.Parallel()
	raw, readError := os.ReadFile("../../schema/CohereOutput.schema.json")
	if readError != nil {
		test.Fatal(readError)
	}
	var schema struct {
		Defs map[string]schemaObject `json:"$defs"`
	}
	if decodeError := json.Unmarshal(raw, &schema); decodeError != nil {
		test.Fatal(decodeError)
	}
	summary := schema.Defs["summary"]
	for name, check := range map[string]struct {
		object schemaObject
		value  any
	}{
		"finding":              {schema.Defs["finding"], findingJson{}},
		"changedFile":          {schema.Defs["changedFile"], changedFileJson{}},
		"engineBuild":          {schema.Defs["engineBuild"], engineBuildJson{}},
		"summary":              {summary, summaryJson{}},
		"summary.phase":        {*summary.Properties["phases"].Items, phaseJson{}},
		"summary.cache":        {summary.Properties["cache"], cacheUse{}},
		"summary.packageCache": {summary.Properties["packageCache"], packageCacheUse{}},
		"summary.gaps":         {summary.Properties["gaps"], runGaps{}},
		"summary.gaps.uncheckedSwift": {
			*summary.Properties["gaps"].Properties["uncheckedSwift"].Items,
			uncheckedSwiftFile{},
		},
		"summary.gaps.crashes": {*summary.Properties["gaps"].Properties["crashes"].Items, runCrash{}},
		"summary.gaps.withheldInDependencyCgo": {
			*summary.Properties["gaps"].Properties["withheldInDependencyCgo"].Items,
			withheldCgoFile{},
		},
	} {
		fields, required := jsonFields(reflect.TypeOf(check.value))
		if got := schemaPropertyNames(check.object.Properties); strings.Join(got, ",") != strings.Join(fields, ",") {
			test.Errorf("%s: the schema's properties are %v, the type writes %v", name, got, fields)
		}
		schemaRequired := append([]string(nil), check.object.Required...)
		sort.Strings(schemaRequired)
		if strings.Join(schemaRequired, ",") != strings.Join(required, ",") {
			test.Errorf("%s: the schema requires %v, the type always writes %v", name, schemaRequired, required)
		}
	}
}

type schemaObject struct {
	Properties map[string]schemaObject `json:"properties"`
	Required   []string                `json:"required"`
	Items      *schemaObject           `json:"items"`
}

// jsonFields is the names a type writes, sorted, and those it never omits, with embedded structs' fields
// flattened in as encoding/json flattens them.
func jsonFields(structType reflect.Type) (fields, required []string) {
	for index := range structType.NumField() {
		field := structType.Field(index)
		if field.Anonymous {
			embeddedFields, embeddedRequired := jsonFields(field.Type)
			fields, required = append(fields, embeddedFields...), append(required, embeddedRequired...)
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		fields = append(fields, name)
		if !strings.Contains(options, "omitempty") {
			required = append(required, name)
		}
	}
	sort.Strings(fields)
	sort.Strings(required)
	return fields, required
}

func schemaPropertyNames(properties map[string]schemaObject) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// A value that does not encode is no line at all rather than a broken one. jsonLine also reports it to package
// streams, which ends the run nonzero; that report is the process's, and no test here finishes the process.
func TestAValueThatDoesNotEncodeIsNoLine(test *testing.T) {
	test.Parallel()
	if line := jsonLine(map[string]float64{"seconds": math.NaN()}); line != "" {
		test.Errorf("a value JSON cannot hold printed %q", line)
	}
}
