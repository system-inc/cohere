package text_rules

// Engine is one of cohere's three engines, which is also the language it judges. Each language has exactly
// one engine, so a case's languages and a ledger row's engine are spelled alike.
type Engine string

const (
	// EngineTypeScript is the TypeScript engine, whose rules run in cohere's own Go linter.
	EngineTypeScript Engine = "typescript"
	// EngineGo is cohere-go, the Go engine.
	EngineGo Engine = "go"
	// EngineSwift is cohere-swift, the Swift engine.
	EngineSwift Engine = "swift"
)

// Engines is every engine, in the order the corpus lists them.
var Engines = []Engine{EngineTypeScript, EngineGo, EngineSwift}

// SourceName is the name of a case's source file for an engine: `case.ts`, `case.go`, `case.swift`.
func (engine Engine) SourceName() string {
	return "case." + extensionOf[engine]
}
