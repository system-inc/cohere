package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// fixtureSource is one file holding each case: two names swapped in one list (a local renamed to free its name
// for another), a rename that would capture an outer variable, and a position that only uses a name.
const fixtureSource = `package fixture

func pair(a, b int) int {
	test := a
	t := b
	return test + t
}

func capture(limit int) int {
	total := 0
	for i := 0; i < limit; i++ {
		total += i
	}
	return total
}
`

const fixtureRenamed = `package fixture

func pair(left, right int) int {
	first := left
	test := right
	return first + test
}

func capture(limit int) int {
	total := 0
	for i := 0; i < limit; i++ {
		total += i
	}
	return total
}
`

// constantSource holds constants declared in a function, which the engine does not take and the verified apply
// proves: a rename of one, and a second function where the new name collides with a variable.
const constantSource = `package fixture

func labels() []int {
	const a = 1
	const b = 2
	return []int{a, b, a}
}

func captured() int {
	itemB := 5
	const c = 3
	return itemB + c
}
`

const constantRenamed = `package fixture

func labels() []int {
	const itemA = 1
	const itemB = 2
	return []int{itemA, itemB, itemA}
}

func captured() int {
	itemB := 5
	const c = 3
	return itemB + c
}
`

func writeFixture(test *testing.T) string {
	return writeFixtureSource(test, fixtureSource)
}

func writeFixtureSource(test *testing.T, source string) string {
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.work":    "go 1.27\n\nuse .\n",
		"go.mod":     "module fixture\n\ngo 1.27\n",
		"fixture.go": source,
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

func writeList(test *testing.T, root string, contents string) string {
	path := filepath.Join(test.TempDir(), "list.txt")
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// A list renaming a local out of the way and another into its name is accepted whole, and written exactly.
func TestTheListIsAppliedWhole(test *testing.T) {
	test.Parallel()
	root := writeFixture(test)
	list := writeList(
		test,
		root,
		"# swapped through\nfixture.go:3:11 left\nfixture.go:3:14 right\nfixture.go:4:2 first\nfixture.go:5:2 test\n",
	)
	if code := run(root, list, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run exited %d, want 0", code)
	}
	written, readError := os.ReadFile(filepath.Join(root, "fixture.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	if string(written) != fixtureRenamed {
		test.Fatalf("wrote\n%s\nwant\n%s", written, fixtureRenamed)
	}
}

// The engine refuses a rename that would capture another variable, a position that declares nothing is refused,
// and either refusal writes nothing at all, not even the requests that were accepted.
func TestARefusalWritesNothing(test *testing.T) {
	test.Parallel()
	cases := map[string]string{
		"captures total":           "fixture.go:3:11 left\nfixture.go:11:6 total\n",
		"a use, not a declaration": "fixture.go:3:11 left\nfixture.go:6:9 first\n",
		"not found":                "fixture.go:3:11 left\nfixture.go:99:1 nothing\n",
	}
	for name, contents := range cases {
		root := writeFixture(test)
		list := writeList(test, root, contents)
		loaded, loadError := workspace.Load(root, nil)
		if loadError != nil {
			test.Fatal(loadError)
		}
		requests, readError := readList(list)
		if readError != nil {
			test.Fatal(readError)
		}
		_, outcomes, _ := plan(loaded, requests)
		if outcomes[requests[0].position] != "accepted" {
			test.Errorf("%s: the first request reads %q, want accepted", name, outcomes[requests[0].position])
		}
		if outcomes[requests[1].position] == "accepted" {
			test.Errorf("%s: the second request was accepted", name)
		}
		if code := run(root, list, nil, false, os.Stdout, os.Stderr); code != 1 {
			test.Errorf("%s: run exited %d, want 1", name, code)
		}
		written, writtenError := os.ReadFile(filepath.Join(root, "fixture.go"))
		if writtenError != nil {
			test.Fatal(writtenError)
		}
		if string(written) != fixtureSource {
			test.Errorf("%s: the file changed though a request was refused", name)
		}
	}
}

// A constant declared in a function is renamed at every read, and a constant renamed into the name of a
// variable in its scope is refused by the reload, which writes nothing.
func TestAFunctionConstantIsRenamedAndProvenByTheReload(test *testing.T) {
	test.Parallel()
	root := writeFixtureSource(test, constantSource)
	list := writeList(test, root, "fixture.go:4:8 itemA\nfixture.go:5:8 itemB\n")
	if code := run(root, list, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run exited %d, want 0", code)
	}
	written, readError := os.ReadFile(filepath.Join(root, "fixture.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	if string(written) != constantRenamed {
		test.Fatalf("wrote\n%s\nwant\n%s", written, constantRenamed)
	}

	root = writeFixtureSource(test, constantSource)
	collidingList := writeList(test, root, "fixture.go:11:8 itemB\n")
	if code := run(root, collidingList, nil, false, os.Stdout, os.Stderr); code != 1 {
		test.Fatalf("a constant colliding with a variable: run exited %d, want 1", code)
	}
	unchanged, unchangedError := os.ReadFile(filepath.Join(root, "fixture.go"))
	if unchangedError != nil {
		test.Fatal(unchangedError)
	}
	if string(unchanged) != constantSource {
		test.Fatal("the file changed though the reload refused the rename")
	}
}

// A type switch's guard names one variable per clause, and naming the guard renames them all, with every read.
func TestATypeSwitchGuardRenamesEveryClause(test *testing.T) {
	test.Parallel()
	root := writeFixtureSource(test, `package fixture

func describe(source any) int {
	switch src := source.(type) {
	case int:
		return src
	case []int:
		return len(src)
	}
	return 0
}
`)
	list := writeList(test, root, "fixture.go:4:9 typed\n")
	if code := run(root, list, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run exited %d, want 0", code)
	}
	written, readError := os.ReadFile(filepath.Join(root, "fixture.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	want := `package fixture

func describe(source any) int {
	switch typed := source.(type) {
	case int:
		return typed
	case []int:
		return len(typed)
	}
	return 0
}
`
	if string(written) != want {
		test.Fatalf("wrote\n%s\nwant\n%s", written, want)
	}
}
