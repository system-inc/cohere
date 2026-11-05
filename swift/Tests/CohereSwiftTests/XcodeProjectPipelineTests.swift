import Foundation
import Testing

@testable import CohereSwift

/*
 A run at an Xcode root, end to end (#ybspptb): an xcodegen spec is generated into the engine's scratch, its
 project file is read for the target's files, those files are linted and formatted like a package's (block A),
 and the project is built with xcodebuild into the scratch, whose compiler records are the types phase (block B,
 #gg0x95n).

 The fixture is an xcodegen spec for an iOS app, the shape of ahraos-presence's CapturePhone: a sources folder the
 target compiles, a target Info.plist the spec asks xcodegen to write, and a script beside them that no target
 compiles. A `.git` directory makes it a repository, so the run reads ignore rules and lists unclaimed files.

 These need xcodegen and Xcode with the iOS SDK. Without them they fail and say so, since a skipped test reads like
 a passing one. Serialized, because each runs an xcodebuild, and several at once on one machine only slow each other.
 */
@Suite(.serialized)
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

    /* Line 3 holds a type error, the probe the build was measured failing on. */
    static let typeErrorSource = """
        struct Counter {
            func doubled(_ value: Int) -> Int {
                let cohereProbe: Int = "not an int"
                return value * 2 + cohereProbe
            }
        }

        """

    /*
     Line 3 holds a result nobody uses, which the compiler warns about and lint does not. It holds no `=` and no `:`,
     so no typed rule asks for symbols, which an Xcode build does not give them until block C (#9ktapz2): a typed rule
     that applied would be a lint crash, and the run incomplete for a reason this file is not about.
     */
    static let warningSource = """
        struct Counter {
            func doubled() -> Int {
                "unused".uppercased()
                return 2
            }
        }

        """

    static let cleanCounter = cleanSource.replacingOccurrences(of: "Greeter", with: "Counter")

    /* An app's entry point, so the fixture links: without one, every build fails at the linker. */
    static let entrySource = """
        @main
        struct FixtureApp {
            static func main() {}
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

        /* Each compiler finding as `<file name>:<line>:<severity>`. */
        var compilerFindings: [String] {
            of("finding").filter { $0["source"] as? String == "compiler" }.map {
                let file = URL(fileURLWithPath: $0["file"] as? String ?? "").lastPathComponent
                return "\(file):\($0["line"] as? Int ?? 0):\($0["severity"] as? String ?? "")"
            }
        }

        var typesRecord: [String: Any] {
            of("types").first ?? [:]
        }

        var typesBuild: String {
            typesRecord["build"] as? String ?? ""
        }

        /* The file names the types record could not vouch for. */
        var filesWithoutRecord: [String] {
            (typesRecord["filesWithoutRecord"] as? [String] ?? []).map { URL(fileURLWithPath: $0).lastPathComponent }
        }

        var complete: Bool? {
            of("summary").first?["complete"] as? Bool
        }
    }

    /* A repository holding the app's folder; the returned URL is the app's folder, the run's root. */
    static func fixture(
        counter: String = dirtySource,
        specification: String = specification,
    ) throws -> (repository: URL, root: URL) {
        let repository = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-xcode-\(UUID().uuidString)",
            isDirectory: true,
        )
        let root = repository.appendingPathComponent("App", isDirectory: true)
        let files = [
            "project.yml": specification,
            "Sources/Greeter.swift": cleanSource,
            "Sources/Counter.swift": counter,
            "Sources/FixtureApp.swift": entrySource,
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

    /* The DerivedData folders under the home folder named for a project, which no check may create. */
    static func derivedDataFolders(named project: String) throws -> Set<String> {
        let derivedData = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(
            "Library/Developer/Xcode/DerivedData",
            isDirectory: true,
        )
        guard FileManager.default.fileExists(atPath: derivedData.path) else { return [] }
        return Set(
            try FileManager.default.contentsOfDirectory(atPath: derivedData.path).filter { $0.hasPrefix(project + "-") }
        )
    }

    /* The fixture's spec with its project named for this test alone, so a folder named for it anywhere is this test's. */
    static func specification(named project: String) -> String {
        specification.replacingOccurrences(of: "name: Fixture\n", with: "name: \(project)\n")
    }

    /* Clean code at an Xcode root, built and linted, is a complete run that exits 0. */
    @Test func aCleanXcodeRootIsCompleteAndGreen() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.cleanCounter)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty)
        #expect(run.phase("types")?.outcome == "ran")
        #expect(run.typesBuild.hasPrefix("xcodebuild of 1 targets for iOS, derived data "), "\(run.typesBuild)")
        #expect(run.typesRecord["files"] as? Int == 3)
        #expect(run.filesWithoutRecord.isEmpty)
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.complete == true, "\(run.records)")
        #expect(run.exitCode == 0)
    }

    /* A force unwrap compiles, so the build vouches for every file and lint finds it: complete, and exit 1 for the finding. */
    @Test func aForceUnwrapInAnXcodeTargetIsALintFindingBesideItsTypes() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings == ["cohere-swift/force-unwrapping:3"])
        let project = try #require(run.of("project").first)
        #expect(project["projectKind"] as? String == "XcodeGen")
        #expect(project["package"] as? String == "Fixture")
        #expect(project["filesOurs"] as? Int == 3)
        let targets = project["targets"] as? [[String: Any]] ?? []
        #expect(
            targets.map { "\($0["name"] ?? ""):\($0["kind"] ?? ""):\($0["files"] ?? ""):\($0["languageMode"] ?? "")" }
                == [
                    "App:application:3:6"
                ]
        )
        let excluded = (project["excluded"] as? [[String: Any]] ?? []).map {
            "\(($0["file"] as? String ?? "").hasSuffix("/Tools/Script.swift")):\($0["reason"] ?? "")"
        }
        #expect(excluded == ["true:no target of this package compiles it"], "the script beside the target is named")
        #expect(run.phase("types")?.outcome == "ran")
        #expect(run.filesWithoutRecord.isEmpty)
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.phase("unused")?.outcome == "skipped")
        #expect(run.complete == true)
        #expect(run.exitCode == 1)
    }

    /* A type error is the compiler's finding at its line, and lint is not reached, as for a package. */
    @Test func aTypeErrorIsACompilerFindingAndLintIsNotReached() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.typeErrorSource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["Counter.swift:3:error"], "\(run.typesBuild)")
        #expect(run.of("finding").count == 1, "the failed build is the type error's, not a second finding")
        #expect(run.typesBuild.contains("; the build of App failed: "), "\(run.typesBuild)")
        #expect(run.phase("lint")?.outcome == "notReached")
        #expect(run.complete == false)
        #expect(run.exitCode == 1)
    }

    /* A warning is a finding, but the code still means what it says, so lint runs. */
    @Test func aCompilerWarningIsAFindingAndLintStillRuns() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.warningSource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["Counter.swift:3:warning"])
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.complete == true)
        #expect(run.exitCode == 1)
    }

    /*
     The build-input skip: a run after a successful build, with nothing changed, reads that build's records without
     running xcodebuild, and finds what it found. A change after it is built: a type error planted then is found.
     */
    @Test func aWarmRunReusesTheLastBuildUntilAnInputChanges() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.warningSource)
        defer { Self.remove(fixture.repository) }

        let cold = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(cold.typesBuild.hasPrefix("xcodebuild of 1 targets"), "\(cold.typesBuild)")
        #expect(cold.compilerFindings == ["Counter.swift:3:warning"])

        let warm = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(
            warm.typesBuild
                == "no input changed since the last successful xcodebuild, so its compiler records were read without building"
        )
        #expect(warm.compilerFindings == ["Counter.swift:3:warning"])
        #expect(warm.complete == true)

        try Self.typeErrorSource.write(
            to: fixture.root.appendingPathComponent("Sources/Counter.swift"),
            atomically: true,
            encoding: .utf8,
        )
        let changed = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(changed.typesBuild.hasPrefix("xcodebuild of 1 targets"), "\(changed.typesBuild)")
        #expect(changed.compilerFindings == ["Counter.swift:3:error"])
    }

    /*
     A build that fails before compiling anything, here a package that `--no-fix` may not resolve, judges what the
     last build left by freshness. Greeter.swift is as that build compiled it, so its record stands. Counter.swift was
     edited since, out of the warning that build recorded, so the old warning is not reported and the file has no
     record. The failure is a finding at the spec, and the words name it.
     */
    @Test func aBuildThatFailsBeforeCompilingReadsOnlyFreshRecords() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.warningSource)
        defer { Self.remove(fixture.repository) }
        let first = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(first.compilerFindings == ["Counter.swift:3:warning"])

        let withPackage =
            Self.specification.replacingOccurrences(
                of: "targets:\n",
                with:
                    "packages:\n  Numerics:\n    url: https://github.com/apple/swift-numerics\n    exactVersion: 1.0.2\ntargets:\n",
            ) + "    dependencies:\n      - package: Numerics\n        product: RealModule\n"
        try withPackage.write(
            to: fixture.root.appendingPathComponent("project.yml"),
            atomically: true,
            encoding: .utf8,
        )
        try Self.cleanCounter.write(
            to: fixture.root.appendingPathComponent("Sources/Counter.swift"),
            atomically: true,
            encoding: .utf8,
        )

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix"])

        #expect(run.compilerFindings == ["project.yml:1:error"], "the stale warning is gone, and the failure is said")
        #expect(run.filesWithoutRecord == ["Counter.swift"])
        #expect(run.typesBuild.contains("under --no-fix packages are never resolved"), "\(run.typesBuild)")
        #expect(run.complete == false)
    }

    /* A target whose SDKROOT names no platform this engine builds for is not built, and its files are a named gap. */
    @Test func aTargetWithNoKnownPlatformIsANamedGap() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            specification: Self.specification.replacingOccurrences(of: "platform: iOS", with: "platform: auto")
        )
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.phase("types")?.outcome == "ran")
        #expect(run.filesWithoutRecord == ["Counter.swift", "FixtureApp.swift", "Greeter.swift"])
        #expect(
            run.typesBuild.hasSuffix(
                "; App was not built, because its SDKROOT auto names no platform this engine builds for"
            ),
            "\(run.typesBuild)",
        )
        #expect(run.complete == false)
    }

    /*
     @system_cohere's invariant: a check writes nothing into the user's tree. Under `--no-cache` the engine's scratch
     is outside the project, so the whole tree must be byte for byte what it was. Without it the scratch is the
     project's own `.cache/cohere/swift`, the engine's cache as it is for every package, and everything else must be
     untouched. Neither may write a DerivedData folder under the home folder: the project is named for this test
     alone, so any folder named for it there is this run's.
     */
    @Test func aCheckWritesNothingIntoTheTree() async throws {
        try Self.requireXcodegen()
        let project = "Fixture\(UUID().uuidString.prefix(8))"
        let fixture = try Self.fixture(specification: Self.specification(named: project))
        defer { Self.remove(fixture.repository) }
        let before = try Self.snapshot(of: fixture.repository)
        let derivedDataBefore = try Self.derivedDataFolders(named: project)

        let fresh = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        #expect(fresh.phase("types")?.outcome == "ran")
        #expect(fresh.filesWithoutRecord.isEmpty, "the build ran, so the check below covers what it writes")
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
        #expect(
            FileManager.default.fileExists(
                atPath: fixture.root.appendingPathComponent(".cache/cohere/swift/xcode-build").path
            ),
            "the build went somewhere other than the engine's scratch, so the test above looked in the wrong place",
        )
        #expect(try Self.derivedDataFolders(named: project) == derivedDataBefore, "xcodebuild wrote DerivedData")
    }

    /*
     A committed project is read where it stands, with no xcodegen run, and built where it stands. Under `--no-fix`
     its bundle is left as it was: xcodebuild makes empty package folders inside it even when it resolves nothing,
     and the phase removes the ones it made.
     */
    @Test func aCommittedProjectIsReadWhereItStands() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture()
        defer { Self.remove(fixture.repository) }
        let generated = try ProcessRunner().run("xcodegen", ["generate", "--quiet"], in: fixture.root)
        try #require(generated.succeeded, "the fixture's project could not be made: \(generated.standardError)")
        try FileManager.default.removeItem(at: fixture.root.appendingPathComponent("project.yml"))
        let before = try Self.snapshot(of: fixture.repository)

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.of("project").first?["projectKind"] as? String == "XcodeProject")
        #expect(run.findings == ["cohere-swift/force-unwrapping:3"])
        #expect(run.phase("types")?.outcome == "ran")
        #expect(run.filesWithoutRecord.isEmpty)
        let after = try Self.snapshot(of: fixture.repository)
        #expect(after == before, "written into the tree: \(Set(after.keys).symmetricDifference(before.keys).sorted())")
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
