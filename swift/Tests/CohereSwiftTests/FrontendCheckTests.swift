import Foundation
import Testing

@testable import CohereSwift

/*
 The check runs the driver's planned frontend jobs as `-emit-sil` with a record and an index unit per primary, reruns
 a failed batch one primary at a time, and names the file and module a crash happened in. Serialized, because the
 tests that reach the toolchain each start a build server and a pool of frontends.
 */
@Suite(.serialized)
struct FrontendCheckTests {
    static func check(root: URL = URL(fileURLWithPath: "/check")) -> FrontendCheck {
        FrontendCheck(
            swiftc: "/usr/bin/swiftc",
            diagnosticsRoot: root,
            indexStorePath: URL(fileURLWithPath: "/store"),
            workingDirectory: URL(fileURLWithPath: "/"),
        )
    }

    static let batch = FrontendCheck.Job(
        module: "Feature",
        executable: "/usr/bin/swift-frontend",
        plannedArguments: [
            "-frontend", "-typecheck", "-primary-file", "/package/Count.swift", "-primary-file",
            "/package/Welcome.swift", "/package/Other.swift", "-serialize-diagnostics-path", "/tmp/Count-1.dia",
            "-serialize-diagnostics-path", "/tmp/Welcome-1.dia", "-module-name", "Feature",
        ],
        primaries: ["/package/Count.swift", "/package/Welcome.swift"],
    )

