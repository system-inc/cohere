package text_rules

// Language is one of cohere's three languages, which is also the language it judges. Each language has exactly
// one language, so a case's languages and a ledger row's language are spelled alike.
type Language string

const (
	// LanguageTypeScript is the TypeScript language, whose rules run in cohere's own Go linter.
	LanguageTypeScript Language = "typescript"
	// LanguageGo is cohere-go, cohere-go.
	LanguageGo Language = "go"
	// LanguageSwift is cohere-swift, the Swift language.
	LanguageSwift Language = "swift"
)

// Languages is every language, in the order the corpus lists them.
var Languages = []Language{LanguageTypeScript, LanguageGo, LanguageSwift}

// SourceName is the name of a case's source file for a language: `case.ts`, `case.go`, `case.swift`.
func (language Language) SourceName() string {
	return "case." + extensionOf[language]
}
