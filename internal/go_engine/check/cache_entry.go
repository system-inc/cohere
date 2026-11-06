package check

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// CacheFormatVersion is the version of the cache entry's format, which the engine hashes into every key as the
// cache's own, so an entry written in another format is never read. It changes whenever cacheEntry or anything it
// holds changes shape, Finding included.
const CacheFormatVersion = "go-engine-cache-1"

// errorUnstorable is a package whose facts an entry cannot hold, so it gets no entry, and is counted.
var errorUnstorable = errors.New("a fact the entry cannot hold")

// cacheEntry is one package's analysis as the cache holds it, gob-encoded: by slot, what each analyzer that a later
// run could ask for reported and exported on the package. An analyzer's slot is its rule's name, or for an analyzer
// no rule runs, its name and its fact types (see newCacheAnalyzers).
type cacheEntry struct {
	// Version is CacheFormatVersion as it was when the entry was written, read back as a check on the key's.
	Version string

	Analyzers map[string]storedAnalysis
}

// storedAnalysis is one analyzer on one package: its findings, as newFinding makes them before the dedupe and the
// withholding, and the facts the package itself exported that an importer inherits.
type storedAnalysis struct {
	Findings     []Finding
	ObjectFacts  []storedObjectFact
	PackageFacts []storedFact
}

// storedObjectFact is a fact about one object of the package: the object as its objectpath within the package, and
// the fact.
type storedObjectFact struct {
	Path objectpath.Path
	Fact storedFact
}

// storedFact is one fact as its type's name, which factTypeName gives, and its gob bytes. Each fact is encoded on
// its own as its concrete type, as checker's codeFact encodes it, so no fact type need be registered with gob.
type storedFact struct {
	Type    string
	Encoded []byte
}

// restoredPackage is a package's entry decoded whole, its facts already the objects and values this run's types
// give them, ready for the checker's restore.
type restoredPackage struct {
	analyses map[*analysis.Analyzer]restoredAnalysis
}

// restoredAnalysis is a storedAnalysis decoded.
type restoredAnalysis struct {
	findings     []Finding
	objectFacts  []analysis.ObjectFact
	packageFacts []analysis.Fact
}

// factTypeName names a fact's type, a pointer to a named type, by its package path and name, which is what an
// entry calls it.
func factTypeName(factType reflect.Type) string {
	named := factType
	if named.Kind() == reflect.Pointer {
		named = named.Elem()
	}
	return named.PkgPath() + "." + named.Name()
}

// encodeEntry is the entry of one package that missed, from every action on it, each of which finished without an
// error, and how many facts it left out because their object has no objectpath. It returns errorUnstorable, wrapped,
// when gob refuses a fact the package exported.
func encodeEntry(
	known *cacheAnalyzers,
	ruleOf map[*analysis.Analyzer]rule.Rule,
	actions []*checker.Action,
) ([]byte, int, error) {
	dropped := 0
	entry := cacheEntry{Version: CacheFormatVersion, Analyzers: map[string]storedAnalysis{}}
	var encoder objectpath.Encoder
	for _, action := range actions {
		slot, held := known.slot(action.Analyzer)
		if !held {
			continue
		}
		stored := storedAnalysis{}
		if actionRule, isRule := ruleOf[action.Analyzer]; isRule {
			for _, diagnostic := range action.Diagnostics {
				stored.Findings = append(stored.Findings, newFinding(actionRule, action.Package.Fset, diagnostic))
			}
		}
		objectFacts, packageFacts := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			path, pathError := encoder.For(objectFact.Object)
			if pathError != nil {
				// An object with no objectpath is one no other package can name, such as the local closure printf
				// marks as a wrapper in `fail := func(format string, arguments ...any)`, which exportedFrom passes
				// since it cannot tell a local from a parameter. Its fact reaches an importer by two roads only.
				// ImportObjectFact needs the object, which the importer never holds. AllObjectFacts returns only the
				// calling analyzer's own fact type, and among our analyzers only staticcheck's purity, nilness and
				// deprecated call it: purity and nilness hold facts only on functions, which are package-level and
				// have a path, and deprecated keys by the declared objects an importer meets. So leaving the fact
				// out changes no finding. A new caller of AllObjectFacts is a reason to look at this again.
				dropped++
				continue
			}
			fact, factError := encodeFact(objectFact.Fact)
			if factError != nil {
				return nil, 0, fmt.Errorf("storing %s's fact on %s: %w", action.Analyzer.Name, objectFact.Object,
					factError)
			}
			stored.ObjectFacts = append(stored.ObjectFacts, storedObjectFact{Path: path, Fact: fact})
		}
		for _, packageFact := range packageFacts {
			fact, factError := encodeFact(packageFact.Fact)
			if factError != nil {
				return nil, 0, fmt.Errorf("storing %s's package fact: %w", action.Analyzer.Name, factError)
			}
			stored.PackageFacts = append(stored.PackageFacts, fact)
		}
		entry.Analyzers[slot] = stored
	}
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(entry); encodeError != nil {
		return nil, 0, fmt.Errorf("encoding the entry: %w: %w", errorUnstorable, encodeError)
	}
	return encoded.Bytes(), dropped, nil
}

