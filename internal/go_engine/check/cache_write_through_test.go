package check

import (
	"bytes"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// The write-through fixture's packages, which every build declares alike.
const (
	throughBasePath = "example.com/through/base"
	throughUserPath = "example.com/through/user"
)

// throughFiles are a module of two packages every build declares alike: base, with a printf wrapper Logf, which
// printf's port learns a fact about, and a pure Double; and user, which hands Logf a format its argument does not fit,
// calls Double, whose purity the probe reports by its answer, and declares a wrapper of its own, Say, so its entry
// holds a fact, findings and answers alike.
var throughFiles = map[string]string{
	"go.mod": "module example.com/through\n\ngo 1.24\n",
	"base/base.go": "// Package base logs and doubles.\npackage base\n\nimport \"fmt\"\n\n" +
		"// Logf formats as fmt.Sprintf does.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn fmt.Sprintf(format, arguments...)\n}\n\n" +
		"// Double is twice value.\nfunc Double(value int) int {\n\treturn value * 2\n}\n",
	"user/user.go": "// Package user logs through base.\npackage user\n\nimport \"example.com/through/base\"\n\n" +
		"// Say formats as base.Logf does.\nfunc Say(format string, arguments ...any) string {\n" +
		"\treturn base.Logf(format, arguments...)\n}\n\n" +
		"// Use hands Logf a format its argument does not fit.\nfunc Use() string {\n" +
		"\treturn base.Logf(\"%d\", \"text\") + Say(\"%d\", base.Double(2))\n}\n",
}

// throughRules are printf's port, whose facts and findings an entry holds, and the purity probe, whose findings read
// answers of another package.
func throughRules() []rule.Rule {
	return []rule.Rule{
		{
			Name:         rule.Namespace + "test-printf",
			Origin:       rule.OriginVet,
			UpstreamName: "printf",
			Analyzer:     printf.Analyzer,
		},
		houseRule(purityProbe),
	}
}

// throughHost is the host's run over root, with its store in storeDirectory, keeping its packages for the builds as
// the engine's host does, once every read of them is whole.
func throughHost(test *testing.T, root string, storeDirectory string) *HostRun {
	test.Helper()
	host, _ := throughHostSession(test, root, storeDirectory)
	return host
}

// throughHostSession is throughHost with the host's session.
func throughHostSession(test *testing.T, root string, storeDirectory string) (*HostRun, *CacheSession) {
	test.Helper()
	rules := throughRules()
	session := NewCacheSession(cachingAt(storeDirectory, rules), rules)
	session.KeepForBuilds()
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, RecordReads: true})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the host's build: %v %+v", loadError, loaded.Diagnostics)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots, KeepForBuilds: true})
	if runError != nil || len(result.Crashes) > 0 {
		test.Fatalf("running the host's build: %v %+v", runError, result.Crashes)
	}
	// The builds judge the host's reads, which are whole only once their walks end, as the engine waits for them.
	result.ReadWalks.Wait()
	return result.Host, session
}

// throughBuild is windows's run over root, with its store in storeDirectory, restoring from host when it is not nil,
// and loaded again as the engine loads an extra build, until no reload is asked, with its session.
func throughBuild(test *testing.T, root string, storeDirectory string, host *HostRun) (*Result, *CacheSession) {
	test.Helper()
	rules := throughRules()
	caching := cachingAt(storeDirectory, rules)
	// The build's keys hold its target, as the engine's do, so they never meet the host's.
	caching.Context.Build = append(caching.Context.Build, "GOOS=windows")
	session := NewCacheSession(caching, rules)
	session.RestoreFromHost(host)
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"},
			nil, load.Caching{Restorer: session})
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading windows's build: %v %+v", loadError, loaded.Diagnostics)
		}
		result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
			Bodiless: loaded.Bodiless})
		if runError != nil {
			test.Fatalf("running windows's build: %v", runError)
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			if len(result.Crashes) > 0 {
				test.Fatalf("windows's build crashed: %+v", result.Crashes)
			}
			return result, session
		}
	}
}

// throughPackage is the package of session's last load at path, failing the test when it has none.
func throughPackage(test *testing.T, session *CacheSession, path string) *packages.Package {
	test.Helper()
	for keyed := range session.keys {
		if keyed.PkgPath == path {
			return keyed
		}
	}
	test.Fatalf("the build keyed no package %s", path)
	return nil
}

// throughEntry is the entry session's store holds of the package at path under the package's first key in the
// session's last load, decoded, with that key, failing the test when the store holds none.
func throughEntry(test *testing.T, session *CacheSession, path string) (restoredPackage, cache.Key) {
	test.Helper()
	keyed := throughPackage(test, session, path)
	key := session.keys[keyed]
	contents, stored := session.caching.Store.Get(key)
	if !stored {
		test.Fatalf("the build's store holds no entry of %s under its key", path)
	}
	restored, decodeError := decodeEntry(session.known, keyed, contents, nil)
	if decodeError != nil {
		test.Fatalf("decoding the entry of %s: %v", path, decodeError)
	}
	return comparableRestored(restored), key
}

