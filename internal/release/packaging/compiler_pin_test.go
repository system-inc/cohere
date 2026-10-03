package release

import (
	"os"
	"path/filepath"
	"strings"
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

// moduleWithCompiler builds a module pinning a two-commit compiler at its first commit, and a committed
// `.gitmodules` naming the fork. The checkout's own origin is whatever it was cloned from, a local path,
// so nothing that reads the remote can produce the fork's name by accident.
func moduleWithCompiler(t *testing.T) compilerFixture {
	t.Helper()

	compiler := t.TempDir()
	gitIn(t, compiler, "init", "--quiet")
	writeFile(t, filepath.Join(compiler, "checker.go"), "package checker\n")
	gitIn(t, compiler, "add", "checker.go")
	gitIn(t, compiler, "commit", "--quiet", "-m", "first")
	recorded := gitIn(t, compiler, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(compiler, "checker.go"), "package checker\n\n// later\n")
	gitIn(t, compiler, "commit", "--quiet", "-am", "later")
	later := gitIn(t, compiler, "rev-parse", "HEAD")

	module := t.TempDir()
	gitIn(t, module, "init", "--quiet")
	gitIn(t, module, "clone", "--quiet", compiler, "TypeScript")
	gitIn(t, filepath.Join(module, "TypeScript"), "checkout", "--quiet", recorded)
	writeFile(t, filepath.Join(module, ".gitmodules"),
		"[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/kirkouimet/TypeScript.git\n")
	gitIn(t, module, "-c", "advice.addEmbeddedRepo=false", "add", ".gitmodules", "TypeScript")
	gitIn(t, module, "commit", "--quiet", "-m", "pin the compiler")

	return compilerFixture{Module: module, Recorded: recorded, Later: later}
}

// TestCompilerPinIsTheCommitsGitlink is the positive half. Without it, a check that refused everything
// would pass the controls below.
func TestCompilerPinIsTheCommitsGitlink(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)

	pin, err := readCompilerPin(fixture.Module)
	if err != nil {
		t.Fatalf("a compiler checked out at its pin was refused: %v", err)
	}
	if pin.Commit != fixture.Recorded {
		t.Fatalf("the pin is %s, and the commit records %s", pin.Commit, fixture.Recorded)
	}
}

// TestCompilerPinAcceptsARelativeModuleDirectory covers a module named relative to the working
// directory. git answers the checkout's top level as an absolute path, and the first version of this
// check compared it against the relative one and refused the real checkout as never initialized,
// while every fixture, all absolute, passed.
func TestCompilerPinAcceptsARelativeModuleDirectory(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, fixture.Module)
	if err != nil || filepath.IsAbs(relative) {
		t.Fatalf("could not name the fixture relatively (%q, %v), so this test would prove nothing", relative, err)
	}

	if _, err := readCompilerPin(relative); err != nil {
		t.Fatalf("a compiler checked out at its pin was refused when named as %s: %v", relative, err)
	}
}

// TestReleaseRefusesACompilerMovedWithoutACommit is the known-dirty control: the checkout is at a real
// compiler commit, just not the one cohere's HEAD records. The refusal names both, since either could
// be the one someone meant.
func TestReleaseRefusesACompilerMovedWithoutACommit(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	gitIn(t, filepath.Join(fixture.Module, "TypeScript"), "checkout", "--quiet", fixture.Later)

	_, err := readCompilerPin(fixture.Module)
	if err == nil {
		t.Fatal("a compiler moved without a commit was released")
	}
	for _, commit := range []string{fixture.Recorded, fixture.Later} {
		if !strings.Contains(err.Error(), commit) {
			t.Fatalf("the refusal does not name %s: %v", commit, err)
		}
	}
}

