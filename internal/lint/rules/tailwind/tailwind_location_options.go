package tailwind

// TailwindLocationOptions are upstream's options that say where Tailwind and its stylesheet are, and how
// the stylesheet's imports resolve, shared by every better-tailwindcss rule: `entryPoint`,
// `tailwindConfig`, `cwd` and `tsconfig` (see tsconfig_paths.go for the last).
//
// Upstream resolves all three in `getTailwindConfigPath` (utils/context.js at 4.7.0). `cwd` is
// resolved against the directory ESLint runs from and is where `tailwindcss` is resolved from. The
// stylesheet is `entryPoint ?? tailwindConfig`, looked for from `cwd` by findPathRecursive. When it is
// not found, or neither is set, upstream reads Tailwind's own default theme, `tailwindcss/theme.css`.
//
// Here, a rule given none of the three keeps finding the project's stylesheet by FindEntryPoint, as it
// always has, and a rule given any of them follows upstream exactly. On a rule that never reads the
// design system they change nothing, in upstream as here.
type TailwindLocationOptions struct {
	EntryPoint            string `json:"entryPoint"`
	TailwindConfiguration string `json:"tailwindConfig"`
	WorkingDirectory      string `json:"cwd"`
	Tsconfig              string `json:"tsconfig"`

	// anchor is the directory the config was written in, which stands for ESLint's working directory,
	// set by the decoder from rule.OptionsBase.
	anchor string
}

// Location is the design system these options name.
func (options TailwindLocationOptions) Location() DesignSystemLocation {
	configurationPath := options.EntryPoint
	if configurationPath == "" {
		configurationPath = options.TailwindConfiguration
	}
	return DesignSystemLocation{
		Anchor:           options.anchor,
		WorkingDirectory: options.WorkingDirectory,
		ConfigPath:       configurationPath,
		Tsconfig:         options.Tsconfig,
	}
}

// anchorAt records where the config was written. Promoted onto every options struct that embeds
// TailwindLocationOptions, so the decoder can set it.
func (options *TailwindLocationOptions) anchorAt(directory string) {
	options.anchor = directory
}
