import Foundation
import Testing

@testable import CohereSwift

/*
 A run at an Xcode root, end to end (#ybspptb, block A): an xcodegen spec is generated into the engine's scratch,
 its project file is read for the target's files, and those files are linted and formatted like a package's. The
 types phase is a named gap until block B builds the project, so a clean run here is still incomplete, never green.

 The fixture is an xcodegen spec for an iOS app, the shape of ahraos-presence's CapturePhone: a sources folder the
 target compiles, a target Info.plist the spec asks xcodegen to write, and a script beside them that no target
 compiles. A `.git` directory makes it a repository, so the run reads ignore rules and lists unclaimed files.

 These need xcodegen. Without it they fail and say so, since a skipped test reads like a passing one.
 */
@Suite
struct XcodeProjectPipelineTests {
    static let specification = """
        name: Fixture
        options:
          bundleIdPrefix: com.cohere
        targets:
          App:
            type: application
            platform: iOS
            deploymentTarget: "18.0"
            sources:
              - Sources
            info:
              path: Info.plist
              properties:
                CFBundleDisplayName: Fixture
            settings:
              base:
                SWIFT_VERSION: "6.0"

        """

    static let cleanSource = """
        struct Greeter {
            let name: String

            func greeting() -> String {
                "Hello, \\(name)"
            }
        }

        """

    /* Line 3 holds the force unwrap the lint phase must find. */
    static let dirtySource = """
        struct Counter {
            func parsed(_ text: String) -> Int {
                Int(text)! * 2
            }
        }

        """

    /* What one run said, by kind, in order. */
    struct Run {
        var exitCode: Int32
        var records: [[String: Any]]

        func of(_ kind: String) -> [[String: Any]] {
            records.filter { $0["kind"] as? String == kind }
        }

        func phase(_ name: String) -> (outcome: String, detail: String)? {
            of("phase").first { $0["name"] as? String == name }.map {
                ($0["outcome"] as? String ?? "", $0["detail"] as? String ?? "")
            }
        }

        var findings: [String] {
            of("finding").map { "\($0["rule"] as? String ?? ""):\($0["line"] as? Int ?? 0)" }
        }
    }

