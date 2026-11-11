package go_engine

import (
	"bytes"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// sourceRecorder is what a planted analyzer read through pass.ReadFile, held to the syntax each file was parsed from.
type sourceRecorder struct {
	lock sync.Mutex

	// read is what each file read as the last time it was read, by name.
	read map[string][]byte

	// unfaithful says how each read that was not the bytes its syntax was parsed from differed: an error, another
	// length, or an identifier whose name the bytes do not hold at its offset, as a rename's would not.
	unfaithful []string
}

func newSourceRecorder() *sourceRecorder {
	return &sourceRecorder{read: map[string][]byte{}}
}

// record reads every file pass was given through pass.ReadFile and holds the bytes to the file's syntax.
func (recorder *sourceRecorder) record(pass *analysis.Pass) {
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		content, readError := pass.ReadFile(tokenFile.Name())
		recorder.lock.Lock()
		recorder.read[tokenFile.Name()] = content
		recorder.lock.Unlock()
		if readError != nil {
			recorder.differs("%s: %v", tokenFile.Name(), readError)
			continue
		}
		if len(content) != tokenFile.Size() {
			recorder.differs("%s: read %d bytes, parsed from %d", tokenFile.Name(), len(content), tokenFile.Size())
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			offset := tokenFile.Offset(identifier.Pos())
			if !bytes.HasPrefix(content[offset:], []byte(identifier.Name)) {
				recorder.differs("%s: %s was parsed at offset %d, where the read holds %q", tokenFile.Name(),
					identifier.Name, offset, content[offset:min(len(content), offset+len(identifier.Name))])
			}
			return true
		})
	}
}

func (recorder *sourceRecorder) differs(template string, values ...any) {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	recorder.unfaithful = append(recorder.unfaithful, fmt.Sprintf(template, values...))
}

// rule is a planted house rule that records every file of every package it runs on and reports nothing.
func (recorder *sourceRecorder) rule() rule.Rule {
	return rule.Rule{
		Name:   rule.Namespace + "correctness-read-every-file",
		Origin: rule.OriginHouse,
		Analyzer: &analysis.Analyzer{
			Name: "readeveryfile",
			Doc:  "readeveryfile reads every file through pass.ReadFile, so a test can see what a rule reads.",
			Run: func(pass *analysis.Pass) (any, error) {
				recorder.record(pass)
				return nil, nil
			},
		},
	}
}

// runRecorded runs the engine over root with extra after `--contract 3 --root <root>`, failing the test when it
// refuses the run, and returns its stream and its stderr.
func runRecorded(test *testing.T, root string, registered []rule.Rule, extra ...string) (string, string) {
	test.Helper()
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, extra...)
	if exitCode := run(arguments, &standardOutput, &standardError, registered, nil); exitCode == exitRefused {
		test.Fatalf("the engine refused the run; stderr:\n%s", standardError.String())
	}
	return standardOutput.String(), standardError.String()
}

// A file the fix phase rewrote before the load, by gofmt or by the long-line wrap, is read by a rule as rewritten: the
// bytes on disk after the run, which the load parsed, and never the bytes the run first read.
func TestARuleReadsAFileAsTheFixPhaseRewroteIt(test *testing.T) {
	test.Parallel()
	for _, rewritten := range []struct {
		name    string
		fixture string
		file    string

		// long is whether the long-line rule is registered, which the wrap needs.
		long bool
	}{
		{name: "gofmt", fixture: "unformatted", file: "unformatted.go"},
		{name: "the long-line wrap", fixture: "long-lines", file: "long_lines.go", long: true},
	} {
		test.Run(rewritten.name, func(test *testing.T) {
			test.Parallel()
			root := copyFixture(test, filepath.Join("testdata", rewritten.fixture))
			path := filepath.Join(root, rewritten.file)
			before, beforeError := os.ReadFile(path)
			if beforeError != nil {
				test.Fatalf("reading %s: %v", path, beforeError)
			}
			recorder := newSourceRecorder()
			registered := []rule.Rule{recorder.rule()}
			if rewritten.long {
				registered = append(registered, consistency_no_long_line.Rule)
			}
			stream, prose := runRecorded(test, root, registered)
			after, afterError := os.ReadFile(path)
			if afterError != nil {
				test.Fatalf("reading %s: %v", path, afterError)
			}
			if bytes.Equal(before, after) {
				test.Fatalf("the fix phase left %s as it was, so this case reads nothing rewritten; stream:\n%s",
					rewritten.file, stream)
			}
			read, wasRead := recorder.read[path]
			if !wasRead {
				test.Fatalf("no rule read %s; stream:\n%s\nstderr:\n%s", rewritten.file, stream, prose)
			}
			if !bytes.Equal(read, after) {
				test.Errorf("a rule read %s as\n%s\nwant it as the fix phase rewrote it\n%s", rewritten.file, read,
					after)
			}
			for _, unfaithful := range recorder.unfaithful {
				test.Errorf("a read is not what the load parsed: %s", unfaithful)
			}
		})
	}
}

