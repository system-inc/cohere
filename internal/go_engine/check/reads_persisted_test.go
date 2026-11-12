package check

import (
	"bytes"
	"encoding/gob"
	"go/types"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// persistedSizes are the sizes every memo of the persisted tests measures by, as a build's memo does, so another
// package's unexported field renders by its properties and the whole twin renders apart.
var persistedSizes = types.SizesFor("gc", "amd64")

// otherSizes are 386's, a build's of another word size and maximum alignment than persistedSizes, whose memo a record
// measured under persistedSizes is declined by as unknown, and otherSizesDeclined the reason.
var otherSizes = types.SizesFor("gc", "386")

// otherSizesDeclined is why a record measured under persistedSizes is declined by a memo measured under otherSizes.
const otherSizesDeclined = "unknown: its reads were measured under word size 8, maximum alignment 8, the build's " +
	"are 4, 4"

// persistedLearned are the fact types the persisted tests' builds learn, by factTypeName, as an entry names them.
var persistedLearned = map[string]reflect.Type{
	factTypeName(reflect.TypeFor[*valueFact]()): reflect.TypeFor[*valueFact](),
	factTypeName(deprecatedFactType):            deprecatedFactType,
}

// measuredReadsBuild is a memo told the persisted tests' sizes.
func measuredReadsBuild() *ReadsBuild {
	memo := NewReadsBuild()
	memo.measureBy(persistedSizes)
	return memo
}

// persistedRestoring is restoring with its reads persisted as an entry persists them, their digests taken by host
// under version, then encoded and decoded, as a build reads them from the store, its fact types read by
// persistedLearned.
func persistedRestoring(test *testing.T, restoring Restoring, host *ReadsBuild, version string) Restoring {
	test.Helper()

	record, persisted := persistReads(restoring, host, version)
	if !persisted {
		test.Fatalf("the reads of %s were not persisted", restoring.Path)
	}
	encoded, encodeError := encodePersistedReads(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	decoded, decodeError := decodePersistedReads(encoded)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	return Restoring{Path: restoring.Path, Imports: restoring.Imports, Assembly: restoring.Assembly,
		persisted: newHeldReads(decoded, persistedLearned)}
}

// tableRestoring is restoring with its reads persisted as the release's table carries them, their digests taken by
// host: beside an entry of the package in a variant's table (StandardLibraryTableOf), written again as the canonical
// table, read back from it, and held as a host restoring the package from the binary's table holds them
// (heldReadsOf, as keepBinaryHit reads them), its fact types read by persistedLearned.
func tableRestoring(test *testing.T, restoring Restoring, host *ReadsBuild) Restoring {
	test.Helper()

	record, persisted := persistReads(restoring, host, ReadsRendererVersion)
	if !persisted {
		test.Fatalf("the reads of %s were not persisted", restoring.Path)
	}
	reads, encodeError := encodePersistedReads(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	var entry bytes.Buffer
	if gobError := gob.NewEncoder(&entry).Encode(cacheEntry{Version: CacheFormatVersion}); gobError != nil {
		test.Fatal(gobError)
	}
	key := cache.DigestOf([]byte(restoring.Path))
	written, writeError := StandardLibraryTableOf(map[cache.Key]StandardLibraryRunEntry{key: {
		PackagePath: restoring.Path, Entry: entry.Bytes(), Reads: reads}}, "/goroot",
		[]cache.StandardLibraryVariant{{Target: "darwin/amd64", CgoEnabled: "0", GoVersion: "go1.27.0", Packages: 1}})
	if writeError != nil {
		test.Fatal(writeError)
	}
	canonical, canonicalError := CanonicalStandardLibraryTable([][]byte{written}, nil)
	if canonicalError != nil {
		test.Fatal(canonicalError)
	}
	tableEntry, held := cache.NewStandardLibraryTable(canonical).Lookup(key)
	if !held || tableEntry.Reads == nil {
		test.Fatalf("the canonical table holds no reads of %s: held %t", restoring.Path, held)
	}
	return Restoring{Path: restoring.Path, Imports: restoring.Imports, Assembly: restoring.Assembly,
		persisted: heldReadsOf(tableEntry.Reads, persistedLearned)}
}

// wholeVerdict is the whole verdict on restoring in a build: its types' half as the load asks it, then its facts' half
// over every fact type it read.
func wholeVerdict(
	restoring Restoring,
	closure map[string]*types.Package,
	restored map[*types.Package]bool,
	host *ReadsBuild,
	build *ReadsBuild,
	fact FactLookup,
) (bool, string) {
	if holds, why := TypesHold(restoring, closure, restored, host, build); !holds {
		return false, why
	}
	return FactsHold(restoring, closure, host, build, fact, ReadFactTypes(restoring))
}

// verdictFactsOf is the fixture's facts of one build, by factKeyOf, as a FactLookup.
func verdictFactsOf(facts map[string]analysis.Fact) FactLookup {
	return func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool) {
		fact, found := facts[factKeyOf(owner, path, factType)]
		return fact, found
	}
}

// persistedComparison is one verdict of the differential, judged from the reads of this run, from the same reads
// persisted, and from them as the release's table carries them, named for its failure, and, where the case states one,
// the reason it is to give. In a build of other sizes the persisted and table verdicts are to decline as unknown
// (otherSizesDeclined), whatever the reads of this run give.
type persistedComparison struct {
	name       string
	inMemory   func() (bool, string)
	persisted  func() (bool, string)
	table      func() (bool, string)
	want       string
	wantStated bool
	otherSizes bool
}

// The differential: on the verdict fixture's darwin host and three builds, darwin again, linux and freebsd, every user
// package's verdict from its persisted reads, and from them as the release's table carries them, is the verdict from
// its reads of this run, holds and reason alike, with reads that hold and reads that do not of every kind: types that
// change by target, a fact found in one build and missing in another, a package fact of another value, lookups
// answered differently and deprecations of another message. The restored fixture adds Interface and Method lookups
// over a package the build restored from the host, whose export data drops an unexported type with the methods, and
// over a restored package the host's closure lacks. A fourth build, darwin again measured by 386's sizes, declines
// every persisted and table verdict as unknown, where the reads of this run, compared, hold some (#pcv6eff).
func TestPersistedReadsJudgeAsTheReadsOfThisRun(test *testing.T) {
	test.Parallel()

	valueFactType := reflect.TypeFor[*valueFact]()
	newStable := factKeyOf(verdictPlatform, "NewStable", valueFactType)
	packageFact := factKeyOf(verdictPlatform, "", valueFactType)
	stableDeprecated := factKeyOf(verdictPlatform, "Stable", deprecatedFactType)
	darwinFacts := map[string]analysis.Fact{newStable: &valueFact{Count: 1}, packageFact: &valueFact{Count: 3},
		stableDeprecated: &deprecated.IsDeprecated{Msg: "use Other"}}
	builds := []struct {
		goos  string
		sizes types.Sizes
		facts map[string]analysis.Fact
	}{
		{"darwin", persistedSizes, darwinFacts},
		{"linux", persistedSizes, map[string]analysis.Fact{newStable: &valueFact{Count: 1},
			packageFact: &valueFact{Count: 4}, stableDeprecated: &deprecated.IsDeprecated{Msg: "use Another"}}},
		{"freebsd", persistedSizes, map[string]analysis.Fact{packageFact: &valueFact{Count: 3}}},
		{"darwin", otherSizes, darwinFacts},
	}
	extras := map[string]func(test *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup){
		"none": func(*testing.T, verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{}
		},
		"a fact hit": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{Object: host.platform.Scope().Lookup("NewStable"), Package: host.platform,
				FactType: valueFactType, Found: true, Value: &valueFact{Count: 1}}, build_reads.Lookup{}
		},
		"a fact miss": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{Object: host.platform.Scope().Lookup("NewStable"), Package: host.platform,
				FactType: valueFactType}, build_reads.Lookup{}
		},
		"a package fact": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{Package: host.platform, FactType: valueFactType, Found: true,
				Value: &valueFact{Count: 3}}, build_reads.Lookup{}
		},
		"a fact of an object with no objectpath": func(test *testing.T, host verdictWorld) (build_reads.Fact,
			build_reads.Lookup,
		) {
			return build_reads.Fact{Object: boxIntField(test, host), Package: host.platform,
				FactType: valueFactType}, build_reads.Lookup{}
		},
		"a scope hit": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform,
				Name: "Shared", Found: true, Answer: host.platform.Scope().Lookup("Shared")}
		},
		"a scope hit of darwin's alone": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform,
				Name: "OnlyDarwin", Found: true, Answer: host.platform.Scope().Lookup("OnlyDarwin")}
		},
		"a scope miss": func(*testing.T, verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform,
				Name: "Flusher"}
		},
		"the closure's interface": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			_, declared := build_reads.ClosureInterface(host.user.Imports(), verdictPlatform, "Closer")
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryClosure, Path: verdictPlatform,
				Name: "Closer", Found: declared != nil, Answer: declared}
		},
		"a package name": func(*testing.T, verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryPackageName, Name: "elsewhere",
				Found: true}
		},
		"an interface method": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryInterface, Name: "Close",
				Subject: methodOf(host.user, "Mine", "Close"), Found: true,
				Answer: host.platform.Scope().Lookup("Closer")}
		},
		"a method of linux's alone": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Flush",
				Subject: methodOf(host.user, "Mine", "Flush")}
		},
		"a method of a type every build declares": func(_ *testing.T, host verdictWorld) (build_reads.Fact,
			build_reads.Lookup,
		) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Size",
				Subject: methodOf(host.user, "Mine", "Flush")}
		},
		"a method with no subject": func(*testing.T, verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Flush"}
		},
		"a deprecation": func(_ *testing.T, host verdictWorld) (build_reads.Fact, build_reads.Lookup) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
				Subject: host.platform.Scope().Lookup("Stable"), Found: true, Text: "use Other"}
		},
		"no deprecation of an instance's field": func(test *testing.T, host verdictWorld) (build_reads.Fact,
			build_reads.Lookup,
		) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
				Subject: boxIntField(test, host)}
		},
		"a deprecation of an instance's field": func(test *testing.T, host verdictWorld) (build_reads.Fact,
			build_reads.Lookup,
		) {
			return build_reads.Fact{}, build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
				Subject: boxIntField(test, host), Found: true, Text: "use Other"}
		},
		"a lookup of a kind the verdict does not know": func(*testing.T, verdictWorld) (build_reads.Fact,
			build_reads.Lookup,
		) {
			return build_reads.Fact{}, build_reads.Lookup{Query: "Planted", Name: "Anything"}
		},
	}
	// One host memo for every package and one memo per build, as a run's verdicts share them. The reads are persisted
	// first, then every verdict is judged at once, each in memory and persisted on a goroutine of its own, so under
	// the race detector the cases prove the persisted path safe across goroutines too.
	host := measuredReadsBuild()
	var judged []persistedComparison
	for _, build := range builds {
		buildWorld := checkedWorld(test, build.goos, "")
		buildMemo := NewReadsBuild()
		buildMemo.measureBy(build.sizes)
		buildName := build.goos
		if build.sizes == otherSizes {
			buildName += " measured by 386's sizes"
		}
		closure := map[string]*types.Package{verdictPlatform: buildWorld.platform}
		fact := verdictFactsOf(build.facts)
		for user := range verdictUsers {
			for extra, read := range extras {
				if user != "stable" && extra != "none" {
					continue
				}
				restoring, hostWorld := hostReads(test, user)
				factRead, lookup := read(test, hostWorld)
				if factRead.Package != nil {
					restoring.Reads.AddFact(factRead)
				}
				if lookup.Query != "" {
					restoring.Reads.AddLookup(lookup)
				}
				persisted := persistedRestoring(test, restoring, host, ReadsRendererVersion)
				tabled := tableRestoring(test, restoring, host)
				judged = append(judged, persistedComparison{
					name: "the " + user + " user with " + extra + " in " + buildName,
					inMemory: func() (bool, string) {
						return wholeVerdict(restoring, closure, nil, host, buildMemo, fact)
					},
					persisted: func() (bool, string) {
						return wholeVerdict(persisted, closure, nil, host, buildMemo, fact)
					},
					table: func() (bool, string) {
						return wholeVerdict(tabled, closure, nil, host, buildMemo, fact)
					},
					otherSizes: build.sizes == otherSizes,
				})
			}
		}
	}
	for _, step := range persistedRestoredSteps(test) {
		persisted := persistedRestoring(test, step.restoring, step.host, ReadsRendererVersion)
		tabled := tableRestoring(test, step.restoring, step.host)
		judged = append(judged, persistedComparison{
			name: "the restored fixture " + step.name,
			inMemory: func() (bool, string) {
				return TypesHold(step.restoring, step.closure, step.restored, step.host, step.build)
			},
			persisted: func() (bool, string) {
				return TypesHold(persisted, step.closure, step.restored, step.host, step.build)
			},
			table: func() (bool, string) {
				return TypesHold(tabled, step.closure, step.restored, step.host, step.build)
			},
			want:       step.want,
			wantStated: true,
		})
	}
	verdicts := make([][3]struct {
		holds bool
		why   string
	}, len(judged))
	var waiting sync.WaitGroup
	for index, each := range judged {
		for side, judge := range []func() (bool, string){each.inMemory, each.persisted, each.table} {
			waiting.Go(func() {
				verdicts[index][side].holds, verdicts[index][side].why = judge()
			})
		}
	}
	waiting.Wait()
	holding, declined, otherSizesHolding := 0, map[string]int{}, 0
	for index, each := range judged {
		inMemory, persisted, table := verdicts[index][0], verdicts[index][1], verdicts[index][2]
		if each.otherSizes {
			if inMemory.holds {
				otherSizesHolding++
			}
			// Measured by other sizes, a record is declined as unknown before any read, never compared.
			if persisted.holds || persisted.why != otherSizesDeclined || table != persisted {
				test.Errorf("%s: want the persisted and table verdicts declined, %q, in memory %t, %q, persisted "+
					"%t, %q, table %t, %q", each.name, otherSizesDeclined, inMemory.holds, inMemory.why,
					persisted.holds, persisted.why, table.holds, table.why)
			}
			continue
		}
		if inMemory.holds {
			holding++
		} else {
			declined[strings.Fields(inMemory.why)[0]]++
		}
		if persisted != inMemory || table != inMemory || each.wantStated && inMemory.why != each.want {
			test.Errorf("%s: want %q, in memory %t, %q, persisted %t, %q, table %t, %q", each.name, each.want,
				inMemory.holds, inMemory.why, persisted.holds, persisted.why, table.holds, table.why)
		}
	}
	// The build of other sizes tells something only when a verdict compared would have held there.
	if otherSizesHolding == 0 {
		test.Error("no verdict of this run held in the build of other sizes, so its declines tell nothing")
	}
	compared := len(judged)
	// The fixture tells something only when reads of every kind both hold and fail in it.
	for _, tag := range []string{"T", "F", "L", "D"} {
		if declined[tag] == 0 {
			test.Errorf("no verdict of the %d declined by a %s read, so the fixture tells nothing of it", compared,
				tag)
		}
	}
	if holding == 0 {
		test.Errorf("none of the %d verdicts held, so the fixture tells nothing", compared)
	}
	test.Logf("%d verdicts judged, %d held, declined by %v, %d of this run held in the build of other sizes", compared,
		holding, declined, otherSizesHolding)
}

