package nextjs

import "testing"

// The cases where upstream's four spellings disagree are the ones worth pinning, because they are
// the reason this package exists. Each row records which upstream spellings would have said what,
// so a reader can see the drift this replaced rather than take the collapse on faith.
func TestIsDocumentFile(t *testing.T) {
	cases := []struct {
		name     string
		filePath string
		want     bool
	}{
		// Every upstream spelling agrees on these two.
		{"the ordinary document", "pages/_document.tsx", true},
		{"under a source directory", "src/pages/_document.tsx", true},

		// Only the widest spelling accepts the directory form. It names the same file.
		{"the directory form", "pages/_document/index.tsx", true},
		{"the directory form with a source directory", "src/pages/_document/index.jsx", true},

		// The basename spellings accept this and `is_document_page` does not, because it requires a
		// `pages` segment. Dropped deliberately: three of the eight rules do not require it, and
		// requiring it here would make this narrower than the rules it serves.
		{"outside a pages directory", "components/_document.tsx", true},

		// The trailing dot, which is the one piece of upstream precision worth keeping. Without it
		// this reads as the document, and that is a real ESLint false positive oxc fixed.
		{"a longer name sharing the prefix", "pages/_documentation.tsx", false},
		// Upstream's directory case uses a bare `starts_with("_document")` with no trailing dot,
		// unlike its basename case. So this DOES match, and it is recorded as upstream's behavior
		// rather than corrected: a fixture asserting the tidier answer was written first here and
		// was wrong, which is the trap of trusting what a rule should do over what it does.
		{"a longer directory name still matches, as upstream", "pages/_documentation/index.tsx", true},

		{"an ordinary page", "pages/index.tsx", false},
		{"an index outside a document directory", "pages/settings/index.tsx", false},
		{"a component", "components/Thing.tsx", false},
		{"an empty path", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsDocumentFile(testCase.filePath); got != testCase.want {
				t.Fatalf("%q: want %v, got %v", testCase.filePath, testCase.want, got)
			}
		})
	}
}

// Every upstream predicate handles both separators, so a path in the Windows shape must answer the
// same as its POSIX twin. A port handling one would be silent on half the platforms upstream lints.
func TestIsDocumentFileHandlesBothSeparators(t *testing.T) {
	pairs := [][2]string{
		{"pages/_document.tsx", `pages\_document.tsx`},
		{"src/pages/_document.tsx", `src\pages\_document.tsx`},
		{"pages/_document/index.tsx", `pages\_document\index.tsx`},
		{"pages/_documentation.tsx", `pages\_documentation.tsx`},
	}
	for _, pair := range pairs {
		posix, windows := pair[0], pair[1]
		if IsDocumentFile(posix) != IsDocumentFile(windows) {
			t.Fatalf("%q and %q disagree: %v against %v",
				posix, windows, IsDocumentFile(posix), IsDocumentFile(windows))
		}
	}
}

// A substring test rather than a segment test, matching upstream. The looseness is reproduced rather
// than tightened, and the case that shows it is asserted so nobody "fixes" it later without seeing
// what they are changing.
func TestIsInApplicationDirectory(t *testing.T) {
	cases := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"an app route", "app/page.tsx", true},
		{"nested under app", "src/app/settings/page.tsx", true},
		{"the windows shape", `src\app\settings\page.tsx`, true},
		// A near-miss that does NOT match, recorded because it is the case a reader assumes the
		// substring test catches: `application/` contains `app` but not `app/`, so the separator in
		// the needle is doing real work.
		{"a directory whose name merely starts with app", "application/thing.tsx", false},
		// The looseness that IS real: a segment ending in `app` answers true.
		{"a directory ending in app", "src/myapp/thing.tsx", true},
		{"a pages route", "pages/index.tsx", false},
		{"an empty path", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsInApplicationDirectory(testCase.filePath); got != testCase.want {
				t.Fatalf("%q: want %v, got %v", testCase.filePath, testCase.want, got)
			}
		})
	}
}

// The tree this gates measured 37 `pages/` directories in source that are not Pages Router at all,
// a PascalCase component convention plus one holding translations. None of them is a document, and
// a predicate that answered otherwise would put a false-positive surface under every rule that
// gates this way.
func TestOurOwnPagesConventionIsNotADocument(t *testing.T) {
	for _, filePath := range []string{
		"libraries/structure/source/modules/account/pages/AccountPage.tsx",
		"libraries/structure/source/modules/account/pages/index.tsx",
		"modules/finance/pages/FinanceOverviewPage.tsx",
	} {
		if IsDocumentFile(filePath) {
			t.Fatalf("%q is our own pages convention and must not read as a Next document", filePath)
		}
	}
}