// encodeFact is one fact as an entry holds it.
func encodeFact(fact analysis.Fact) (storedFact, error) {
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(fact); encodeError != nil {
		return storedFact{}, fmt.Errorf("encoding a %T fact: %w: %w", fact, errorUnstorable, encodeError)
	}
	return storedFact{Type: factTypeName(reflect.TypeOf(fact)), Encoded: encoded.Bytes()}, nil
}

// decodeEntry is a package's entry decoded whole, or an error, which is a miss: an entry in another format, one
// that holds nothing of an analyzer the package can be asked for, a fact of a type its analyzer does not learn, an
// objectpath this package's types do not hold, or bytes gob refuses. given is whether Analyze is given the package,
// which asks its rules' findings of it.
func decodeEntry(
	known *cacheAnalyzers,
	restoring *packages.Package,
	given bool,
	contents []byte,
) (*restoredPackage, error) {
	var entry cacheEntry
	if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&entry); decodeError != nil {
		return nil, fmt.Errorf("decoding the entry of %s: %w", restoring.ID, decodeError)
	}
	if entry.Version != CacheFormatVersion {
		return nil, fmt.Errorf("reading the entry of %s: it is in format %q, not %q", restoring.ID, entry.Version,
			CacheFormatVersion)
	}
	if restoring.Types == nil {
		return nil, fmt.Errorf("reading the entry of %s: the package has no types to find its facts' objects in",
			restoring.ID)
	}
	asked := known.facts
	if given {
		asked = append(append([]*analysis.Analyzer{}, known.rules...), known.facts...)
	}
	restored := &restoredPackage{analyses: map[*analysis.Analyzer]restoredAnalysis{}}
	for _, analyzer := range asked {
		if _, done := restored.analyses[analyzer]; done {
			continue
		}
		slot, _ := known.slot(analyzer)
		stored, held := entry.Analyzers[slot]
		if !held {
			return nil, fmt.Errorf("reading the entry of %s: it holds nothing of %s", restoring.ID, analyzer.Name)
		}
		analysisRestored, restoreError := restoreAnalysis(known, analyzer, restoring.Types, stored)
		if restoreError != nil {
			return nil, fmt.Errorf("reading the entry of %s: %w", restoring.ID, restoreError)
		}
		restored.analyses[analyzer] = analysisRestored
	}
	return restored, nil
}

// restoreAnalysis is one analyzer's stored analysis decoded against the package's types.
func restoreAnalysis(
	known *cacheAnalyzers,
	analyzer *analysis.Analyzer,
	packageTypes *types.Package,
	stored storedAnalysis,
) (restoredAnalysis, error) {
	restored := restoredAnalysis{findings: stored.Findings}
	for _, objectFact := range stored.ObjectFacts {
		object, objectError := objectpath.Object(packageTypes, objectFact.Path)
		if objectError != nil {
			return restoredAnalysis{}, fmt.Errorf("finding the object of %s's fact at %q: %w", analyzer.Name,
				objectFact.Path, objectError)
		}
		fact, factError := decodeFact(known, analyzer, objectFact.Fact)
		if factError != nil {
			return restoredAnalysis{}, factError
		}
		restored.objectFacts = append(restored.objectFacts, analysis.ObjectFact{Object: object, Fact: fact})
	}
	for _, packageFact := range stored.PackageFacts {
		fact, factError := decodeFact(known, analyzer, packageFact)
		if factError != nil {
			return restoredAnalysis{}, factError
		}
		restored.packageFacts = append(restored.packageFacts, fact)
	}
	return restored, nil
}

// decodeFact is one stored fact as a value of the type its analyzer learns by that name.
func decodeFact(known *cacheAnalyzers, analyzer *analysis.Analyzer, stored storedFact) (analysis.Fact, error) {
	factType, learned := known.factTypes[analyzer][stored.Type]
	if !learned {
		return nil, fmt.Errorf("decoding a fact of %s: it learns no fact of type %s", analyzer.Name, stored.Type)
	}
	fact, isFact := reflect.New(factType.Elem()).Interface().(analysis.Fact)
	if !isFact {
		return nil, fmt.Errorf("decoding a fact of %s: a new %s is not a fact", analyzer.Name, stored.Type)
	}
	if decodeError := gob.NewDecoder(bytes.NewReader(stored.Encoded)).Decode(fact); decodeError != nil {
		return nil, fmt.Errorf("decoding a %s fact of %s: %w", stored.Type, analyzer.Name, decodeError)
	}
	return fact, nil
}
