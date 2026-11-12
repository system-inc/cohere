package check

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
)

// The method fixture's paths: foreign declares what a rule's Method or Interface lookup could match, some of it on
// windows alone, and user declares the method whose name the lookup asks about.
const (
	methodForeignPath = "example.com/methods/foreign"
	methodUserPath    = "example.com/methods/user"
)

// methodWorld is one build of a method fixture: foreign as goos's build context matches its files, and user over it,
// with user's type information and syntax, which a step that records user's type reads walks.
type methodWorld struct {
	foreign    *types.Package
	user       *types.Package
	userInfo   *types.Info
	userSyntax []*ast.File
}

// checkedMethodWorld type-checks foreign from foreignFiles, by file name, for goos, and user from userSource over it.
func checkedMethodWorld(test *testing.T, goos string, foreignFiles map[string]string, userSource string) methodWorld {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, foreignFiles)
	context := build.Default
	context.GOOS, context.GOARCH, context.CgoEnabled = goos, "amd64", false
	fileSet := token.NewFileSet()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		test.Fatal(readError)
	}
	var foreignSyntax []*ast.File
	for _, entry := range entries {
		matched, matchError := context.MatchFile(directory, entry.Name())
		if matchError != nil {
			test.Fatal(matchError)
		}
		if !matched {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, filepath.Join(directory, entry.Name()), nil,
			parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		foreignSyntax = append(foreignSyntax, parsed)
	}
	foreign, checkError := (&types.Config{}).Check(methodForeignPath, fileSet, foreignSyntax, nil)
	if checkError != nil {
		test.Fatalf("type-checking foreign for %s: %v", goos, checkError)
	}
	userSyntax, parseError := parser.ParseFile(fileSet, "user.go", userSource, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == methodForeignPath {
			return foreign, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Instances: map[*ast.Ident]types.Instance{}}
	user, checkError := configuration.Check(methodUserPath, fileSet, []*ast.File{userSyntax}, info)
	if checkError != nil {
		test.Fatalf("type-checking user for %s: %v", goos, checkError)
	}
	return methodWorld{foreign: foreign, user: user, userInfo: info, userSyntax: []*ast.File{userSyntax}}
}

// methodUser is the user package every method fixture shares but where a step says otherwise: Mine, whose Close and
// Errorf a lookup asks about, and a call into foreign, so foreign is in its import closure.
const methodUser = "package user\n\nimport \"example.com/methods/foreign\"\n\n" +
	"type Mine struct{}\n\nfunc (Mine) Close() error { return nil }\n\n" +
	"func (Mine) Errorf(format string, values ...any) {}\n\nfunc Use() int { return foreign.Count() }\n"

// methodForeign is foreign's file every method fixture holds, with Count, which user calls.
const methodForeign = "package foreign\n\nfunc Count() int { return 1 }\n"

// closerBase is a type of every build with Close declared, and promotedOnly a windows-only type with Close by
// promotion alone, through its embedded closerBase: the closure judgment counts it among the type names with Close,
// and no rule indexes it.
const (
	closerBase   = "package foreign\n\ntype inner struct{}\n\nfunc (inner) Close() error { return nil }\n"
	promotedOnly = "package foreign\n\ntype Handle struct{ inner }\n"
)

// constrainedPool is foreign's generic Pool, whose Close has the subject's shape, with its type parameter constrained
// by constraint.
func constrainedPool(constraint string) string {
	return "package foreign\n\ntype Pool[Item " + constraint + "] struct{ items []Item }\n\n" +
		"func (Pool[Item]) Close() error { return nil }\n"
}

// putter is foreign's generic interface Putter, with parameters as its type parameter list, whose Put takes the first
// parameter bare and gives a string: abbreviated reads Item off a subject's Put when Item is its only parameter, and
// instantiates nothing when another parameter appears nowhere bare, judging by shape alone.
func putter(parameters string) string {
	return "package foreign\n\ntype Putter[" + parameters + "] interface{ Put(Item) string }\n"
}

// box is foreign's generic concrete Box, with parameters as its type parameter list and receiver as its methods'
// receiver's, whose Put takes Item bare and gives a string: abbreviated matches an interface method of ours to it by
// instance when Item is its only parameter, and by shape alone when another appears nowhere bare.
func box(parameters string, receiver string) string {
	return "package foreign\n\ntype Box[" + parameters + "] struct{}\n\n" +
		"func (Box[" + receiver + "]) Put(value Item) string { return \"\" }\n"
}

// putUser is the user package of the Putter fixtures: Mine's Put takes an int and gives an error, the shape of
// Putter's Put, but no instance of it.
const putUser = "package user\n\nimport \"example.com/methods/foreign\"\n\ntype Mine struct{}\n\n" +
	"func (Mine) Put(value int) error { return nil }\n\nfunc Use() int { return foreign.Count() }\n"

// methodLookupOf is the lookup of query a rule records about the method named method of user's Mine on the host:
// found when answer names something of the host's foreign, as a type name, or as type name and method.
type methodLookupOf func(host methodWorld) build_reads.Lookup

// askedBy is lookup as the rule that asks its query records it: an Interface lookup initialism's, a Method lookup
// abbreviated's, which instantiates generic candidates.
func askedBy(lookup build_reads.Lookup) build_reads.Lookup {
	switch lookup.Query {
	case build_reads.QueryInterface:
		lookup.Rule = consistency_no_uppercase_initialism.Name
	case build_reads.QueryMethod:
		lookup.Rule, lookup.Instantiates = consistency_no_abbreviated_identifier.Name, true
	}
	return lookup
}

// askedByNoAny is a Method lookup as no_any records it, which matches a generic candidate by shape and instantiates
// nothing.
func askedByNoAny(lookup methodLookupOf) methodLookupOf {
	return func(host methodWorld) build_reads.Lookup {
		asked := lookup(host)
		asked.Rule, asked.Instantiates = consistency_no_any.Name, false
		return asked
	}
}

// A Method or Interface lookup holds in another build when its rule's one bit cannot have flipped there, and is
// declined when it can. Each step's build is windows unless it names another, its host darwin. The closure judgment,
// which compares every type name with a method of the name, would decline every step whose build adds or changes one;
// the steps marked so hold under the one-bit judgment, and the two kept on the closure judgment, a generic candidate
// of the subject's shape beside the rule that instantiates it and a receiver embedding a foreign type, decline as it
// does. Each lookup is abbreviated's for Method and initialism's for Interface unless the step names no_any's, and a
// step's earlier lookup is another rule's of the same subject. Each step's reads, persisted as an entry persists them,
// are judged alike.
func TestAMethodLookupHoldsUnlessItsRulesBitCouldFlip(test *testing.T) {
	test.Parallel()

	closerAnswer := func(host methodWorld) types.Object { return host.foreign.Scope().Lookup("Closer") }
	unanswered := func(query string, method string) methodLookupOf {
		return func(host methodWorld) build_reads.Lookup {
			return askedBy(build_reads.Lookup{Query: query, Name: method, Subject: methodOf(host.user, "Mine", method)})
		}
	}
	interfaceAnswered := func(host methodWorld) build_reads.Lookup {
		return askedBy(build_reads.Lookup{Query: build_reads.QueryInterface, Name: "Close",
			Subject: methodOf(host.user, "Mine", "Close"), Found: true, Answer: closerAnswer(host)})
	}
	methodAnswered := func(typeName string, method string) methodLookupOf {
		return func(host methodWorld) build_reads.Lookup {
			return askedBy(build_reads.Lookup{Query: build_reads.QueryMethod, Name: method,
				Subject: methodOf(host.user, "Mine", method), Found: true,
				Answer: methodSetMember(host.foreign, typeName, method)})
		}
	}
	closer := "package foreign\n\ntype Closer interface{ Close() error }\n"
	// Closer gains Flush on windows, which Mine, closing alone, lacks: its Close still answers the Method lookup, but
	// Mine implements Closer on the host alone, which abbreviated's concrete subject needs of the answer's interface.
	closerGainsFlush := map[string]string{"foreign.go": methodForeign, "closer_darwin.go": closer,
		"closer_windows.go": "package foreign\n\ntype Closer interface {\n\tClose() error\n\tFlush()\n}\n"}
	pool := "package foreign\n\ntype Pool[Item any] struct{ items []Item }\n\n" +
		"func (Pool[Item]) Close() error { return nil }\n"
	for _, step := range []struct {
		name   string
		files  map[string]string
		user   string
		build  string
		before methodLookupOf
		lookup methodLookupOf
		// typeReads records user's type reads, as a run does, where every other step records none, so its lookup is
		// all it is judged by.
		typeReads bool
		want      string
	}{
		{
			name: "a windows-only interface of the subject's shape",
			files: map[string]string{"foreign.go": methodForeign,
				"foreign_windows.go": "package foreign\n\ntype Closer interface{ Close() error }\n"},
			lookup: unanswered(build_reads.QueryInterface, "Close"),
			want: "L lookup Interface Close: " + methodForeignPath + ".Closer.Close is new in the build, of the " +
				"subject's shape",
		},
		{
			name: "a windows-only interface of another shape, held where the closure declines",
			files: map[string]string{"foreign.go": methodForeign,
				"foreign_windows.go": "package foreign\n\ntype Closer interface{ Close(force bool) error }\n"},
			lookup: unanswered(build_reads.QueryInterface, "Close"),
		},
		{
			name: "a windows-only method of another shape, held where the closure declines",
			files: map[string]string{"foreign.go": methodForeign,
				"handle_windows.go": "package foreign\n\ntype Handle struct{}\n\n" +
					"func (Handle) Close(force bool) error { return nil }\n"},
			lookup: unanswered(build_reads.QueryMethod, "Close"),
		},
		{
			name: "a windows-only method promoted alone, held where the closure declines",
			files: map[string]string{"foreign.go": methodForeign, "inner.go": closerBase,
				"promoted_windows.go": promotedOnly},
			lookup: unanswered(build_reads.QueryMethod, "Close"),
		},
		{
			name: "an answer still found alike, held where the closure declines",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer, "inner.go": closerBase,
				"promoted_windows.go": promotedOnly},
			lookup: interfaceAnswered,
		},
		{
			name: "an answer whose type differs by its fields alone, held where the closure declines",
			files: map[string]string{"foreign.go": methodForeign,
				"common.go": "package foreign\n\ntype Common struct{ started stamp }\n\n" +
					"func (*Common) Errorf(format string, values ...any) {}\n",
				"stamp_darwin.go":  "package foreign\n\ntype stamp struct{ wall, monotonic uint64; location *int }\n",
				"stamp_windows.go": "package foreign\n\ntype stamp struct{ wall uint64 }\n"},
			lookup: methodAnswered("Common", "Errorf"),
		},
		{
			name: "an answer with another method in the build",
			files: map[string]string{"foreign.go": methodForeign,
				"closer_darwin.go":  closer,
				"closer_windows.go": "package foreign\n\ntype Closer interface {\n\tClose() error\n\tFlush()\n}\n"},
			lookup: interfaceAnswered,
			want: "L lookup Interface Close: its answer " + methodForeignPath + ".Closer has other methods in " +
				"the build",
		},
		{
			name: "a generic candidate of the subject's shape under constraints of all types, held where the closure " +
				"declines",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer, "pool.go": pool,
				"inner.go": closerBase, "promoted_windows.go": promotedOnly},
			lookup: methodAnswered("Closer", "Close"),
		},
		{
			name: "a generic candidate whose constraint has a method keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer,
				"pool.go": constrainedPool("interface{ Close() error }"), "inner.go": closerBase,
				"promoted_windows.go": promotedOnly},
			lookup: methodAnswered("Closer", "Close"),
			want:   "L lookup Method Close: 3 type names have the method on the host, 4 in the build",
		},
		{
			name: "a generic candidate constrained by comparable keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer,
				"pool.go": constrainedPool("comparable"), "inner.go": closerBase, "promoted_windows.go": promotedOnly},
			lookup: methodAnswered("Closer", "Close"),
			want:   "L lookup Method Close: 3 type names have the method on the host, 4 in the build",
		},
		{
			name: "a generic candidate constrained by a union keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer,
				"pool.go": constrainedPool("~int | ~string"), "inner.go": closerBase,
				"promoted_windows.go": promotedOnly},
			lookup: methodAnswered("Closer", "Close"),
			want:   "L lookup Method Close: 3 type names have the method on the host, 4 in the build",
		},
		{
			name: "a constraint of all types on the host that gains a method in the build keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer,
				"pool.go":             constrainedPool("Flushing"),
				"flushing_darwin.go":  "package foreign\n\ntype Flushing interface{}\n",
				"flushing_windows.go": "package foreign\n\ntype Flushing interface{ Flush() }\n",
				"inner.go":            closerBase},
			lookup: methodAnswered("Closer", "Close"),
			want:   "L lookup Method Close: " + methodForeignPath + ".Pool differs in the build",
		},
		{
			name: "a constraint with a method on the host that is of all types in the build keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer,
				"pool.go":             constrainedPool("Flushing"),
				"flushing_darwin.go":  "package foreign\n\ntype Flushing interface{ Flush() }\n",
				"flushing_windows.go": "package foreign\n\ntype Flushing interface{}\n",
				"inner.go":            closerBase},
			lookup: methodAnswered("Closer", "Close"),
			want:   "L lookup Method Close: " + methodForeignPath + ".Pool differs in the build",
		},
		{
			// abbreviated reads Item off the subject's first parameter, foreign.ID, so the instance Pair[ID] has
			// Put(ID, ID): Mine's Put(ID, First) matches it where ID stands for First and not where it stands for
			// Second, both structs, so of one kind. Every line the lookup is judged by renders ID by name, so it is
			// the user package's own read of ID, by the type it stands for, that declines it.
			name: "an unconstrained generic whose type argument the subject names by an alias that differs by " +
				"build, declined by the subject's type reads",
			files: map[string]string{"foreign.go": methodForeign,
				"pair.go":       "package foreign\n\ntype Pair[Item any] interface{ Put(a Item, b Item) string }\n",
				"pair_types.go": "package foreign\n\ntype First struct{}\n\ntype Second struct{}\n",
				"id_darwin.go":  "package foreign\n\ntype ID = First\n",
				"id_windows.go": "package foreign\n\ntype ID = Second\n"},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\ntype Mine struct{}\n\n" +
				"func (Mine) Put(a foreign.ID, b foreign.First) string { return \"\" }\n\n" +
				"func Use() int { return foreign.Count() }\n",
			lookup:    methodAnswered("Pair", "Put"),
			typeReads: true,
			want:      "T object " + methodForeignPath + ".ID: its name or kind differs",
		},
		{
			name: "an unanswered generic interface whose type parameters change under the same signature",
			files: map[string]string{"foreign.go": methodForeign,
				"putter_darwin.go":  putter("Item any"),
				"putter_windows.go": putter("Item, Extra any")},
			user:   putUser,
			lookup: unanswered(build_reads.QueryMethod, "Put"),
			want: "L lookup Method Put: " + methodForeignPath + ".Putter.Put is changed in the build, of the " +
				"subject's shape",
		},
		{
			name: "an unanswered generic concrete type whose type parameters change under the same signature, beside " +
				"an interface method of ours",
			files: map[string]string{"foreign.go": methodForeign,
				"box_darwin.go":  box("Item any", "Item"),
				"box_windows.go": box("Item, Extra any", "Item, Extra")},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\n" +
				"type Mine interface{ Put(value int) error }\n\nfunc Use() int { return foreign.Count() }\n",
			lookup: func(host methodWorld) build_reads.Lookup {
				return askedBy(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Put",
					Subject: methodSetMember(host.user, "Mine", "Put")})
			},
			want: "L lookup Method Put: " + methodForeignPath + ".Box.Put is changed in the build, of the subject's " +
				"shape",
		},
		{
			// Box[Item, Extra any] holds Extra nowhere bare, so abbreviated matches Mine's interface method to its Put
			// by shape alone; Box[Item any] reads Item off it, and Put(int) string is not Put(int) error.
			name: "a generic concrete type's method as the answer, whose type parameters change under the same " +
				"signature",
			files: map[string]string{"foreign.go": methodForeign,
				"box_darwin.go":  box("Item, Extra any", "Item, Extra"),
				"box_windows.go": box("Item any", "Item")},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\n" +
				"type Mine interface{ Put(value int) error }\n\nfunc Use() int { return foreign.Count() }\n",
			lookup: func(host methodWorld) build_reads.Lookup {
				return askedBy(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Put",
					Subject: methodSetMember(host.user, "Mine", "Put"), Found: true,
					Answer: methodSetMember(host.foreign, "Box", "Put")})
			},
			want: "L lookup Method Put: its answer " + methodForeignPath + ".Put has another signature in the " +
				"build",
		},
		{
			name: "a generic answer whose type parameters change under the same signature",
			files: map[string]string{"foreign.go": methodForeign,
				"putter_darwin.go":  putter("Item, Extra any"),
				"putter_windows.go": putter("Item any")},
			user:   putUser,
			lookup: methodAnswered("Putter", "Put"),
			want: "L lookup Method Put: its answer " + methodForeignPath + ".Put has another signature in the " +
				"build",
		},
		{
			name: "the same generic candidate beside no_any's lookup, which instantiates nothing, held where the " +
				"closure declines",
			files: map[string]string{"foreign.go": methodForeign, "closer.go": closer, "pool.go": pool,
				"inner.go": closerBase, "promoted_windows.go": promotedOnly},
			lookup: askedByNoAny(methodAnswered("Closer", "Close")),
		},
		{
			name:   "a windows-only generic of the subject's shape beside no_any's miss",
			files:  map[string]string{"foreign.go": methodForeign, "pool_windows.go": pool},
			lookup: askedByNoAny(unanswered(build_reads.QueryMethod, "Close")),
			want: "L lookup Method Close: " + methodForeignPath + ".Pool.Close is new in the build, of the subject's " +
				"shape",
		},
		{
			name: "abbreviated's miss beside no_any's answer of one subject, judged as its own",
			files: map[string]string{"foreign.go": methodForeign,
				"base.go":           "package foreign\n\ntype Base struct{}\n\nfunc (Base) Close() error { return nil }\n",
				"closer_windows.go": closer},
			before: askedByNoAny(methodAnswered("Base", "Close")),
			lookup: unanswered(build_reads.QueryMethod, "Close"),
			want: "L lookup Method Close: " + methodForeignPath + ".Closer.Close is new in the build, of the " +
				"subject's shape",
		},
		{
			name: "a receiver embedding a foreign type keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign + "\ntype Base struct{}\n", "closer.go": closer,
				"inner.go": closerBase, "promoted_windows.go": promotedOnly},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\ntype Mine struct{ foreign.Base }\n\n" +
				"func (Mine) Close() error { return nil }\n\nfunc Use() int { return foreign.Count() }\n",
			lookup: interfaceAnswered,
			want:   "L lookup Interface Close: 2 type names have the method on the host, 3 in the build",
		},
		{
			name: "a receiver embedding a foreign type through a type of its own keeps the closure judgment",
			files: map[string]string{"foreign.go": methodForeign + "\ntype Base struct{}\n", "closer.go": closer,
				"inner.go": closerBase, "promoted_windows.go": promotedOnly},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\ntype inner struct{ *foreign.Base }\n\n" +
				"type Mine struct{ inner }\n\nfunc (Mine) Close() error { return nil }\n\n" +
				"func Use() int { return foreign.Count() }\n",
			lookup: interfaceAnswered,
			want:   "L lookup Interface Close: 2 type names have the method on the host, 3 in the build",
		},
		{
			name: "an answer that is the predeclared error, the same in every build",
			files: map[string]string{"foreign.go": methodForeign, "inner.go": closerBase,
				"promoted_windows.go": promotedOnly},
			user: "package user\n\nimport \"example.com/methods/foreign\"\n\ntype Mine struct{}\n\n" +
				"func (Mine) Error() string { return \"\" }\n\nfunc Use() int { return foreign.Count() }\n",
			lookup: func(host methodWorld) build_reads.Lookup {
				return askedBy(build_reads.Lookup{Query: build_reads.QueryInterface, Name: "Error",
					Subject: methodOf(host.user, "Mine", "Error"), Found: true,
					Answer: types.Universe.Lookup("error")})
			},
		},
		{
			name:   "a concrete subject's answer whose interface gains a method it lacks",
			files:  closerGainsFlush,
			lookup: methodAnswered("Closer", "Close"),
			want: "L lookup Method Close: " + methodForeignPath + ".Closer, whose method is its answer, has other " +
				"methods in the build",
		},
		{
			name:   "the same answer on a darwin build, where the interface is the host's",
			files:  closerGainsFlush,
			build:  "darwin",
			lookup: methodAnswered("Closer", "Close"),
		},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			user := methodUser
			if step.user != "" {
				user = step.user
			}
			host := checkedMethodWorld(test, "darwin", step.files, user)
			buildGoos := "windows"
			if step.build != "" {
				buildGoos = step.build
			}
			built := checkedMethodWorld(test, buildGoos, step.files, user)
			reads := &build_reads.Reads{}
			// No syntax and no type information: Mine's reads are none, and the lookup is what each step is about,
			// unless the step is about what the type reads catch.
			if step.typeReads {
				reads.RecordTypes(host.user, host.userInfo, host.userSyntax)
			} else {
				reads.RecordTypes(host.user, &types.Info{}, nil)
			}
			if step.before != nil {
				reads.AddLookup(step.before(host))
			}
			reads.AddLookup(step.lookup(host))
			restoring := Restoring{Path: methodUserPath, Imports: []string{methodForeignPath}, Reads: reads}
			closure := map[string]*types.Package{methodForeignPath: built.foreign}
			hostMemo := measuredReadsBuild()
			holds, why := TypesHold(restoring, closure, nil, hostMemo, measuredReadsBuild())
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
			// The same reads as an entry persists them are judged alike, by the same judgment (methodLookupHolds).
			persisted := persistedRestoring(test, restoring, hostMemo, ReadsRendererVersion)
			persistedHolds, persistedWhy := TypesHold(persisted, closure, nil, hostMemo, measuredReadsBuild())
			if persistedHolds != holds || persistedWhy != why {
				test.Errorf("persisted, want %t, %q as in memory, got %t, %q", holds, why, persistedHolds,
					persistedWhy)
			}
		})
	}
}

// methodSetMember is the method named method in the method set of the type named typeName in declaring, an interface's
// own or embedded methods as well as a named type's declared ones, which methodOf does not reach.
func methodSetMember(declaring *types.Package, typeName string, method string) types.Object {
	declared := declaring.Scope().Lookup(typeName).Type()
	if !types.IsInterface(declared) {
		declared = types.NewPointer(declared)
	}
	methods := types.NewMethodSet(declared)
	for index := range methods.Len() {
		if member := methods.At(index).Obj(); member.Name() == method {
			return member
		}
	}
	return nil
}
