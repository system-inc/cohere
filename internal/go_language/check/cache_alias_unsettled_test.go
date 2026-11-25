package check

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// The alias fixture's paths: relay is restored from the host's run and declined at check, top imports relay and
// middle, and middle is a miss the first wave saves.
const (
	aliasRelayPath = "example.com/alias/relay"
	aliasTopFile   = "top.go"
)

// aliasRelayDirect is relay with Say forwarding to fmt.Sprintf, a printf wrapper in every build.
const aliasRelayDirect = "// Package relay says.\npackage relay\n\nimport (\n\t\"fmt\"\n\n" +
	"\t\"example.com/alias/platform\"\n)\n\n// Say formats.\nfunc Say(format string, arguments ...any) string {\n" +
	"\t_ = platform.Logf\n\treturn fmt.Sprintf(format, arguments...)\n}\n"

// aliasRelayForwarding is relay with Say forwarding to platform.Logf, a printf wrapper where Logf is one: on the host,
// and not on windows.
const aliasRelayForwarding = "// Package relay says.\npackage relay\n\nimport (\n\t\"fmt\"\n\n" +
	"\t\"example.com/alias/platform\"\n)\n\n// Say formats.\nfunc Say(format string, arguments ...any) string {\n" +
	"\t_ = fmt.Sprintf\n\treturn platform.Logf(format, arguments...)\n}\n"

// aliasFiles are a module whose platform declares Logf alike in every build, a printf wrapper on the host and not on
// windows; relay's Say, a printf wrapper in either version, forwarding to fmt.Sprintf (aliasRelayDirect) or to
// platform.Logf (aliasRelayForwarding); middle and top, each with a file of windows's own, so neither is ever restored
// from the host; and top, which hands Say a format its argument does not fit.
var aliasFiles = map[string]string{
	"go.mod": "module example.com/alias\n\ngo 1.24\n",
	"platform/platform.go": "// Package platform is declared alike in every build and each build's own way.\n" +
		"package platform\n",
	"platform/platform_other.go": "//go:build !windows\n\npackage platform\n\nimport \"fmt\"\n\n" +
		"// Logf formats as fmt.Sprintf does.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn fmt.Sprintf(format, arguments...)\n}\n",
	"platform/platform_windows.go": "package platform\n\n" +
		"// Logf is its format, its arguments left alone.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn format + string(rune(len(arguments)))\n}\n",
	"relay/relay.go": aliasRelayDirect,
	"middle/middle.go": "// Package middle is a miss the first wave saves.\npackage middle\n\n" +
		"// Value is a word.\nfunc Value() string {\n\treturn \"one\"\n}\n",
	"middle/middle_windows.go": "package middle\n\n// windows is windows's own.\nconst windows = true\n",
	"top/top.go": "// Package top says through relay.\npackage top\n\nimport (\n\t\"example.com/alias/middle\"\n" +
		"\t\"example.com/alias/relay\"\n)\n\n// Use hands Say a format its argument does not fit.\n" +
		"func Use() string {\n\treturn relay.Say(\"%d\", \"text\") + middle.Value()\n}\n",
	"top/top_windows.go": "package top\n\n// windows is windows's own.\nconst windows = true\n",
}

// aliasRules are printf's port alone, whose wrapper fact is relay's whole output and top's finding: no rule reads an
// answer, so an entry of top holds no read a later alias hit could fail on.
func aliasRules() []rule.Rule {
	return throughRules()[:1]
}

// saysOnTop reports whether findings hold printf's finding on top's call of Say.
func saysOnTop(findings []Finding) bool {
	return slices.ContainsFunc(findings, func(finding Finding) bool {
		return strings.HasSuffix(finding.File, aliasTopFile) && strings.Contains(finding.Message, "relay.Say")
	})
}

// A package restored by its second key over a restore from the host that is not settled gets no alias of its first
// key (#k36hp1a). windows's first run, restoring nothing from the host, saves top over relay's Say as a wrapper, with
// its second key aliased. Then relay forwards to platform.Logf, a wrapper on the host and not on windows, and middle's
// body moves. windows restores relay from the host, whose output, Say a wrapper, is the first run's, so top's second
// key is the first run's, and the first wave restores top by it over relay's restore, not settled yet; then check
// declines relay, whose read of Logf's wrapper fact differs, and the reload checks relay from source, Say no wrapper.
// An alias of top's first key written in the first wave has the reload hit top's old entry by it, which holds no read
// to fail, and say printf's finding on Say, which a run restoring nothing from the host does not.
func TestASecondKeyRestoreOverAnUnsettledHostRestoreWritesNoAlias(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, aliasFiles)
	stores := test.TempDir()
	buildStore := filepath.Join(stores, "build")
	rules := aliasRules()
	first, _ := buildOf(test, root, buildStore, nil, rules)
	if !saysOnTop(first.Findings) {
		test.Fatalf("windows's first run found %+v, want printf's finding on top's call of Say", first.Findings)
	}

	editFile(test, root, "relay/relay.go", replaced(test, aliasRelayDirect, aliasRelayForwarding))
	editFile(test, root, "middle/middle.go", replaced(test, "\"one\"", "\"two\""))
	host, _ := hostSessionOf(test, root, filepath.Join(stores, "host"), rules)
	second, _ := buildOf(test, root, buildStore, host, rules)
	if why := second.FromHost.Declined[aliasRelayPath]; !strings.HasPrefix(why, "F ") {
		test.Fatalf("windows declined relay for %q, want its read of Logf's wrapper fact", why)
	}
	reference, _ := buildOf(test, root, filepath.Join(stores, "reference"), nil, rules)
	if saysOnTop(reference.Findings) {
		test.Fatalf("a run restoring nothing from the host found %+v, want no finding on Say", reference.Findings)
	}
	if !reflect.DeepEqual(second.Findings, reference.Findings) {
		test.Errorf("windows found\n%+v\nand a run restoring nothing from the host\n%+v", second.Findings,
			reference.Findings)
	}
}
