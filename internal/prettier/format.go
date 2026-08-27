package prettier

import (
	"fmt"
	"sync"

	"github.com/dop251/goja"
)

// formatScript calls into the loaded bundles.
//
// The options mirror the ahra Prettier configuration. They are passed explicitly rather than resolved from
// a .prettierrc because resolution is Node's job and this runtime has no filesystem: the config is
// the caller's to supply, and hardcoding it here would silently ignore a project that configured
// something else.
const formatScript = `
	(function () {
		return prettier.format(__source, {
			parser: __parser,
			plugins: prettierPlugins,
			tabWidth: __tabWidth,
			useTabs: __useTabs,
			semi: __semi,
			singleQuote: __singleQuote,
			printWidth: __printWidth
		});
	})()
`

// Options are the Prettier settings the engine formats with.
type Options struct {
	TabWidth    int
	UseTabs     bool
	Semi        bool
	SingleQuote bool
	PrintWidth  int
}

// DefaultOptions mirrors the prettier block in the ahra package.json.
func DefaultOptions() Options {
	return Options{TabWidth: 4, UseTabs: false, Semi: true, SingleQuote: true, PrintWidth: 120}
}

// Engine formats source by running our Prettier fork inside goja.
//
// A goja.Runtime cannot be used by two goroutines at once and its values cannot cross runtimes, so
// the runtime is guarded rather than shared freely. A mutex rather than a pool because the candidate
// set is changed files, typically one to a dozen, where a pool's construction cost -- roughly 100ms
// of bundle evaluation per runtime -- would exceed what it saves.
type Engine struct {
	options Options

	mutex   sync.Mutex
	runtime *goja.Runtime
}

// New builds an engine with the bundles loaded and evaluated.
//
// Evaluation happens once, here, rather than per format. It costs about 100ms, and paying it per
// file would dominate the formatting itself.
func New(options Options) (*Engine, error) {
	sources, err := loadBundles()
	if err != nil {
		return nil, err
	}

	runtime := goja.New()
	for _, source := range sources {
		if _, err := runtime.RunString(source.text); err != nil {
			return nil, fmt.Errorf("evaluating prettier bundle %s: %w", source.name, err)
		}
	}

	return &Engine{options: options, runtime: runtime}, nil
}

// Handles reports whether the engine formats this file's type at all.
//
// Separate from Format because the caller needs the answer before deciding a file is a candidate,
// and asking by attempting a format means running the formatter to learn it should not have run.
func (engine *Engine) Handles(fileName string) bool {
	_, handled := parserFor(fileName)
	return handled
}

// Format returns the formatted text, or an error.
//
// It never signals "nothing to do" by returning its input unchanged. A caller cannot distinguish
// that from "already correctly formatted", so a formatter that silently handled nothing would report
// a whole tree as clean.
func (engine *Engine) Format(fileName string, text string) (string, error) {
	parser, handled := parserFor(fileName)
	if !handled {
		return "", fmt.Errorf("prettier does not format %s", fileName)
	}

	engine.mutex.Lock()
	defer engine.mutex.Unlock()

	engine.runtime.Set("__source", text)
	engine.runtime.Set("__parser", parser)
	engine.runtime.Set("__tabWidth", engine.options.TabWidth)
	engine.runtime.Set("__useTabs", engine.options.UseTabs)
	engine.runtime.Set("__semi", engine.options.Semi)
	engine.runtime.Set("__singleQuote", engine.options.SingleQuote)
	engine.runtime.Set("__printWidth", engine.options.PrintWidth)

	value, err := engine.runtime.RunString(formatScript)
	if err != nil {
		return "", fmt.Errorf("formatting %s: %w", fileName, err)
	}

	formatted, err := drainPromise(engine.runtime, value)
	if err != nil {
		return "", fmt.Errorf("formatting %s: %w", fileName, err)
	}
	return formatted, nil
}
