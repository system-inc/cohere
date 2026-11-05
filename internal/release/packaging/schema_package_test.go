package release

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestDispatcherPackageShipsTheSettingsSchemas builds the dispatcher package from this module and
// requires every settings schema in it, byte for byte the committed one, with schema/ in the files npm
// publishes. A schema left out of `files` would be staged here and dropped by `npm publish`.
func TestDispatcherPackageShipsTheSettingsSchemas(test *testing.T) {
	test.Parallel()

	module, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	staged, buildDispatcherPackageError := buildDispatcherPackage(
		Options{ModuleDirectory: module, OutputDirectory: test.TempDir(), Version: "1.0.0"},
		placeholderChecksums,
	)
	if buildDispatcherPackageError != nil {
		test.Fatal(buildDispatcherPackageError)
	}

	for _, name := range SettingsSchemaFileNames {
		committed, readError := os.ReadFile(filepath.Join(module, SchemaDirectoryName, name))
		if readError != nil {
			test.Fatal(readError)
		}
		shipped, readError := os.ReadFile(filepath.Join(staged.Directory, SchemaDirectoryName, name))
		if readError != nil {
			test.Fatalf("the dispatcher package does not ship %s: %v", name, readError)
		}
		if !bytes.Equal(shipped, committed) {
			test.Fatalf("the dispatcher ships a %s that differs from the committed one", name)
		}
	}

	manifestBytes, readError := os.ReadFile(filepath.Join(staged.Directory, "package.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var manifest struct {
		Files []string `json:"files"`
	}
	if decodeError := json.Unmarshal(manifestBytes, &manifest); decodeError != nil {
		test.Fatal(decodeError)
	}
	if !slices.Contains(manifest.Files, SchemaDirectoryName+"/") {
		test.Fatalf("the dispatcher's files are %v, so npm publish would leave the schemas out", manifest.Files)
	}
}

// TestDispatcherPackageRefusesASchemaThatValidatesNothing holds each way the copy could ship a $schema
// target that checks nothing: the file absent, empty, or not JSON.
func TestDispatcherPackageRefusesASchemaThatValidatesNothing(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name     string
		contents *string
		says     string
	}{
		{"absent", nil, "reading the settings schema"},
		{"an empty object", new("{}"), "is not a JSON schema"},
		{"not JSON", new("{\"type\":"), "is not a JSON schema"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			module := test.TempDir()
			for _, name := range SettingsSchemaFileNames {
				writeFile(test, filepath.Join(module, SchemaDirectoryName, name), `{"type":"object"}`)
			}
			broken := filepath.Join(
				module,
				SchemaDirectoryName,
				SettingsSchemaFileNames[len(SettingsSchemaFileNames)-1],
			)
			if testCase.contents == nil {
				if removeError := os.Remove(broken); removeError != nil {
					test.Fatal(removeError)
				}
			} else {
				writeFile(test, broken, *testCase.contents)
			}

			_, buildDispatcherPackageError := buildDispatcherPackage(
				Options{ModuleDirectory: module, OutputDirectory: test.TempDir(), Version: "1.0.0"},
				placeholderChecksums,
			)
			if buildDispatcherPackageError == nil {
				test.Fatal("the dispatcher was staged with a schema that validates nothing")
			}
			if !strings.Contains(buildDispatcherPackageError.Error(), testCase.says) {
				test.Fatalf("refused, but without saying %q: %v", testCase.says, buildDispatcherPackageError)
			}
		})
	}
}
