package check

import (
	"encoding/json"
	"flag"
	"fmt"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// keyStandardLibrary keys every standard-library package among reachable (see cache.StandardLibraryKeys), reading the
// digest of each file hashed holds rather than hashing it again, under the build's context with its fact analyzers and
// loader named, as the system cache's store holds a standard-library package's entry (storeFor). Keys that cannot be
// computed are a problem, and every standard-library package is then keyed as any other package is, or not at all in a
// run without the project's store.
func (session *CacheSession) keyStandardLibrary(
	loader string,
	reachable []*packages.Package,
	hashed cache.FileDigests,
) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil {
		return
	}
	shared := standardLibrary.Context
	shared.Facts = factAnalyzerSet(session.known)
	shared.Loader = loader
	keys, keysError := cache.StandardLibraryKeys(reachable, shared, hashed)
	if keysError != nil {
		session.problem(fmt.Sprintf("computing the standard library's keys: %v", keysError))
		return
	}
	session.standardLibraryKeys = keys
	session.standardLibraryByPath = standardLibraryPackagesByPath(keys)
	session.standardLibraryContext = &shared
}

// StandardLibraryContext is what every standard-library key of the last Restore shared, its fact analyzers named, and
// false before a Restore keyed the standard library or when it could not.
func (session *CacheSession) StandardLibraryContext() (cache.StandardLibraryContext, bool) {
	if session.standardLibraryContext == nil {
		return cache.StandardLibraryContext{}, false
	}
	return *session.standardLibraryContext, true
}

// factAnalyzerSet is each analyzer that learns facts as its slot, then its flags with their values as one JSON object,
// sorted, which every standard-library key holds: the system cache's entries hold only what these analyzers learned,
// under these slots, so a run whose fact analyzers, or their options, are another run's keys every package apart. The
// flags are read, as cacheRuleSet reads a rule's, since a flag's default is what an option left out resolves to.
func factAnalyzerSet(known *cacheAnalyzers) []string {
	set := make([]string, 0, len(known.facts))
	for _, analyzer := range known.facts {
		options := map[string]string{}
		analyzer.Flags.VisitAll(func(option *flag.Flag) {
			options[option.Name] = option.Value.String()
		})
		// Ignored: a map of strings always encodes, and its keys come out sorted.
		encoded, _ := json.Marshal(options)
		set = append(set, known.slots[analyzer]+" "+string(encoded))
	}
	slices.Sort(set)
	return set
}