// persistedRestoredSteps are the restored fixture's verdicts on its user, whose Method lookup of Close and Interface
// lookup of close ask of hidden, each with the reason it is to give: restored from the host, whose export data drops
// closer with its exported Close and its unexported close, so the host's type names stand for the build's; read from
// that export data without being restored, where closer is missing; and checked from source with an import of its
// own the host's closure lacks, which the build restored from the host, so its Opener's Close counts in the build.
func persistedRestoredSteps(test *testing.T) []struct {
	name      string
	restoring Restoring
	closure   map[string]*types.Package
	restored  map[*types.Package]bool
	host      *ReadsBuild
	build     *ReadsBuild
	want      string
} {
	test.Helper()

	world := checkedRestoredWorld(test)
	subject := world.restoring.Reads.Lookups()[0].Subject
	world.restoring.Reads.AddLookup(build_reads.Lookup{Query: build_reads.QueryInterface, Name: "close",
		Subject: methodOf(subject.Pkg(), "Mine", "close")})
	const extraPath = "example.com/restored/extra"
	withExtra := checkedInMemory(test, restoredHiddenPath, "package hidden\n\nimport \""+extraPath+"\"\n\n"+
		"var Held extra.Opener\n\ntype closer struct{}\n\nfunc (closer) Close() error { return nil }\n\n"+
		"func (closer) close() {}\n\nfunc Count() int { return 1 }\n",
		map[string]string{extraPath: "package extra\n\ntype Opener struct{}\n\nfunc (Opener) Close() error { " +
			"return nil }\n"})
	host := measuredReadsBuild()
	return []struct {
		name      string
		restoring Restoring
		closure   map[string]*types.Package
		restored  map[*types.Package]bool
		host      *ReadsBuild
		build     *ReadsBuild
		want      string
	}{
		{"restored from the host", world.restoring,
			map[string]*types.Package{restoredHiddenPath: world.exportHidden},
			map[*types.Package]bool{world.exportHidden: true}, host, measuredReadsBuild(), ""},
		{"checked from source", world.restoring,
			map[string]*types.Package{restoredHiddenPath: world.exportHidden}, nil, host, measuredReadsBuild(),
			"L lookup Interface close: 1 type names have the method on the host, 0 in the build"},
		{"with a restored import the host lacks", world.restoring,
			map[string]*types.Package{restoredHiddenPath: withExtra},
			map[*types.Package]bool{withExtra.Imports()[0]: true}, host, measuredReadsBuild(),
			"L lookup Method Close: 1 type names have the method on the host, 2 in the build"},
	}
}

