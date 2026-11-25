//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_report

import (
	"golang.org/x/tools/go/analysis"
)

// Option shapes one finding Report makes, as staticcheck's report.Option does.
type Option func(options *reportOptions)

// reportOptions is what Report reads from its Options, applied in the order given: Related reads ShortRange as it
// stands when Related is applied, as staticcheck's does, so a Related given before ShortRange keeps the full range.
type reportOptions struct {
	// shortRange reports the main part of a node with a body, not the body (see shortRange).
	shortRange bool
	// filterGenerated reports nothing in a file the context judges generated.
	filterGenerated bool
	// fixes are the finding's suggested fixes.
	fixes []analysis.SuggestedFix
	// related is the finding's related information.
	related []analysis.RelatedInformation
	// minimumLanguageVersion, when set, reports nothing where the language version is older.
	minimumLanguageVersion string
	// maximumLanguageVersion, when set, reports nothing where the language version is newer.
	maximumLanguageVersion string
	// minimumStandardLibraryVersion, when set, reports nothing where the standard library's version is older.
	minimumStandardLibraryVersion string
	// maximumStandardLibraryVersion, when set, reports nothing where the standard library's version is newer.
	maximumStandardLibraryVersion string
}

// ShortRange reports the main part of a node with a body, a function's signature or a loop's header, not the body.
func ShortRange() Option {
	return func(options *reportOptions) {
		options.shortRange = true
	}
}

// FilterGenerated reports nothing in a file the context's IsGenerated judges generated.
func FilterGenerated() Option {
	return func(options *reportOptions) {
		options.filterGenerated = true
	}
}

// Fixes adds fixes to the finding's suggested fixes.
func Fixes(fixes ...analysis.SuggestedFix) Option {
	return func(options *reportOptions) {
		options.fixes = append(options.fixes, fixes...)
	}
}

// Related adds message at node to the finding's related information, at node's range as ShortRange stands when this
// option is applied. A node with no range, an instruction with no source, adds nothing.
func Related(node Positioner, message string) Option {
	return func(options *reportOptions) {
		position, end, hasRange := rangeOf(node, options.shortRange)
		if !hasRange {
			return
		}
		options.related = append(options.related,
			analysis.RelatedInformation{Pos: position, End: end, Message: message})
	}
}

// MinimumLanguageVersion reports nothing where the language version (see LanguageVersion) is older than version, a
// Go version such as "go1.22".
func MinimumLanguageVersion(version string) Option {
	return func(options *reportOptions) {
		options.minimumLanguageVersion = version
	}
}

// MaximumLanguageVersion reports nothing where the language version (see LanguageVersion) is newer than version.
// staticcheck's sets the minimum instead, a slip no staticcheck check meets since none calls it; this one is the
// maximum its name says.
func MaximumLanguageVersion(version string) Option {
	return func(options *reportOptions) {
		options.maximumLanguageVersion = version
	}
}

// MinimumStandardLibraryVersion reports nothing where the standard library's version (see StandardLibraryVersion) is
// older than version: staticcheck's MinimumStdlibVersion.
func MinimumStandardLibraryVersion(version string) Option {
	return func(options *reportOptions) {
		options.minimumStandardLibraryVersion = version
	}
}

// MaximumStandardLibraryVersion reports nothing where the standard library's version (see StandardLibraryVersion) is
// newer than version: staticcheck's MaximumStdlibVersion.
func MaximumStandardLibraryVersion(version string) Option {
	return func(options *reportOptions) {
		options.maximumStandardLibraryVersion = version
	}
}
