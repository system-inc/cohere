package structure

import (
	"strings"

	"github.com/system-inc/verify/internal/rule"
)

var messageHookShouldEndWithRequest = rule.Message{
	Id: "hookShouldEndWithRequest",
	Description: "This hook calls NetworkService but its name does not end in Request. The suffix is " +
		"what makes a network call visible at the import site: a caller reading `useUser()` cannot " +
		"tell it reaches the server, and so cannot know it needs loading and error handling until " +
		"the hook is already wired in. Rename it to end in Request.",
}

var messageFileShouldEndWithRequest = rule.Message{
	Id: "fileShouldEndWithRequest",
	Description: "This file declares a hook that calls NetworkService, but the file name does not " +
		"end in Request. The name is the only signal a grep or a file tree gives, so a network " +
		"module that does not carry the suffix is invisible to anyone auditing what talks to the " +
		"server. Rename the file to end in Request.",
}

// NetworkRequireHookRequestSuffix flags a NetworkService hook, or its file, not named `*Request`.
//
//	valid:   // UserRequest.ts       export function useUserRequest() { networkService.useGraphQlQuery(...) }
//	invalid: // UserRequest.ts       export function useUser() { networkService.useGraphQlQuery(...) }
//	invalid: // User.ts             export function useUserRequest() { networkService.useGraphQlQuery(...) }
//
// Two message ids because they are two different repairs. A hook name is fixed by a rename in this
// file; a file name is fixed by moving the file and updating every importer, and a reader who sees
// one message for both has no way to tell which is being asked of them.
//
// The file finding reports on the first hook's identifier rather than on the file, which is the
// original's choice and worth keeping: a finding needs a range, and a whole-file range underlines
// everything and points at nothing. The first hook is where a reader will start reading anyway.
//
// The suggestion strips a trailing Query, Mutation, or Hook before appending Request, so
// `useUserQuery` is suggested as `useUserRequest` rather than `useUserQueryRequest`. Only one suffix
// is stripped, matching the original: chaining the strips would rewrite `useQueryHook` past what
// anyone asked for.
var NetworkRequireHookRequestSuffix = rule.Rule{
	Name: "network-require-hook-request-suffix",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		analysis := NetworkFileAnalysisFor(ctx)
		if len(analysis.HookDeclarations) == 0 {
			// Declining the file rather than registering a listener that would find nothing. A file
			// with no NetworkService hooks is the overwhelming majority of files.
			return nil
		}

		for _, hook := range analysis.HookDeclarations {
			if !strings.HasSuffix(hook.Name, "Request") {
				ctx.ReportNode(hook.NameNode, messageHookShouldEndWithRequest)
			}
		}

		// The file check reports at most once, against the first hook, even when several hooks are
		// declared. The defect is the file's name, and reporting it once per hook would say the same
		// thing three times about one rename.
		if fileNeedsRequestSuffix(ctx.SourceFile.FileName()) {
			ctx.ReportNode(analysis.HookDeclarations[0].NameNode, messageFileShouldEndWithRequest)
		}

		return nil
	},
}

// fileNeedsRequestSuffix reports whether a TypeScript file's base name lacks the Request suffix.
//
// Non-TypeScript files are exempt rather than flagged. The original tests the extension explicitly,
// and the reason holds: this rule is about how a module announces itself to importers, and something
// that is not a module here is not making that announcement.
//
// Both separators are folded before taking the base name, the same normalization FileContextFor
// does and for the same reason: every test below is a suffix test, and a backslash-separated path
// would take the whole path as its base name and silently pass.
func fileNeedsRequestSuffix(fileName string) bool {
	normalizedPath := strings.ReplaceAll(fileName, `\`, "/")
	base := normalizedPath
	if index := strings.LastIndex(normalizedPath, "/"); index >= 0 {
		base = normalizedPath[index+1:]
	}

	for _, extension := range []string{".tsx", ".ts"} {
		if strings.HasSuffix(base, extension) {
			return !strings.HasSuffix(strings.TrimSuffix(base, extension), "Request")
		}
	}
	return false
}
