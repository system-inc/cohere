package check

import (
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// unnamedFacts are the facts each restored package's entry holds about an object its export data does not hold, which
// restore skips, each with its ObjectPath and no Object: what the package's own run exported, for the census of facts
// across builds to find by path in a load of the package from source (#ss4t5g6).
func (session *CacheSession) unnamedFacts() []ExportedFact {
	var unnamed []ExportedFact
	for restoredPackage, restored := range session.hits {
		if restoredPackage.Types == nil {
			continue
		}
		for analyzer, analysis := range restored.analyses {
			key := actionKey{analyzer: analyzer.Name, owner: restoredPackage}
			for _, objectFact := range analysis.objectFacts {
				if _, objectError := objectpath.Object(restoredPackage.Types, objectFact.path); objectError == nil {
					continue
				}
				fact := exportedFact(key, nil, objectFact.fact)
				fact.ObjectPath = objectFact.path
				unnamed = append(unnamed, fact)
			}
		}
	}
	return unnamed
}

// restoredAnswers are the answers the store holds of each package the run restored, as the resolver reads them
// (Answers), by package.
func (session *CacheSession) restoredAnswers() map[*packages.Package]*fact_resolver.Answers {
	answers := map[*packages.Package]*fact_resolver.Answers{}
	for restoredPackage := range session.hits {
		if restoredPackage.Types == nil {
			continue
		}
		if held := session.Answers(restoredPackage.Types); held != nil {
			answers[restoredPackage] = held
		}
	}
	return answers
}
