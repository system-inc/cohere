package release

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// compilerFixture is a module whose HEAD pins a compiler at TypeScript, with the compiler checked out at
// that pin.
type compilerFixture struct {
	// Module is the module's directory.
	Module string

	// Recorded is the compiler commit the module's HEAD pins.
	Recorded string

	// Later is a second compiler commit, which the submodule can be moved to without a commit.
	Later string
}

// compilerTemplate is the one module-with-compiler the package builds, the first time a test asks for one.
// Building it takes eleven git commands, about 8s under load, and every test here wants the same module, so
// it is built once and each test reads it or a copy of it. TestMain removes it.
var compilerTemplate struct {
	once    sync.Once
	root    string
	fixture compilerFixture
	err     error
}

// TestMain removes the compiler template once every test is done with it.
func TestMain(suite *testing.M) {
	code := suite.Run()
	if compilerTemplate.root != "" {
		// Ignored: the template is under the system's temporary directory, and one left there decides no test.
		os.RemoveAll(compilerTemplate.root)
	}
	os.Exit(code)
}

// sharedModuleWithCompiler is the template itself, for a test that only reads it. A test that changes
// anything in the module or its compiler takes its own copy with moduleWithCompiler.
func sharedModuleWithCompiler(test *testing.T) compilerFixture {
	test.Helper()

	compilerTemplate.once.Do(func() {
		compilerTemplate.root, compilerTemplate.err = os.MkdirTemp("", "cohere-compiler-pin-")
		if compilerTemplate.err == nil {
			compilerTemplate.fixture, compilerTemplate.err = buildModuleWithCompiler(compilerTemplate.root)
		}
	})
	if compilerTemplate.err != nil {
		test.Fatalf("building the compiler fixture: %v", compilerTemplate.err)
	}
	return compilerTemplate.fixture
}

// moduleWithCompiler is a copy of the template the test owns, so it can move, patch or remove the
// compiler without another test seeing it. Both repositories are copied whole, .git directories and all,
// and the compiler's origin still names the template's compiler, a local path, as a fresh clone's does.
func moduleWithCompiler(test *testing.T) compilerFixture {
	test.Helper()

	template := sharedModuleWithCompiler(test)
	module := filepath.Join(test.TempDir(), "module")
	if copyFSError := os.CopyFS(module, os.DirFS(template.Module)); copyFSError != nil {
		test.Fatalf("copying the compiler fixture: %v", copyFSError)
	}
	return compilerFixture{Module: module, Recorded: template.Recorded, Later: template.Later}
}

// buildModuleWithCompiler builds, under root, a module pinning a two-commit compiler at its first commit,
// and a committed `.gitmodules` naming the fork. The checkout's own origin is whatever it was cloned from,
// a local path, so nothing that reads the remote can produce the fork's name by accident.
func buildModuleWithCompiler(root string) (compilerFixture, error) {
	// The first failure stops every step after it, and is what the builder returns.
	var failed error
	git := func(directory string, arguments ...string) string {
		if failed != nil {
			return ""
		}
		output, gitRunError := gitRun(directory, arguments...)
		failed = gitRunError
		return output
	}
	write := func(path string, contents string) {
		if failed == nil {
			failed = os.WriteFile(path, []byte(contents), 0o644)
		}
	}

	compiler := filepath.Join(root, "compiler")
	module := filepath.Join(root, "module")
	for _, directory := range []string{compiler, module} {
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
			return compilerFixture{}, createDirectoryError
		}
	}

	// A commit starts git's auto-maintenance in the background, detached, and it outlives the command:
	// it can create and remove .git/objects/maintenance.lock while moduleWithCompiler copies the
	// template, and the copy fails on a file that was listed and is gone. Release dry run 41784756965
	// failed that way. Off in every repository here, so the template is still once it is built.
	quiet := func(directory string) {
		git(directory, "config", "maintenance.auto", "false")
		git(directory, "config", "gc.auto", "0")
	}

	git(compiler, "init", "--quiet")
	quiet(compiler)
	write(filepath.Join(compiler, "checker.go"), "package checker\n")
	git(compiler, "add", "checker.go")
	git(compiler, "commit", "--quiet", "-m", "first")
	recorded := git(compiler, "rev-parse", "HEAD")
	write(filepath.Join(compiler, "checker.go"), "package checker\n\n// later\n")
	git(compiler, "commit", "--quiet", "-am", "later")
	later := git(compiler, "rev-parse", "HEAD")

	git(module, "init", "--quiet")
	quiet(module)
	git(module, "clone", "--quiet", compiler, "TypeScript")
	quiet(filepath.Join(module, "TypeScript"))
	git(filepath.Join(module, "TypeScript"), "checkout", "--quiet", recorded)
	write(filepath.Join(module, ".gitmodules"),
		"[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/system-inc/TypeScript.git\n\tbranch = adamic\n")
	git(module, "-c", "advice.addEmbeddedRepo=false", "add", ".gitmodules", "TypeScript")
	git(module, "commit", "--quiet", "-m", "pin the compiler")

	return compilerFixture{Module: module, Recorded: recorded, Later: later}, failed
}

