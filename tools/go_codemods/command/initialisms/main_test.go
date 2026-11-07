package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// The cases are the rules circle's analyzer fixtures: what it flags and what it suggests, and what it leaves.
func TestWordCased(test *testing.T) {
	test.Parallel()
	cases := map[string]string{
		"ID":                "Id",
		"userID":            "userId",
		"HTTPClient":        "HttpClient",
		"JSONEncoder":       "JsonEncoder",
		"HTTPServer":        "HttpServer",
		"UserID":            "UserId",
		"homeURLs":          "homeUrls",
		"ParseURL":          "ParseUrl",
		"requestID":         "requestId",
		"responseURL":       "responseUrl",
		"parseURLPath":      "parseUrlPath",
		"SHA256Sum":         "Sha256Sum",
		"nameID":            "nameId",
		"URLsToFetch":       "UrlsToFetch",
		"retryHTTP":         "retryHttp",
		"KeyID":             "KeyId",
		"serverAPI":         "serverApi",
		"ServeJSON":         "ServeJson",
		"userIDs":           "userIds",
		"MD5":               "Md5",
		"sessionTTL":        "sessionTtl",
		"EnforceForTSTypes": "EnforceForTsTypes",
		"TestParseURL":      "TestParseUrl",
		"groupID":           "groupId",
		// Left as they are.
		"IPv4":   "IPv4",
		"TKey":   "TKey",
		"AValue": "AValue",
		"Id":     "Id",
		"id":     "id",
		"X":      "X",
	}
	for name, want := range cases {
		if got := wordCased(name); got != want {
			test.Errorf("wordCased(%q) = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"MAX_RETRIES", "HTTP_STATUS_OK", "_"} {
		if flagged(name) {
			test.Errorf("%q flagged, want it left", name)
		}
	}
}

// An override chooses the name where the word-cased one is taken: parseSCSS beside upstream's parseScss reads
// scssParse, by its file and old name, and every other candidate keeps its word-cased name.
func TestNameOverrideChoosesTheName(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"internal/format/css/parser.go": `package css

func parseSCSS(text string) string { return parseScss(text, "scss") }

func parseScss(text string, parser string) string { return text + parser }

func parseCSSValue(text string) string { return parseSCSS(text) }
`,
	} {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	proposals, _, _ := findCandidates(loaded)
	got := map[string]string{}
	for _, proposal := range proposals {
		got[proposal.Object.Name()] = proposal.NewName
	}
	want := map[string]string{"parseSCSS": "scssParse", "parseCSSValue": "parseCssValue"}
	if len(got) != len(want) || got["parseSCSS"] != want["parseSCSS"] || got["parseCSSValue"] != want["parseCSSValue"] {
		test.Fatalf("proposed %v, want %v", got, want)
	}
}

// Every override's name is one the rule leaves, so a pass that applies it does not trade one finding for another.
func TestNameOverridesAreNamesTheRuleLeaves(test *testing.T) {
	test.Parallel()
	for declaration, newName := range nameOverrides {
		if flagged(newName) {
			test.Errorf("%s: %s is itself flagged; the rule suggests %s", declaration, newName, wordCased(newName))
		}
	}
}

// The FS word reads FileSystem, from a fresh tree's FS and from a word-cased tree's Fs alike; a run that only
// holds the letters, DFS or FSI, is another word.
func TestFileSystemIsSpelledOut(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]string{
		"FS":                     "FileSystem",
		"RecordingFS":            "RecordingFileSystem",
		"recordingFS":            "recordingFileSystem",
		"NewMemoryFS":            "NewMemoryFileSystem",
		"copyFSError":            "copyFileSystemError",
		"DesignSystemFS":         "DesignSystemFileSystem",
		"errMemoryFSReadOnly":    "errMemoryFileSystemReadOnly",
		"TestMemoryFSReadsBytes": "TestMemoryFileSystemReadsBytes",
		"RecordingFs":            "RecordingFileSystem",
		"timingFs":               "timingFileSystem",
		"fsPath":                 "fileSystemPath",
		// Left as they are, or only word-cased.
		"DFS":    "Dfs",
		"FSI":    "Fsi",
		"LUFS":   "Lufs",
		"fs":     "fs",
		"Fsync":  "Fsync",
		"offset": "offset",
	} {
		if got := houseName(name); got != want {
			test.Errorf("houseName(%q) = %q, want %q", name, got, want)
		}
	}
	for name, want := range map[string]bool{
		"RecordingFs": true,
		"fsPath":      true,
		"Dfs":         false,
		"Fsi":         false,
		"fs":          false,
	} {
		if got := holdsFileSystemWord(name); got != want {
			test.Errorf("holdsFileSystemWord(%q) = %v, want %v", name, got, want)
		}
	}
}