// A file the run holds no bytes for, here one git ignores in a package with a file in scope, which the rules are still
// given with its package, is read from disk.
func TestAFileTheRunDoesNotHoldIsReadFromDisk(test *testing.T) {
	test.Parallel()
	root := excludedModule(test, map[string]string{
		".gitignore": "scratch.go\n",
		"value.go":   "package value\n\n// Value is one.\nfunc Value() int { return 1 }\n",
		"scratch.go": "package value\n\n// Scratch is two.\nfunc Scratch() int { return Value() + Value() }\n",
	})
	recorder := newSourceRecorder()
	stream, prose := runRecorded(test, root, []rule.Rule{recorder.rule()})
	for _, relative := range []string{"scratch.go", "value.go"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		onDisk, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatalf("reading %s: %v", path, readError)
		}
		read, wasRead := recorder.read[path]
		if !wasRead {
			test.Fatalf("no rule read %s; stream:\n%s\nstderr:\n%s", relative, stream, prose)
		}
		if !bytes.Equal(read, onDisk) {
			test.Errorf("a rule read %s as %q, want what is on disk, %q", relative, read, onDisk)
		}
	}
	for _, unfaithful := range recorder.unfaithful {
		test.Errorf("a read is not what the load parsed: %s", unfaithful)
	}
}

// The naming rules run again over each round of renames the rename proved, a load that parsed the rewrite from memory
// before it is written, and read that rewrite, never the bytes on disk the round replaces.
func TestTheNamingRulesReadTheRewriteTheyRunOver(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "rename"))
	recorder := newSourceRecorder()
	analyzer := consistency_no_abbreviated_identifier.NewAnalyzer()
	judge := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		recorder.record(pass)
		return judge(pass)
	}
	registered := rules.All()
	index := slices.IndexFunc(registered, func(each rule.Rule) bool {
		return each.Name == consistency_no_abbreviated_identifier.Name
	})
	if index < 0 {
		test.Fatalf("the registry holds no %s", consistency_no_abbreviated_identifier.Name)
	}
	registered[index] = withOwnAnalyzer(registered[index], analyzer)
	stream, prose := runRecorded(test, root, registered)
	if len(recorder.read) == 0 {
		test.Fatalf("the naming rule read nothing; stream:\n%s\nstderr:\n%s", stream, prose)
	}
	for _, unfaithful := range recorder.unfaithful {
		test.Errorf("a read is not what the load parsed: %s", unfaithful)
	}
}

// sourceReader serves a file from the overlay first, then from the bytes the run holds, and reads any other from
// disk.
func TestSourceReaderServesTheOverlayThenTheHeldBytesThenTheDisk(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	overlaid, held, onDisk := filepath.Join(directory, "overlaid.go"), filepath.Join(directory, "held.go"),
		filepath.Join(directory, "disk.go")
	for _, path := range []string{overlaid, held, onDisk} {
		if writeError := os.WriteFile(path, []byte("package disk\n"), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", path, writeError)
		}
	}
	engine := &pipeline{described: &describedProject{contents: map[string][]byte{
		overlaid: []byte("package held\n"),
		held:     []byte("package held\n"),
	}}}
	read := engine.sourceReader(map[string][]byte{overlaid: []byte("package overlay\n")}, nil)
	for path, expected := range map[string]string{
		overlaid: "package overlay\n",
		held:     "package held\n",
		onDisk:   "package disk\n",
	} {
		content, readError := read(path)
		if readError != nil || string(content) != expected {
			test.Errorf("reading %s: %q, %v, want %q", filepath.Base(path), content, readError, expected)
		}
	}
	if _, readError := read(filepath.Join(directory, "absent.go")); readError == nil {
		test.Error("reading a file that is nowhere gave no error")
	}
}
