import Foundation
import Testing

@testable import CohereSwift

/*
 Reusing the last build's compiler records, on one real package across several runs, so the build cache is
 warm the way a developer's is.

 A reuse is proven by the types record's own words, and every change a build would see must end it: a planted
 type error after a warm run must be found (the failure a cache that never invalidates would hide), and a
 record removed from the scratch must mean building, never a run that comes back incomplete.

 Not serialized: each test writes its own UUID-named package, the build cache is keyed by the package's path,
 and the process-wide state is two Mutex-guarded singletons that parallel callers queue on. In series this
 suite took 57s; in parallel, 7s, green in every round measured (#7r8mct7).
 */
@Suite
struct BuildInputSnapshotTests {
    static let reusedWords = "no input changed since the last successful build"

    final class Package {
        let root: URL
        let sources: URL

        init() throws {
            root = FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-build-\(UUID().uuidString)",
                isDirectory: true,
            )
            sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
            try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
            try PipelineControlTests.manifest.write(
                to: root.appendingPathComponent("Package.swift"),
                atomically: true,
                encoding: .utf8,
            )
            try write(PipelineControlTests.cleanSource)
        }

        func write(_ source: String) throws {
            try source.write(to: sources.appendingPathComponent("Control.swift"), atomically: true, encoding: .utf8)
        }

