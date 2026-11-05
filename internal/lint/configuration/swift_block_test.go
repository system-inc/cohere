package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The `swift` block is the Swift engine's (RuleConfiguration.swift): one settings file configures every
// engine, so this loader accepts the block, leaves its insides to that engine, and refuses a block no Swift
// run reads, naming the file.

const swiftBlock = `"swift": {"rules": {"cohere-swift/file-length": ["error", {"max": 1}]}}`

// A file holding a swift block loads, through Load and through IgnorePatternsOf, the one read discovery
// makes of a Swift-only package's settings. Another key the loader does not know is still refused beside it.
func TestASwiftBlockLoadsAndAnUnknownKeyBesideItIsStillRefused(t *testing.T) {
	t.Parallel()
	alone := writeConfigs(t, map[string]string{"CohereSettings.json": `{` + swiftBlock + `}`})
	loadOrFail(t, filepath.Join(alone, "CohereSettings.json"))
	if _, err := IgnorePatternsOf(filepath.Join(alone, "CohereSettings.json")); err != nil {
		t.Errorf("discovery's read refused a swift block: %v", err)
	}

	withRules := writeConfigs(t, map[string]string{"CohereSettings.json": `{"rules": {"no-debugger": "error"}, ` + swiftBlock + `}`})
	loadOrFail(t, filepath.Join(withRules, "CohereSettings.json"))

	beside := writeConfigs(t, map[string]string{"CohereSettings.json": `{` + swiftBlock + `, "swiftRules": {}}`})
	refusedWith(t, filepath.Join(beside, "CohereSettings.json"), `"swiftRules", which this loader does not implement`)
}

// The Swift engine reads one file and never follows extends, so a swift block in a base configures nothing
// and is refused naming the base and the file extending it, on discovery's read as on Load's.
func TestASwiftBlockInAnExtendedBaseIsRefusedNamingIt(t *testing.T) {
	t.Parallel()
	directory := writeConfigs(t, map[string]string{
		"Base.json":           `{"rules": {}, ` + swiftBlock + `}`,
		"CohereSettings.json": `{"extends": "./Base.json"}`,
	})
	base := filepath.Join(directory, "Base.json")
	own := filepath.Join(directory, "CohereSettings.json")
	want := `lint config ` + base + ` declares "swift", and it is extended by ` + own
	refusedWith(t, own, want)
	if _, err := IgnorePatternsOf(own); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("discovery's read did not refuse a swift block in a base, naming it: %v", err)
	}
	// Control: the base alone, read as the project's own file, loads.
	loadOrFail(t, base)
}

// A set is never the file beside a Swift project, so a swift block in one is refused, whether the set is
// extended or read directly.
// Not parallel: it swaps the package's setFiles through withSetFiles, which every test reading a set sees.
func TestASwiftBlockInASetIsRefused(t *testing.T) {
	withSetFiles(t, map[string]string{"swifty": `{"rules": {}, ` + swiftBlock + `}`})
	if _, err := Load(SetPrefix + "swifty"); err == nil || !strings.Contains(err.Error(), `rule set cohere:swifty declares "swift"`) {
		t.Errorf("a set holding a swift block, read directly, was not refused naming it: %v", err)
	}
	directory := writeConfigs(t, map[string]string{"CohereSettings.json": `{"extends": "cohere:swifty"}`})
	refusedWith(t, filepath.Join(directory, "CohereSettings.json"), `rule set cohere:swifty declares "swift"`)
}

// A swift block is read only beside a Swift project, by the engine's own rule: a regular Package.swift, a
// regular project.yml, or exactly one .xcodeproj. Anywhere else it is refused naming the file and its
// directory, and a file with no swift block, or one that does not parse, is not this check's to refuse.
func TestASwiftBlockBesideNoSwiftProjectIsRefused(t *testing.T) {
	t.Parallel()
	settings := `{` + swiftBlock + `}`
	for _, test := range []struct {
		name    string
		files   map[string]string
		bundles []string
		read    bool
	}{
		{name: "beside a Package.swift", files: map[string]string{"Package.swift": "// swift-tools-version:6.0\n"}, read: true},
		{name: "beside a project.yml", files: map[string]string{"project.yml": "name: App\n"}, read: true},
		{name: "beside one .xcodeproj", bundles: []string{"App.xcodeproj"}, read: true},
		{name: "beside two .xcodeproj", bundles: []string{"App.xcodeproj", "Other.xcodeproj"}},
		{name: "beside only a tsconfig", files: map[string]string{"tsconfig.json": "{}"}},
		{name: "beside a directory named Package.swift", bundles: []string{"Package.swift"}},
		{name: "with a Package.swift only below it", files: map[string]string{"apple/Toy/Package.swift": "// swift-tools-version:6.0\n"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"CohereSettings.json": settings}
			for name, contents := range test.files {
				files[name] = contents
			}
			directory := writeConfigs(t, files)
			for _, bundle := range test.bundles {
				if err := os.Mkdir(filepath.Join(directory, bundle), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(directory, "CohereSettings.json")
			err := CheckSwiftBlockHasAReader(path)
			if test.read {
				if err != nil {
					t.Fatalf("a swift block a Swift run reads was refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), `lint config `+path+` declares "swift", and `+directory+` holds no Swift project`) {
				t.Fatalf("a swift block no Swift run reads was not refused naming the file: %v", err)
			}
		})
	}

	t.Run("no swift block, or a file that does not parse", func(t *testing.T) {
		t.Parallel()
		directory := writeConfigs(t, map[string]string{
			"CohereSettings.json": `{"rules": {}}`,
			"Broken.json":         `{"swift": `,
		})
		for _, name := range []string{"CohereSettings.json", "Broken.json"} {
			if err := CheckSwiftBlockHasAReader(filepath.Join(directory, name)); err != nil {
				t.Errorf("%s was refused, and it has no swift block to read: %v", name, err)
			}
		}
	})
}
