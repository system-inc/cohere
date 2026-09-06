package unused_exports

import "testing"

// TestRootSetSparesFrameworkFiles is the half that proves the root set declines. Every path here is
// alive by convention with no import statement anywhere in the tree, and reporting one would be the
// failure that gets this report closed and never reopened.
func TestRootSetSparesFrameworkFiles(t *testing.T) {
	t.Parallel()
	roots := &RootSet{dynamicImportDirectories: []string{"/translations/"}}

	for _, testCase := range []struct {
		name string
		path string
		want rootReason
	}{
		{"app router page", "/repo/app/os/page.tsx", rootNextAppRoute},
		{"app router route handler", "/repo/app/api/tasks/route.ts", rootNextAppRoute},
		{"app router layout", "/repo/app/layout.tsx", rootNextAppRoute},
		{"app router error", "/repo/app/error.tsx", rootNextAppRoute},
		{"app router not-found", "/repo/app/not-found.tsx", rootNextAppRoute},
		{"nested project app router", "/repo/projects/www-ahra-ai/app/page.tsx", rootNextAppRoute},
		{"pages directory route", "/repo/pages/about.tsx", rootNextPagesRoute},
		{"next config", "/repo/next.config.ts", rootBuildConfig},
		{"open next config", "/repo/open-next.config.ts", rootBuildConfig},
		{"instrumentation", "/repo/instrumentation.ts", rootBuildConfig},
		{"middleware", "/repo/middleware.ts", rootBuildConfig},
		{"project settings", "/repo/ProjectSettings.tsx", rootBuildConfig},
		{"tailwind configuration", "/repo/TailwindConfiguration.ts", rootBuildConfig},
		{"declaration file", "/repo/next-env.d.ts", rootDeclaration},
		{"global declarations", "/repo/libraries/structure/GlobalDeclarations.d.ts", rootDeclaration},
		{"test file", "/repo/modules/Thing.test.ts", rootTest},
		{"spec file", "/repo/modules/Thing.spec.tsx", rootTest},
		{"dynamically imported locale", "/repo/libraries/structure/source/localization/translations/de.ts", rootDynamicImportTarget},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			isRoot, reason := roots.IsRoot(testCase.path)
			if !isRoot {
				t.Fatalf("%s was not spared, so a live file would be reported as unused", testCase.path)
			}
			if reason != testCase.want {
				t.Fatalf("spared for %q, want %q", reason, testCase.want)
			}
		})
	}
}

// TestRootSetDoesNotSpareOrdinaryFiles is the half that proves the root set can still report. A root
// set that spares everything is indistinguishable from a broken analysis, and it produces exactly
// the clean-looking empty report this project exists to make impossible.
func TestRootSetDoesNotSpareOrdinaryFiles(t *testing.T) {
	t.Parallel()
	roots := &RootSet{dynamicImportDirectories: []string{"/translations/"}}

	for _, path := range []string{
		"/repo/libraries/structure/source/utilities/Strings.ts",
		"/repo/app/(os-layout)/_components/profile/OsProfilePage.tsx",
		"/repo/modules/tasks/TasksCommandLineInterface.ts",
		// A private component parked under pages/ is not a route, so it stays reportable.
		"/repo/pages/_components/Widget.tsx",
		// Named like a route but not in a route directory.
		"/repo/modules/errors/error.ts",
		// A near-miss on the marker word, which must not be read as a convention.
		"/repo/modules/apple/Pages.ts",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if isRoot, reason := roots.IsRoot(path); isRoot {
				t.Fatalf("%s was spared as %q, so a genuine finding would be hidden", path, reason)
			}
		})
	}
}

// TestIntentionalMarker proves the marker is read where written and not read where it is merely
// resembled, so a word like `cohere-keeper` cannot silently suppress a finding.
func TestIntentionalMarker(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		text       string
		wantMarked bool
		wantReason string
	}{
		{"bare marker", "// cohere-keep\nexport function f() {}", true, ""},
		{"marker with reason", "// cohere-keep: public API for the SDK\nexport function f() {}", true, "public API for the SDK"},
		{"marker with reason no colon", "// cohere-keep held for an external consumer\nexport function f() {}", true, "held for an external consumer"},
		{"block comment", "/* cohere-keep: parked deliberately */\nexport const x = 1;", true, "parked deliberately"},
		{"no marker", "// an ordinary comment\nexport function f() {}", false, ""},
		{"marker is a prefix of another word", "// cohere-keeper is not a directive\nexport function f() {}", false, ""},
		// Both edges of the word are guarded. `unverify-keeping` embeds the marker but is not one,
		// and reading it as a directive would suppress a finding nobody meant to suppress.
		{"marker inside a longer word", "// unverify-keeping\nexport function f() {}", false, ""},
		// The marker only counts in a comment. A string or identifier that happens to contain it is
		// source, not a directive.
		{"marker in code rather than a comment", "export const label = 'cohere-keep';", false, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := findIntentionalMarker(testCase.text)
			if got.Present != testCase.wantMarked {
				t.Fatalf("marked = %v, want %v", got.Present, testCase.wantMarked)
			}
			if got.Present && got.Text != testCase.wantReason {
				t.Fatalf("reason = %q, want %q", got.Text, testCase.wantReason)
			}
		})
	}
}
