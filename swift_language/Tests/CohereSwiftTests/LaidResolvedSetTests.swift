import Foundation
import Synchronization
import Testing

@testable import CohereSwift

/*
 The laid resolved set `COHERE_SWIFT_RESOLVED` names (#je1r32a), as both of cohere-swift's SwiftPM calls take it: the
 describe and dump-package `PackageModel` runs, and the build server `BuildServerClient` starts. Each test makes its
 own set, root and scratch in folders of its own, and hands its own environment, so none reads the process's.
 */
@Suite
struct LaidResolvedSetTests {
    static let resolved = #"{ "originHash" : "laid", "pins" : [ ], "version" : 3 }"#

    /* A folder of this test's own, resolved, so the paths compared are the ones the code is handed. */
    static func folder(_ name: String) throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-laid-set-\(name)-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder.resolvingSymlinksInPath()
    }

    static func write(_ text: String, _ relativePath: String, under folder: URL) throws {
        let file = folder.appendingPathComponent(relativePath)
        try FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try text.write(to: file, atomically: true, encoding: .utf8)
    }

    /*
     A set laid as Shackler lays one: its Package.resolved, and a scratch with a checkout, a repository and a state,
     every file and folder of the scratch without write permission, since the set is read-only.
     */
    static func laidSet() throws -> URL {
        let set = try folder("set")
        try write(resolved, "Package.resolved", under: set)
        try write("laid\n", "scratch/checkouts/swift-example/marker", under: set)
        try write("ref: refs/heads/main\n", "scratch/repositories/swift-example-1a2b3c4d/HEAD", under: set)
        try write(#"{ "version" : 7, "laid" : true }"#, "scratch/workspace-state.json", under: set)
        let scratch = set.appendingPathComponent("scratch", isDirectory: true)
        let walk = FileManager.default.enumerator(atPath: scratch.path)?.allObjects as? [String] ?? []
        /* Deepest first, so a folder loses its write permission only after everything in it has. */
        for relative in walk.sorted(by: >) + [""] {
            let path = scratch.appendingPathComponent(relative).path
            let isFolder =
                (try FileManager.default.attributesOfItem(atPath: path)[.type] as? FileAttributeType) == .typeDirectory
            try FileManager.default.setAttributes([.posixPermissions: isFolder ? 0o555 : 0o444], ofItemAtPath: path)
        }
        return set
    }

    /* A package root, holding `resolved` as its Package.resolved, or none when nil. */
    static func root(resolved: String?) throws -> URL {
        let root = try folder("root")
        try write("// swift-tools-version: 6.2\n", "Package.swift", under: root)
        if let resolved {
            try write(resolved, "Package.resolved", under: root)
        }
        return root
    }

    static func environment(naming set: URL) -> [String: String] {
        ["PATH": "/usr/bin", LaidResolvedSet.variable: set.path]
    }

    static func describeArguments(root: URL, scratch: URL, _ laidResolvedSet: LaidResolvedSet) throws -> [String] {
        try PackageModel.packageCommandArguments(
            ["describe", "--type", "json"],
            root: root,
            scratchPath: scratch,
            laidResolvedSet: laidResolvedSet,
        )
    }

    static func serverArguments(root: URL, scratch: URL, _ laidResolvedSet: LaidResolvedSet) throws -> [String] {
        try BuildServerClient.launchArguments(
            .init(
                packageRoot: root,
                scratchPath: scratch,
                resolutionAllowed: false,
                laidResolvedSet: laidResolvedSet,
            )
        )
    }

    /* Unset, or set to nothing, every call is the one it was before the set existed, and nothing is written. */
    @Test func unsetLeavesBothCallsAsTheyWere() throws {
        let root = try Self.root(resolved: Self.resolved)
        let scratch = try Self.folder("scratch").appendingPathComponent("swift", isDirectory: true)
        for environment in [["PATH": "/usr/bin"], [LaidResolvedSet.variable: ""]] {
            let laidResolvedSet = LaidResolvedSet(environment: environment)
            #expect(
                try Self.describeArguments(root: root, scratch: scratch, laidResolvedSet) == [
                    "package", "--scratch-path", scratch.path, "describe", "--type", "json",
                ]
            )
            #expect(
                try PackageModel.packageCommandArguments(
                    ["dump-package"],
                    root: root,
                    scratchPath: scratch,
                    laidResolvedSet: laidResolvedSet,
                ) == ["package", "--scratch-path", scratch.path, "dump-package"]
            )
            #expect(
                try Self.serverArguments(root: root, scratch: scratch, laidResolvedSet) == [
                    "swift", "package", "--package-path", root.path, "--scratch-path", scratch.path,
                    "--disable-automatic-resolution", "experimental-build-server",
                ]
            )
        }
        #expect(!FileManager.default.fileExists(atPath: scratch.path))
    }

    /*
     Set, both calls start SwiftPM with its own sandbox off, a global option before the subcommand, whatever the root
     pins. A root pinning other versions, or none, takes nothing else: no `--skip-update` and nothing seeded.
     */
    @Test func setButNotMatchingDisablesTheSandboxAndSeedsNothing() throws {
        let laidResolvedSet = LaidResolvedSet(environment: Self.environment(naming: try Self.laidSet()))
        for rootResolved in [#"{ "originHash" : "other", "pins" : [ ], "version" : 3 }"#, nil] {
            let root = try Self.root(resolved: rootResolved)
            let scratch = try Self.folder("scratch").appendingPathComponent("swift", isDirectory: true)
            #expect(
                try Self.describeArguments(root: root, scratch: scratch, laidResolvedSet) == [
                    "package", "--scratch-path", scratch.path, "--disable-sandbox", "describe", "--type", "json",
                ]
            )
            #expect(
                try Self.serverArguments(root: root, scratch: scratch, laidResolvedSet) == [
                    "swift", "package", "--package-path", root.path, "--scratch-path", scratch.path,
                    "--disable-automatic-resolution", "--disable-sandbox", "experimental-build-server",
                ]
            )
            #expect(!FileManager.default.fileExists(atPath: scratch.appendingPathComponent("checkouts").path))
        }
    }

    /*
     Set, and the root pins exactly the set's bytes: both calls skip updating as well, and the first clones the set's
     scratch in, its checkouts and the state beside them, replacing a state an earlier run left. The second finds the
     checkouts and seeds nothing again, so what SwiftPM wrote since stays.
     */
    @Test func setAndMatchingSeedsTheScratchOnceAndSkipsUpdating() throws {
        let set = try Self.laidSet()
        let laidResolvedSet = LaidResolvedSet(environment: Self.environment(naming: set))
        let root = try Self.root(resolved: Self.resolved)
        let scratch = try Self.folder("scratch").appendingPathComponent("swift", isDirectory: true)
        try Self.write(#"{ "version" : 7, "earlier" : true }"#, "workspace-state.json", under: scratch)

        #expect(
            try Self.describeArguments(root: root, scratch: scratch, laidResolvedSet) == [
                "package", "--scratch-path", scratch.path, "--disable-sandbox", "--skip-update",
                "describe", "--type", "json",
            ]
        )
        let marker = scratch.appendingPathComponent("checkouts/swift-example/marker")
        #expect(try String(contentsOf: marker, encoding: .utf8) == "laid\n")
        #expect(
            try String(contentsOf: scratch.appendingPathComponent("workspace-state.json"), encoding: .utf8)
                == #"{ "version" : 7, "laid" : true }"#
        )
        #expect(
            FileManager.default.fileExists(
                atPath: scratch.appendingPathComponent("repositories/swift-example-1a2b3c4d/HEAD").path
            )
        )
        /* The set's modes say read-only; SwiftPM writes its state and its checkouts, so the clone is writable. */
        for written in ["workspace-state.json", "checkouts", "checkouts/swift-example", "repositories"] {
            #expect(FileManager.default.isWritableFile(atPath: scratch.appendingPathComponent(written).path))
        }

        try Self.write("written by SwiftPM since\n", "checkouts/swift-example/marker", under: scratch)
        #expect(
            try Self.serverArguments(root: root, scratch: scratch, laidResolvedSet) == [
                "swift", "package", "--package-path", root.path, "--scratch-path", scratch.path,
                "--disable-automatic-resolution", "--disable-sandbox", "--skip-update", "experimental-build-server",
            ]
        )
        #expect(try String(contentsOf: marker, encoding: .utf8) == "written by SwiftPM since\n")
        #expect(
            try String(
                contentsOf: set.appendingPathComponent("scratch/checkouts/swift-example/marker"),
                encoding: .utf8,
            )
                == "laid\n"
        )
    }

    /* Calls racing on one empty scratch, as a run's describe and build server can, seed it whole once, leaving nothing staged. */
    @Test func racingCallsSeedTheScratchOnce() throws {
        let laidResolvedSet = LaidResolvedSet(environment: Self.environment(naming: try Self.laidSet()))
        let root = try Self.root(resolved: Self.resolved)
        let scratch = try Self.folder("scratch").appendingPathComponent("swift", isDirectory: true)
        let failures = Mutex<[String]>([])
        DispatchQueue.concurrentPerform(iterations: 8) { _ in
            do {
                _ = try Self.serverArguments(root: root, scratch: scratch, laidResolvedSet)
            }
            catch {
                failures.withLock { $0.append("\(error)") }
            }
        }
        #expect(failures.withLock { $0 }.isEmpty)
        #expect(
            try String(contentsOf: scratch.appendingPathComponent("checkouts/swift-example/marker"), encoding: .utf8)
                == "laid\n"
        )
        let entries = try FileManager.default.contentsOfDirectory(atPath: scratch.path).sorted()
        #expect(entries == ["checkouts", "laid-resolved-set.lock", "repositories", "workspace-state.json"])
    }

    /*
     A set named but not there, or holding no Package.resolved, fails the run's first SwiftPM call before it starts,
     naming the path, from the package's load and from the build server's launch alike. Nothing falls back to the network.
     */
    @Test func aMissingSetFailsNamingItsPath() throws {
        let missing = try Self.folder("parent").appendingPathComponent("not-laid", isDirectory: true)
        let empty = try Self.folder("empty")
        let root = try Self.root(resolved: Self.resolved)
        let scratch = try Self.folder("scratch").appendingPathComponent("swift", isDirectory: true)
        for named in [missing, empty] {
            let laidResolvedSet = LaidResolvedSet(environment: Self.environment(naming: named))
            do {
                _ = try PackageModel.load(
                    root: root,
                    scratchPath: scratch,
                    runner: ProcessRunner(),
                    toolchain: nil,
                    laidResolvedSet: laidResolvedSet,
                )
                Issue.record("the package loaded with \(named.path) named")
            }
            catch let failure as LaidResolvedSet.Failure {
                #expect(failure.description.contains(named.path))
                #expect(failure.description.contains(LaidResolvedSet.variable))
            }
            do {
                _ = try BuildServerClient(
                    configuration: .init(
                        packageRoot: root,
                        scratchPath: scratch,
                        resolutionAllowed: false,
                        laidResolvedSet: laidResolvedSet,
                    )
                )
                Issue.record("the build server started with \(named.path) named")
            }
            catch {
                #expect(error.step == .launch)
                #expect(error.description.contains(named.path))
            }
        }
        #expect(!FileManager.default.fileExists(atPath: scratch.path))
    }
}
