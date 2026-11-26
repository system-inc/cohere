package main

import (
	"os"
	"path/filepath"
	"strings"
)

// raceGated are the packages whose tests the land gate runs a second time under -race (#bnabymg). A concurrency test
// catches its race only under the race detector, and the gate's go test runs without it, so a change that put back
// the race the test was written for landed green: TestDependenceSetsComputedAtOnceOverOneHostAgree was red with 26
// races with its lock removed, but only by hand.
//
// The run is the gate box's, after the module's plain run passes there (box_verdict.go): measured on Kirk's Mac on
// 2026-11-12, the five heaviest of these took 591s of wall and about 1,050 CPU-seconds under -race one after another
// with their race build cached, against a gate of about seven minutes, so not on every landing there. A landing with
// the gate off runs them on Kirk's Mac after its module passes, through the gate's token, since one with no -race run
// at all let a race land whenever Home, the gate's box then, was busy: the whole list there, with nothing replayed,
// took 428s of wall and 1,055 CPU-seconds at a load of 8 to 42 (Nov 12).
//
// Every package holding a test whose name or file says it runs code at once is here, which
// TestEveryConcurrencyTestIsRaceGated holds, and a package may be here for a reason its test names don't show. An
// entry runs its whole package unless it names a run, which TestEveryRaceRunSelectsItsPackagesConcurrencyTests holds
// to every test of its package the guard calls concurrent.
var raceGated = []raceEntry{
	{directory: "command/cohere", reason: "TestFormatInParallelLeavesTheRulesToTheSerialPath formats files at once " +
		"(fix_parallel_test.go)"},
	{directory: "command/cohere-dev", reason: "the pool's tokens, its line, borrowing and the bounded run's watchdog " +
		"run side by side, and its tests call code that writes the package-wide streams, which raced on Home " +
		"(#bnabymg)"},
	{directory: "internal/edit", reason: "TestConcurrentReadersNeverSeeAPartialFile reads while a write replaces " +
		"the file"},
	{
		directory: "internal/edit/write_root",
		reason:    "TestCopiesAskedAtOnceAgree asks for one copy from several goroutines",
	},
	{
		directory: "internal/go_language",
		// The guard's own words (concurrencyTestName), so the run selects every test whose name says it runs code at
		// once, and the tests that start goroutines in their own bodies, which the guard reads from the code
		// (bodyConcurrentTests, #q1v0ern), and none of the package's others.
		run: "Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad|^(" + strings.Join([]string{
			"TestABuildAfterOneThatFailedIsRefusedAtTheShareGate",
			"TestABuildsWaitAtTheShareGateIsNotItsLoadTime",
			"TestAFailingMiddleBuildWritesWhatOneAtATimeWrites",
			"TestAReloadingBuildIsAdmittedAtTheShareGateOnce",
			"TestBuildsPassTheShareGateInTheOrderTheyStarted",
			"TestEachBuildWritesItsMustCheckSet",
			"TestEachBuildsDependenceIsTheLibrarysJudgment",
			"TestTheBuildsReadFromTheDescriptionsReadsAreTheDisks",
			"TestTheBuildsSelectionReturnsWhenAReadNeverComes",
			"TestTheExtraBuildsReadTheHostsReadsWhole",
		}, "|") + ")$",
		reason: "the early start writes the host's go env while a load it was handed reads it (early_start_test.go: " +
			"TestTheHostsGoEnvironmentIsWholeWhenALoadReadsItAtOnceWithTheEarlyStart, #e8w514b), extra builds run " +
			"side by side (pipeline_builds_beside_test.go), and the lint runs beside the load " +
			"(overlap_test.go, #2xddz1z). It runs only those tests, the ones whose names say they run code " +
			"at once: the whole package under -race takes about 28 minutes on 2 to 3 threads, past the box's " +
			"30-minute timeout (sharedBoxTestTimeout)",
	},
	{directory: "internal/go_language/bounded", reason: "its worker pool runs a function on every index at once, as " +
		"many as there are cores"},
	{
		directory: "internal/go_language/build_dependence",
		reason: "TestPackagesAddedAtOnceWriteTheRecordsOfASerialBuild " +
			"adds packages from many goroutines, each through a scratch of the shared pool (scratch_test.go, #86qvr6b)",
	},
	{directory: "internal/go_language/build_reads", reason: "TestOneRecordReadByManyAtOnceIsJudgedOnce reads one " +
		"package's record of function uses from several goroutines, each asking first for the judgment it makes " +
		"once (function_uses_judgment_test.go, #kr1m304)"},
	{
		directory: "internal/go_language/cache",
		reason: "TestConcurrentPutsOfOneKeyLeaveAWholeEntry writes one key from " +
			"several goroutines",
	},
	{
		directory: "internal/go_language/check",
		reason: "dependence sets and type graph digests are computed at once over one host " +
			"(dependence_concurrency_test.go, type_graph_concurrency_test.go), a run beside the load saves and keeps " +
			"only what the load has made final (cache_session_beside_load_test.go, #641hyrh), and the cache " +
			"session fans its restores, decides and writes out over the run's pool. Its -race run keeps the tests " +
			"the guard calls concurrent and two for each place check starts goroutines, each driving it at least " +
			"two goroutines wide, by the census's count (#q1v0ern): the whole package took about 800 s under -race " +
			"on Shackler",
		kept: &raceKept{
			census: "J-jt23dev",
			concurrent: []raceKeptTest{
				{
					test:   "TestADependenceSetIsComputedOncePerPackage",
					reason: "it starts goroutines itself (reads_build_dependence_test.go)",
				},
				{
					test:   "TestALoadThatFindsNothingFailsTheObjectDifferential",
					reason: "it starts goroutines itself (object_broken_load_test.go)",
				},
				{
					test:   "TestAPackageLoadedWithErrorsFailsTheObjectDifferential",
					reason: "it starts goroutines itself (object_broken_load_test.go)",
				},
				{
					test:   "TestARunBesideTheLoadKeepsAPackageOnlyOnceTheLoadHasMadeItFinal",
					reason: "its name or file says it runs code at once and it starts goroutines itself (cache_session_beside_load_test.go)",
				},
				{
					test:   "TestARunBesideTheLoadSavesAPackageOnlyOnceTheLoadHasMadeItFinal",
					reason: "its name or file says it runs code at once and it starts goroutines itself (cache_session_beside_load_test.go)",
				},
				{
					test:   "TestBlockOnesWalkEndsOverAnInstanceThatExpandsWithoutEnd",
					reason: "it starts goroutines itself (object_architecture_test.go)",
				},
				{
					test:   "TestDependenceSetsComputedAtOnceOverOneHostAgree",
					reason: "its name or file says it runs code at once and it starts goroutines itself (dependence_concurrency_test.go)",
				},
				{
					test:   "TestEveryObjectWhoseLinesDifferByArchitectureHasItsBitSet",
					reason: "it starts goroutines itself (object_architecture_test.go)",
				},
				{
					test:   "TestEveryObjectWhoseLinesDifferHasItsBitSet",
					reason: "it starts goroutines itself (object_differential_test.go)",
				},
				{
					test:   "TestEveryStandardLibraryObjectWhoseLinesDifferHasItsBitSet",
					reason: "it starts goroutines itself (object_differential_test.go)",
				},
				{
					test:   "TestKeptExportDataHoldsOnlyThePackagesTypes",
					reason: "it starts goroutines itself (host_run_test.go)",
				},
				{
					test:   "TestStandingBodiesTakenAndCountedAtOnceCountAsOneAfterAnother",
					reason: "its name or file says it runs code at once and it starts goroutines itself (host_standing_test.go)",
				},
				{
					test:   "TestTheClosureDigestLegFailsWhereNoNameItReachesHasABit",
					reason: "it starts goroutines itself (object_differential_test.go)",
				},
				{
					test:   "TestTheClosureIndexAnswersAsTheWalks",
					reason: "it starts goroutines itself (closure_index_test.go)",
				},
				{
					test:   "TestTheDecideIsGrantedSlotsBeforeAnalysis",
					reason: "it starts goroutines itself (cache_session_fan_out_deciding_test.go)",
				},
				{
					test:   "TestTheFieldSetLegFailsWhereAPromotedFieldMovesUnderAnUnsetNameBit",
					reason: "it starts goroutines itself (object_differential_test.go)",
				},
				{
					test:   "TestTheFixturesJudgmentReportsABuildTheFixtureIsBrokenUnder",
					reason: "it starts goroutines itself (object_broken_load_test.go)",
				},
				{
					test:   "TestVerdictsDigestingAtOnceFinishWithTheDigestsOfOne",
					reason: "its name or file says it runs code at once and it starts goroutines itself (type_graph_concurrency_test.go)",
				},
			},
			sites: []raceSite{
				{site: "cache_session.go:CacheSession.fanOut#1", claims: []raceClaim{
					{test: "TestAPackageAboveADeclinedOneIsJudgedAgainstItsOwnRendering", width: 4},
					{test: "TestTheHostRestoresDecidedOnEveryCoreAreTheOnesDecidedOnOne", width: 8},
				}},
				{site: "cache_session.go:CacheSession.fanOut#2", claims: []raceClaim{
					{test: "TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed", width: 6},
					{test: "TestAHitIsAMissForEveryColumnPlacedRefuses", width: 2},
				}},
				{site: "cache_session.go:CacheSession.fanOutDeciding#1", claims: []raceClaim{
					{test: "TestTheHostRestoresDecidedOnEveryCoreAreTheOnesDecidedOnOne", width: 8},
					{test: "TestEveryReaderOfAnAnswerTheBuildCannotAskIsDeclinedOnEverySlot", width: 4},
					{test: "TestEveryRestoreThatHoldsIsWrittenThroughOnEverySlot", width: 4},
				}},
				{site: "cache_session.go:CacheSession.fanOutDeciding#2", claims: []raceClaim{
					{test: "TestARecordWrittenThroughWhoseHostRecordMovedIsAMiss", width: 8},
					{test: "TestARestoreReadingAnOwnerTheBuildRanNothingOnIsDeclined", width: 6},
				}},
				{site: "cache_session.go:CacheSession.fanOutWriting#1", claims: []raceClaim{
					{test: "TestEveryEntryRewrittenWithItsAnswersIsReadWholeBesideTheOthers", width: 4},
					{test: "TestEveryEntryWhosePlacedAnswersGrewIsWrittenAgainBesideTheOthers", width: 4},
					{test: "TestEveryRestoreThatHoldsIsWrittenThroughOnEverySlot", width: 2},
				}},
				{site: "cache_session.go:CacheSession.fanOutWriting#2", claims: []raceClaim{
					{test: "TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed", width: 8},
					{test: "TestAPackageWithoutReadsIsUntouchedOnlyWhenItsClosureIsClean", width: 2},
				}},
				{site: "cache_session_answers.go:CacheSession.rewriteEarlier#1", claims: []raceClaim{
					{test: "TestEveryEntryRewrittenWithItsAnswersIsReadWholeBesideTheOthers", width: 8},
					{test: "TestEveryEntryWhosePlacedAnswersGrewIsWrittenAgainBesideTheOthers", width: 8},
				}},
				{site: "check.go:recordUnreachedTypes#1", claims: []raceClaim{
					{test: "TestEveryNeverMarkedDependencyNoActionReachedIsHandedItsDeclarations", width: 2},
					{test: "TestEveryNeverMarkedRootTheRunDoesNotAnalyzeIsHandedItsDeclarations", width: 2},
				}},
				{site: "check.go:recordUnreachedTypes#2", claims: []raceClaim{
					{test: "TestAPackagesTypeReadsAreEveryForeignObjectItReached", width: 8},
					{test: "TestARunBesideTheLoadKeepsAPackageOnlyOnceTheLoadHasMadeItFinal", width: 8},
				}},
				{site: "host_run.go:HostRun.computeDependence#1", claims: []raceClaim{
					{test: "TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed", width: 77},
					{test: "TestAPackageWithoutReadsIsUntouchedOnlyWhenItsClosureIsClean", width: 3},
				}},
			},
		},
	},
	{directory: "internal/go_language/check/checker", reason: "the scheduler runs groups and actions at once, up to " +
		"its slots and workers"},
	{
		directory: "internal/go_language/fact_resolver",
		reason: "TestObjectsAskedAtOnceAreNamedAsObjectpathNamesThem asks the resolver's objectpath encoders from " +
			"several goroutines while each indexes its package (object_path_test.go, #8d9r2rh). The resolver's own " +
			"code starts no goroutines, so its -race run is that test alone (#q1v0ern)",
		kept: &raceKept{
			census: "J-yqn9arq",
			concurrent: []raceKeptTest{
				{
					test:   "TestObjectsAskedAtOnceAreNamedAsObjectpathNamesThem",
					reason: "its encoders are asked from several goroutines at once (object_path_test.go)",
				},
			},
		},
	},
	{
		directory: "internal/go_language/intermediate_representation",
		reason: "the intermediate representation is built " +
			"in parallel (parallel_build_test.go)",
	},
	{
		directory: "internal/go_language/load",
		reason: "the loader lists, reads and parses packages side by side, and its in-process lister fans out over " +
			"directories and the standard library. Its -race run keeps the tests the guard calls concurrent and two " +
			"for each place the loader starts goroutines, each driving it at least two goroutines wide, by the " +
			"census's count (#q1v0ern): the whole package took about 600 s under -race on Shackler",
		kept: &raceKept{
			census: "J-yqn9arq",
			concurrent: []raceKeptTest{
				{
					test:   "TestAListingFingerprintIsReadOnceARunForEachSetOfInputs",
					reason: "it starts goroutines itself (listing_share_test.go)",
				},
				{
					test:   "TestALoadsInputsAreCollectedWithEverySlotHeld",
					reason: "it starts goroutines itself (inputs_beside_test.go)",
				},
				{
					test:   "TestASharedHeadIsNeverWrittenOverByALaterRead",
					reason: "it starts goroutines itself (shared_file_test.go)",
				},
				{
					test:   "TestATakenReadIsGoBuildsOwn",
					reason: "it starts goroutines itself (package_reads_test.go)",
				},
				{
					test:   "TestATestVariantKeepsItsPackagesOrderWhicheverClaimsFirst",
					reason: "it starts goroutines itself (file_positions_test.go)",
				},
				{
					test:   "TestAWalkIsGrantedALoadSlotOnlyAfterEveryPackagesWork",
					reason: "it starts goroutines itself (walk_slots_test.go)",
				},
				{
					test:   "TestAnAnalysisBesideTheLoadMeetsADependencysCgoDeclarationsWithheld",
					reason: "its name or file says it runs code at once and it starts goroutines itself (overlap_test.go)",
				},
				{
					test:   "TestEveryDirectoryReadsAsGoBuildReadsItOnCoheresBuilds",
					reason: "it starts goroutines itself (header_share_test.go)",
				},
				{
					test:   "TestTheLoadParsesTheLongestPathFirst",
					reason: "it starts goroutines itself (load_order_test.go)",
				},
			},
			sites: []raceSite{
				{site: "in_process_lister.go:inProcessLister.match#1", claims: []raceClaim{
					{test: "TestInProcessListingIsGoListsAnswer", width: 3},
					{test: "TestAListingFingerprintIsReadOnceARunForEachSetOfInputs", width: 2},
				}},
				{site: "in_process_lister.go:inProcessLister.packagesIn#1", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 4},
					{test: "TestAModuleFileTheRunsGoCanRewriteIsReadAgainAtEveryAsk", width: 3},
				}},
				{site: "in_process_lister.go:inProcessLister.prefetchStandardLibrary#1", claims: []raceClaim{
					{test: "TestAListingFingerprintIsReadOnceARunForEachSetOfInputs", width: 4},
					{test: "TestEveryDirectoryReadsAsGoBuildReadsItOnCoheresBuilds", width: 7},
				}},
				{site: "in_process_lister.go:inProcessLister.prefetchStandardLibrary#2", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 13},
					{test: "TestAModuleFileTheRunsGoCanRewriteIsReadAgainAtEveryAsk", width: 189},
				}},
				{site: "in_process_lister.go:inProcessLister.load#1", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 2},
					{test: "TestAModuleFileTheRunsGoCanRewriteIsReadAgainAtEveryAsk", width: 12},
				}},
				{site: "kept_listing.go:steadyFingerprint#1", claims: []raceClaim{
					{test: "TestAModuleFileTheRunsGoCanRewriteIsReadAgainAtEveryAsk", width: 3},
					{test: "TestASteadyFingerprintIsKeyedByEachListAlone", width: 2},
				}},
				{site: "lister_state.go:listerState.fetch#1", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 2},
					{test: "TestAGoCommandThatCannotRunAsideSaysItMayRewriteTheSum", width: 2},
				}},
				{site: "loader.go:loader.load#1", claims: []raceClaim{
					{test: "TestACgoProjectsPackageIsRestoredFromExportData", width: 2},
					{test: "TestRestoredPackagesCheckedFromSourceClaimInTheSameOrderEveryLoad", width: 2},
					// Both claimants above may skip on a gate box, one without cgo translation and one on one core, so
					// check's restores, which drive the same restore here under check's own run, hold it.
					{test: "TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed", width: 32,
						from: "internal/go_language/check"},
					{test: "TestARecordWrittenThroughWhoseHostRecordMovedIsAMiss", width: 33,
						from: "internal/go_language/check"},
				}},
				{site: "load.go:loadUnder#1", claims: []raceClaim{
					// bounded.Beside starts one goroutine and returns at once, so its caller goes on beside it: two
					// live, the census's one started there and the load returning.
					{test: "TestALoadReturnsBeforeItsInputsAreCollected", width: 2},
					{test: "TestAPanicCollectingALoadsInputsReachesItsWaiter", width: 2},
				}},
				{site: "loader.go:loader.load#2", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 2},
					{test: "TestAGoCommandThatCannotRunAsideSaysItMayRewriteTheSum", width: 2},
				}},
				{site: "loader.go:loader.parseClaimed#1", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 2},
					{test: "TestAGoCommandThatCannotRunAsideSaysItMayRewriteTheSum", width: 2},
				}},
				{site: "loader.go:loader.parseClaimed#2", claims: []raceClaim{
					{test: "TestADeclinedRestoreLeavesItsImportsScopesAsChecked", width: 2},
					{test: "TestAGoCommandThatCannotRunAsideSaysItMayRewriteTheSum", width: 2},
				}},
			},
		},
	},
	{
		directory: "internal/go_language/rules/marker",
		reason: "TestAnIndexAskedAtOnceAnswersAsOneBuiltUpFront asks one " +
			"file's lazily built comment index from several goroutines, as rules and a package's shards do (#fex6s0v)",
	},
	{directory: "internal/go_language/suppression", reason: "TestBuildConcurrently builds suppression tables from " +
		"several goroutines"},
	{directory: "internal/lint/rules/typescript", reason: "TestNoDupeClassMembersAndACheckerReadTheClassAtOnce runs " +
		"the rule beside a checker's read of the class it lints (no_dupe_class_members_concurrency_test.go, " +
		"#72kn826)"},
	{
		directory: "internal/release/dispatch",
		reason: "TestInstallingTheSwiftBinaryConcurrentlyLeavesOneBinary installs " +
			"from two runs at once",
	},
	{
		directory: "internal/testing/executable_testing",
		reason: "TestAScriptWrittenWhileOthersForkRunsAtOnce writes and runs " +
			"scripts while other goroutines fork (#g93ts58)",
	},
	{directory: "internal/types/program", reason: "programs check, parse libraries and record their run cache at " +
		"once, and every rule walks a file while a checker and a reader read its tree (checker_concurrency_test.go, " +
		"library_parses_test.go, run_cache_test.go, rule_walk_concurrency_test.go, #qb5xxj1)"},
}

// raceRunSeparator divides a race token's directory from its run, as racePatterns writes it and the gate's scripts
// read it.
const raceRunSeparator = "@"

// racePatterns is the race tokens of the raceGated packages that exist in the tree at root, so a tree that holds none
// of them, a fixture's, runs no -race at all. A token is `./<directory>`, a package whose tests all run, or
// `./<directory>@<run>`, one whose -race run is only the tests its run selects. The gate's scripts take them
// comma-separated and run the plain ones in one go test, and each with a run in a go test of its own after it.
func racePatterns(root string) []string {
	patterns := []string{}
	for _, entry := range raceGated {
		if information, statError := os.Stat(filepath.Join(root, entry.directory)); statError == nil &&
			information.IsDir() {
			token := "./" + entry.directory
			if run := entry.keptRun(); run != "" {
				token += raceRunSeparator + run
			}
			patterns = append(patterns, token)
		}
	}
	return patterns
}
