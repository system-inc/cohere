package prettier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
)

/*
 * Why a Go linter depends on a JavaScript interpreter.
 *
 * verify replaces Prettier, and replacing Prettier means matching it byte for byte on a tree that is
 * already Prettier-formatted. Anything short of that produces a diff nobody asked for and buries
 * real changes in noise. The alternative was measured rather than assumed: typescript-go ships a
 * formatter, and its FormatCodeSettings has 21 fields and no printWidth, so it has no line-breaking
 * engine at all. It leaves a 130-character line at 130 characters. 21.8% of tracked files diverge
 * from our Prettier after every settings-reachable fix.
 *
 * So the only thing that reproduces our formatter is our formatter. goja runs it in-process, which
 * costs about 9.6 MB in the stripped binary and buys byte-identical output on every file type
 * Prettier handles, including the four our own fork customizes.
 */

// ForkPathVariable overrides where the Prettier fork is read from.
//
// It lives here rather than in the package that first defined it, and that relocation is the fix
// rather than a tidy-up. The fork was reachable by two independent paths -- this engine's own
// constant, and the release guard's environment lookup -- and neither consulted the other. The
// defect was not that either path was wrong. It was that they existed separately, so overriding one
// left the other pinned: a CI runner that set this variable got a release guard reporting green over
// an engine still reading a home directory on one laptop. Measured, not reasoned: with the variable
// pointed at a nonexistent path, `New` returned a working engine.
//
// So the engine that loads the bundles owns the name, and every other consumer resolves through it.
// `prettier` imports nothing internal, which is what makes it the safe home; `release` already
// depends on it, so the reference points down rather than sideways. Anyone adding a third consumer
// of the fork should find one place here rather than guess which of two to copy.
const ForkPathVariable = "VERIFY_PRETTIER_FORK"

// DefaultForkPath is where the fork lives on the machine this was built on.
//
// A default rather than a requirement: the common case is Kirk's laptop, and making everyone set a
// variable to reproduce the common case is friction that buys nothing. The variable exists for every
// other machine.
const DefaultForkPath = "/Users/kirkouimet/Projects/system/prettier"

// BundleDirectory returns where the fork's build output is read from.
//
// This is the seam the build step replaces. Today it resolves a local checkout; the vendoring step
// will generate the bundles into the package and embed them, the same way typescript-go generates
// and embeds its lib files rather than committing them. Keeping every path decision behind this
// function means that change is one function body rather than a rewrite.
func BundleDirectory() string {
	forkPath := strings.TrimSpace(os.Getenv(ForkPathVariable))
	if forkPath == "" {
		forkPath = DefaultForkPath
	}
	return filepath.Join(forkPath, "dist", "prettier")
}

// BundleFiles are the Prettier bundles the engine evaluates, in dependency order.
//
// standalone.js first because the plugins register themselves against it. Everything else is one
// language, and the set is what parserForExtension can ask for.
//
// Exported so the vendoring build step can copy exactly what the engine loads rather than keeping a
// second list that drifts from this one. A bundle vendored but not loaded is dead weight; a bundle
// loaded but not vendored is a build that fails at runtime on a machine without the fork.
var BundleFiles = []string{
	"standalone.js",
	"plugins/estree.js",
	"plugins/typescript.js",
	"plugins/babel.js",
	"plugins/postcss.js",
	"plugins/markdown.js",
	"plugins/graphql.js",
	"plugins/yaml.js",
}

// loadBundles returns each bundle's source, in evaluation order.
//
// A missing bundle is a hard error rather than a skipped language. An engine that quietly loaded
// seven of eight would format the eighth's files by falling through to no parser at all, and the
// failure would look like a file type nobody formats rather than a broken build.
func loadBundles() ([]namedSource, error) {
	directory := BundleDirectory()
	sources := make([]namedSource, 0, len(BundleFiles))
	for _, name := range BundleFiles {
		text, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("reading prettier bundle %s: %w", name, err)
		}
		sources = append(sources, namedSource{name: name, text: string(text)})
	}
	return sources, nil
}

// namedSource is one bundle and where it came from, so an evaluation failure can name the file.
type namedSource struct {
	name string
	text string
}

// drainPromise resolves a value that may be a promise, and this is the least obvious part of the
// engine.
//
// prettier.format returns a promise. goja implements promises but has no event loop, because an
// event loop is a host concern rather than a language one -- the interpreter has a job queue and
// nothing that pumps it. So a promise handed back to Go sits Pending forever, and a caller that
// simply stringifies the result gets the literal text "[object Promise]" written into the file. That
// is not hypothetical; it is what the first working version of this produced.
//
// The pump is goja's own: every RunString drains pending jobs as part of finishing, so evaluating a
// trivial expression advances the queue. Looping on that is what settles the promise.
//
// The iteration cap is a deadlock guard rather than a timeout. Prettier's formatting is synchronous
// under the hood, so a promise that has not settled after this many pumps is not slow, it is stuck,
// and returning an error beats hanging a build.
func drainPromise(vm *goja.Runtime, value goja.Value) (string, error) {
	promise, isPromise := value.Export().(*goja.Promise)
	if !isPromise {
		return value.String(), nil
	}

	const maxPumps = 100000
	for pump := 0; pump < maxPumps && promise.State() == goja.PromiseStatePending; pump++ {
		if _, err := vm.RunString("0"); err != nil {
			return "", fmt.Errorf("pumping the job queue: %w", err)
		}
	}

	switch promise.State() {
	case goja.PromiseStateFulfilled:
		return promise.Result().String(), nil
	case goja.PromiseStateRejected:
		return "", fmt.Errorf("%s", strings.TrimSpace(promise.Result().String()))
	default:
		return "", fmt.Errorf("the formatter's promise never settled after %d pumps", maxPumps)
	}
}