// TestCompilerPinIsTheCommitsGitlink is the positive half. Without it, a check that refused everything
// would pass the controls below.
func TestCompilerPinIsTheCommitsGitlink(test *testing.T) {
	test.Parallel()

	fixture := sharedModuleWithCompiler(test)

	pin, readCompilerPinError := readCompilerPin(fixture.Module)
	if readCompilerPinError != nil {
		test.Fatalf("a compiler checked out at its pin was refused: %v", readCompilerPinError)
	}
	if pin.Commit != fixture.Recorded {
		test.Fatalf("the pin is %s, and the commit records %s", pin.Commit, fixture.Recorded)
	}
}

// TestCompilerPinAcceptsARelativeModuleDirectory covers a module named relative to the working
// directory. git answers the checkout's top level as an absolute path, and the first version of this
// check compared it against the relative one and refused the real checkout as never initialized,
// while every fixture, all absolute, passed.
func TestCompilerPinAcceptsARelativeModuleDirectory(test *testing.T) {
	test.Parallel()

	fixture := sharedModuleWithCompiler(test)
	workingDirectory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	relative, relativePathError := filepath.Rel(workingDirectory, fixture.Module)
	if relativePathError != nil || filepath.IsAbs(relative) {
		test.Fatalf("could not name the fixture relatively (%q, %v), so this test would prove nothing", relative, relativePathError)
	}

	if _, readCompilerPinError := readCompilerPin(relative); readCompilerPinError != nil {
		test.Fatalf(
			"a compiler checked out at its pin was refused when named as %s: %v",
			relative,
			readCompilerPinError,
		)
	}
}

// TestReleaseRefusesACompilerMovedWithoutACommit is the known-dirty control: the checkout is at a real
// compiler commit, just not the one cohere's HEAD records. The refusal names both, since either could
// be the one someone meant.
func TestReleaseRefusesACompilerMovedWithoutACommit(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)
	gitIn(test, filepath.Join(fixture.Module, "TypeScript"), "checkout", "--quiet", fixture.Later)

	_, readCompilerPinError := readCompilerPin(fixture.Module)
	if readCompilerPinError == nil {
		test.Fatal("a compiler moved without a commit was released")
	}
	for _, commit := range []string{fixture.Recorded, fixture.Later} {
		if !strings.Contains(readCompilerPinError.Error(), commit) {
			test.Fatalf("the refusal does not name %s: %v", commit, readCompilerPinError)
		}
	}
}

// TestPatchedCompilerAtThePinProceeds holds the compatibility the compiler patches need. They change
// files in the checkout without moving its HEAD, so a check that compared trees would refuse every
// patched release, which is every correct one.
func TestPatchedCompilerAtThePinProceeds(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)
	writeFile(test, filepath.Join(fixture.Module, "TypeScript", "checker.go"), "package checker\n\n// patched\n")
	if status := gitIn(test, filepath.Join(fixture.Module, "TypeScript"), "status", "--porcelain"); status == "" {
		test.Fatal("the patch did not change the checkout, so this test would prove nothing")
	}

	pin, readCompilerPinError := readCompilerPin(fixture.Module)
	if readCompilerPinError != nil {
		test.Fatalf("a patched compiler at its pin was refused: %v", readCompilerPinError)
	}
	if pin.Commit != fixture.Recorded {
		test.Fatalf("the pin is %s, and the commit records %s", pin.Commit, fixture.Recorded)
	}
}

