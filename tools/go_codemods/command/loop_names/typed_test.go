package main

import (
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
)

// typedPrelude opens every fixture of the type and use rules: the imports their bodies call, each read once so
// a body that does not call it still type-checks.
const typedPrelude = "package fixture\n\nimport (\n\t\"os\"\n\t\"slices\"\n\t\"testing\"\n\t\"unicode/utf8\"\n)\n\n" +
	"var _, _, _, _ = os.Open, slices.Sort[[]int], utf8.DecodeRuneInString, (*testing.T).Helper\n\n"

// TestTypedNames runs every rule that names a letter no loop or comparator names, each with a case it must
// change and a case it must leave.
func TestTypedNames(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name string
		// file is the fixture's base name, fixture.go when empty.
		file string
		body string
		want []string
	}{
		{
			name: "a pair of parameters of one type are left and right",
			body: "func merge(a, b []int) []int { return append(a, b...) }",
			want: []string{"a:left", "b:right"},
		},
		{
			name: "a and b of different types are no pair",
			body: "func describe(a int, b string) string { return b + string(rune(a)) }",
			want: []string{
				"a=no rule names a basic type (int) held by a",
				"b=no rule names a basic type (string) held by b",
			},
		},
		{
			name: "integer s and e are a start and an end",
			body: "func width(s, e int) int { return e - s }",
			want: []string{"s:start", "e:end"},
		},
		{
			name: "string s and e are no span",
			body: "func join(s, e string) string { return s + e }",
			want: []string{"s:text", "e=no rule names a basic type (string) held by e"},
		},
		{
			name: "a literal's parameter takes the singular of the collection handed with it",
			body: "func some(items []string, keep func(string) bool) bool " +
				"{ return slices.ContainsFunc(items, keep) }\n" +
				"func caller(names []string) bool { return some(names, func(n string) bool { return n != \"\" }) }",
			want: []string{"n:name"},
		},
		{
			name: "a literal handed no collection of its element type takes no singular",
			body: "func run(count int, keep func(string) bool) bool { return count > 0 && keep(\"\") }\n" +
				"func caller() bool { return run(1, func(n string) bool { return n != \"\" }) }",
			want: []string{"n=no rule names a basic type (string) held by n"},
		},
		{
			name: "a decoded rune and its size",
			body: "func first(text string) (rune, int) { r, w := utf8.DecodeRuneInString(text); return r, w }",
			want: []string{"r:character", "w:size"},
		},
		{
			name: "an error takes its call's name",
			body: "func open() error { _, e := os.Open(\"x\"); return e }",
			want: []string{"e:openError"},
		},
		{
			name: "an error no call names is left",
			body: "func keep(e error) error { return e }",
			want: []string{"e=an error no call names"},
		},
		{
			name: "a named type gives its last word, then two words, then its whole name",
			body: "type syntaxTreeNode struct{}\nfunc visit(n *syntaxTreeNode) { _ = n }",
			want: []string{"n:node|treeNode|syntaxTreeNode"},
		},
		{
			name: "a type whose last word is a keyword gives its other words",
			body: "type accessType int\nfunc join(t accessType) accessType { return t }",
			want: []string{"t:accessType"},
		},
		{
			name: "a type named by one keyword takes its substitute",
			body: "type Range struct{}\nfunc cover(r Range) Range { return r }",
			want: []string{"r:span"},
		},
		{
			name: "the type table names a testing handle",
			body: "func check(t *testing.T) { t.Helper() }",
			want: []string{"t:test"},
		},
		{
			name: "a slice of a named type is its plural",
			body: "type violation struct{}\nfunc report(v []violation) int { return len(v) }",
			want: []string{"v:violations"},
		},
		{
			name: "an initialism in a type's name is word-cased",
			body: "type SSAViolation struct{}\nfunc report(v SSAViolation) { _ = v }",
			want: []string{"v:violation|ssaViolation"},
		},
		{
			name: "a value of a type parameter",
			body: "func first[T any](a T) T { return a }",
			want: []string{"a:value"},
		},
		{
			name: "a function answering yes or no is a predicate, one mapping a value a transform",
			body: "func filter(f func(int) bool, g func(int) string) { _, _ = f, g }",
			want: []string{"f:predicate", "g:transform"},
		},
		{
			name: "a counter from zero is a count, an n of another use is left",
			body: "func tally(text string) int { n := 0; for range text { n++ }; m := len(text); return n + m }",
			want: []string{"n:count", "m=no rule names a basic type (int) held by m"},
		},
		{
			name: "an n a function named for a count takes, and one from a call named for none",
			body: "func pushCount(n int) int { return n }\nfunc indexOf(text string) int { return len(text) }\n" +
				"func caller(text string) int { n := indexOf(text); return n }",
			want: []string{"n:count", "n=no rule names a basic type (int) held by n"},
		},
		{
			name: "a v of a basic type is a value, a lone k is left",
			body: "func double(v int, k int) int { return v * k }",
			want: []string{"v:value", "k=no rule names a basic type (int) held by k"},
		},
		{
			name: "a struct ranged over in a test is a test case",
			file: "fixture_test.go",
			body: "func cases() int { total := 0; for _, c := range []struct{ in string }{{\"x\"}} { total += len(c.in) }; return total }",
			want: []string{"c:testCase"},
		},
		{
			name: "a struct ranged over outside a test is left",
			body: "func cases() int { total := 0; for _, c := range []struct{ in string }{{\"x\"}} { total += len(c.in) }; return total }",
			want: []string{"c=range value: the collection is not a named identifier"},
		},
		{
			name: "a name the file imports, or a package a reader takes it for, is never proposed",
			body: "type json struct{}\nfunc decode(j json) { _ = j }",
			want: []string{"j=every name the rules give is a keyword, predeclared, an import or a package: json"},
		},
		{
			name: "a letter a fixture spells is the callee's word and the letter",
			body: "func worktree(name string) string { return name }\nfunc caller() string { a := worktree(\"a\"); return a }",
			want: []string{"a:worktreeA"},
		},
		{
			name: "a letter a fixture spells otherwise is left",
			body: "func worktree(name string) string { return name }\nfunc caller() string { a := worktree(\"b\"); return a }",
			want: []string{"a=no rule names a basic type (string) held by a"},
		},
		{
			name: "an element of a plurally named collection is its singular, of another is left",
			body: "func caller(beforeLines []string, matrix []string) string { b := beforeLines[0]; a := matrix[0]; return a + b }",
			want: []string{"b:beforeLine", "a=no rule names a basic type (string) held by a"},
		},
		{
			name: "a getter's word names a basic value, a call with arguments does not",
			body: "type named struct{}\nfunc (named) Name() string { return \"\" }\n" +
				"func (named) Describe(int) string { return \"\" }\n" +
				"func caller(value named) string { n := value.Name(); d := value.Describe(1); return n + d }",
			want: []string{"n:name", "d=no rule names a basic type (string) held by d"},
		},
		{
			name: "the member types of a union and the parts of an iterator",
			body: "type kind struct{}\nfunc (kind) Types() []*kind { return nil }\n" +
				"func PartsSeq() func(func(int) bool) { return nil }\n" +
				"func caller(subject *kind) int { total := 0; for _, t := range subject.Types() { _ = t; total++ }; " +
				"for p := range PartsSeq() { total += p }; return total }",
			want: []string{"t:memberType|kind", "p:part"},
		},
		{
			name: "a generic plural takes the singular of what it is selected from",
			body: "type list struct{ Nodes []string }\n" +
				"func caller(modifiers list) bool { return slices.ContainsFunc(modifiers.Nodes, func(m string) bool { return m != \"\" }) }",
			want: []string{"m:modifier"},
		},
		{
			name: "a binary search's comparator takes an element and the target",
			body: "func find(values []int, wanted int) bool {\n" +
				"\t_, found := slices.BinarySearchFunc(values, wanted, func(v, t int) int { return v - t })\n\treturn found\n}",
			want: []string{"v:value", "t:target"},
		},
		{
			name: "a type's letter receivers take one name, its package's after its own",
			body: "type parser struct{ count int }\nfunc (p *parser) next() int { return p.count }\nfunc (p *parser) peek() int { return p.count }",
			want: []string{"p:parser|fixtureParser", "p:parser|fixtureParser"},
		},
		{
			name: "a letter receiver takes the name its siblings share",
			body: "type Index struct{}\nfunc (index *Index) Size() int { return 0 }\nfunc (i *Index) Empty() bool { return i.Size() == 0 }",
			want: []string{"i:index|fixtureIndex"},
		},
		{
			name: "a receiver whose type is a package's name takes the package's name before it",
			body: "type Context struct{}\nfunc (c Context) Name() string { return \"\" }",
			want: []string{"c:fixtureContext"},
		},
		{
			name: "a receiver whose type's name its methods use takes the name the package gives the type",
			body: "type reactivity struct{ next *reactivity }\n" +
				"func newReactivity() *reactivity { state := &reactivity{}; return state }\n" +
				"func (r *reactivity) clone() *reactivity { return &reactivity{next: r} }",
			want: []string{"r:reactivity|state|fixtureReactivity"},
		},
		{
			name: "a type switch's letter is typed, then typed and its subject, in every clause",
			body: "func describe(value any) string { switch v := value.(type) { case string: return v; default: _ = v; return \"\" } }",
			want: []string{"v:typed|typedValue", "v:typed|typedValue"},
		},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			file := testCase.file
			if file == "" {
				file = "fixture.go"
			}
			checked := typeCheckFile(test, file, typedPrelude+testCase.body+"\n")
			got := outcomes(findCandidates(checked.packageTypes, checked.info, checked.fileSet, checked.files, words))
			if !reflect.DeepEqual(got, testCase.want) {
				test.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

// settled runs findCandidates and settle over source and returns each letter's accepted name by declaration
// line, and the plan.
func settled(test *testing.T, source string) (map[int]string, rename.Plan, fixture) {
	test.Helper()
	checked := typeCheck(test, typedPrelude+source)
	renamePackage := rename.Package{
		FileSet: checked.fileSet,
		Types:   checked.packageTypes,
		Info:    checked.info,
		Files:   checked.files,
	}
	candidates := findCandidates(checked.packageTypes, checked.info, checked.fileSet, checked.files, fixtureWords(test))
	plan, _ := settle(renamePackage, candidates)
	accepted := map[int]string{}
	for object, newName := range plan.Accepted {
		accepted[checked.fileSet.Position(object.Pos()).Line] = newName
	}
	return accepted, plan, checked
}

// TestSettleTakesTheNextNameTheEngineAccepts confirms that a letter whose first name is taken in its scope takes
// its next one, and that a letter whose every name is taken keeps its letter.
func TestSettleTakesTheNextNameTheEngineAccepts(test *testing.T) {
	test.Parallel()
	accepted, _, _ := settled(test, "type testBlock struct{}\n"+
		"func taken(b *testBlock) { block := 1; _ = block; _ = b }\n"+
		"func exhausted(b *testBlock) { block, testBlock := 1, 2; _, _, _ = block, testBlock, b }\n")
	if accepted[13] != "testBlock" {
		test.Fatalf("got %q on line 13, want testBlock after block is taken: %v", accepted[13], accepted)
	}
	if name, renamed := accepted[14]; renamed {
		test.Fatalf("got %q on line 14, want the letter kept when every name is taken", name)
	}
}

// TestSettleRefusesTwoLettersWantingOneName confirms that two letters of one type in one scope, which want the
// same names, are both kept rather than one taking the plain name and the other a qualified one.
func TestSettleRefusesTwoLettersWantingOneName(test *testing.T) {
	test.Parallel()
	accepted, _, _ := settled(test, "type testBlock struct{}\nfunc newBlock() *testBlock { return nil }\n"+
		"func caller() { a, b := newBlock(), newBlock(); _, _ = a, b }\n")
	if len(accepted) != 0 {
		test.Fatalf("got %v, want both letters kept", accepted)
	}
}

// TestSettleMovesATypeSwitchAsOne confirms that when `typed` is taken in one clause, every clause variable of the
// switch moves to the next name together, and the guard is rewritten with them.
func TestSettleMovesATypeSwitchAsOne(test *testing.T) {
	test.Parallel()
	_, plan, checked := settled(test, "func describe(value any, typed int) int {\n"+
		"\tswitch v := value.(type) {\n\tcase string:\n\t\treturn len(v) + typed\n\tdefault:\n\t\t_ = v\n\t\treturn 0\n\t}\n}\n")
	if len(plan.Accepted) != 2 {
		test.Fatalf("got %d clause variables accepted, want both", len(plan.Accepted))
	}
	for _, newName := range plan.Accepted {
		if newName != "typedValue" {
			test.Fatalf("got %q, want typedValue for every clause", newName)
		}
	}
	renamePackage := rename.Package{
		FileSet: checked.fileSet,
		Types:   checked.packageTypes,
		Info:    checked.info,
		Files:   checked.files,
	}
	guardRewritten := false
	for _, edit := range rename.Edits(renamePackage, plan.Accepted) {
		if checked.fileSet.File(checked.files[0].Pos()).Line(
			checked.fileSet.File(checked.files[0].Pos()).Pos(edit.Offset),
		) == 13 &&
			edit.OldText == "v" && edit.NewText == "typedValue" {
			guardRewritten = true
		}
	}
	if !guardRewritten {
		test.Fatal("the guard on line 13 was not rewritten")
	}
}

// TestLetterMentions confirms which mentions of a renamed letter in a doc comment are the parameter: the bare
// ones and a possessive, never one in a code span, an article, an abbreviation or a hyphenated word.
func TestLetterMentions(test *testing.T) {
	test.Parallel()
	renamed := map[string]string{"s": "text", "e": "end", "a": "left", "b": "right", "t": "subjectType"}
	text := "// reports whether s is empty, e.g. when `s` or `a === b` is a b-tree, t's own, and b."
	var got []string
	for _, offset := range letterMentions(text, renamed) {
		got = append(got, text[max(offset-2, 0):min(offset+3, len(text))])
	}
	want := []string{"r s i", ", t's", "d b."}
	if !slices.Equal(got, want) {
		test.Fatalf("got %q, want %q", got, want)
	}
	if !isBetweenCodePunctuation("pattern[i]", 8) || isBetweenCodePunctuation("ends at i, returning", 8) {
		test.Fatal("a letter in brackets is code, and one in prose is not")
	}
}

// TestStructFieldDecisions names the letters of a struct: a pair, a testing handle, a rune, and an int it leaves.
func TestStructFieldDecisions(test *testing.T) {
	test.Parallel()
	checked := typeCheck(
		test,
		typedPrelude+"type pair struct {\n\ta, b []int\n\tt    *testing.T\n\tr    rune\n\tn    int\n}\n",
	)
	var got []string
	for _, decided := range structFieldDecisions(checked.info, checked.files[0], fixtureWords(test)) {
		if decided.newName != "" {
			got = append(got, decided.variable.Name()+":"+decided.newName)
		} else {
			got = append(got, decided.variable.Name()+"="+decided.category)
		}
	}
	want := []string{
		"a:left",
		"b:right",
		"t:test",
		"r:character",
		"n=field: no rule names a basic type (int) held by n",
	}
	if !slices.Equal(got, want) {
		test.Fatalf("got %q, want %q", got, want)
	}
}