    /* The driver's words come back as a shell would split them: quoted spaces, `'\''` and backslashes. */
    @Test func aPrintedJobSplitsAsAShellSplitsIt() {
        let line = #"/usr/bin/swift-frontend -frontend -typecheck 'a b.swift' 'it'\''s' x\ y '/p#/q'"#
        #expect(
            FrontendCheck.shellWords(line) == [
                "/usr/bin/swift-frontend", "-frontend", "-typecheck", "a b.swift", "it's", "x y", "/p#/q",
            ]
        )
    }

    /*
     The rewrite: `-emit-sil` for `-typecheck`, the driver's records dropped, and per primary in the driver's order `SIL`
     to /dev/null, a record under the module and an index unit, then the store.
     */
    @Test func aPlannedJobIsRewrittenToEmitSilWithARecordAndUnitPerPrimary() {
        let arguments = Self.check().frontendArguments(of: Self.batch)
        #expect(!arguments.contains("-typecheck"))
        #expect(!arguments.contains("/tmp/Count-1.dia"))
        #expect(
            arguments == [
                "-frontend", "-emit-sil", "-primary-file", "/package/Count.swift", "-primary-file",
                "/package/Welcome.swift", "/package/Other.swift", "-module-name", "Feature", "-o", "/dev/null", "-o",
                "/dev/null", "-serialize-diagnostics-path", "/check/Feature.build/Objects-normal/check/Count.dia",
                "-serialize-diagnostics-path", "/check/Feature.build/Objects-normal/check/Welcome.dia",
                "-index-unit-output-path", "/Feature/Count.o", "-index-unit-output-path", "/Feature/Welcome.o",
                "-index-store-path", "/store", "-index-system-modules",
            ]
        )
    }

    /* A batch narrowed to one primary keeps every other file as a plain input. */
    @Test func aNarrowedJobKeepsItsMatesAsInputs() {
        let narrowed = Self.batch.narrowed(to: "/package/Welcome.swift")
        #expect(narrowed.primaries == ["/package/Welcome.swift"])
        #expect(
            Array(narrowed.plannedArguments.prefix(6)) == [
                "-frontend", "-typecheck", "/package/Count.swift", "-primary-file", "/package/Welcome.swift",
                "/package/Other.swift",
            ]
        )
    }

    /* Prepare's flags and every output and planning flag the server's arguments carry are gone before planning. */
    @Test func theServersArgumentsAreCleanedOfPrepareAndOutputs() {
        let cleaned = FrontendCheck.cleaned([
            "-module-name", "Core", "-Xfrontend", "-experimental-allow-module-with-compiler-errors", "-Xfrontend",
            "-empty-abi-descriptor", "-Xfrontend", "-serialize-debugging-options", "-j16", "-index-unit-output-path",
            "/Core.o", "/package/Greeting.swift",
        ])
        #expect(
            cleaned == [
                "-module-name", "Core", "-Xfrontend", "-serialize-debugging-options", "/package/Greeting.swift",
            ]
        )
    }

    /*
     A frontend a signal ends is a crash naming its file and module: a batch's crash is pinned to each file by rerunning
     them alone. `/bin/sh` stands in for the frontend, killing itself the way a crashing compiler dies.
     */
    @Test func aSignalCrashNamesTheFileAndModule() throws {
        let root = CheckFixture.scratch()
        var crashing = Self.batch
        crashing.executable = "/bin/sh"
        crashing.plannedArguments = ["-c", "kill -SEGV $$"] + crashing.plannedArguments
        let outcome = try Self.check(root: root).run([crashing])
        #expect(outcome.rerunPrimaries == ["/package/Count.swift", "/package/Welcome.swift"])
        #expect(
            outcome.crashes.map { "\($0.module) \($0.file) \($0.signal)" } == [
                "Feature /package/Count.swift 11", "Feature /package/Welcome.swift 11",
            ]
        )
        #expect(outcome.failedJobs.isEmpty)
        #expect(
            outcome.crashes.first?.description.hasPrefix(
                "the compiler crashed with signal 11 checking /package/Count.swift in module Feature"
            ) == true
        )
    }

    /* A job that exits with an error is a failed job, not a crash, even when its status is a signal's number. */
    @Test func anExitIsNotACrash() throws {
        var failing = Self.batch.narrowed(to: "/package/Count.swift")
        failing.executable = "/bin/sh"
        failing.plannedArguments = ["-c", "exit 11"] + failing.plannedArguments
        let outcome = try Self.check(root: CheckFixture.scratch()).run([failing])
        #expect(outcome.crashes.isEmpty)
        #expect(outcome.rerunPrimaries.isEmpty)
        #expect(outcome.failedJobs.map(\.exitCode) == [11])
    }

    /*
     The batch rerun. The test target is checked as one batch holding a type error and six `SIL`-stage errors and
     warnings. The type error stops the frontend before `SIL` for the whole batch, so the six come back only because
     each of the batch's primaries is rerun alone; a check that skips the rerun reports only the type error.
     */
    @Test func aFailedBatchRerunsItsMatesOnePrimaryAtATime() throws {
        let root = try CheckFixture.package(plants: CheckFixture.swiftPlants)
        let checked = try CheckFixture.check(root, scratch: CheckFixture.scratch(), batchSize: 100, parallelism: 1)
        let testJobs = checked.swift.records.filter { $0.module == "FeatureTests" }
        #expect(testJobs.count == 9, "the test target's nine files, the generated entry point among them")
        #expect(Set(checked.swift.rerunPrimaries) == Set(testJobs.map(\.file)))
        #expect(checked.swift.crashes.isEmpty)

        let diagnostics = try CheckFixture.diagnostics(root, directories: checked.targetDirectories)
        for plant in CheckFixture.swiftPlants {
            #expect(
                diagnostics.byFile[plant.path, default: []].contains { $0.contains(plant.message) },
                "\(plant.path) lost its \"\(plant.message)\": \(diagnostics.byFile[plant.path, default: []])",
            )
        }
    }

    /*
     The index the check writes holds what a build's holds, references inside bodies included: every occurrence of
     Welcome.swift, compared with the store a `swift build` of the same package writes.
     */
    @Test func theIndexHoldsWhatABuildsHolds() throws {
        let root = try CheckFixture.package()
        let checked = try CheckFixture.check(root, scratch: CheckFixture.scratch())
        let buildScratch = CheckFixture.scratch()
        let build = try CheckFixture.build(root, scratch: buildScratch)
        try #require(build.succeeded, "the fixture does not build: \(build.standardError)")

        let welcome = root.appendingPathComponent("Sources/Feature/Welcome.swift")
        let checkedOccurrences = try CheckFixture.occurrences(
            of: welcome,
            storePath: checked.description.indexStorePath,
        )
        let builtOccurrences = try CheckFixture.occurrences(
            of: welcome,
            storePath: buildScratch.appendingPathComponent("out", isDirectory: true),
        )
        #expect(checkedOccurrences == builtOccurrences)
        /* `greeting.text()` on line 5, inside `welcome`'s body: a reference only a full check records. */
        #expect(
            checkedOccurrences.contains {
                $0.hasPrefix("5:21 ") && $0.hasSuffix(" text()")
                    && (UInt64($0.split(separator: " ")[1]) ?? 0) & IndexStore.referenceRole != 0
            },
            "no reference to text() inside welcome's body: \(checkedOccurrences)",
        )
    }
}