    /* A repository holding the app's folder; the returned URL is the app's folder, the run's root. */
    static func fixture(dirty: Bool = true) throws -> (repository: URL, root: URL) {
        let repository = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-xcode-\(UUID().uuidString)",
            isDirectory: true,
        )
        let root = repository.appendingPathComponent("App", isDirectory: true)
        let files = [
            "project.yml": specification,
            "Sources/Greeter.swift": cleanSource,
            "Sources/Counter.swift": dirty
                ? dirtySource : cleanSource.replacingOccurrences(of: "Greeter", with: "Counter"),
            "Tools/Script.swift": "print(\"a script no target compiles\")\n",
            ".gitignore": "Info.plist\n*.xcodeproj\n",
        ]
        for (path, text) in files {
            let file = root.appendingPathComponent(path)
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try text.write(to: file, atomically: true, encoding: .utf8)
        }
        try FileManager.default.createDirectory(
            at: repository.appendingPathComponent(".git", isDirectory: true),
            withIntermediateDirectories: true,
        )
        return (repository, root)
    }

    static func remove(_ directory: URL) {
        do {
            try FileManager.default.removeItem(at: directory)
        }
        catch {
            /* A leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
        }
    }

    static func requireXcodegen() throws {
        let result = try ProcessRunner().run("xcodegen", ["--version"], in: FileManager.default.temporaryDirectory)
        try #require(
            result.succeeded,
            "xcodegen is not installed, so an Xcode root cannot be generated and these tests check nothing",
        )
    }

    static func run(root: URL, arguments: [String]) async throws -> Run {
        let options = try CommandOptions.parse(
            ["--contract", "\(EngineVersion.contract)", "--root", root.path] + arguments,
            workingDirectory: root,
        )
        var lines = Data()
        let exitCode = try await Pipeline(
            options: options,
            writer: ContractWriter { lines.append($0) },
            workingDirectory: root,
        )
        .run()
        let records = try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }
        return Run(exitCode: exitCode, records: records)
    }

    /* Every file under `directory`, by path relative to it, with its bytes; directories by path alone. */
    static func snapshot(of directory: URL) throws -> [String: Data] {
        var files: [String: Data] = [:]
        let base = directory.resolvingSymlinksInPath().path
        guard let walker = FileManager.default.enumerator(at: directory, includingPropertiesForKeys: [.isDirectoryKey])
        else { return files }
        for case let url as URL in walker {
            let path = String(url.resolvingSymlinksInPath().path.dropFirst(base.count))
            let isDirectory = try url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory ?? false
            files[path] = isDirectory ? Data() : try Data(contentsOf: url)
        }
        return files
    }

    @Test func aForceUnwrapInAnXcodeTargetIsFoundAndTheMissingTypesAreAGap() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings == ["cohere-swift/force-unwrapping:3"])
        let project = try #require(run.of("project").first)
        #expect(project["projectKind"] as? String == "XcodeGen")
        #expect(project["package"] as? String == "Fixture")
        #expect(project["filesOurs"] as? Int == 2)
        let targets = project["targets"] as? [[String: Any]] ?? []
        #expect(
            targets.map { "\($0["name"] ?? ""):\($0["kind"] ?? ""):\($0["files"] ?? ""):\($0["languageMode"] ?? "")" }
                == [
                    "App:application:2:6"
                ]
        )
        let excluded = (project["excluded"] as? [[String: Any]] ?? []).map {
            "\(($0["file"] as? String ?? "").hasSuffix("/Tools/Script.swift")):\($0["reason"] ?? "")"
        }
        #expect(excluded == ["true:no target of this package compiles it"], "the script beside the target is named")
        #expect(run.phase("types")?.outcome == "skipped")
        #expect(run.phase("types")?.detail == Pipeline.xcodeTypesNotBuilt, "a gap, never `not requested`")
        #expect(run.phase("lint")?.outcome == "ran")
        let summary = try #require(run.of("summary").first)
        #expect(summary["complete"] as? Bool == false)
        #expect(summary["exitCode"] as? Int == 1)
    }

    /* Clean code at an Xcode root is still not a green run, because nothing checked its types. */
    @Test func aCleanXcodeRootIsIncompleteNotGreen() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(dirty: false)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty)
        #expect(run.exitCode == 1)
        #expect(run.of("summary").first?["complete"] as? Bool == false)
    }

    /*
     @system_cohere's invariant for block A: a check writes nothing into the user's tree. Under `--no-cache` the
     engine's scratch is outside the project, so the whole tree must be byte for byte what it was. Without it the
     scratch is the project's own `.cache/cohere/swift`, the engine's cache as it is for every package, and
     everything else must be untouched.
     */
    @Test func aCheckWritesNothingIntoTheTree() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }
        let before = try Self.snapshot(of: fixture.repository)

        _ = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        let afterFreshScratch = try Self.snapshot(of: fixture.repository)
        #expect(
            afterFreshScratch == before,
            "written under --no-cache: \(Set(afterFreshScratch.keys).symmetricDifference(before.keys).sorted())",
        )

        _ = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        let cache = "/App/.cache/cohere/"
        let afterCachedRun = try Self.snapshot(of: fixture.repository).filter { !$0.key.hasPrefix(cache) }
        #expect(
            afterCachedRun.filter { $0.key != "/App/.cache" && $0.key != "/App/.cache/cohere" } == before,
            "written outside the engine's cache: \(Set(afterCachedRun.keys).symmetricDifference(before.keys).sorted())",
        )
        #expect(
            FileManager.default.fileExists(
                atPath: fixture.root.appendingPathComponent(".cache/cohere/swift/xcode").path
            ),
            "the generated project went somewhere other than the engine's scratch, so the test above looked in the wrong place",
        )
    }

    /* A committed project is read where it stands, with no xcodegen run. */
    @Test func aCommittedProjectIsReadWhereItStands() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }
        let generated = try ProcessRunner().run("xcodegen", ["generate", "--quiet"], in: fixture.root)
        try #require(generated.succeeded, "the fixture's project could not be made: \(generated.standardError)")
        try FileManager.default.removeItem(at: fixture.root.appendingPathComponent("project.yml"))

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.of("project").first?["projectKind"] as? String == "XcodeProject")
        #expect(run.findings == ["cohere-swift/force-unwrapping:3"])
    }

    @Test func twoProjectsAtOneRootAreRefusedByName() async throws {
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }
        try FileManager.default.removeItem(at: fixture.root.appendingPathComponent("project.yml"))
        for name in ["One", "Two"] {
            try FileManager.default.createDirectory(
                at: fixture.root.appendingPathComponent("\(name).xcodeproj", isDirectory: true),
                withIntermediateDirectories: true,
            )
        }
        await #expect {
            try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        } throws: { error in
            "\(error)".contains("One.xcodeproj, Two.xcodeproj")
        }
    }

    @Test func coveredPackagesAtAnXcodeRootNamesTheRootAlone() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--covered-packages", "--no-cache"])

        #expect(run.records.map { $0["kind"] as? String } == ["provenance", "coveredPackages"])
        let covered = try #require(run.of("coveredPackages").first)
        #expect(covered["checked"] as? [String] == [fixture.root.standardizedFileURL.path])
        #expect(covered["vendored"] as? [String] == [])
    }
}