// A record a renderer of another version wrote is declined, never matched, at both layers: its header, which declines
// a record of misses alone, which hold in every build, and its digests, which decline it even under a header of this
// version.
func TestPersistedReadsOfAnotherRendererAreDeclined(test *testing.T) {
	test.Parallel()

	const older = "reads-renderer-0"
	linux := linuxBuild(test, nil)
	linux.build.measureBy(persistedSizes)

	misses, hostWorld := hostReads(test, "name")
	onlyMisses := &build_reads.Reads{}
	onlyMisses.RecordTypes(hostWorld.user, &types.Info{})
	onlyMisses.AddFact(build_reads.Fact{Object: hostWorld.platform.Scope().Lookup("NewStable"),
		Package: hostWorld.platform, FactType: reflect.TypeFor[*valueFact]()})
	misses.Reads = onlyMisses
	host := measuredReadsBuild()
	if holds, why := holdsIn(misses, host, linux); !holds {
		test.Fatalf("a fact miss alone does not hold in memory: %s", why)
	}
	want := "unknown: its reads were rendered by " + older + ", this binary renders " + ReadsRendererVersion
	holds, why := holdsIn(persistedRestoring(test, misses, host, older), host, linux)
	if holds || why != want {
		test.Errorf("want a record of misses under an older header declined, %q, got %t, %q", want, holds, why)
	}
	if reasonWord(why) != declineKeys["unknown"] {
		test.Errorf("the older header counts as %s, want %s", reasonWord(why), declineKeys["unknown"])
	}

	stable, _ := hostReads(test, "stable")
	darwin := checkedWorld(test, "darwin", "")
	again := linuxWorld{build: measuredReadsBuild(), fact: linux.fact,
		closure: map[string]*types.Package{verdictPlatform: darwin.platform}}
	if holds, why := holdsIn(stable, host, again); !holds {
		test.Fatalf("the stable user does not hold in memory in darwin again: %s", why)
	}
	relabeled := persistedRestoring(test, stable, host, older)
	relabeled.persisted.record.Renderer = ReadsRendererVersion
	holds, why = holdsIn(relabeled, host, again)
	if holds || !strings.HasPrefix(why, "T object ") || !strings.HasSuffix(why, " renders differently") {
		test.Errorf("want digests of an older renderer declined as rendering differently, got %t, %q", holds, why)
	}
}

