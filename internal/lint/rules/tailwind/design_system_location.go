package tailwind

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/system-inc/cohere/internal/lint/rule"
	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// TailwindLocationOptions are upstream's options that say where Tailwind and its stylesheet are,
// shared by every better-tailwindcss rule: `entryPoint`, `tailwindConfig` and `cwd`.
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
	EntryPoint     string `json:"entryPoint"`
	TailwindConfig string `json:"tailwindConfig"`
	Cwd            string `json:"cwd"`

	// anchor is the directory the config was written in, which stands for ESLint's working directory,
	// set by the decoder from rule.OptionsBase.
	anchor string
}

// Location is the design system these options name.
func (options TailwindLocationOptions) Location() DesignSystemLocation {
	configPath := options.EntryPoint
	if configPath == "" {
		configPath = options.TailwindConfig
	}
	return DesignSystemLocation{Anchor: options.anchor, Cwd: options.Cwd, ConfigPath: configPath}
}

// anchorAt records where the config was written. Promoted onto every options struct that embeds
// TailwindLocationOptions, so the decoder can set it.
func (options *TailwindLocationOptions) anchorAt(directory string) {
	options.anchor = directory
}

// decodeTailwindOptionsAt decodes a better-tailwindcss rule's options strictly and anchors their paths
// at the config's directory. A bare severity decodes to the defaults, still anchored.
func decodeTailwindOptionsAt[Options any, Anchored interface {
	*Options
	anchorAt(directory string)
}]() func(raw []byte, base rule.OptionsBase) (any, error) {
	return func(raw []byte, base rule.OptionsBase) (any, error) {
		var decoded Options
		if len(raw) > 0 {
			if err := rule.UnmarshalOptions(raw, &decoded); err != nil {
				return decoded, fmt.Errorf("decoding %T: %w", decoded, err)
			}
		}
		anchor := base.ConfigDirectory
		if anchor == "" {
			anchor = base.ProjectRoot
		}
		Anchored(&decoded).anchorAt(anchor)
		return decoded, nil
	}
}

// ErrTailwindNotInstalled is returned when no `tailwindcss` resolves from a configured `cwd`. Upstream
// disables the rule with a warning there, so a rule holding it skips the file rather than declining
// loudly.
var ErrTailwindNotInstalled = errors.New("tailwindcss does not resolve from the configured cwd")

// designSystemSkipReason is why a rule skips a file for want of Tailwind, or empty when the error is
// a design system that failed to build, which a rule declines loudly instead.
func designSystemSkipReason(err error) string {
	switch {
	case errors.Is(err, ErrNoTailwindEntryPoint):
		return "no Tailwind entry point is configured"
	case errors.Is(err, ErrTailwindNotInstalled):
		return "tailwindcss does not resolve from the configured cwd"
	}
	return ""
}

// loadConfiguredDesignSystem is upstream's resolution for a rule that named a location.
func loadConfiguredDesignSystem(projectRoot string, fileSystem *rule.RecordingFS, location DesignSystemLocation) DesignSystemResult {
	anchor := location.Anchor
	if anchor == "" {
		anchor = projectRoot
	}
	cwd := anchor
	if location.Cwd != "" {
		cwd = resolvePath(anchor, location.Cwd)
	}

	packageRoot := findTailwindPackageRoot(cwd, fileSystem.FileExists)
	if packageRoot == "" {
		return DesignSystemResult{Err: fmt.Errorf("%w: %s", ErrTailwindNotInstalled, cwd)}
	}

	// Not found falls back to the default theme, as upstream does, which then warns in every message;
	// the warning is not ported, since these rules' messages are their own.
	entryPoint := ""
	if location.ConfigPath != "" {
		entryPoint = findPathRecursive(cwd, cwd, location.ConfigPath, fileSystem.FileExists)
	}
	if entryPoint == "" {
		entryPoint = filepath.Join(packageRoot, "theme.css")
		if !fileSystem.FileExists(entryPoint) {
			return DesignSystemResult{Err: fmt.Errorf("no default Tailwind theme at %s", entryPoint)}
		}
	}

	system, err := tailwindengine.LoadDesignSystem(tailwindengine.LoadOptions{
		EntryPoint:          entryPoint,
		TailwindPackageRoot: packageRoot,
	})
	if err != nil {
		return DesignSystemResult{EntryPoint: entryPoint, Err: err}
	}
	for _, stylesheet := range system.Stylesheets {
		fileSystem.Stat(stylesheet)
	}
	return DesignSystemResult{System: system, Table: tailwindengine.NewTable(system), EntryPoint: entryPoint}
}

// findPathRecursive is upstream's search (async-utils/fs.js at 4.7.0) for one path: the path resolved
// against start, and if it is not there, the same file name one directory up, and so on, stopping at
// the filesystem root or once the directory searched is cwd itself.
func findPathRecursive(cwd string, start string, entry string, fileExists func(path string) bool) string {
	for candidate := resolvePath(start, entry); ; {
		if fileExists(candidate) {
			return candidate
		}
		directory := filepath.Dir(candidate)
		parent := filepath.Dir(directory)
		if parent == directory || directory == cwd {
			return ""
		}
		candidate = filepath.Join(parent, filepath.Base(candidate))
	}
}

// resolvePath is Node's path.resolve for two segments: an absolute path stands, a relative one joins.
func resolvePath(base string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
