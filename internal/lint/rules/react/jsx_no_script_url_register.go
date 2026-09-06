package react

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers this package's `jsx-no-script-url` rule.
//
// A file of its own rather than a line in `register.go`, so a port is three owned files and no
// shared edit.
//
// The decoder is hand-rolled rather than `rule.DecodeOptionsInto`, because upstream's option
// surface is POSITIONAL across two slots whose shapes differ, and cohere's config layer hands a
// decoder one value rather than a list. The accepted body wraps upstream's own list under a
// `positional` key, which is the only spelling that can carry both slots at once. It also has to
// answer an empty body with the built-in `a` to `href` pair rather than an error, since an
// unconfigured rule still checks anchors.
func init() {
	rule.Register(rule.Registration{
		Rule:   JsxNoScriptUrl,
		Decode: DecodeJsxNoScriptUrlOptions,
	})
}
