package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// The mentions of removed files: a whole name in a comment of the same directory or a path from the root is
// rewritten to the file the content went to, byte for byte, and every other shape is left as it was, listed when
// a person has to decide it.
func TestRewriteMentions(test *testing.T) {
	test.Parallel()
	moves := []fileMove{
		{from: "graph/cfg.go", to: "graph/builder.go", whole: true},
		{from: "graph/resolve.go", to: "graph/resolved.go", whole: true},
		{from: "graph/split.go", to: "graph/left.go"},
		{from: "graph/split.go", to: "graph/right.go", whole: true},
		{from: "graph/renamed.go", to: "graph/helper_type.go"},
		{from: "graph/renamed.go", to: "graph/walker.go", whole: true},
		{from: "graph/renamed.go", to: "graph/walker.go", continues: true},
		{from: "graph/stays.go", to: "graph/helper.go"},
		{from: "graph/stays.go", to: "graph/stays.go"},
	}
	before := map[string]string{
		"graph/dominators.go": `package graph

// This file is a local addition. See the change list in cfg.go's header, and ` + "`graph/cfg.go`" + `.
// Not graph_cfg.go, cfg.go.bak or internal/other/cfg.go, and stays.go is still here.
// A line number goes stale: ` + "`resolve.go:96`" + `. And split.go went to two files.
// renamed.go lost a type and goes on as another name.
func dominators() string { return "cfg.go" }
`,
		"other/walk.go":     "package other\n\n// cfg.go here is another directory's, and `graph/cfg.go` is the root's path.\nfunc walk() {}\n",
		"graph/long.go":     "package graph\n\n// " + strings.Repeat("word ", 22) + "cfg.go\nfunc long() {}\n",
		"docs/notes.md":     "See `graph/cfg.go`, not cfg.go.\n",
		"CONTRIBUTING.md":   "The header on `graph/cfg.go` names the commit; cfg.go alone is no path.\n",
		"internal/notes.md": "Markdown outside docs/ and the root is not read: `graph/cfg.go`.\n",
	}
	after := map[string]string{
		"graph/dominators.go": `package graph

// This file is a local addition. See the change list in builder.go's header, and ` + "`graph/builder.go`" + `.
// Not graph_cfg.go, cfg.go.bak or internal/other/cfg.go, and stays.go is still here.
// A line number goes stale: ` + "`resolve.go:96`" + `. And split.go went to two files.
// walker.go lost a type and goes on as another name.
func dominators() string { return "cfg.go" }
`,
		"other/walk.go":     "package other\n\n// cfg.go here is another directory's, and `graph/builder.go` is the root's path.\nfunc walk() {}\n",
		"graph/long.go":     "package graph\n\n// " + strings.Repeat("word ", 22) + "cfg.go\nfunc long() {}\n",
		"docs/notes.md":     "See `graph/builder.go`, not cfg.go.\n",
		"CONTRIBUTING.md":   "The header on `graph/builder.go` names the commit; cfg.go alone is no path.\n",
		"internal/notes.md": "Markdown outside docs/ and the root is not read: `graph/cfg.go`.\n",
	}
	tree := map[string][]byte{}
	// The tree is what mentionTree hands over: only the files in scope.
	for relative, content := range before {
		if inMentionScope(relative) {
			tree[relative] = []byte(content)
		}
	}
	exists := func(relative string) bool { return relative == "graph/stays.go" }
	removed := referents(moves, exists)
	if !maps.Equal(removed, map[string]string{
		"graph/cfg.go":     "graph/builder.go",
		"graph/resolve.go": "graph/resolved.go",
		"graph/split.go":   "",
		"graph/renamed.go": "graph/walker.go",
	}) {
		test.Fatalf("referents %v", removed)
	}
	outcome := rewriteMentions(tree, removed)
	for _, relative := range slices.Sorted(maps.Keys(before)) {
		got := before[relative]
		if content, changed := outcome.changes[relative]; changed {
			got = string(content)
		}
		if got != after[relative] {
			test.Errorf("%s is\n%s\nwant\n%s", relative, got, after[relative])
		}
	}
	wantLeft := []string{
		"graph/dominators.go:5: resolve.go: carries a line number, which the move made wrong",
		"graph/dominators.go:5: split.go: its content went to several files",
		"graph/long.go:3: cfg.go: the line would pass 120 columns",
	}
	if !slices.Equal(outcome.left, wantLeft) {
		test.Errorf("left\n%s\nwant\n%s", strings.Join(outcome.left, "\n"), strings.Join(wantLeft, "\n"))
	}
	if len(outcome.rewritten) != 6 {
		test.Errorf("rewritten %q, want 6", outcome.rewritten)
	}
}

// A file split and then renamed goes on as its new name, which a mention of it means: phases.go loses tokenRun and
// is named for pipelineReport, and pool.go keeps first, the earlier of two alike, and is named for it.
func TestContinuations(test *testing.T) {
	test.Parallel()
	_, result := splitFiles(test, map[string]string{
		"phases.go": `package report

type tokenRun struct{ count int }

func (run tokenRun) Count() int { return run.count }

type pipelineReport struct {
	width  int
	height int
}

func (report pipelineReport) Area() int {
	return report.width * report.height
}

func (report pipelineReport) Perimeter() int {
	return 2 * (report.width + report.height)
}
`,
		"pool.go": "package report\n\ntype first struct{}\n\nfunc (first) A() int { return 1 }\n\ntype second struct{}\n\nfunc (second) B() int { return 2 }\n",
	}, nil)
	want := []fileMove{
		{from: "phases.go", to: "pipeline_report.go", continues: true},
		{from: "pool.go", to: "first.go", continues: true},
	}
	if !slices.Equal(result.continuations, want) {
		test.Errorf("continuations %+v, want %+v", result.continuations, want)
	}
}
