package check

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// The project's keys hold the C toolchain under the cgo-translated loader, as the standard library's do: two runs whose
// go env differs in `CGO_CFLAGS` alone key a load the cgo tool translated apart, and a load from source alike, since
// the cgo tool never ran there.
func TestTheProjectsKeyHoldsTheCToolchainUnderTheCgoTranslatedLoader(test *testing.T) {
	test.Parallel()

	contextUnder := func(cgoFlags string, loader string) cache.Context {
		session := NewCacheSession(&Caching{
			Context:      cache.Context{FormatVersion: CacheFormatVersion, Build: []string{"GOOS=linux"}},
			CgoToolchain: []string{"CC=clang", "CGO_CFLAGS=" + cgoFlags},
		}, nil)
		return session.keyContextOf(loader)
	}
	plain := contextUnder("-O2 -g", load.LoaderCgoTranslated)
	flagged := contextUnder("-O2 -g -DPROBE", load.LoaderCgoTranslated)
	if slices.Equal(plain.Build, flagged.Build) {
		test.Errorf("under the cgo-translated loader, two values of CGO_CFLAGS key alike: %q", plain.Build)
	}
	if !slices.Contains(flagged.Build, "loader="+load.LoaderCgoTranslated) {
		test.Errorf("the build %q names no loader", flagged.Build)
	}
	fromSource := contextUnder("-O2 -g", load.LoaderSource)
	flaggedFromSource := contextUnder("-O2 -g -DPROBE", load.LoaderSource)
	if !slices.Equal(fromSource.Build, flaggedFromSource.Build) {
		test.Errorf("under the source loader, CGO_CFLAGS changed the key: %q against %q", fromSource.Build,
			flaggedFromSource.Build)
	}
}