// TestUncheckedOutCompilerIsNamedAsSuch covers the empty directory a submodule that was never initialized
// leaves. `git -C` from there walks up and answers with the module's own HEAD, which would be reported
// as a moved compiler naming a commit that is not a compiler.
func TestUncheckedOutCompilerIsNamedAsSuch(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)
	submodule := filepath.Join(fixture.Module, "TypeScript")
	for _, name := range []string{".git", "checker.go"} {
		if removeError := os.RemoveAll(filepath.Join(submodule, name)); removeError != nil {
			test.Fatal(removeError)
		}
	}

	_, readCompilerPinError := readCompilerPin(fixture.Module)
	if readCompilerPinError == nil {
		test.Fatal("a compiler that is not checked out was released")
	}
	if !strings.Contains(readCompilerPinError.Error(), "not checked out") {
		test.Fatalf("an uninitialized submodule was reported as something else: %v", readCompilerPinError)
	}
}

// TestUpstreamIsReadFromTheCommittedGitmodules is the control for a checkout cloned before the pin moved
// to the fork: its origin still names Microsoft, and its working `.gitmodules` has been edited without a
// commit. Only the committed file is a fact about the release, so both have to lose.
func TestUpstreamIsReadFromTheCommittedGitmodules(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)
	gitIn(
		test,
		filepath.Join(fixture.Module, "TypeScript"),
		"remote",
		"set-url",
		"origin",
		"git@github.com:microsoft/TypeScript.git",
	)
	writeFile(test, filepath.Join(fixture.Module, ".gitmodules"),
		"[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/someone/else.git\n")

	pin, readCompilerPinError := readCompilerPin(fixture.Module)
	if readCompilerPinError != nil {
		test.Fatal(readCompilerPinError)
	}
	if pin.Upstream != "system-inc/TypeScript" {
		test.Fatalf("the upstream is %q, and the commit's .gitmodules names system-inc/TypeScript", pin.Upstream)
	}
}

// TestCompilerUpstreamFromGitmodules finds the entry whose path is the compiler's directory, whatever its
// name or position, in either url shape, and says "unknown" when the file does not say.
func TestCompilerUpstreamFromGitmodules(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name       string
		gitmodules string
		want       string
	}{
		{"the fork, after another submodule, over ssh",
			"[submodule \"libraries/other\"]\n\tpath = libraries/other\n" +
				"\turl = https://github.com/someone/other.git\n" +
				"[submodule \"compiler\"]\n\tpath = TypeScript\n\turl = git@github.com:system-inc/TypeScript.git\n",
			"system-inc/TypeScript"},
		{
			"over https, quoted",
			"[submodule \"TypeScript\"]\n\tpath = \"TypeScript\"\n\turl = \"https://github.com/system-inc/TypeScript.git\"\n\tbranch = adamic\n",
			"system-inc/TypeScript",
		},
		{
			"no entry for the compiler",
			"[submodule \"other\"]\n\tpath = other\n\turl = https://github.com/someone/other.git\n",
			"unknown",
		},
		{
			"a url that names no repository",
			"[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/\n",
			"unknown",
		},
		{"empty", "", "unknown"},
	} {
		if got := CompilerUpstreamFromGitmodules(testCase.gitmodules); got != testCase.want {
			test.Errorf("%s: named %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestBuildRefusesAMovedCompiler proves Build calls the check, which the tests above cannot: they drive
// readCompilerPin directly and would pass with the call deleted from Build.
func TestBuildRefusesAMovedCompiler(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)
	gitIn(test, filepath.Join(fixture.Module, "TypeScript"), "checkout", "--quiet", fixture.Later)

	_, buildError := Build(Options{Version: "1.0.0", ModuleDirectory: fixture.Module, OutputDirectory: test.TempDir()})
	if buildError == nil {
		test.Fatal("Build released a compiler moved without a commit")
	}
	if !strings.Contains(buildError.Error(), fixture.Later) {
		test.Fatalf("Build failed, but not on the moved compiler: %v", buildError)
	}
}
