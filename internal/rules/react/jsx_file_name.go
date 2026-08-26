package react

import "strings"

// isJsxFileName reports whether a file name is one the parser reads as JSX.
//
// This stands in for oxc's `source_type().is_jsx()`, which `rule.Context` does not expose. The
// suffix is what decides the script kind in the harness and in a real run alike, so asking it here
// gives the same answer by the same route rather than by a parallel one.
//
// # Why this lives in a file of its own
//
// It was written inside `no_string_refs.go`, back when that rule was ported from oxc and carried
// oxc's file gate. Three other rules in this package then reached for it. When `no-string-refs` was
// re-ported against `eslint-plugin-react`, which has no file gate at all, its only caller in that
// file went away and the helper would have gone with it, breaking `no-did-mount-set-state`,
// `no-direct-mutation-state` and `no-this-in-sfc`. Moved here rather than into any one of their
// files, so it belongs to none of them and the next rule to drop its gate cannot take it away
// again.
//
// Whether each remaining caller SHOULD gate on the file name is a question for that rule's own
// port against its own authority, not one this move decides. `no-string-refs` measured its gate as
// oxc's alone; the other three are untouched here.
func isJsxFileName(fileName string) bool {
	normalizedPath := strings.ReplaceAll(fileName, `\`, "/")
	return strings.HasSuffix(normalizedPath, ".tsx") || strings.HasSuffix(normalizedPath, ".jsx")
}
