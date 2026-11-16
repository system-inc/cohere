package directive_test

// The registry marks this pass a syntax-only walk as it initializes, wrapping Analyzer in place so it runs on a pass
// without type information (rules.syntaxOnlyWalks), and only a test binary that links the registry runs the fixtures
// through that wrap, which is what shows the pass reads nothing the mark forbids. The package's own tests are internal,
// and the registry imports the package, so the link is made from this external test file, which the same binary holds.
import _ "github.com/system-inc/cohere/internal/go_engine/rules"
