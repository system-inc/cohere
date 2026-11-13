package corpus

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// StructurePin is the commit of the Structure corpus the gate's walks over it read, so a landing's verdict never
// depends on what someone pushed to Structure that hour (#jf5dz34). The nightly runs the same walks over the live
// checkout (LiveVariable), where new code is, and a construct no one anticipated still turns up within a day. When a
// nightly's live run is green, this may move to that night's Structure head, in one commit that says so. An exact
// count reads a pin of its own, which moves only when its analysis does.
const StructurePin = "1fa60efa5922c795ce8c42b7390ca21bd8030bec"

// LiveVariable, set, makes At read a corpus's live checkout instead of its pinned commit: the nightly's census over
// the tree as it is now. No gate sets it.
const LiveVariable = "COHERE_CORPUS_LIVE"

// At is a path inside the corpus as it stood at commit, in a copy of that commit made for this test alone, or the
// skip or failure Root gives when the corpus is unset. The copy is the test's own, so nothing another test or job
// wrote can stand in for it. A corpus set to a checkout that lacks the commit fails the test: it asked for a frozen
// input and would otherwise read something else. With LiveVariable set it is Path instead, the checkout as it is.
func (corpus Corpus) At(test testing.TB, commit string, elements ...string) string {
	test.Helper()
	root := corpus.Root(test)
	if os.Getenv(LiveVariable) != "" {
		return filepath.Join(append([]string{root}, elements...)...)
	}
	copied := test.TempDir()
	archive := exec.Command("git", "-C", root, "archive", "--format=tar", commit+"^{tree}")
	var archiveErrors bytes.Buffer
	archive.Stderr = &archiveErrors
	extract := exec.Command("tar", "-x", "-f", "-", "-C", copied)
	pipe, pipeError := archive.StdoutPipe()
	if pipeError != nil {
		test.Fatal(pipeError)
	}
	extract.Stdin = pipe
	if startError := archive.Start(); startError != nil {
		test.Fatalf("reading %s at %s: %v", corpus.Name, commit, startError)
	}
	extracted, extractError := extract.CombinedOutput()
	if waitError := archive.Wait(); waitError != nil {
		test.Fatalf("%s at %s holds no commit %s, so this test can't read the frozen input it asks for: %v %s",
			corpus.Variable, root, commit, waitError, bytes.TrimSpace(archiveErrors.Bytes()))
	}
	if extractError != nil {
		test.Fatalf("copying %s at %s: %v %s", corpus.Name, commit, extractError, extracted)
	}
	return filepath.Join(append([]string{copied}, elements...)...)
}