// A persisted read that does not resolve in the build declines its package, naming the read, as the same read of this
// run does: an objectpath the build's package does not have, and an owner the build does not have at all. A package
// with no reads at all, recorded or persisted, is declined as unpersisted, which the record counts on its own.
func TestUnresolvablePersistedReadsDecline(test *testing.T) {
	test.Parallel()

	host := measuredReadsBuild()
	linux := linuxBuild(test, nil)
	linux.build.measureBy(persistedSizes)
	darwinUser, _ := hostReads(test, "darwin")
	stableUser, _ := hostReads(test, "stable")
	for _, step := range []struct {
		name      string
		restoring Restoring
		closure   map[string]*types.Package
		want      string
	}{
		{"an objectpath that does not resolve", darwinUser, linux.closure,
			"T object " + verdictPlatform + ".OnlyDarwin: the build's " + verdictPlatform + " has no OnlyDarwin"},
		{"an owner the build lacks", stableUser, map[string]*types.Package{},
			"T object " + verdictPlatform + ".Identity: the build has no package " + verdictPlatform},
	} {
		inMemory, inMemoryWhy := TypesHold(step.restoring, step.closure, nil, host, linux.build)
		persisted, persistedWhy := TypesHold(persistedRestoring(test, step.restoring, host, ReadsRendererVersion),
			step.closure, nil, host, linux.build)
		if inMemory || inMemoryWhy != step.want || persisted || persistedWhy != step.want {
			test.Errorf("%s: want declined, %q, in memory and persisted, got %t, %q and %t, %q", step.name,
				step.want, inMemory, inMemoryWhy, persisted, persistedWhy)
		}
	}
	holds, why := holdsIn(Restoring{Path: verdictUser, Imports: []string{verdictPlatform}}, host, linux)
	if holds || why != unpersistedReads || reasonWord(why) != "readsNotPersisted" {
		test.Errorf("want a package with no reads declined as %q, counted as readsNotPersisted, got %t, %q, %s",
			unpersistedReads, holds, why, reasonWord(why))
	}
}