// The differential of a write-through (#b2t9xmf): the entry windows writes of a package it restored from the host's
// run decodes to the entry it writes when it checks the package from source itself, with the restore off and a store
// of its own: the same findings, facts, answers, reads and output digest, byte for byte. A digest that differed would
// have the package's importers hit after one run and miss after the other.
func TestAnEntryWrittenThroughIsTheOneTheBuildWritesFromSource(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	host := throughHost(test, root, filepath.Join(stores, "host"))
	through, throughSession := throughBuild(test, root, filepath.Join(stores, "through"), host)
	_, sourceSession := throughBuild(test, root, filepath.Join(stores, "source"), nil)
	for _, path := range []string{throughBasePath, throughUserPath} {
		if !slices.Contains(through.FromHost.Restored, path) {
			test.Fatalf("windows restored %q from the host, want %s among them", through.FromHost.Restored, path)
		}
	}
	if through.Cache.WrittenThrough != len(through.FromHost.Restored) {
		test.Fatalf("windows wrote %d entries through of the %d it restored, want every one",
			through.Cache.WrittenThrough, len(through.FromHost.Restored))
	}
	// A held restore names every read's owner in the build (readsInBuild), so writing it through reports no problem.
	if len(through.Cache.Problems) > 0 {
		test.Errorf("windows reported problems writing through: %q", through.Cache.Problems)
	}
	for _, path := range []string{throughBasePath, throughUserPath} {
		written, writtenKey := throughEntry(test, throughSession, path)
		checked, checkedKey := throughEntry(test, sourceSession, path)
		if writtenKey != checkedKey {
			test.Fatalf("%s: the two builds keyed it %x and %x, want one key", path, writtenKey[:], checkedKey[:])
		}
		if !written.hasOutput || !checked.hasOutput || !bytes.Equal(written.output[:], checked.output[:]) {
			test.Errorf("%s: written through with output %x (held %t), checked from source with %x (held %t), want "+
				"the same digest", path, written.output[:], written.hasOutput, checked.output[:], checked.hasOutput)
		}
		// user reads Double's purity, an answer of base, so its entry holds that read, named in this build.
		if written.readsIncomplete || path == throughUserPath && len(written.reads) == 0 {
			test.Errorf("%s: written through with %d reads, incomplete %t, want every read named", path,
				len(written.reads), written.readsIncomplete)
		}
		if !reflect.DeepEqual(written, checked) {
			test.Errorf("%s: the entry written through\n%+v\nthe entry checked from source\n%+v", path, written,
				checked)
		}
	}
}

// The seam of a write-through (#b2t9xmf): an entry written through stands under the build's key only while
// everything its reads were judged against is as it was, which the key covers through every dependency's key. A
// comment edited in base leaves its output as it was and moves its key, so user's key moves with it, and the entry
// windows wrote through of user is never hit by its first key again; whatever windows does with user then, its
// findings are a run's that restores nothing from the host.
func TestAWrittenThroughEntryMissesOnceADependencysKeyMoves(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	hostStore, buildStore := filepath.Join(stores, "host"), filepath.Join(stores, "build")
	cold, coldSession := throughBuild(test, root, buildStore, throughHost(test, root, hostStore))
	if !slices.Contains(cold.FromHost.Restored, throughUserPath) || cold.Cache.WrittenThrough == 0 {
		test.Fatalf("windows restored %q and wrote %d through, want %s written through", cold.FromHost.Restored,
			cold.Cache.WrittenThrough, throughUserPath)
	}
	_, writtenKey := throughEntry(test, coldSession, throughUserPath)

	editFile(test, root, "base/base.go", replaced(test, "// Double is twice value.", "// Double is value times two."))
	edited, editedSession := throughBuild(test, root, buildStore, throughHost(test, root, hostStore))
	user := throughPackage(test, editedSession, throughUserPath)
	if editedSession.keys[user] == writtenKey {
		test.Fatalf("user's key stayed %x when base's comment moved, so its entry written through would stand",
			writtenKey[:])
	}
	if stored, hit := editedSession.stored[user]; hit && stored == writtenKey {
		test.Errorf("windows restored user by the first key of the entry it wrote through, %x", writtenKey[:])
	}
	reference, _ := throughBuild(test, root, filepath.Join(stores, "reference"), nil)
	if !reflect.DeepEqual(edited.Findings, reference.Findings) {
		test.Errorf("after the edit windows found\n%+v\nand a run restoring nothing from the host\n%+v",
			edited.Findings, reference.Findings)
	}
	if !slices.ContainsFunc(reference.Findings, func(finding Finding) bool {
		return strings.HasSuffix(finding.File, "user.go") && finding.Rule == throughRules()[0].Name
	}) {
		test.Errorf("windows found no printf finding in user.go, which the fixture plants: %+v", reference.Findings)
	}
}
