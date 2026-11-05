package notices

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func moduleRoot(test *testing.T) string {
	test.Helper()
	root, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	return root
}

func committed(test *testing.T, name string) string {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(moduleRoot(test), name))
	if readError != nil {
		test.Fatalf("%s is not at the module root: %v", name, readError)
	}
	return string(contents)
}

// TestTheCommittedNoticesAreCurrent regenerates both files and requires the committed ones to match. It
// is what fails when a dependency moves, the compiler's NOTICE.txt changes, the Swift engine resolves a
// new package or the curated list is edited, and the fix is always the same command.
func TestTheCommittedNoticesAreCurrent(test *testing.T) {
	test.Parallel()

	files, generateError := Generate(moduleRoot(test))
	if generateError != nil {
		test.Fatal(generateError)
	}
	for name, generated := range files {
		if committed(test, name) != string(generated) {
			test.Errorf(
				"%s differs from what the generator writes now. Run `go run ./internal/release/tools/notices` from the module root and commit the result",
				name,
			)
		}
		// The repository stores text as LF, so a CR written here is converted on commit, and the next
		// clone fails the comparison above on bytes nobody changed. Microsoft's NOTICE.txt is CRLF.
		if bytes.ContainsRune(generated, '\r') {
			test.Errorf("%s is generated with a carriage return, which git would convert to LF on commit", name)
		}
	}
}

// TestEveryGoModRequirementIsCredited holds go.mod against the committed file directly, apart from the
// generator, so a generator that dropped a module would still be caught.
func TestEveryGoModRequirementIsCredited(test *testing.T) {
	test.Parallel()

	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = moduleRoot(test)
	output, outputError := command.Output()
	if outputError != nil {
		test.Fatal(outputError)
	}
	var goMod struct{ Require []struct{ Path string } }
	if decodeError := json.Unmarshal(output, &goMod); decodeError != nil {
		test.Fatal(decodeError)
	}
	notices := committed(test, ThirdPartyNoticesFileName)
	credited := 0
	for _, requirement := range goMod.Require {
		if strings.HasPrefix(requirement.Path, compilerModulePrefix) || isCohere(requirement.Path) {
			continue
		}
		if !strings.Contains(notices, "\n### "+requirement.Path+"\n") {
			test.Errorf("go.mod requires %s, and %s does not credit it", requirement.Path, ThirdPartyNoticesFileName)
		}
		credited++
	}
	if credited == 0 {
		test.Fatalf("go.mod required no third-party module, so this test checked nothing")
	}
}

func TestEverySwiftPackageIsCredited(test *testing.T) {
	test.Parallel()

	pins, readSwiftPinsError := readSwiftPins(moduleRoot(test))
	if readSwiftPinsError != nil {
		test.Fatal(readSwiftPinsError)
	}
	if len(pins) == 0 {
		test.Fatalf("swift/Package.resolved pins nothing, so this test checked nothing")
	}
	notices := committed(test, ThirdPartyNoticesFileName)
	for _, pin := range pins {
		if !strings.Contains(notices, "\n### "+pin.Identity+"\n") ||
			!strings.Contains(notices, "- Version: "+pin.Version+"\n") {
			test.Errorf(
				"the Swift engine resolves %s %s, and %s does not credit that version",
				pin.Identity,
				pin.Version,
				ThirdPartyNoticesFileName,
			)
		}
	}
}

func TestTheGeneratorRefusesAModuleWithNoRecordedLicense(test *testing.T) {
	test.Parallel()

	var builder strings.Builder
	writeModuleError := writeModule(
		&builder,
		Module{Path: "example.com/uncredited", Version: "v1.0.0", Directory: test.TempDir()},
		"carries",
	)
	if writeModuleError == nil || !strings.Contains(writeModuleError.Error(), "example.com/uncredited") {
		test.Fatalf("a module with no recorded license was written rather than refused: %v", writeModuleError)
	}

	// Recorded but with no license file on disk: refused too, rather than credited with nothing.
	module := Module{Path: "golang.org/x/sync", Version: "v0.0.0", Directory: test.TempDir()}
	if writeModuleError := writeModule(&builder, module, "carries"); writeModuleError == nil {
		test.Fatalf("a module whose license file is missing was credited")
	}
}

// TestAModulesOwnThirdPartyCreditFollowsItsLicense writes a fixture module that ships a credit for code it
// includes, under each spelling the module cache holds, and requires the credit verbatim after the module's
// license; and a module that ships none gets no credit line.
func TestAModulesOwnThirdPartyCreditFollowsItsLicense(test *testing.T) {
	test.Parallel()

	cases := []struct {
		directory string
		fileName  string
	}{
		{directory: "longspelling", fileName: "LICENSE-THIRD-PARTY"},
		{directory: "shortspelling", fileName: "THIRD-PARTY"},
	}
	for _, testCase := range cases {
		directory := filepath.Join("testdata", testCase.directory)
		license, licenseError := os.ReadFile(filepath.Join(directory, "LICENSE"))
		if licenseError != nil {
			test.Fatal(licenseError)
		}
		credit, creditError := os.ReadFile(filepath.Join(directory, testCase.fileName))
		if creditError != nil {
			test.Fatal(creditError)
		}

		// golang.org/x/sync, because writeModule writes only a module whose license is recorded.
		var builder strings.Builder
		module := Module{Path: "golang.org/x/sync", Version: "v0.0.0", Directory: directory}
		if writeError := writeModule(&builder, module, "carries"); writeError != nil {
			test.Fatalf("%s: %v", testCase.directory, writeError)
		}
		written := builder.String()
		licenseBlock := "````text\n" + strings.TrimRight(string(license), "\n") + "\n````\n"
		creditBlock := "The module's own credit for code it includes, from its " + testCase.fileName + ":\n\n" +
			"````text\n" + strings.TrimRight(string(credit), "\n") + "\n````\n"
		licenseIndex := strings.Index(written, licenseBlock)
		creditIndex := strings.Index(written, creditBlock)
		if licenseIndex < 0 || creditIndex < 0 || creditIndex < licenseIndex {
			test.Errorf("%s: expected its license and then its %s verbatim, and it wrote:\n%s",
				testCase.directory, testCase.fileName, written)
		}
	}

	bare := test.TempDir()
	writeError := os.WriteFile(filepath.Join(bare, "LICENSE"), []byte("A license and nothing more.\n"), 0o644)
	if writeError != nil {
		test.Fatal(writeError)
	}
	var builder strings.Builder
	bareModule := Module{Path: "golang.org/x/sync", Version: "v0.0.0", Directory: bare}
	if writeError := writeModule(&builder, bareModule, "carries"); writeError != nil {
		test.Fatal(writeError)
	}
	if strings.Contains(builder.String(), "own credit for code it includes") {
		test.Errorf("a module that ships no third-party credit was given one:\n%s", builder.String())
	}
}

