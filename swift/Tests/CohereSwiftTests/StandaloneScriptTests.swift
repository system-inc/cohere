import Foundation
import Testing

@testable import CohereSwift

/*
 Standalone scripts (#13nbb1s): a file run as `swift <file>` that no target compiles is checked alone, for the platform
 its imports name, with the house's settings, the compiler's `SIL` passes and every file rule, and a file of
 declarations only stays the named gap it was. ahraos-presence's two `Glyph.swift` files were the first such scripts.

 The pipeline tests run the whole engine over a package in a repository (a `.git` beside it), so the file set walks
 for the files no target compiles, with the scripts planted beside its one clean target. Serialized, because each
 runs a check, and several at once only slow each other.
 */
@Suite(.serialized)
struct StandaloneScriptTests {
    /* A clean AppKit script, formatted the house way: top-level code a person runs to draw an image. */
    static let appKitScript = """
        import AppKit

        let canvas = NSSize(width: 64, height: 64)
        let image = NSImage(size: canvas)
        if image.size.width < canvas.width {
            exit(1)
        }

        """

    /* A clean UIKit script, which compiles only against the iOS SDK: the macOS SDK has no UIKit module. */
    static let uiKitScript = """
        import UIKit

        let view = UIView()
        view.backgroundColor = .systemBackground

        """

