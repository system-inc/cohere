package doc

import (
	"fmt"
	"sync"
	"testing"

	"github.com/dop251/goja"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// bundleOracle asks the embedded Prettier bundles' public API (prettier.doc, prettier.util) what upstream
// answers, for what prettier.Formatter cannot be asked. Its answers are recorded (see oracle_testing), so the
// runtime is built only when they are recorded again, and a plain run evaluates no JavaScript.
type bundleOracle struct {
	golden    *oracle_testing.Golden
	once      sync.Once
	runtime   *goja.Runtime
	loadError error
}

func newBundleOracle(test *testing.T) *bundleOracle {
	test.Helper()
	return &bundleOracle{golden: oracle_testing.Open(test, test.Name())}
}

func (oracle *bundleOracle) load() {
	bundles, bundlesError := prettier.Bundles()
	if bundlesError != nil {
		oracle.loadError = bundlesError
		return
	}
	runtime := goja.New()
	for _, name := range prettier.BundleFiles {
		if _, runStringError := runtime.RunString(string(bundles.Files[name])); runStringError != nil {
			oracle.loadError = fmt.Errorf("evaluating %s: %w", name, runStringError)
			return
		}
	}
	oracle.runtime = runtime
}

// answer is the recorded answer under key. When recording, script runs with the string __input set to input
// (JSON, for an input with structure) and must evaluate to a string; a JavaScript exception is recorded as the
// answer's error.
func (oracle *bundleOracle) answer(key string, script string, input string) (string, error) {
	return oracle.golden.Answer(key, func() (string, error) {
		oracle.once.Do(oracle.load)
		if oracle.loadError != nil {
			return "", oracle.loadError
		}
		if setError := oracle.runtime.Set("__input", input); setError != nil {
			return "", fmt.Errorf("setting the oracle's input: %w", setError)
		}
		value, runStringError := oracle.runtime.RunString(script)
		if runStringError != nil {
			return "", runStringError
		}
		return value.String(), nil
	})
}