// The store keeps a record beside its entry only once the entry is there, and a record whose entry is missing reads
// as none, so a reader that finds a record always finds its entry.
func TestPersistedReadsAreKeptOnlyBesideTheirEntry(test *testing.T) {
	test.Parallel()

	store := cache.Open(test.TempDir())
	key := cache.DigestOf([]byte("an entry"))
	restoring, _ := hostReads(test, "stable")
	record, persisted := persistReads(restoring, measuredReadsBuild(), ReadsRendererVersion)
	if !persisted {
		test.Fatal("the stable user's reads were not persisted")
	}
	if putError := putBuildReads(store, key, record); putError == nil {
		test.Error("a record was kept before its entry")
	}
	encoded, encodeError := encodePersistedReads(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if putError := store.Put(buildReadsKey(key), encoded); putError != nil {
		test.Fatal(putError)
	}
	if _, found := getBuildReads(store, key); found {
		test.Error("a record whose entry is missing was read")
	}
	if putError := store.Put(key, []byte("the entry")); putError != nil {
		test.Fatal(putError)
	}
	if putError := putBuildReads(store, key, record); putError != nil {
		test.Fatal(putError)
	}
	read, found := getBuildReads(store, key)
	if !found || !reflect.DeepEqual(read, record) {
		test.Errorf("the record kept beside its entry reads back %t, %+v, want %+v", found, read, record)
	}
}
