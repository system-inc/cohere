package regexp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGreekPropertyNode(t *testing.T) {
	data, err := os.ReadFile("testdata/greek_property.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases [][3]string
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	// Probe every pinned interval's boundaries, with Node alone deciding membership.
	for _, property := range []struct {
		name   string
		ranges [][2]rune
	}{{"Script=Greek", greekScriptRanges}, {"Script_Extensions=Greek", greekExtensionRanges}} {
		for _, span := range property.ranges {
			for _, point := range []rune{span[0] - 1, span[0], span[1], span[1] + 1} {
				if point < 0 || point > 0x10ffff {
					continue
				}
				for _, flags := range []string{"u", "iu"} {
					cases = append(cases, [3]string{`^\p{` + property.name + `}$`, flags, string(point)}, [3]string{`^\P{` + property.name + `}$`, flags, string(point)})
				}
			}
		}
	}
	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "-e", `const fs=require('fs');const cases=JSON.parse(fs.readFileSync(0,'utf8'));const answers=cases.map(([pattern,flags,subject])=>{try{return String(new RegExp(pattern,flags).test(subject))}catch{return 'SyntaxError'}});process.stdout.write(JSON.stringify({unicode:process.versions.unicode,answers}));`)
	command.Stdin = strings.NewReader(string(encoded))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Node: %v\n%s", err, output)
	}
	var node struct {
		Unicode string
		Answers []string
	}
	if err := json.Unmarshal(output, &node); err != nil {
		t.Fatal(err)
	}
	if node.Unicode != greekPropertyUnicodeVersion {
		t.Fatalf("property tables Unicode %s; Node Unicode %s", greekPropertyUnicodeVersion, node.Unicode)
	}
	got := make([]string, 0, len(cases))
	for _, row := range cases {
		pattern, err := Compile(row[0], row[1])
		if err != nil {
			got = append(got, "SyntaxError")
			continue
		}
		matched, err := pattern.TestOrError(row[2])
		if err != nil {
			t.Fatal(err)
		}
		answer := "false"
		if matched {
			answer = "true"
		}
		got = append(got, answer)
	}
	if !reflect.DeepEqual(got, node.Answers) {
		for index := range got {
			if got[index] != node.Answers[index] {
				t.Errorf("case %d %q: Go %s Node %s", index, cases[index], got[index], node.Answers[index])
				if index > 24 {
					break
				}
			}
		}
	}
	t.Logf("Node Unicode %s: %d Greek property observations", node.Unicode, len(cases))
}

func TestGreekPropertyNodeMutant(t *testing.T) {
	source, err := os.ReadFile("script_property.go")
	if err != nil {
		t.Fatal(err)
	}
	const before = "ranges = greekScriptRanges"
	if strings.Count(string(source), before) != 1 {
		t.Fatal("Greek property mutation site moved")
	}
	changed := strings.Replace(string(source), before, "ranges = [][2]rune{{'A', 'Z'}}", 1)
	directory := t.TempDir()
	replacement := filepath.Join(directory, "script_property.go")
	if err := os.WriteFile(replacement, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := filepath.Abs("script_property.go")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"Replace": map[string]string{original: replacement}})
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlay, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-overlay="+overlay, ".", "-run", "^TestGreekPropertyNode$", "-count=1", "-v")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "case 0") || !strings.Contains(string(output), "Go false Node true") || strings.Contains(string(output), "build failed") {
		t.Fatalf("Greek-as-Latin mutant escaped Node or failed before behavior: %v\n%s", err, output)
	}
	t.Log("Greek-as-Latin mutant compiled and ran; caught only by comparison with Node")
}
