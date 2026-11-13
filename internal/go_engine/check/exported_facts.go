package check

import (
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// ExportedFact is one fact a package's analysis exported in the run, as an importer inherits it: the package, the
// analyzer and the fact's type, as factTypeName names it, the object it is about, nil for a fact about the package
// itself, the fact, and the digest of its value (factValueDigest), which two builds that export equal facts digest
// alike whatever bytes gob wrote. Undigestable says the value could not be walked, which a comparison reads as
// differing.
//
// An answer the fact resolver computed on demand (#w7zmvat), purity's, nil-ness's and the rest, is one too: its
// Analyzer is its kind, FactType is "answer", Answer is set, and Encoded is its JSON, which Digest is of. A question
// the run never asked has no answer, so an answer only one build holds says nothing of the fact.
type ExportedFact struct {
	Package      *packages.Package
	Analyzer     string
	FactType     string
	Object       types.Object
	Fact         analysis.Fact
	Digest       cache.Key
	Undigestable bool
	Answer       bool
	Encoded      []byte
}

// answerFactType is every answer's FactType.
const answerFactType = "answer"

// computedAnswers are every answer the resolver computed in the packages analyzed reaches, as facts, each about the
// object its objectpath names in its package; one whose path no longer names an object is left out.
func computedAnswers(resolver *fact_resolver.Resolver, analyzed []*packages.Package) []ExportedFact {
	var answers []ExportedFact
	packages.Visit(analyzed, nil, func(owner *packages.Package) {
		if owner.Types == nil {
			return
		}
		for kind, byPath := range resolver.Recorded(owner.Types) {
			for path, encoded := range byPath {
				object, objectError := objectpath.Object(owner.Types, path)
				if objectError != nil {
					continue
				}
				answers = append(answers, ExportedFact{Package: owner, Analyzer: kind, FactType: answerFactType,
					Object: object, Digest: cache.DigestOf(encoded), Answer: true, Encoded: encoded})
			}
		}
	})
	return answers
}

// actionKey is one analyzer on one package.
type actionKey struct {
	analyzer string
	owner    *packages.Package
}

// exportedFacts are every fact the actions of graphs exported, of every package they ran on, dependencies included,
// for the census of facts across builds (#0ghk7vj). A later wave's action on a package is the one that counts, as the
// findings read it, and an action that failed exported nothing an importer could trust. A package the findings cache
// restored ran no action, so its facts are not here; the census runs with nothing restored.
func exportedFacts(graphs []*checker.Graph) []ExportedFact {
	byAction := map[actionKey][]ExportedFact{}
	for _, graph := range graphs {
		for action := range graph.All() {
			if action.Err != nil {
				continue
			}
			key := actionKey{analyzer: action.Analyzer.Name, owner: action.Package}
			objectFacts, packageFacts := action.ExportedFacts()
			facts := make([]ExportedFact, 0, len(objectFacts)+len(packageFacts))
			for _, objectFact := range objectFacts {
				facts = append(facts, exportedFact(key, objectFact.Object, objectFact.Fact))
			}
			for _, packageFact := range packageFacts {
				facts = append(facts, exportedFact(key, nil, packageFact.Fact))
			}
			byAction[key] = facts
		}
	}
	var every []ExportedFact
	for _, facts := range byAction {
		every = append(every, facts...)
	}
	return every
}

// exportedFact is one fact of the action key names, about object, nil for its package.
func exportedFact(key actionKey, object types.Object, fact analysis.Fact) ExportedFact {
	exported := ExportedFact{Package: key.owner, Analyzer: key.analyzer, Object: object, Fact: fact,
		FactType: factTypeName(reflect.TypeOf(fact))}
	digest, digestError := factValueDigest(fact)
	if digestError != nil {
		exported.Undigestable = true
		return exported
	}
	exported.Digest = digest
	return exported
}
