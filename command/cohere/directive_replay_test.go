package main

import (
	"regexp"
	"strings"
	"testing"
)

// A file with directives replays from the findings cache, and its directives are counted exactly as a run with no
// cache counts them (#kdee854). Such a file was never cached, since a directive's accounting spans cached and walked
// rules alike, so on ahra 84 files were walked by every rule on every edit run. Its entry now keeps which directive
// withheld each cacheable rule's finding, and a replay marks those directives applied for the rules it replays.
//
// Use.ts holds a blanket directive over a line where a pure rule (no-debugger) and type-aware rules both report,
// so one directive silences findings from two classes. Other.ts holds one whose findings all come from type-aware
// rules, and Dead.ts one naming a rule that finds nothing there, which is dead however that rule's verdict arrived. Value.ts, which both import, then changes twice: to `string | null`, which re-runs the type-aware class
// while the pure class replays, and to `string | number`, after which Other.ts's directive silences nothing. Every
// step is held to a run with no cache: the same findings, suppression counts and dead directives, and --explain
// prints the same accounting. Deleting a directive is noticed.
func TestADirectiveFileReplaysAndCountsItsDirectivesAsAColdRunDoes(t *testing.T) {
	t.Parallel()
	fixture := newRunCacheFixture(t, buildCohere(t))
	fixture.write("CohereSettings.json", `{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","@typescript-eslint/non-nullable-type-assertion-style":"error"}}`)
	value := func(union string) {
		fixture.write("source/Value.ts", "export const value: "+union+" = Math.random() > 0.5 ? 'text' : "+map[string]string{
			"string | undefined": "undefined", "string | null": "null", "string | number": "1"}[union]+";\n")
	}
	value("string | undefined")
	useWithDirective := "import { value } from './Value';\n// cohere-disable-next-line -- the mixed case\nexport const used = value as string; debugger;\n"
	fixture.write("source/Use.ts", useWithDirective)
	fixture.write("source/Other.ts", "import { value } from './Value';\n// cohere-disable-next-line -- only type-aware findings\nexport const other = value as string;\n")
	// A directive naming a rule that ran and found nothing is dead, whether that rule was walked or replayed.
	fixture.write("source/Dead.ts", "// cohere-disable-next-line no-debugger -- left behind\nexport const dead = 1;\n")
	fixture.commit("directive files")

	accounting := regexp.MustCompile(`(?m)^(\S+:\d+:\d+ - .*|.*findings suppressed.*|.*silenced nothing.*|.*dead disable comments.*|    \S+:\d+ .*)$`)
	compare := func(step string) string {
		t.Helper()
		warm, warmExit := fixture.run(true, "--no-fix")
		cold, coldExit := fixture.run(false, "--no-fix")
		if isRunCacheReplay(warm) {
			t.Fatalf("%s: the run replayed whole, so it says nothing about the findings cache:\n%s", step, warm)
		}
		got, want := accounting.FindAllString(warm, -1), accounting.FindAllString(cold, -1)
		if strings.Join(got, "\n") != strings.Join(want, "\n") || warmExit != coldExit {
			t.Fatalf("%s: the warm run's findings and directive accounting differ from a run with no cache\n--- warm\n%s\n--- cold\n%s",
				step, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		return warm
	}
	withheld := func(step string, fileName string) {
		t.Helper()
		dump, _ := fixture.run(true, "--cache-dump")
		section := dump[strings.Index(dump, fileName):]
		if next := strings.Index(section[1:], "\n  /"); next >= 0 {
			section = section[:next+1]
		}
		if !strings.Contains(section, "withheld by directive") {
			t.Fatalf("%s: %s's entry records no withheld finding, so its replay below proves nothing:\n%s", step, fileName, section)
		}
	}

	fixture.run(true, "--no-fix")
	fixture.run(true, "--no-fix")

	// An unrelated file changes, so both directive files replay whole.
	fixture.write("source/a.ts", "export const a: number = 2;\n")
	unrelated := compare("an unrelated edit")
	withheld("an unrelated edit", "source/Use.ts")
	withheld("an unrelated edit", "source/Other.ts")
	if !strings.Contains(unrelated, "files replayed from cache") {
		t.Fatalf("nothing replayed after an unrelated edit:\n%s", unrelated)
	}
	if !strings.Contains(unrelated, "findings suppressed") {
		t.Fatalf("the fixture's directives silenced nothing, so it proves nothing:\n%s", unrelated)
	}

	// The type-aware class re-runs on both files while their pure class replays, and still finds what it silenced.
	value("string | null")
	compare("Value.ts became string | null")

	// Other.ts's findings are gone, so its directive is dead; Use.ts's still silences no-debugger.
	value("string | number")
	dead := compare("Value.ts became string | number")
	if !strings.Contains(dead, "silenced nothing") {
		t.Fatalf("after Value.ts became string | number, Other.ts's directive should silence nothing:\n%s", dead)
	}

	// --explain walks the file itself, and its account of the file, suppressed findings and all, reads the same
	// either way.
	timing := regexp.MustCompile(`[0-9]+(\.[0-9]+)?(ms|µs|s)\b`)
	explanation := func(cached bool) string {
		t.Helper()
		output, _ := fixture.run(cached, "--explain", "source/Use.ts")
		start := strings.Index(output, "explain: ")
		if start < 0 {
			t.Fatalf("--explain printed no explanation:\n%s", output)
		}
		// The run's own cache line and footer follow the explanation, and differ warm and cold by design.
		var kept []string
		for _, line := range strings.Split(output[start:], "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "cache:") || strings.HasPrefix(trimmed, "✓") || strings.HasPrefix(trimmed, "✗") {
				continue
			}
			kept = append(kept, line)
		}
		return timing.ReplaceAllString(strings.TrimSpace(strings.Join(kept, "\n")), "T")
	}
	explainWarm, explainCold := explanation(true), explanation(false)
	if explainWarm != explainCold {
		t.Errorf("--explain's account of Use.ts differs warm and cold\n--- warm\n%s\n--- cold\n%s", explainWarm, explainCold)
	}
	if !strings.Contains(explainCold, "suppressed") {
		t.Errorf("--explain names no suppressed finding in Use.ts, so the comparison above is not about directives:\n%s", explainCold)
	}

	// Deleting Use.ts's directive is noticed: its findings report.
	fixture.write("source/Use.ts", strings.Replace(useWithDirective, "// cohere-disable-next-line -- the mixed case\n", "", 1))
	if undirected := compare("Use.ts's directive deleted"); !strings.Contains(undirected, "no-debugger") {
		t.Fatalf("with its directive deleted, Use.ts should report no-debugger:\n%s", undirected)
	}
}
