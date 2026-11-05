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
    /* The App target's settings with every toolchain setting the rules require on, so a fixture is clean of them unless it says otherwise. */
    static let toolchainOn = """
              base:
                SWIFT_VERSION: "6.0"
                SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY: YES
                SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY: YES
                SWIFT_STRICT_MEMORY_SAFETY: YES

        """

    static let specification = specification(appSettings: toolchainOn)

    /* The fixture's spec with `appSettings` as the App target's `settings:` map, the target's line being line 5. */
    static func specification(appSettings: String) -> String {
        """
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

        """ + appSettings
    }

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

    /* Line 3 holds a variable that is never mutated, which the compiler warns about and lint does not. */
    static let warningSource = """
        struct Counter {
            func doubled(_ value: Int) -> Int {
                var neverMutated = value
                return neverMutated * 2
            }
        }

        """

    /* Line 3 compares an array's count with zero, which `empty-count` finds only from the type the build's index gives `items`. */
    static let emptyCountSource = """
        struct Counter {
            func isBare(_ items: [Int]) -> Bool {
                items.count == 0
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

        /* The note the lint record carries when no config was read. */
        var configurationNote: String {
            of("lint").first?["configNote"] as? String ?? ""
        }

        /* Each unused record as `<file name>:<line> <subject>`. */
        var unused: [String] {
            of("unused").map {
                "\(URL(fileURLWithPath: $0["file"] as? String ?? "").lastPathComponent):\($0["line"] as? Int ?? 0) \($0["subject"] as? String ?? "")"
            }
        }

        /* One unused rule's coverage record. */
        func unusedCoverage(_ rule: String) -> [String: Any] {
            of("unusedCoverage").first { $0["rule"] as? String == rule } ?? [:]
        }

        /* Each lint crash as `<file name>: <error>`. */
        var lintCrashes: [String] {
            (of("lint").first?["crashes"] as? [[String: Any]] ?? []).map {
                "\(URL(fileURLWithPath: $0["file"] as? String ?? "").lastPathComponent): \($0["error"] as? String ?? "")"
            }
        }

        /* The toolchain rules' findings as `<rule, its prefix off>@<file name>:<line>`, in the order written. */
        var toolchainFindings: [String] {
            toolchainFindingRecords.map {
                let rule = ($0["rule"] as? String ?? "").replacingOccurrences(of: "cohere-swift/", with: "")
                let file = URL(fileURLWithPath: $0["file"] as? String ?? "").lastPathComponent
                return "\(rule)@\(file):\($0["line"] as? Int ?? 0)"
            }
        }

        var toolchainFindingRecords: [[String: Any]] {
            of("finding").filter { ($0["rule"] as? String ?? "").hasPrefix("cohere-swift/toolchain-") }
        }

        /* The message of the one toolchain finding from `rule`, its prefix off. */
        func toolchainMessage(_ rule: String) -> String {
            toolchainFindingRecords.first { $0["rule"] as? String == "cohere-swift/\(rule)" }?["message"] as? String
                ?? ""
        }

        /* The rules the lint record says were offered nothing, which a clean twin must not list: silent is not judged. */
        var rulesSilent: [String] {
            of("lint").first?["rulesSilent"] as? [String] ?? []
        }

        var fixRecord: [String: Any] {
            of("fix").first ?? [:]
        }
    }

    static let toolchainRules = RuleRegistry.packageRules.map(\.name)

    /* Every toolchain setting off: Swift 5, no upcoming feature, no strict memory safety. */
    static let toolchainOff = """
              base:
                SWIFT_VERSION: "5.0"

        """

    /* The same settings as `toolchainOn`, the features and memory safety spelled as flags in `OTHER_SWIFT_FLAGS`. */
    static let toolchainOnInFlags = """
              base:
                SWIFT_VERSION: "6.0"
                OTHER_SWIFT_FLAGS: "-enable-upcoming-feature ExistentialAny -enable-upcoming-feature MemberImportVisibility -strict-memory-safety"

        """

    /* MemberImportVisibility and strict memory safety in Debug alone, which Release ships without. */
    static let toolchainOnInDebugOnly = """
              base:
                SWIFT_VERSION: "6.0"
                SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY: YES
              configs:
                Debug:
                  SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY: YES
                  SWIFT_STRICT_MEMORY_SAFETY: YES

        """

    /* Strict memory safety turned off by Release's own settings, which a key in base cannot override. */
    static let strictMemorySafetyOffInRelease = """
              base:
                SWIFT_VERSION: "6.0"
                SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY: YES
                SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY: YES
              configs:
                Release:
                  SWIFT_STRICT_MEMORY_SAFETY: NO

        """

    /* An entry point that uses both of the fixture's types, so nothing the unused report judges is unused by default. */
    static let usingEntrySource = """
        @main
        struct FixtureApp {
            static func main() {
                _ = Greeter(name: "fixture").greeting()
                _ = Counter().doubled(2)
            }
        }

        """

    /* Line 1 imports a module nothing in the file uses, and line 8 declares a private function nothing calls. */
    static let unusedSource = """
        import Foundation

        struct Counter {
            func doubled(_ value: Int) -> Int {
                value * 2
            }

            private func neverCalled() -> Int {
                3
            }
        }

        """

    /* The same file with every import and declaration used: Foundation for `Date`, and the private function by `doubled`. */
    static let usedSource = """
        import Foundation

        struct Counter {
            func doubled(_ value: Int) -> Int {
                twice(value) + seconds(since: Date())
            }

            private func twice(_ value: Int) -> Int {
                value * 2
            }

            private func seconds(since start: Date) -> Int {
                Int(Date().timeIntervalSince(start))
            }
        }

        """

    /* A framework target beside the app, which the app never links, so nothing in the build names its public function. */
    static let frameworkTarget = """
          Kit:
            type: framework
            platform: iOS
            deploymentTarget: "18.0"
            sources:
              - Kit
            settings:
              base:
                SWIFT_VERSION: "6.0"
                SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY: YES
                SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY: YES
                SWIFT_STRICT_MEMORY_SAFETY: YES
                GENERATE_INFOPLIST_FILE: YES

        """

    static let frameworkSource = """
        public func kitGreeting() -> String {
            "kit"
        }

        """

    /* A repository holding the app's folder; the returned URL is the app's folder, the run's root. */
    static func fixture(
        counter: String = dirtySource,
        specification: String = specification,
        entry: String = entrySource,
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
            "Sources/FixtureApp.swift": entry,
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

    /*
     A warning is a finding, but the code still means what it says, so lint runs. The warning source holds a `:` and
     an `=`, so `redundant-type-annotation` applies to it and reads its names from the xcodebuild's index (block C,
     #9ktapz2): every file is recorded and judged, so the run is complete.
     */
    @Test func aCompilerWarningIsAFindingAndLintStillRuns() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.warningSource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["Counter.swift:3:warning"])
        #expect(run.filesWithoutRecord.isEmpty)
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.lintCrashes.isEmpty)
        #expect(run.complete == true, "\(run.records)")
        #expect(run.exitCode == 1)
    }

    /* A typed rule finds its violation at an Xcode root, from what the xcodebuild's index says `items` is. */
    @Test func aTypedRuleFindsItsViolationAtAnXcodeRoot() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.emptyCountSource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.findings == ["cohere-swift/empty-count:3"], "\(run.records)")
        #expect(run.lintCrashes.isEmpty)
        #expect(run.complete == true)
        #expect(run.exitCode == 1)
    }

    /*
     Both sources the typed rules read at an Xcode root, exactly as for a package: the xcodebuild's index for every
     file it compiled as it stands, and sourcekitd fed the derived data's task store for a file edited since, which
     the index cannot vouch for. Either missing turns this red even where the other would cover for it in a run.
     */
    @Test func theTypedRulesReadTheXcodeIndexThenSourcekitd() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.emptyCountSource)
        defer { Self.remove(fixture.repository) }
        let built = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(built.complete == true, "\(built.records)")

        let scratch = Pipeline.scratchPath(for: fixture.root)
        let source = try #require(try XcodeProject.source(at: fixture.root))
        /* The project read afresh for each ask, as a run reads it: a URL keeps the modification date it was first asked for. */
        func symbols() async throws -> SymbolProvider.Result {
            let project = try XcodeProject.load(
                source: source,
                root: fixture.root,
                scratchPath: scratch,
                runner: ProcessRunner(),
            )
            let files = await SourceParser().parse(try FileSet.build(package: project.model).owned).files
            return SymbolProvider(
                layouts: Pipeline.symbolLayouts(package: project.model, scratch: scratch),
                runner: ProcessRunner(),
            )
            .symbols(for: files)
        }
        let fromBuild = try await symbols()
        #expect(fromBuild.fromIndex == 3, "\(fromBuild.unavailable)")
        #expect(fromBuild.unavailable.isEmpty)

        try FileManager.default.setAttributes(
            [.modificationDate: Date().addingTimeInterval(60)],
            ofItemAtPath: fixture.root.appendingPathComponent("Sources/Counter.swift").path,
        )
        let edited = try await symbols()
        #expect(edited.fromIndex == 2)
        #expect(edited.fromSourcekitd == 1, "\(edited.unavailable)")
        #expect(edited.unavailable.isEmpty)
    }

    /* `--unused` at an Xcode root reads the xcodebuild's index: the import nothing uses and the private function nothing calls. */
    @Test func theUnusedReportFindsAnUnusedImportAndAPrivateDeclarationAtAnXcodeRoot() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.unusedSource, entry: Self.usingEntrySource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--unused"])

        #expect(run.phase("unused")?.outcome == "ran", "\(String(describing: run.phase("unused")))")
        #expect(run.unused == ["Counter.swift:1 import Foundation", "Counter.swift:8 func neverCalled()"])
        #expect(run.unusedCoverage(UnusedImports.ruleName)["filesChecked"] as? Int == 3)
        #expect(run.unusedCoverage(UnusedDeclarations.ruleName)["filesChecked"] as? Int == 3)
    }

    /* Every import and declaration used, so the report checks every file and finds nothing. */
    @Test func theUnusedReportFindsNothingWhereEverythingIsUsedAtAnXcodeRoot() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.usedSource, entry: Self.usingEntrySource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--unused"])

        #expect(run.phase("unused")?.outcome == "ran")
        #expect(run.unused.isEmpty, "\(run.unused)")
        #expect(run.unusedCoverage(UnusedImports.ruleName)["filesChecked"] as? Int == 3)
        #expect(run.unusedCoverage(UnusedDeclarations.ruleName)["filesChecked"] as? Int == 3)
        #expect(run.unusedCoverage(UnusedDeclarations.ruleName)["checked"] as? Int ?? 0 > 0)
    }

    /*
     A framework target's public function is API for whoever links the framework, though nothing in this build names
     it (`XcodeProject.isAPI`), so it is kept and counted as public API, never reported.
     */
    @Test func aFrameworkTargetsPublicDeclarationIsAPIAtAnXcodeRoot() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.usedSource,
            specification: Self.specification + Self.frameworkTarget,
            entry: Self.usingEntrySource,
        )
        defer { Self.remove(fixture.repository) }
        let kit = fixture.root.appendingPathComponent("Kit/Kit.swift")
        try FileManager.default.createDirectory(at: kit.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Self.frameworkSource.write(to: kit, atomically: true, encoding: .utf8)

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--unused"])

        #expect(run.filesWithoutRecord.isEmpty, "\(run.typesBuild)")
        #expect(run.phase("unused")?.outcome == "ran")
        #expect(run.unused.isEmpty, "\(run.unused)")
        let skipped = run.unusedCoverage(UnusedDeclarations.ruleName)["skipped"] as? [String: Int] ?? [:]
        #expect(skipped[UnusedDeclarations.publicAPI] == 1, "\(skipped)")
    }

    /* Which product types are API: run, never linked, is not; linked is; one this engine does not know fails closed. */
    @Test(arguments: [
        ("com.apple.product-type.application", false), ("com.apple.product-type.app-extension", false),
        ("com.apple.product-type.extensionkit-extension", false), ("com.apple.product-type.bundle.unit-test", false),
        ("com.apple.product-type.bundle.ui-testing", false), ("com.apple.product-type.tool", false),
        ("com.apple.product-type.framework", true), ("com.apple.product-type.framework.static", true),
        ("com.apple.product-type.library.static", true), ("com.apple.product-type.library.dynamic", true),
        ("com.apple.product-type.bundle", true), ("com.apple.product-type.xpc-service", true),
    ])
    func aProductTypeIsAPIWhenSomethingOutsideTheBuildLinksIt(productType: String, isAPI: Bool) {
        #expect(XcodeProject.isAPI(kind: XcodeProject.kind(of: productType)) == isAPI)
    }

    /*
     The same violation under `--lint` alone, where the build is lint's input rather than a types phase (e877e8f4),
     so the typed rule reads an index the run built for it.
     */
    @Test func aLintOnlyRunAtAnXcodeRootBuildsForItsTypedRules() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.emptyCountSource)
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--lint"])

        #expect(run.findings == ["cohere-swift/empty-count:3"], "\(run.records)")
        #expect(run.phase("types")?.outcome == "skipped")
        #expect(run.lintCrashes.isEmpty)
        #expect(run.complete == true)
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
        #expect(cold.complete == true)

        let warm = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(
            warm.typesBuild
                == "no input changed since the last successful xcodebuild, so its compiler records were read without building"
        )
        #expect(warm.compilerFindings == ["Counter.swift:3:warning"])
        #expect(warm.filesWithoutRecord.isEmpty)
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
     A build that fails before compiling anything, here a package at a host no network reaches (`.invalid` never
     resolves), judges what the last build left by freshness. Greeter.swift is as that build compiled it, so its
     record stands. Counter.swift was edited since, out of the warning that build recorded, so the old warning is not
     reported and the file has no record. The failure is a finding at the spec that says resolution failed and why.
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
                    "packages:\n  Missing:\n    url: https://example.invalid/Missing.git\n    exactVersion: 1.0.0\ntargets:\n",
            ) + "    dependencies:\n      - package: Missing\n        product: Missing\n"
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
        let message = run.of("finding").first?["message"] as? String ?? ""
        #expect(
            message.hasPrefix(
                "xcodebuild could not resolve the packages this project names, so nothing was built and no file's types are known: xcodebuild: error: Could not resolve package dependencies: "
            ),
            "\(message)",
        )
        #expect(message.contains("example.invalid"), "the words say which package and why: \(message)")
        #expect(run.filesWithoutRecord == ["Counter.swift"])
        #expect(
            run.typesBuild.contains("; the build of App failed: xcodebuild: error: Could not resolve"),
            "\(run.typesBuild)",
        )
        #expect(run.complete == false)
    }

    /*
     A target whose SDKROOT names no platform this engine builds for is not built, and its files are a named gap. Its
     build settings are not asked for either, so the toolchain rules leave it unchecked, named at the spec with why,
     rather than judging it on or off.
     */
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
        #expect(run.toolchainFindings.isEmpty, "\(run.toolchainFindingRecords)")
        #expect(
            run.lintCrashes.contains(
                "project.yml: the build settings of App could not be read: its SDKROOT auto names no platform this engine builds for; so the rules that judge them did not (\(Self.toolchainRules.sorted().joined(separator: ", ")))"
            ),
            "\(run.lintCrashes)",
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

    /* A source that uses both packages, so a build that did not resolve them cannot vouch for it. */
    static let packagesSource = """
        import Lib
        import RemoteKit

        struct Uses {
            func both() -> String {
                libGreeting() + remoteGreeting()
            }
        }

        """

    /*
     Two packages beside the app, the shapes CapturePhone links: `Lib` by path, like CaptureWire, and `Remote` by
     URL at an exact version, like MediaPipe, a git repository of its own reached through `file://`, so no test needs
     the network. Measured 2026-11-05: a path package alone builds under `-disableAutomaticPackageResolution` with
     no pins, so it cannot tell a resolve from none, while a URL package fails with "a resolved file is required".
     Returns the spec, its app target depending on both.
     */
    static func addPackages(to repository: URL, specification: String = specification) throws -> String {
        let manifest = """
            // swift-tools-version: 6.0
            import PackageDescription

            let package = Package(
                name: "NAME",
                platforms: [.iOS(.v18)],
                products: [.library(name: "PRODUCT", targets: ["PRODUCT"])],
                targets: [.target(name: "PRODUCT")]
            )

            """
        let packages = [
            ("Lib", "Lib", "public func libGreeting() -> String { \"lib\" }\n"),
            ("Remote", "RemoteKit", "public func remoteGreeting() -> String { \"remote\" }\n"),
        ]
        for (name, product, source) in packages {
            let directory = repository.appendingPathComponent(name, isDirectory: true)
            let sources = directory.appendingPathComponent("Sources/\(product)", isDirectory: true)
            try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
            try manifest.replacingOccurrences(of: "NAME", with: name).replacingOccurrences(
                of: "PRODUCT",
                with: product,
            )
            .write(to: directory.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
            try source.write(to: sources.appendingPathComponent("\(product).swift"), atomically: true, encoding: .utf8)
        }
        let remote = repository.appendingPathComponent("Remote", isDirectory: true)
        let identity = [
            "-c", "user.name=cohere", "-c", "user.email=cohere@example.com", "-c", "commit.gpgsign=false", "-c",
            "tag.gpgsign=false", "-c", "init.defaultBranch=main", "-c", "core.hooksPath=/dev/null",
        ]
        for arguments in [["init", "-q"], ["add", "."], ["commit", "-q", "-m", "one"], ["tag", "1.0.0"]] {
            let result = try ProcessRunner().run("git", identity + arguments, in: remote)
            try #require(result.succeeded, "git \(arguments) failed: \(result.standardError)")
        }
        return specification.replacingOccurrences(
            of: "targets:\n",
            with:
                "packages:\n  Lib:\n    path: ../Lib\n  Remote:\n    url: file://\(remote.path)\n    exactVersion: 1.0.0\ntargets:\n",
        ) + "    dependencies:\n      - package: Lib\n      - package: Remote\n        product: RemoteKit\n"
    }

    /* The app with both packages, as a fresh checkout: no project, no pins, no scratch. */
    static func fixtureWithPackages() throws -> (repository: URL, root: URL) {
        let fixture = try Self.fixture(counter: Self.cleanCounter)
        let specification = try Self.addPackages(to: fixture.repository)
        try specification.write(
            to: fixture.root.appendingPathComponent("project.yml"),
            atomically: true,
            encoding: .utf8,
        )
        try Self.packagesSource.write(
            to: fixture.root.appendingPathComponent("Sources/Uses.swift"),
            atomically: true,
            encoding: .utf8,
        )
        return fixture
    }

    /*
     #s8p20fe: the read-only gate on a fresh checkout of an xcodegen project with package dependencies resolves them
     into the engine's scratch and type-checks every file, and the tree is byte for byte what it was. Before, `--no-fix`
     disabled resolution, the scratch held none, and the build failed until someone ran a mutating check.
     */
    @Test func aNoFixRunResolvesAGeneratedProjectsPackagesIntoTheScratch() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixtureWithPackages()
        defer { Self.remove(fixture.repository) }
        let before = try Self.snapshot(of: fixture.repository)

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.phase("types")?.outcome == "ran")
        #expect(run.compilerFindings.isEmpty, "\(run.typesBuild) \(run.of("finding"))")
        #expect(run.typesRecord["files"] as? Int == 4)
        #expect(run.filesWithoutRecord.isEmpty, "\(run.typesBuild)")
        let after = try Self.snapshot(of: fixture.repository)
        #expect(after == before, "written into the tree: \(Set(after.keys).symmetricDifference(before.keys).sorted())")
    }

    /*
     A committed project with no pins of its own, under `--no-fix`, is built through a workspace in the scratch, so
     the pins the resolve writes land there and the bundle is left as it was. Built as a project, xcodebuild would
     write them into the bundle.
     */
    @Test func aNoFixRunResolvesACommittedProjectsPackagesIntoTheScratch() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixtureWithPackages()
        defer { Self.remove(fixture.repository) }
        try Self.commitProject(at: fixture.root)
        let before = try Self.snapshot(of: fixture.repository)

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.of("project").first?["projectKind"] as? String == "XcodeProject")
        #expect(run.compilerFindings.isEmpty, "\(run.typesBuild) \(run.of("finding"))")
        #expect(run.typesRecord["files"] as? Int == 4)
        #expect(run.filesWithoutRecord.isEmpty, "\(run.typesBuild)")
        let after = try Self.snapshot(of: fixture.repository)
        #expect(after == before, "written into the tree: \(Set(after.keys).symmetricDifference(before.keys).sorted())")
    }

    /*
     A resolution the repository commits holds a `--no-fix` build as it stands: on a fresh scratch the pinned
     revision is cloned and the build succeeds, the pins unchanged. Pins the project no longer matches fail by name,
     at the project file, with the committed file in the words, and are left as they were.
     */
    @Test func aCommittedResolutionHoldsANoFixBuild() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixtureWithPackages()
        defer { Self.remove(fixture.repository) }
        try Self.commitProject(at: fixture.root)
        let pins = fixture.root.appendingPathComponent(
            "Fixture.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
        )
        _ = try await Self.run(root: fixture.root, arguments: ["--no-cache"])
        let pinned = try String(contentsOf: pins, encoding: .utf8)
        try #require(pinned.contains("\"version\" : \"1.0.0\""), "the plain run wrote no pins to commit: \(pinned)")
        let before = try Self.snapshot(of: fixture.repository)

        let held = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        #expect(held.compilerFindings.isEmpty, "\(held.typesBuild) \(held.of("finding"))")
        #expect(held.filesWithoutRecord.isEmpty, "\(held.typesBuild)")
        #expect(try Self.snapshot(of: fixture.repository) == before)

        let stale = pinned.replacingOccurrences(of: "1.0.0", with: "0.9.0")
        try stale.write(to: pins, atomically: true, encoding: .utf8)
        let refused = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        #expect(refused.compilerFindings == ["project.pbxproj:1:error"], "\(refused.typesBuild)")
        /* The run names files by their real path, which Foundation's own resolving does not spell (it drops `/private`), so the project file's finding gives it. */
        let projectFile = refused.of("finding").first?["file"] as? String ?? ""
        let realPins =
            URL(fileURLWithPath: projectFile).deletingLastPathComponent().path
            + "/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
        #expect(
            refused.typesBuild.contains(
                "(under --no-fix the build is held to the resolution the repository commits, \(realPins), and a check never moves it)"
            ),
            "\(refused.typesBuild)",
        )
        #expect(refused.filesWithoutRecord.count == 4)
        #expect(try String(contentsOf: pins, encoding: .utf8) == stale)
    }

    /* The app's project generated where it stands and its spec removed, with the bundle no longer ignored, as a repository that commits its project. */
    static func commitProject(at root: URL) throws {
        let generated = try ProcessRunner().run("xcodegen", ["generate", "--quiet"], in: root)
        try #require(generated.succeeded, "the fixture's project could not be made: \(generated.standardError)")
        try FileManager.default.removeItem(at: root.appendingPathComponent("project.yml"))
        try "Info.plist\n".write(to: root.appendingPathComponent(".gitignore"), atomically: true, encoding: .utf8)
    }

    /*
     A committed project is read where it stands, with no xcodegen run. Under `--no-fix` its bundle is left as it
     was: built as a project, xcodebuild made empty package folders inside it even when it resolved nothing, so it
     is built through a workspace in the scratch.
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

    /*
     An Xcode root has no Package.swift, so the config note names the file its project is defined by, which the config
     is looked for beside: the spec for an xcodegen root, the bundle for a committed project (#zskc1pa).
     */
    @Test func theConfigNoteNamesTheXcodeProjectsDefinition() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(counter: Self.cleanCounter)
        defer { Self.remove(fixture.repository) }
        let settings = fixture.root.appendingPathComponent("CohereSettings.json").path

        let generated = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--lint"])
        #expect(
            generated.configurationNote
                == "config: no CohereSettings.json beside project.yml, so every house rule ran at error — put one at \(settings) to change that"
        )

        try Self.commitProject(at: fixture.root)
        let committed = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache", "--lint"])
        #expect(
            committed.configurationNote
                == "config: no CohereSettings.json beside Fixture.xcodeproj, so every house rule ran at error — put one at \(settings) to change that"
        )
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

    /*
     Block D (#6vjs9n7): an Xcode target with every toolchain setting off is flagged by each of the three rules, at the
     target's line in project.yml, with the build setting to set. Under `--no-fix` the tree is byte for byte as it was.
     */
    @Test func anXcodeTargetWithItsToolchainSettingsOffIsFlaggedAtItsLineInTheSpec() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: Self.toolchainOff),
        )
        defer { Self.remove(fixture.repository) }
        let before = try Self.snapshot(of: fixture.repository)

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(
            run.toolchainFindings == [
                "toolchain-require-swift-6-language-mode@project.yml:5",
                "toolchain-require-upcoming-features@project.yml:5",
                "toolchain-require-strict-memory-safety@project.yml:5",
            ],
            "\(run.records)",
        )
        #expect(
            run.toolchainMessage("toolchain-require-swift-6-language-mode").hasPrefix(
                "App compiles in Swift 5 language mode. Swift 6 is required"
            )
        )
        #expect(
            run.toolchainMessage("toolchain-require-upcoming-features").hasPrefix(
                "App does not enable ExistentialAny and MemberImportVisibility. Set SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY and SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY to YES in the target's build settings, in every configuration."
            ),
            "\(run.toolchainMessage("toolchain-require-upcoming-features"))",
        )
        #expect(
            run.toolchainMessage("toolchain-require-strict-memory-safety").hasPrefix(
                "App does not compile under strict memory safety. Set the target's SWIFT_STRICT_MEMORY_SAFETY build setting to YES"
            )
        )
        #expect(run.lintCrashes.isEmpty)
        let after = try Self.snapshot(of: fixture.repository)
        #expect(after == before, "written into the tree: \(Set(after.keys).symmetricDifference(before.keys).sorted())")
    }

    /*
     The clean twins: every setting on by its build setting, and the features and memory safety on by flags in
     `OTHER_SWIFT_FLAGS` instead. Each rule judged the target (it is not silent) and found nothing.
     */
    @Test(arguments: ["build settings", "flags"])
    func anXcodeTargetWithItsToolchainSettingsOnIsClean(spelling: String) async throws {
        try Self.requireXcodegen()
        let settings = spelling == "flags" ? Self.toolchainOnInFlags : Self.toolchainOn
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: settings),
        )
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.toolchainFindings.isEmpty, "\(run.toolchainFindingRecords)")
        #expect(Set(run.rulesSilent).isDisjoint(with: Self.toolchainRules), "\(run.rulesSilent)")
        #expect(run.lintCrashes.isEmpty)
        #expect(run.complete == true)
    }

    /* A setting on in Debug alone is not the target's setting: each is flagged, and the words name the configuration it is missing from. */
    @Test func aToolchainSettingOnInDebugOnlyIsFlagged() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: Self.toolchainOnInDebugOnly),
        )
        defer { Self.remove(fixture.repository) }

        let run = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])

        #expect(
            run.toolchainFindings == [
                "toolchain-require-upcoming-features@project.yml:5",
                "toolchain-require-strict-memory-safety@project.yml:5",
            ],
            "\(run.records)",
        )
        #expect(
            run.toolchainMessage("toolchain-require-upcoming-features").hasPrefix(
                "App does not enable MemberImportVisibility in Release. Set SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY to YES"
            ),
            "\(run.toolchainMessage("toolchain-require-upcoming-features"))",
        )
        #expect(
            run.toolchainMessage("toolchain-require-strict-memory-safety").hasPrefix(
                "App does not compile under strict memory safety in Release."
            )
        )
    }

    /*
     A plain run writes the missing settings into the target's base settings in project.yml, keeps them because the
     project generated from it again reads them as on, and reports the fix; the next run finds nothing to say.
     */
    @Test func aPlainRunFixesTheSpecAndTheNextRunIsClean() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: Self.toolchainOff),
        )
        defer { Self.remove(fixture.repository) }
        let spec = fixture.root.appendingPathComponent("project.yml")

        let fixed = try await Self.run(root: fixture.root, arguments: [])

        #expect(fixed.toolchainFindings.isEmpty, "\(fixed.toolchainFindingRecords) \(fixed.fixRecord)")
        let changed = fixed.fixRecord["changedFiles"] as? [[String: Any]] ?? []
        let specChange = changed.first { ($0["file"] as? String ?? "").hasSuffix("/project.yml") }
        #expect(
            specChange?["fixedBy"] as? [String: Int]
                == Dictionary(uniqueKeysWithValues: Self.toolchainRules.map { ($0, 1) }),
            "\(fixed.fixRecord)",
        )
        #expect(
            try String(contentsOf: spec, encoding: .utf8)
                == Self.specification(
                    appSettings: """
                              base:
                                SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY: YES
                                SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY: YES
                                SWIFT_STRICT_MEMORY_SAFETY: YES
                                SWIFT_VERSION: "6.0"

                        """
                )
        )

        let next = try await Self.run(root: fixture.root, arguments: ["--no-fix"])
        #expect(next.toolchainFindings.isEmpty, "\(next.toolchainFindingRecords)")
        #expect(Set(next.rulesSilent).isDisjoint(with: Self.toolchainRules))
        #expect(next.complete == true, "\(next.records)")
    }

    /*
     A fix that would not take: Release turns strict memory safety off in its own settings, which a key in base cannot
     override. The project generated from the edited spec still reads it as off, so project.yml is put back byte for
     byte, the refusal is counted, and the finding stands.
     */
    @Test func aFixThatWouldNotTakeIsPutBack() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: Self.strictMemorySafetyOffInRelease),
        )
        defer { Self.remove(fixture.repository) }
        let spec = fixture.root.appendingPathComponent("project.yml")
        let before = try Data(contentsOf: spec)

        let run = try await Self.run(root: fixture.root, arguments: [])

        #expect(try Data(contentsOf: spec) == before)
        #expect(run.toolchainFindings == ["toolchain-require-strict-memory-safety@project.yml:5"], "\(run.records)")
        let refusals = run.fixRecord["refusalsByReason"] as? [String: Int] ?? [:]
        #expect(refusals[XcodeSpecificationFix.notRegenerated] == 1, "\(run.fixRecord)")
        let changed = run.fixRecord["changedFiles"] as? [[String: Any]] ?? []
        #expect(!changed.contains { ($0["file"] as? String ?? "").hasSuffix("/project.yml") })
    }

    /*
     A committed project's findings point at the target's line in project.pbxproj, and no run writes that file: a plain
     run leaves it byte for byte as it was, and the findings stand.
     */
    @Test func aCommittedProjectsFindingsPointAtItsTargetAndAreNeverFixed() async throws {
        try Self.requireXcodegen()
        let fixture = try Self.fixture(
            counter: Self.cleanCounter,
            specification: Self.specification(appSettings: Self.toolchainOff),
        )
        defer { Self.remove(fixture.repository) }
        try Self.commitProject(at: fixture.root)
        let projectFile = fixture.root.appendingPathComponent("Fixture.xcodeproj/project.pbxproj")
        let before = try Data(contentsOf: projectFile)
        let lines = String(decoding: before, as: UTF8.self).split(separator: "\n", omittingEmptySubsequences: false)
        let targetLine =
            try #require(
                lines.indices.first { index in
                    lines[index].hasSuffix("/* App */ = {")
                        && lines.indices.contains(index + 1) && lines[index + 1].contains("isa = PBXNativeTarget;")
                }
            ) + 1

        let checked = try await Self.run(root: fixture.root, arguments: ["--no-fix", "--no-cache"])
        #expect(
            checked.toolchainFindings == [
                "toolchain-require-swift-6-language-mode@project.pbxproj:\(targetLine)",
                "toolchain-require-upcoming-features@project.pbxproj:\(targetLine)",
                "toolchain-require-strict-memory-safety@project.pbxproj:\(targetLine)",
            ],
            "\(checked.records)",
        )
        #expect(!checked.toolchainMessage("toolchain-require-swift-6-language-mode").contains("first line"))

        let plain = try await Self.run(root: fixture.root, arguments: ["--no-cache"])
        #expect(try Data(contentsOf: projectFile) == before)
        #expect(plain.toolchainFindings.count == 3, "\(plain.toolchainFindingRecords)")
    }
}
