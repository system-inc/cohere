package tailwind

// TailwindClassLiteralOptions are the selector options every better-tailwindcss rule accepts: the flat
// `selectors`, and the legacy `attributes`, `callees`, `tags` and `variables`, each a list of names,
// or of names with their matchers.
//
// A legacy kind that is written replaces that kind's selectors whole, an empty list included, which
// then reads nothing of that kind; a kind not written keeps the flat selectors of its kind, upstream's
// defaults when `selectors` is not written. That is upstream's createRule (mergeSelectors).
type TailwindClassLiteralOptions struct {
	Selectors  *[]Selector      `json:"selectors"`
	Attributes []LegacySelector `json:"attributes"`
	Callees    []LegacySelector `json:"callees"`
	Tags       []LegacySelector `json:"tags"`
	Variables  []LegacySelector `json:"variables"`

	// surfaces is these options compiled, set once by the decoder, so a rule asking on every file reads
	// a pointer rather than rebuilding the settings and their key. Nil on options a test builds by hand,
	// which compile on each ask instead.
	surfaces *ClassLiteralSurfaces
}

// ClassLiteralSurfaces is these options compiled: the one shared by every decode that merged to the
// same settings.
func (options TailwindClassLiteralOptions) ClassLiteralSurfaces() *ClassLiteralSurfaces {
	if options.surfaces != nil {
		return options.surfaces
	}
	return ClassLiteralSurfacesFor(options.ClassLiteralSettings())
}

// compileClassLiterals checks and compiles the options once, after the decoder has merged the settings
// into them. Promoted onto every options struct that embeds TailwindClassLiteralOptions, so the decoder
// can call it.
func (options *TailwindClassLiteralOptions) compileClassLiterals() error {
	if options.Selectors != nil {
		if checkSelectorsError := checkSelectors(*options.Selectors); checkSelectorsError != nil {
			return checkSelectorsError
		}
	}
	options.surfaces = ClassLiteralSurfacesFor(options.ClassLiteralSettings())
	return nil
}

// ClassLiteralSettings is these options merged into one list of selectors, as upstream merges them.
func (options TailwindClassLiteralOptions) ClassLiteralSettings() ClassLiteralSettings {
	return ClassLiteralSettings{Selectors: mergeSelectors(
		options.Selectors,
		options.Attributes,
		options.Callees,
		options.Tags,
		options.Variables,
	)}
}
