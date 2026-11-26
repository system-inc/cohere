package oracle_testing

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// Evaluate is the recorded answer under key, for what Formatter cannot ask: a script against the bundles'
// public API (prettier.format with an option Options does not carry, prettier.doc, prettier.util,
// prettier.__debug). When recording, script runs with the string __input set to input (JSON, for an input
// with structure) and must evaluate to a string or a promise of one; an exception or a rejection is recorded
// as the answer's error. The runtime holding the bundles is built on the golden's first recording Evaluate and
// kept for the rest of the test, so a plain run, which only reads the golden, never builds it.
func (golden *Golden) Evaluate(key string, script string, input string) (string, error) {
	return golden.Answer(key, func() (string, error) {
		golden.runtimeOnce.Do(func() { golden.runtime, golden.runtimeError = golden.loadRuntime() })
		if golden.runtimeError != nil {
			return "", golden.runtimeError
		}
		// One runtime serves every Evaluate on this golden, and a goja runtime is not safe for two goroutines.
		golden.runtimeMutex.Lock()
		defer golden.runtimeMutex.Unlock()
		if setError := golden.runtime.Set("__input", input); setError != nil {
			return "", fmt.Errorf("setting the oracle's input: %w", setError)
		}
		value, runStringError := golden.runtime.RunString(script)
		if runStringError != nil {
			return "", runStringError
		}
		promise, isPromise := value.Export().(*goja.Promise)
		if !isPromise {
			return value.String(), nil
		}
		// Each RunString drains goja's job queue, so pump until the promise settles.
		for pump := 0; pump < 100000 && promise.State() == goja.PromiseStatePending; pump++ {
			if _, runStringError := golden.runtime.RunString("0"); runStringError != nil {
				return "", runStringError
			}
		}
		if promise.State() != goja.PromiseStateFulfilled {
			return "", fmt.Errorf("%v", promise.Result())
		}
		return promise.Result().String(), nil
	})
}

// loadBundleRuntime is a runtime that has evaluated every embedded bundle, so prettier and prettierPlugins
// are its globals.
func loadBundleRuntime() (*goja.Runtime, error) {
	bundles, bundlesError := prettier.Bundles()
	if bundlesError != nil {
		return nil, bundlesError
	}
	runtime := goja.New()
	for _, name := range prettier.BundleFiles {
		if _, runStringError := runtime.RunString(string(bundles.Files[name])); runStringError != nil {
			return nil, fmt.Errorf("evaluating %s: %w", name, runStringError)
		}
	}
	return runtime, nil
}