// TestEveryLicenseTextIsUsedAndEveryReferenceExists keeps licenses/ and the lists naming it in step: a
// text nothing names is a credit nobody reads, and a name with no text stops the generator.
func TestEveryLicenseTextIsUsedAndEveryReferenceExists(test *testing.T) {
	test.Parallel()

	named := map[string]bool{"go.txt": true}
	for _, upstream := range Upstreams {
		if (upstream.LicenseFile == "") == (upstream.Note == "") {
			test.Errorf("%s needs exactly one of a license file and a note saying why there is none", upstream.Name)
		}
		if upstream.LicenseFile != "" {
			named[upstream.LicenseFile] = true
		}
	}
	for _, license := range swiftLicenses {
		named[license.File] = true
	}
	present := map[string]bool{}
	walkDirectoryError := fs.WalkDir(licenseTexts, "licenses", func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		present[filepath.Base(path)] = true
		contents, readError := licenseTexts.ReadFile(path)
		if readError == nil && len(bytes.TrimSpace(contents)) == 0 {
			test.Errorf("%s is empty", path)
		}
		return readError
	})
	if walkDirectoryError != nil {
		test.Fatal(walkDirectoryError)
	}
	for name := range named {
		if !present[name] {
			test.Errorf("licenses/%s is named but not there", name)
		}
	}
	for name := range present {
		if !named[name] {
			test.Errorf("licenses/%s is there but nothing names it", name)
		}
	}
}

// TestNoGPLFileIsTracked holds the copyleft check the license survey came back clean on. git's own test
// corpus, which is GPL, is read at test time from COHERE_GIT_SOURCE and must never be committed, and
// nothing else GPL may be either. A tracked file holding a GNU license's title fails here by name.
func TestNoGPLFileIsTracked(test *testing.T) {
	test.Parallel()

	// Built from pieces, so this file doesn't match its own search. Fixed strings in the two casings a
	// license title and a license header use, since a case-insensitive pattern search over every tracked
	// file took 6.9 seconds and these take a fifth of one.
	var titles []string
	for _, kind := range []string{"GENERAL", "LESSER GENERAL", "AFFERO GENERAL"} {
		title := "GNU " + kind + " PUBLIC LICENSE"
		titles = append(titles, title, "GNU "+cases(kind)+" Public License")
	}
	arguments := []string{"grep", "-l", "-I", "-F"}
	for _, title := range titles {
		arguments = append(arguments, "-e", title)
	}
	command := exec.Command("git", arguments...)
	command.Dir = moduleRoot(test)
	output, outputError := command.Output()
	var exitError *exec.ExitError
	if outputError != nil && !(errors.As(outputError, &exitError) && exitError.ExitCode() == 1) {
		test.Fatalf("git grep could not search the tracked files: %v", outputError)
	}
	for _, path := range strings.Fields(string(output)) {
		// Microsoft's notice mentions the LGPL in a sentence and carries no GPL code.
		if path == NoticeFileName {
			continue
		}
		test.Errorf("%s is tracked and carries a GNU license", path)
	}

	// And the search can find one: a GPL title in a file git tracks is reported.
	probe := test.TempDir()
	for _, step := range [][]string{{"init", "--quiet"}, {"add", "COPYING"}} {
		if step[0] == "add" {
			if writeError := os.WriteFile(
				filepath.Join(probe, "COPYING"),
				[]byte("                    "+titles[0]+"\n"),
				0o644,
			); writeError != nil {
				test.Fatal(writeError)
			}
		}
		if output, outputError := exec.Command(
			"git",
			append([]string{"-C", probe}, step...)...,
		).CombinedOutput(); outputError != nil {
			test.Fatalf("git %v: %v\n%s", step, outputError, output)
		}
	}
	probeCommand := exec.Command("git", arguments...)
	probeCommand.Dir = probe
	found, probeError := probeCommand.Output()
	if probeError != nil {
		test.Fatalf("git grep could not search the probe repository: %v", probeError)
	}
	if strings.TrimSpace(string(found)) != "COPYING" {
		test.Fatalf(
			"the search found %q in a repository tracking a GPL file, so it can't be trusted to find one",
			found,
		)
	}
}

// cases capitalizes each word of an all-caps phrase: "LESSER GENERAL" becomes "Lesser General".
func cases(phrase string) string {
	words := strings.Fields(strings.ToLower(phrase))
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