    /* A package with one clean target, in a repository, with these files beside it, its root resolved. */
    static func package(files: [String: String], inRepository: Bool = true) throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-script-\(UUID().uuidString)",
            isDirectory: true,
        )
        let all = files.merging(
            [
                "Package.swift": PipelineControlTests.manifest,
                "Sources/Control/Control.swift": PipelineControlTests.cleanSource,
            ]
            .merging(inRepository ? [".git/info/exclude": ""] : [:], uniquingKeysWith: { first, _ in first }),
            uniquingKeysWith: { first, _ in first },
        )
        for (path, text) in all {
            let file = root.appendingPathComponent(path)
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try text.write(to: file, atomically: true, encoding: .utf8)
        }
        return root.resolvingSymlinksInPath()
    }

    /* The project record's excluded files as `<file name>: <reason>`, the manifest left out. */
    static func excluded(_ run: XcodeProjectPipelineTests.Run) -> [String] {
        (run.of("project").first?["excluded"] as? [[String: Any]] ?? []).compactMap { entry in
            let name = URL(fileURLWithPath: entry["file"] as? String ?? "").lastPathComponent
            return name == "Package.swift" ? nil : "\(name): \(entry["reason"] as? String ?? "")"
        }
    }

    /* Each script's target in the project record, as `<name>:<files>`. */
    static func scriptTargets(_ run: XcodeProjectPipelineTests.Run) -> [String] {
        (run.of("project").first?["targets"] as? [[String: Any]] ?? []).filter { $0["kind"] as? String == "script" }
            .map { "\($0["name"] ?? ""):\($0["files"] ?? "")" }
    }

    @Test func topLevelCodeOrAShebangMakesAScript() {
        #expect(StandaloneScript.read(source: "print(1)\n") == .script(.macOS))
        #expect(
            StandaloneScript.read(source: "#!/usr/bin/env swift\nstruct Quiet {}\n")
                == .script(.macOS)
        )
        #expect(
            StandaloneScript.read(
                source: "#if os(macOS)\nguard CommandLine.arguments.count > 1 else { exit(1) }\n#endif\n"
            )
                == .script(.macOS),
            "a statement inside `#if` is still top-level code",
        )
    }

    @Test func declarationsAloneAreNoScript() {
        #expect(StandaloneScript.read(source: "") == .declarationsOnly)
        #expect(
            StandaloneScript.read(source: "let limit = 3\nstruct Box {\n    let value: Int\n}\n") == .declarationsOnly
        )
        #expect(StandaloneScript.read(source: "#if DEBUG\nfunc trace() {}\n#endif\n") == .declarationsOnly)
    }

    @Test func theImportsDecideThePlatformAndNeverGuess() {
        #expect(StandaloneScript.read(source: "import AppKit\nprint(1)\n") == .script(.macOS))
        #expect(StandaloneScript.read(source: "import Cocoa\nprint(1)\n") == .script(.macOS))
        #expect(StandaloneScript.read(source: "import SwiftUI\nprint(1)\n") == .script(.macOS))
        #expect(StandaloneScript.read(source: "import UIKit\nprint(1)\n") == .script(.iOS))
        #expect(StandaloneScript.read(source: "import Foundation\nprint(1)\n") == .script(.macOS))
        #expect(
            StandaloneScript.read(
                source: "#if canImport(UIKit)\nimport UIKit\n#else\nimport AppKit\n#endif\nprint(1)\n"
            )
                == .platformsDisagree("it imports AppKit for macOS and UIKit for iOS")
        )
    }

    @Test func aScriptsModuleIsTheOneSwiftRunsItIn() {
        #expect(StandaloneScript.moduleName(of: URL(fileURLWithPath: "/a/Icon/Glyph.swift")) == "Glyph")
        #expect(StandaloneScript.moduleName(of: URL(fileURLWithPath: "/a/generate-icon.swift")) == "main")
        #expect(StandaloneScript.moduleName(of: URL(fileURLWithPath: "/a/2x.swift")) == "main")
    }

    /*
     A script below a project of its own is that project's to check, never this run's: a file below a Package.swift or a
     project.yml keeps the reason the front door leaves to that project's run. Two scripts of one name each get a module.
     */
    @Test func onlyTheScriptsWhoseNearestProjectIsThisRunsAreOurs() throws {
        let root = try Self.package(files: [
            "Icon/Glyph.swift": "print(1)\n",
            "Phone/Icon/Glyph.swift": "print(2)\n",
            "Tools/Sim/Package.swift": "",
            "Tools/Sim/run.swift": "print(3)\n",
            "Capture/project.yml": "name: Capture\n",
            "Capture/Icon/Glyph.swift": "print(4)\n",
        ])
        defer { XcodeProjectPipelineTests.remove(root) }
        let describe =
            #"{"name":"Control","targets":[{"name":"Control","type":"library","path":"Sources/Control","sources":["Control.swift"]}]}"#
        let dump = #"{"toolsVersion":{"_version":"6.2.0"},"swiftLanguageVersions":null,"targets":[]}"#
        let package = try PackageModel(root: root, describeJson: Data(describe.utf8), dumpPackageJson: Data(dump.utf8))

        let fileSet = try FileSet.build(package: package, environment: ["HOME": root.path])

        #expect(
            fileSet.scripts.map { "\(CheckFixture.relative($0.url.path, to: root)):\($0.targetName)" }.sorted() == [
                "Icon/Glyph.swift:Glyph", "Phone/Icon/Glyph.swift:Glyph_2",
            ]
        )
        #expect(fileSet.targetFiles.map(\.url.lastPathComponent) == ["Control.swift"])
        let reasons = Dictionary(
            uniqueKeysWithValues: fileSet.excluded.map { (CheckFixture.relative($0.file, to: root), $0.reason) }
        )
        #expect(reasons["Tools/Sim/run.swift"] == "no target of this package compiles it")
        #expect(reasons["Capture/Icon/Glyph.swift"] == "no target of this package compiles it")
    }

    /* A clean AppKit script is checked and reads 0: a file of ours with a target of its own, counted in the types record. */
    @Test func aCleanAppKitScriptReadsZero() async throws {
        let root = try Self.package(files: ["Icon/Glyph.swift": Self.appKitScript])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty, "\(run.of("finding"))")
        #expect(run.exitCode == 0)
        #expect(run.complete == true)
        #expect(Self.excluded(run).isEmpty, "the script is not named as checked by nothing")
        #expect(Self.scriptTargets(run) == ["Glyph:1"])
        #expect(run.of("project").first?["filesOurs"] as? Int == 2)
        #expect(run.typesRecord["files"] as? Int == 2)
        #expect(
            run.typesBuild.hasSuffix(
                "1 script checked alone with -emit-sib, each in a module of its own, as `swift <file>` runs it (1 for macOS)"
            ),
            "\(run.typesBuild)",
        )
        #expect(run.phase("lint")?.outcome == "ran")
    }

    /* The type checker's error and a `SIL` pass's (a missing return, which plain `-typecheck` never reports), each at its line. */
    @Test func aScriptsCompilerErrorsAreReportedAtTheirLines() async throws {
        let root = try Self.package(files: [
            "Scripts/Typed.swift": "let wrong: Int = \"nope\"\nprint(wrong)\n",
            "Scripts/Returns.swift": """
            func missing(_ value: Int) -> Int {
                if value > 0 {
                    return value
                }
            }

            print(missing(1))

            """,
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["Returns.swift:5:error", "Typed.swift:1:error"], "\(run.of("finding"))")
        let messages = run.of("finding").compactMap { $0["message"] as? String }
        #expect(messages.contains { $0.hasPrefix("missing return in global function expected to return 'Int'") })
        #expect(messages.contains { $0.hasPrefix("cannot convert value of type 'String' to specified type 'Int'") })
        #expect(run.phase("lint")?.outcome == "notReached", "a script's type error stops lint as a target's does")
        #expect(run.exitCode == 1)
    }

    /*
     A script is a command-line tool whatever it imports: `swift <file>` prints to the shell that ran it, as
     `GenerateIcon.sh` reads `Glyph.swift`, so an AppKit script that prints is no `consistency-no-print` finding.
     */
    @Test func anAppKitScriptThatPrintsIsATool() async throws {
        let root = try Self.package(files: [
            "Icon/Glyph.swift": Self.appKitScript.replacingOccurrences(
                of: "    exit(1)",
                with: "    print(\"the image is smaller than its canvas\")\n    exit(1)",
            )
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty, "\(run.of("finding"))")
        #expect(run.phase("lint")?.outcome == "ran")
    }

    /* A file rule judges a script as it judges any file of ours. */
    @Test func aScriptThatBreaksAFileRuleIsReportedAtItsLine() async throws {
        let root = try Self.package(files: ["Scripts/Parse.swift": "let parsed = Int(\"2\")!\nprint(parsed)\n"])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings == ["cohere-swift/force-unwrapping:1"], "\(run.of("finding"))")
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.complete == true)
    }

    /* A UIKit script is checked against the iOS SDK, where `UIView` is; against the macOS host it does not compile at all. */
    @Test func aUIKitScriptIsCheckedForIOS() async throws {
        try PlatformPackagePipelineTests.requireIosSdk()
        let root = try Self.package(files: ["Phone/Frame.swift": Self.uiKitScript])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty, "\(run.of("finding"))")
        #expect(run.exitCode == 0)
        #expect(run.typesBuild.hasSuffix("(1 for iOS)"), "\(run.typesBuild)")
    }

    /* ahra's shape in a fresh repository with no project at its root: a script, a file of declarations, a dependency's Swift and a nested package's. */
    static func scriptsOnlyRoot() throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-scripts-root-\(UUID().uuidString)",
            isDirectory: true,
        )
        for (path, text) in [
            ".git/info/exclude": "",
            "package.json": "{}\n",
            "modules/ebay/EbayVision.swift":
                "import Foundation\n\nlet words = CommandLine.arguments.count\nprint(words)\n",
            "modules/stray/Leftover.swift": "struct Leftover {\n    let value: Int\n}\n",
            "node_modules/dependency/Tool.swift": "print(1)\n",
            "Nested/Package.swift": PipelineControlTests.manifest,
            "Nested/run.swift": "print(2)\n",
        ] {
            let file = root.appendingPathComponent(path)
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try text.write(to: file, atomically: true, encoding: .utf8)
        }
        return root
    }

    /*
     A root holding no Package.swift and no Xcode project is the scripts-only root discovery gives the Swift no project
     claims (#wpd36zj), ahra's shape: its scripts are checked alone with no target checked before them, a file of
     declarations stays a named gap, and what discovery never enters or another project claims is not this run's.
     Its coverage query names the root alone.
     */
    @Test func aRootHoldingNoProjectChecksItsScriptsAlone() async throws {
        let root = try Self.scriptsOnlyRoot()
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty, "\(run.of("finding"))")
        #expect(run.exitCode == 0)
        #expect(run.complete == true)
        let project = try #require(run.of("project").first)
        #expect(project["projectKind"] as? String == "Scripts")
        #expect(project["filesOurs"] as? Int == 1)
        #expect(Self.scriptTargets(run) == ["EbayVision:1"])
        #expect(
            Self.excluded(run) == [
                "Leftover.swift: no target of this package compiles it, and it holds only declarations, so it is no script to check alone"
            ]
        )
        #expect(run.typesRecord["files"] as? Int == 1)
        #expect(
            run.typesBuild.hasPrefix("a scripts-only root, which no package or Xcode project holds"),
            "\(run.typesBuild)",
        )

        let covered = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--covered-packages"])
        let record = try #require(covered.of("coveredPackages").first)
        #expect(record["checked"] as? [String] == [root.standardizedFileURL.path])
    }

    /*
     What a scripts-only root's verdict is decided from, which its inputs record names so the front door replays it only
     while none has moved (#wpd36zj): each script, each Swift file it read to tell whether it is one (a file of
     declarations becomes a script by an edit), and each directory its walk listed, so a script added beside the others
     moves a listing. It walks no root, so nothing under `node_modules`, which discovery never enters, is named, and a
     dependency's install never stops a replay.
     */
    @Test func aScriptsOnlyRootsRecordNamesWhatDecidesItsVerdict() async throws {
        let root = try Self.scriptsOnlyRoot()
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        let inputs = try #require(run.of("inputs").first)
        let relative = { (key: String) -> Set<String> in
            Set((inputs[key] as? [String] ?? []).map { CheckFixture.relative($0, to: root) })
        }
        let files = relative("files")
        let directories = relative("directories")
        #expect(files.contains("modules/ebay/EbayVision.swift"), "the script is not named: \(files.sorted())")
        #expect(
            files.contains("modules/stray/Leftover.swift"),
            "the file read to tell it is no script: \(files.sorted())",
        )
        #expect(
            directories.contains("modules/ebay"),
            "a script added beside it moves no listing: \(directories.sorted())",
        )
        let dependency = (files.union(directories).union(relative("absent"))).filter { $0.hasPrefix("node_modules") }
        #expect(dependency.isEmpty, "a dependency's install would stop a replay: \(dependency.sorted())")
    }

    /*
     Outside a git repository, a `git archive` copy of ahraos-presence say, a script is checked and a file of
     declarations named, as in a checkout: the walk reads the package's own ignore files as git would, so what they
     ignore (here a build folder holding a generated script) is no more this run's than in a repository. Before, no
     walk ran outside git, and the copy's two Glyph scripts were neither checked nor named (the presence Circle,
     2026-11-07).
     */
    @Test func outsideAnyRepositoryAScriptIsCheckedAndADeclarationsFileNamed() async throws {
        let root = try Self.package(
            files: [
                ".gitignore": "Build/\n",
                "Icon/Glyph.swift": Self.appKitScript,
                "Stray/Leftover.swift": "struct Leftover {\n    let value: Int\n}\n",
                "Build/Generated.swift": "print(1)\n",
            ],
            inRepository: false,
        )
        defer { XcodeProjectPipelineTests.remove(root) }
        #expect(
            IgnoreRules.Repository.containing(root) == nil,
            "the temporary directory sits inside a repository, so this test proves nothing",
        )

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings.isEmpty, "\(run.of("finding"))")
        #expect(Self.scriptTargets(run) == ["Glyph:1"])
        #expect(
            Self.excluded(run) == [
                "Leftover.swift: no target of this package compiles it, and it holds only declarations, so it is no script to check alone"
            ]
        )
        #expect(run.of("project").first?["filesOurs"] as? Int == 2)
        #expect(run.typesRecord["files"] as? Int == 2)
        #expect(run.complete == true)
    }

    /* A file of declarations no target compiles is no script: it stays a named gap, with the reason saying why. */
    @Test func aFileOfDeclarationsOnlyStaysANamedGap() async throws {
        let root = try Self.package(files: ["Stray/Leftover.swift": "struct Leftover {\n    let value: Int\n}\n"])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(
            Self.excluded(run) == [
                "Leftover.swift: no target of this package compiles it, and it holds only declarations, so it is no script to check alone"
            ]
        )
        #expect(Self.scriptTargets(run).isEmpty)
        #expect(run.of("project").first?["filesOurs"] as? Int == 1)
        #expect(run.typesRecord["files"] as? Int == 1)
    }

    /* A script that needs a companion file does not compile alone, and says so after the compiler's own message. */
    @Test func aScriptThatNeedsACompanionDoesNotCompileAlone() async throws {
        let root = try Self.package(files: [
            "Tools/Greet.swift": "print(companionGreeting())\n",
            "Tools/Companion.swift": "func companionGreeting() -> String {\n    \"hello\"\n}\n",
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["Greet.swift:1:error"], "\(run.of("finding"))")
        let message = run.of("finding").first?["message"] as? String ?? ""
        #expect(
            message
                == "cannot find 'companionGreeting' in scope (checked alone, as `swift Greet.swift` runs it: a script that needs other files to compile belongs in a package with them)"
        )
        #expect(
            Self.excluded(run).first?.hasPrefix("Companion.swift: ") == true,
            "the companion, declarations only, stays a gap",
        )
    }

    /*
     A warm run and a `--no-cache` run agree, after an edit too: the warm run checks the script again rather than read
     the record an earlier run left, and the `--no-cache` run writes nothing into the project's `.cache`.
     */
    @Test func noCacheAndAWarmRunAgree() async throws {
        let root = try Self.package(files: ["Icon/Glyph.swift": Self.appKitScript])
        defer { XcodeProjectPipelineTests.remove(root) }
        let script = root.appendingPathComponent("Icon/Glyph.swift")

        let first = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix"])
        #expect(first.findings.isEmpty, "\(first.of("finding"))")
        try Self.appKitScript.replacingOccurrences(
            of: "let image = NSImage(size: canvas)",
            with: "let image = NSImage(size: canvas)\nlet wrong: Int = \"nope\"",
        )
        .write(to: script, atomically: true, encoding: .utf8)

        let warm = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix"])
        let cache = root.appendingPathComponent(".cache")
        try FileManager.default.removeItem(at: cache)
        let fresh = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(warm.compilerFindings == ["Glyph.swift:5:error"], "\(warm.of("finding"))")
        #expect(fresh.compilerFindings == warm.compilerFindings)
        #expect(fresh.findings == warm.findings)
        #expect(!FileManager.default.fileExists(atPath: cache.path), "a --no-cache run wrote to the project's .cache")
    }
}