        /* One `--no-fix` run: the types record's `build` words, and the compiler findings. */
        func run() async throws -> (build: String, compilerFindings: [String]) {
            let options = try CommandOptions.parse(
                ["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix"],
                workingDirectory: root,
            )
            var lines = Data()
            let writer = ContractWriter { lines.append($0) }
            _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
            var build = ""
            var compiler: [String] = []
            for line in lines.split(separator: UInt8(ascii: "\n")) {
                let record = try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
                if record["kind"] as? String == "types" {
                    build = record["build"] as? String ?? ""
                }
                if record["kind"] as? String == "finding", record["source"] as? String == "compiler" {
                    compiler.append("\(record["line"] as? Int ?? 0): \(record["message"] as? String ?? "")")
                }
            }
            return (build, compiler)
        }
    }

    @Test func aRunWithNothingChangedReadsTheLastBuildWithoutBuilding() async throws {
        let package = try Package()
        let first = try await package.run()
        #expect(!first.build.contains(Self.reusedWords), "a first run has nothing to reuse")
        let second = try await package.run()
        #expect(second.build.contains(Self.reusedWords), "the second run built again: \(second.build)")
        #expect(second.compilerFindings == first.compilerFindings)
    }

    /* The test that matters: after a warm run, a planted type error must be found. */
    @Test func aChangeAfterAWarmRunIsFound() async throws {
        let package = try Package()
        _ = try await package.run()
        _ = try await package.run()
        try package.write(
            PipelineControlTests.cleanSource.replacingOccurrences(
                of: "        value * 2",
                with: "        let text: Int = \"two\"\n        return value * text",
            )
        )
        let changed = try await package.run()
        #expect(!changed.build.contains(Self.reusedWords))
        #expect(
            changed.compilerFindings.contains { $0.hasPrefix("5: cannot convert") },
            "the planted error was not found: \(changed.compilerFindings)",
        )
    }

    /* A failed build is never the one reused: fixing the error after it must build again and come back clean. */
    @Test func aFailedBuildIsNeverReused() async throws {
        let package = try Package()
        try package.write(
            PipelineControlTests.cleanSource.replacingOccurrences(
                of: "        value * 2",
                with: "        let text: Int = \"two\"\n        return value * text",
            )
        )
        _ = try await package.run()
        let again = try await package.run()
        #expect(!again.build.contains(Self.reusedWords), "a failed build's records were reused")
        #expect(!again.compilerFindings.isEmpty)
    }

    @Test func aManifestEditBuilds() async throws {
        let package = try Package()
        _ = try await package.run()
        try (PipelineControlTests.manifest + "// edited\n").write(
            to: package.root.appendingPathComponent("Package.swift"),
            atomically: true,
            encoding: .utf8,
        )
        #expect(!(try await package.run().build.contains(Self.reusedWords)))
    }

    /* A record gone from the scratch means building, not a run that reports a file without a record. */
    @Test func aMissingRecordBuildsInsteadOfComingBackIncomplete() async throws {
        let package = try Package()
        _ = try await package.run()
        let scratch = Pipeline.scratchPath(for: package.root)
        let records =
            FileManager.default.enumerator(at: scratch, includingPropertiesForKeys: nil)?
            .compactMap { $0 as? URL }
            .filter { $0.pathExtension == "dia" && $0.lastPathComponent.hasPrefix("Control") } ?? []
        #expect(!records.isEmpty, "found no record to remove, so this test would prove nothing")
        for record in records {
            try FileManager.default.removeItem(at: record)
        }
        let after = try await package.run()
        #expect(!after.build.contains(Self.reusedWords))
    }

    /* A record libclang cannot read must not end the run; it is dropped, and the build that follows writes a good one. */
    @Test func anUnreadableRecordIsRebuiltNotFatal() async throws {
        let package = try Package()
        let first = try await package.run()
        let scratch = Pipeline.scratchPath(for: package.root)
        let records =
            FileManager.default.enumerator(at: scratch, includingPropertiesForKeys: nil)?
            .compactMap { $0 as? URL }
            .filter { $0.pathExtension == "dia" && $0.lastPathComponent.hasPrefix("Control") } ?? []
        #expect(!records.isEmpty, "found no record to corrupt, so this test would prove nothing")
        for record in records {
            try Data("not a serialized diagnostics file".utf8).write(to: record)
        }
        let after = try await package.run()
        #expect(!after.build.contains(Self.reusedWords), "a corrupt record was reused")
        #expect(after.compilerFindings == first.compilerFindings)
    }

    @Test func theSnapshotSeesSizeAndTime() throws {
        let package = try Package()
        let take = {
            BuildInputSnapshot.take(
                roots: [package.root],
                resolved: package.root.appendingPathComponent("Package.resolved"),
                toolchain: "t",
                arguments: ["a"],
            )
        }
        let first = take()
        #expect(take() == first, "two snapshots of an unchanged tree differ")
        try package.write(PipelineControlTests.cleanSource + "// longer\n")
        #expect(take() != first)
        #expect(
            BuildInputSnapshot.take(
                roots: [package.root],
                resolved: package.root.appendingPathComponent("Package.resolved"),
                toolchain: "u",
                arguments: ["a"],
            ) != take()
        )
    }

    /* Bytes, not time, decide a restored file: the same bytes rewritten are restored, different bytes are edited. */
    @Test func aFileRewrittenWithTheSameBytesIsRestored() throws {
        let package = try Package()
        let control = package.sources.appendingPathComponent("Control.swift").resolvingSymlinksInPath()
        let take = {
            BuildInputSnapshot.take(
                roots: [package.root],
                resolved: package.root.appendingPathComponent("Package.resolved"),
                toolchain: "t",
                arguments: ["a"],
            )
        }
        var built = take()
        built.contents = [control.path: PackageDescriptionCache.fingerprint(of: control)]
        let builtModified = try #require(
            try control.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
        )
        try FileManager.default.setAttributes(
            [.modificationDate: Date(timeIntervalSinceNow: 60)],
            ofItemAtPath: control.path,
        )
        let rewritten = try #require(take().changes(since: built))
        #expect(rewritten.edited.isEmpty, "\(rewritten.edited)")
        #expect(rewritten.restored.keys.sorted() == [control.path])
        #expect(
            abs((rewritten.restored[control.path] ?? .distantPast).timeIntervalSince(builtModified)) < 0.001,
            "a restored file is not judged by the time the build saw",
        )
        try package.write(PipelineControlTests.cleanSource.replacingOccurrences(of: "* 2", with: "* 3"))
        let edited = try #require(take().changes(since: built))
        #expect(edited.edited.count == 1)
        #expect(edited.restored.isEmpty)
    }

    static let plantedError = PipelineControlTests.cleanSource.replacingOccurrences(
        of: "        value * 2",
        with: "        let text: Int = \"two\"\n        return value * text",
    )

    static let replayedWords = "answered by an earlier run's check of the same text"

    /*
     An edit inside a function body is checked by sourcekitd in this process, never built, and its error is
     found. It stays found on the run after, answered by that check since the text did not change (the stored
     snapshot is still the last build's, so the edit is never read from a record that predates it), and fixing
     it inside the body is asked about again and comes back clean.
     */
    @Test func aBodyOnlyEditIsCheckedInProcessAndFound() async throws {
        let package = try Package()
        _ = try await package.run()
        try package.write(Self.plantedError)
        let planted = try await package.run()
        #expect(
            planted.build.contains(TypesPhase.checkedInProcessWords),
            "a body-only edit was built: \(planted.build)",
        )
        #expect(!planted.build.contains(Self.replayedWords), "a first check replayed an answer: \(planted.build)")
        #expect(
            planted.compilerFindings.contains { $0.hasPrefix("5: cannot convert") },
            "the planted error was not found: \(planted.compilerFindings)",
        )
        let again = try await package.run()
        #expect(again.build.contains(Self.replayedWords), "an unchanged edit was asked about again: \(again.build)")
        #expect(again.compilerFindings == planted.compilerFindings)
        try package.write(PipelineControlTests.cleanSource + "\n")
        let fixed = try await package.run()
        #expect(fixed.build.contains(TypesPhase.checkedInProcessWords))
        #expect(!fixed.build.contains(Self.replayedWords), "a changed text replayed its old answer: \(fixed.build)")
        #expect(fixed.compilerFindings.isEmpty, "the fix was not seen: \(fixed.compilerFindings)")
    }

    /* A body that keeps a `var` it never mutates, so the build records a warning against this file. */
    static let warnedSource = PipelineControlTests.cleanSource.replacingOccurrences(
        of: "        value * 2",
        with: "        var twice = value * 2\n        return twice",
    )

    /*
     An edit undone, the file's bytes again the ones the last build compiled though its time moved, is
     unchanged: the build's records are read again, its warning about this very file among them, judged
     against the time the build saw the file at. Before, the time alone decided, so every run after the revert
     checked the file in sourcekitd until the next build.
     */
    @Test func anEditUndoneReadsTheBuildsRecordsAgain() async throws {
        let package = try Package()
        try package.write(Self.warnedSource)
        let built = try await package.run()
        #expect(
            built.compilerFindings.contains { $0.contains("never mutated") },
            "the build recorded no warning, so this test would prove nothing: \(built.compilerFindings)",
        )
        try package.write(Self.plantedError)
        let planted = try await package.run()
        #expect(planted.build.contains(TypesPhase.checkedInProcessWords))
        #expect(planted.compilerFindings.contains { $0.hasPrefix("5: cannot convert") })
        try package.write(Self.warnedSource)
        let undone = try await package.run()
        #expect(undone.build.contains(Self.reusedWords), "an undone edit was not read from the build: \(undone.build)")
        #expect(undone.build.contains("again the bytes it compiled"), "\(undone.build)")
        #expect(undone.compilerFindings == built.compilerFindings)
    }

    /* The builds these tests run plan their modules explicitly, so the in-process checks above use the build's own modules. */
    @Test func theBuildsCommandsCarryTheModulesItCompiled() async throws {
        let package = try Package()
        _ = try await package.run()
        let commands = try CompileCommands.read(
            layout: .swiftPackage(scratchPath: Pipeline.scratchPath(for: package.root))
        )
        let command = try #require(commands.command(for: package.sources.appendingPathComponent("Control.swift")))
        #expect(command.arguments.contains("-explicit-swift-module-map-file"), "\(command.arguments)")
        #expect(command.arguments.contains("-disable-implicit-swift-modules"))
    }

    /* An edit another file could see, a signature here, is never checked alone: it builds. */
    @Test func anInterfaceEditBuilds() async throws {
        let package = try Package()
        _ = try await package.run()
        try package.write(
            PipelineControlTests.cleanSource.replacingOccurrences(
                of: "func doubled() -> Int {\n        value * 2",
                with: "func doubled() -> Double {\n        Double(value) * 2",
            )
        )
        let edited = try await package.run()
        #expect(
            !edited.build.contains(TypesPhase.checkedInProcessWords),
            "an interface edit was checked in process: \(edited.build)",
        )
        #expect(!edited.build.contains(Self.reusedWords))
        #expect(edited.compilerFindings.isEmpty)
    }

    /* A file added is a new file list, which only a build knows how to compile. */
    @Test func aNewFileBuilds() async throws {
        let package = try Package()
        _ = try await package.run()
        try "struct Added {}\n".write(
            to: package.sources.appendingPathComponent("Added.swift"),
            atomically: true,
            encoding: .utf8,
        )
        let added = try await package.run()
        #expect(!added.build.contains(TypesPhase.checkedInProcessWords))
        #expect(!added.build.contains(Self.reusedWords))
    }
}
