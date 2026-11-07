package build_inputs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refusal is what a stoppingTest panics with when OutsideModule fails it, so the call ends where a real test's
// would and the case reads the message.
type refusal struct{ message string }

// stoppingTest is a test whose Fatal and Fatalf stop the call that made them, as a real test's do, and keep the
// message, so OutsideModule's refusal can be watched without failing this test.
type stoppingTest struct{ testing.TB }

func (stopping stoppingTest) Helper() {
	// No frame is ever reported from a stand-in, so none needs marking.
}

func (stopping stoppingTest) Fatal(arguments ...any) { panic(refusal{fmt.Sprint(arguments...)}) }

func (stopping stoppingTest) Fatalf(format string, arguments ...any) {
	panic(refusal{fmt.Sprintf(format, arguments...)})
}

// outsideModule runs OutsideModule on directory and returns what it returned, or the message it refused with.
func outsideModule(test *testing.T, directory string) (returned string, refused string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stopped, isRefusal := recovered.(refusal)
			if !isRefusal {
				panic(recovered)
			}
			refused = stopped.message
		}
	}()
	return OutsideModule(stoppingTest{test}, directory), ""
}

// OutsideModule passes a directory of the test's own and refuses this package's, and one that only reaches this
// package through a symbolic link, since what go reads there is the module's code (#h1e0b8b).
func TestOutsideModuleRefusesADirectoryInsideTheModule(test *testing.T) {
	test.Parallel()
	fixture := test.TempDir()
	if returned, refused := outsideModule(test, fixture); refused != "" || returned != fixture {
		test.Errorf("a temporary directory was refused or changed: returned %q, refused %q", returned, refused)
	}
	if _, refused := outsideModule(test, "."); !strings.Contains(refused, "is inside the module") {
		test.Errorf("this package's own directory was not refused as inside the module: %q", refused)
	}
	here, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	link := filepath.Join(fixture, "link")
	if symlinkError := os.Symlink(here, link); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	if _, refused := outsideModule(test, link); !strings.Contains(refused, "is inside the module") {
		test.Errorf("a link into the module was not refused: %q", refused)
	}
}