// TestPatchedCompilerAtThePinProceeds holds the compatibility the compiler patches need. They change
// files in the checkout without moving its HEAD, so a check that compared trees would refuse every
// patched release, which is every correct one.
func TestPatchedCompilerAtThePinProceeds(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	writeFile(t, filepath.Join(fixture.Module, "TypeScript", "checker.go"), "package checker\n\n// patched\n")
	if status := gitIn(t, filepath.Join(fixture.Module, "TypeScript"), "status", "--porcelain"); status == "" {
		t.Fatal("the patch did not change the checkout, so this test would prove nothing")
	}

	pin, err := readCompilerPin(fixture.Module)
	if err != nil {
		t.Fatalf("a patched compiler at its pin was refused: %v", err)
	}
	if pin.Commit != fixture.Recorded {
		t.Fatalf("the pin is %s, and the commit records %s", pin.Commit, fixture.Recorded)
	}
}

// TestUncheckedOutCompilerIsNamedAsSuch covers the empty directory a submodule that was never initialized
// leaves. `git -C` from there walks up and answers with the module's own HEAD, which would be reported
// as a moved compiler naming a commit that is not a compiler.
func TestUncheckedOutCompilerIsNamedAsSuch(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	submodule := filepath.Join(fixture.Module, "TypeScript")
	for _, name := range []string{".git", "checker.go"} {
		if err := os.RemoveAll(filepath.Join(submodule, name)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := readCompilerPin(fixture.Module)
	if err == nil {
		t.Fatal("a compiler that is not checked out was released")
	}
	if !strings.Contains(err.Error(), "not checked out") {
		t.Fatalf("an uninitialized submodule was reported as something else: %v", err)
	}
}

// TestUpstreamIsReadFromTheCommittedGitmodules is the control for a checkout cloned before the pin moved
// to the fork: its origin still names Microsoft, and its working `.gitmodules` has been edited without a
// commit. Only the committed file is a fact about the release, so both have to lose.
func TestUpstreamIsReadFromTheCommittedGitmodules(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	gitIn(t, filepath.Join(fixture.Module, "TypeScript"), "remote", "set-url", "origin", "git@github.com:microsoft/TypeScript.git")
	writeFile(t, filepath.Join(fixture.Module, ".gitmodules"),
		"[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/someone/else.git\n")

	pin, err := readCompilerPin(fixture.Module)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Upstream != "kirkouimet/TypeScript" {
		t.Fatalf("the upstream is %q, and the commit's .gitmodules names kirkouimet/TypeScript", pin.Upstream)
	}
}

// TestCompilerUpstreamFromGitmodules finds the entry whose path is the compiler's directory, whatever its
// name or position, in either url shape, and says "unknown" when the file does not say.
func TestCompilerUpstreamFromGitmodules(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		gitmodules string
		want       string
	}{
		{"the fork, after another submodule, over ssh",
			"[submodule \"libraries/other\"]\n\tpath = libraries/other\n\turl = https://github.com/someone/other.git\n" +
				"[submodule \"compiler\"]\n\tpath = TypeScript\n\turl = git@github.com:kirkouimet/TypeScript.git\n",
			"kirkouimet/TypeScript"},
		{"over https, quoted", "[submodule \"TypeScript\"]\n\tpath = \"TypeScript\"\n\turl = \"https://github.com/kirkouimet/TypeScript.git\"\n",
			"kirkouimet/TypeScript"},
		{"no entry for the compiler", "[submodule \"other\"]\n\tpath = other\n\turl = https://github.com/someone/other.git\n", "unknown"},
		{"a url that names no repository", "[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/\n", "unknown"},
		{"empty", "", "unknown"},
	} {
		if got := CompilerUpstreamFromGitmodules(testCase.gitmodules); got != testCase.want {
			t.Errorf("%s: named %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestBuildRefusesAMovedCompiler proves Build calls the check, which the tests above cannot: they drive
// readCompilerPin directly and would pass with the call deleted from Build.
func TestBuildRefusesAMovedCompiler(t *testing.T) {
	t.Parallel()

	fixture := moduleWithCompiler(t)
	gitIn(t, filepath.Join(fixture.Module, "TypeScript"), "checkout", "--quiet", fixture.Later)

	_, err := Build(Options{Version: "1.0.0", ModuleDirectory: fixture.Module, OutputDirectory: t.TempDir()})
	if err == nil {
		t.Fatal("Build released a compiler moved without a commit")
	}
	if !strings.Contains(err.Error(), fixture.Later) {
		t.Fatalf("Build failed, but not on the moved compiler: %v", err)
	}
}
