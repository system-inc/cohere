import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The lint verdict cache (`LintVerdictCache`, #da5fx6d): a verdict is replayed only while everything the rules read
 still holds. Each test moves one input the cache keys by, leaves the rest, and needs the run after it to say what a
 run with nothing kept says; each is red under the mutant its comment names, which drops that one key part or one
 refusal. Serialized, because the tests that check a package each start a build server and a pool of frontends.
 */
@Suite(.serialized)
struct LintVerdictCacheTests {
    /* A package in a fresh temporary folder, with runs of the pipeline over it. */
    final class Package {
        let root: URL

        init(manifest: String = PipelineControlTests.manifest, files: [String: String]) throws {
            root = FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-lint-cache-\(UUID().uuidString)",
                isDirectory: true,
            )
            .resolvingSymlinksInPath()
            try write(manifest, to: "Package.swift")
            for (path, text) in files {
                try write(text, to: path)
            }
        }

        func write(_ text: String, to path: String) throws {
            try CheckFixture.write(text, to: path, in: root)
        }

        var storeFile: URL { LintVerdictCache.storeFile(scratchPath: Pipeline.scratchPath(for: root)) }

        /* One run with these arguments after `--root`: what it wrote, read as records. */
        func run(_ arguments: [String]) async throws -> Run {
            let options = try CommandOptions.parse(
                ["--contract", "\(EngineVersion.contract)", "--root", root.path] + arguments,
                workingDirectory: root,
            )
            var stream = Data()
            _ = try await Pipeline(
                options: options,
                writer: ContractWriter { stream.append($0) },
                workingDirectory: root,
            )
            .run()
            let records = try stream.split(separator: UInt8(ascii: "\n")).map { line in
                try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
            }
            return Run(records: records, root: root)
        }
    }

    /* What one run wrote. */
    struct Run {
        var records: [[String: Any]]
        var root: URL

        /* Every rule finding as `rule path:line:column message`, the path relative to the root, sorted. */
        var findings: [String] {
            records.filter { $0["kind"] as? String == "finding" && $0["source"] as? String == "rule" }.map { record in
                let path = CheckFixture.relative(record["file"] as? String ?? "", to: root)
                return
                    "\(record["rule"] as? String ?? "") \(path):\(record["line"] as? Int ?? 0):\(record["column"] as? Int ?? 0) \(record["message"] as? String ?? "")"
            }
            .sorted()
        }

        func findings(of rule: String) -> [String] { findings.filter { $0.hasPrefix(rule + " ") } }

        var lint: [String: Any]? { records.first { $0["kind"] as? String == "lint" } }

        var filesReplayed: Int? { lint?["filesReplayed"] as? Int }

        /* The lint record with the fields a replay may change taken out, as sorted JSON. */
        var lintAccounting: String {
            var record = lint ?? [:]
            record["elapsedMilliseconds"] = nil
            record["filesReplayed"] = nil
            let data = (try? JSONSerialization.data(withJSONObject: record, options: [.sortedKeys])) ?? Data()
            return String(decoding: data, as: UTF8.self)
        }

        var lintOutcome: String? {
            records.first { $0["kind"] as? String == "phase" && $0["name"] as? String == "lint" }?["outcome"] as? String
        }
    }

    static let lintOnly = ["--no-fix", "--lint"]
    static let checked = ["--no-fix"]

    static let forceUnwrap = "cohere-swift/force-unwrapping"
    static let emptyCount = EmptyCount().name
    static let utilsFolder = ConsistencyNoUtilsFolder().name
    static let noPrint = ConsistencyNoPrint().name
    static let fileLength = FileLength().name

    /* A library target that prints, which `consistency-no-print` reports in a library and not in an executable. */
    static func manifest(kind: String) -> String {
        """
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "Control",
            targets: [
                .\(kind)(
                    name: "Control",
                    swiftSettings: [.enableUpcomingFeature("ExistentialAny"), .enableUpcomingFeature("MemberImportVisibility"), .strictMemorySafety()]
                )
            ]
        )

        """
    }

    // MARK: The file rules' key

    /* 1. Bytes. An edit that adds a second force unwrap is reported. Red under: the file key without the source's hash. */
    @Test func anEditedFileIsJudgedAgain() async throws {
        let package = try Package(files: [
            "Sources/Control/Control.swift": "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n"
        ])
        _ = try await package.run(Self.lintOnly)
        let warm = try await package.run(Self.lintOnly)
        #expect(warm.filesReplayed == 1, "the control: an unchanged file is replayed")
        try package.write(
            "func first(_ values: [Int]) -> Int {\n    values.first! + values.last!\n}\n",
            to: "Sources/Control/Control.swift",
        )
        let edited = try await package.run(Self.lintOnly)
        #expect(edited.findings(of: Self.forceUnwrap).count == 2, "\(edited.findings)")
        #expect(edited.filesReplayed == 0)
    }

    /*
     2. A link retargeted. The file is a link into `Shared/Utils/`, then into `Shared/Tools/`, at the same path with the
     same bytes, so only the resolved path moves `consistency-no-utils-folder`. Red under: the file key without the
     resolved path.
     */
    @Test func aRetargetedLinkIsJudgedAgain() async throws {
        let text = "struct Control {\n    let value: Int\n}\n"
        let package = try Package(files: ["Shared/Utils/Control.swift": text, "Shared/Tools/Control.swift": text])
        let link = package.root.appendingPathComponent("Sources/Control/Control.swift")
        try FileManager.default.createDirectory(
            at: link.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try FileManager.default.createSymbolicLink(
            atPath: link.path,
            withDestinationPath: "../../Shared/Utils/Control.swift",
        )
        let before = try await package.run(Self.lintOnly)
        #expect(
            before.findings(of: Self.utilsFolder).count == 1,
            "the control: the link resolves into Utils: \(before.findings)",
        )

        try FileManager.default.removeItem(at: link)
        try FileManager.default.createSymbolicLink(
            atPath: link.path,
            withDestinationPath: "../../Shared/Tools/Control.swift",
        )
        let after = try await package.run(Self.lintOnly)
        #expect(after.findings(of: Self.utilsFolder).isEmpty, "\(after.findings)")
    }

    /* 3. The package root. The same bytes at the same path under another package root miss. Red under: the file key without the package root. */
    @Test func anotherPackageRootIsAnotherKey() throws {
        let path = URL(fileURLWithPath: "/package/Libraries/Utils/Sources/Kit/Kit.swift")
        let source = "struct Kit {}\n"
        func parsed(root: String) -> ParsedFile {
            ParsedFile(
                url: path,
                targetName: "Kit",
                targetKind: "library",
                source: source,
                tree: Parser.parse(source: source),
                nodeCount: 1,
                packageRoot: URL(fileURLWithPath: root),
            )
        }
        let inside = parsed(root: "/package/Libraries")
        let atUtils = parsed(root: "/package/Libraries/Utils")
        #expect(
            !ConsistencyNoUtilsFolder().findings(in: inside).isEmpty
                && ConsistencyNoUtilsFolder().findings(in: atUtils).isEmpty,
            "the control: the root decides the verdict",
        )
        #expect(LintVerdictCache.fileKey(of: inside) != LintVerdictCache.fileKey(of: atUtils))
    }

    /* 4. The target's kind. A library that prints becomes an executable, so `consistency-no-print` no longer applies. Red under: the file key without the target kind. */
    @Test func anotherTargetKindIsJudgedAgain() async throws {
        let package = try Package(
            manifest: Self.manifest(kind: "target"),
            files: ["Sources/Control/Ready.swift": "func ready() {\n    print(\"ready\")\n}\n"],
        )
        let library = try await package.run(Self.lintOnly)
        #expect(library.findings(of: Self.noPrint).count == 1, "the control: a library's print: \(library.findings)")
        try package.write(Self.manifest(kind: "executableTarget"), to: "Package.swift")
        let executable = try await package.run(Self.lintOnly)
        #expect(executable.findings(of: Self.noPrint).isEmpty, "\(executable.findings)")
    }

    /* 5. A rule's options. `file-length`'s `max` lowered below the file's length reports it. Red under: the rule set without options. */
    @Test func aRulesOptionsAreKeyed() async throws {
        let package = try Package(files: [
            "Sources/Control/Control.swift": "struct Control {\n    let value: Int\n    let other: Int\n}\n"
        ])
        try package.write(
            #"{"swift":{"rules":{"cohere-swift/file-length":["error",{"max":100}]}}}"#,
            to: "CohereSettings.json",
        )
        let loose = try await package.run(Self.lintOnly)
        #expect(loose.findings(of: Self.fileLength).isEmpty, "\(loose.findings)")
        try package.write(
            #"{"swift":{"rules":{"cohere-swift/file-length":["error",{"max":2}]}}}"#,
            to: "CohereSettings.json",
        )
        let strict = try await package.run(Self.lintOnly)
        #expect(strict.findings(of: Self.fileLength).count == 1, "\(strict.findings)")
    }

    /* 6. The rule set. A rule turned on reports on an unedited file. Red under: the context without the rule set. */
    @Test func aRuleTurnedOnJudgesKeptFiles() async throws {
        let package = try Package(files: ["Sources/Control/Control.swift": "func ready() {\n    print(\"ready\")\n}\n"])
        try package.write(
            #"{"swift":{"rules":{"cohere-swift/consistency-no-print":"off"}}}"#,
            to: "CohereSettings.json",
        )
        let off = try await package.run(Self.lintOnly)
        #expect(off.findings(of: Self.noPrint).isEmpty)
        try package.write(
            #"{"swift":{"rules":{"cohere-swift/consistency-no-print":"error"}}}"#,
            to: "CohereSettings.json",
        )
        let on = try await package.run(Self.lintOnly)
        #expect(on.findings(of: Self.noPrint).count == 1, "\(on.findings)")
    }

    /*
     7. The engine. A store another engine wrote is not read: the engine's sha256 stands for every rule's code and every
     vocabulary, word list and swift-format configuration compiled into it. Red under: the context without the engine.
     */
    @Test func anotherEnginesStoreIsNotRead() throws {
        let scratch = CheckFixture.scratch()
        let ruleSet = LintVerdictCache.ruleSet(fileRules: [Self.forceUnwrap], typedRules: [], options: [:])
        let written = LintVerdictCache(
            scratchPath: scratch,
            context: LintVerdictCache.context(engine: "one engine", ruleSet: ruleSet),
            enabledFileRules: [Self.forceUnwrap],
        )
        let entry = LintVerdictCache.Entry(
            key: "key",
            fileRulesNotApplying: [],
            fileFindings: [:],
            typedRules: [],
            typed: nil,
        )
        written.store(["/package/Control.swift": entry])
        let same = LintVerdictCache(
            scratchPath: scratch,
            context: LintVerdictCache.context(engine: "one engine", ruleSet: ruleSet),
            enabledFileRules: [Self.forceUnwrap],
        )
        #expect(
            same.fileVerdict(path: "/package/Control.swift", key: "key") != nil,
            "the control: the same engine reads it",
        )
        let another = LintVerdictCache(
            scratchPath: scratch,
            context: LintVerdictCache.context(engine: "another engine", ruleSet: ruleSet),
            enabledFileRules: [Self.forceUnwrap],
        )
        #expect(another.fileVerdict(path: "/package/Control.swift", key: "key") == nil)
    }

    // MARK: The typed rules' key

    static let holder = "struct Holder {\n    var values: [Int]\n}\n"
    static let reader = "func isEmpty(_ holder: Holder) -> Bool {\n    holder.values.count == 0\n}\n"

    /*
     Checks the package, then edits `Holder.swift` alone, then checks again: the typed verdict on the unedited
     `Reader.swift` must be what a run with nothing kept says, and must have moved, the edit's control.
     */
    static func expectTypedVerdictMoves(
        holder edited: String,
        reader: String = reader,
        rule: String = emptyCount,
        readerFindingsAfter expected: Int,
    ) async throws {
        let package = try Package(files: [
            "Sources/Control/Holder.swift": holder, "Sources/Control/Reader.swift": reader,
        ])
        let before = try await package.run(checked)
        #expect(before.findings(of: rule).count == 1, "the control: the standard library's member: \(before.findings)")
        let warm = try await package.run(checked)
        #expect(warm.filesReplayed == 2, "the control: the unchanged package is replayed")

        try package.write(edited, to: "Sources/Control/Holder.swift")
        let after = try await package.run(checked)
        let fresh = try await package.run(checked + ["--no-cache"])
        #expect(fresh.findings(of: rule).count == expected, "the edit did not move the verdict: \(fresh.findings)")
        #expect(after.findings == fresh.findings)
    }

    /* 8. A type changed in A: `values` becomes a tally of ours, whose `count` is no collection's. Red under: the typed key without the record. */
    @Test func aTypeChangedInAnotherFileMovesThisFilesVerdict() async throws {
        try await Self.expectTypedVerdictMoves(
            holder: "struct Holder {\n    var values: Tally\n}\n\nstruct Tally {\n    var count: Int\n}\n",
            readerFindingsAfter: 0,
        )
    }

    /*
     9. An overload added in A, more specific than the standard library's: `first(where:)` on `Array`, which the
     unedited call now resolves to rather than `Sequence`'s. Red under: the typed key without the record.
     */
    @Test func anOverloadAddedInAnotherFileMovesThisFilesVerdict() async throws {
        try await Self.expectTypedVerdictMoves(
            holder: Self.holder
                + "\nextension Array {\n    func first(where predicate: (Element) -> Bool) -> Element? {\n        nil\n    }\n}\n",
            reader:
                "func hasPositive(_ holder: Holder) -> Bool {\n    holder.values.first(where: { $0 > 0 }) != nil\n}\n",
            rule: ContainsOverFirstNotNil().name,
            readerFindingsAfter: 0,
        )
    }

    /*
     10. A conformance swapped through an extension in A: `count` came from a protocol of ours, and comes from
     `Collection` once `Box` conforms to it instead. Red under: the typed key without the record.
     */
    @Test func aConformanceAddedInAnotherFileMovesThisFilesVerdict() async throws {
        let ours = """
            struct Box {
                var items: [Int]
            }

            protocol Counted {}

            extension Counted {
                var count: Int { 1 }
            }

            extension Box: Counted {}

            """
        let collection = """
            struct Box {
                var items: [Int]
            }

            extension Box: Collection {
                var startIndex: Int { 0 }
                var endIndex: Int { items.endIndex }

                subscript(position: Int) -> Int { items[position] }

                func index(after index: Int) -> Int { index + 1 }
            }

            """
        let package = try Package(files: [
            "Sources/Control/Box.swift": ours,
            "Sources/Control/Reader.swift": "func isEmpty(_ box: Box) -> Bool {\n    box.count == 0\n}\n",
        ])
        let before = try await package.run(Self.checked)
        #expect(before.findings(of: Self.emptyCount).isEmpty, "the control: our own count: \(before.findings)")
        try package.write(collection, to: "Sources/Control/Box.swift")
        let after = try await package.run(Self.checked)
        let fresh = try await package.run(Self.checked + ["--no-cache"])
        #expect(
            fresh.findings(of: Self.emptyCount).count == 1,
            "the conformance did not move the verdict: \(fresh.findings)",
        )
        #expect(after.findings == fresh.findings)
    }

    /*
     21. A record deleted under a unit that still names it. A read passes over it, so the verdict kept under it is not
     replayed, and the file says what a run with nothing kept says. Red under: offering the first fresh record to the
     replay, readable or not (`candidates.first`).
     */
    @Test func aRecordDeletedUnderALiveUnitIsNotReplayed() async throws {
        let package = try Package(files: [
            "Sources/Control/Holder.swift": Self.holder, "Sources/Control/Reader.swift": Self.reader,
        ])
        _ = try await package.run(Self.checked)
        let warm = try await package.run(Self.checked)
        #expect(warm.filesReplayed == 2, "the control: both files are replayed while their records stand")

        let storePath = IndexStore.storePath(layout: .check(scratchPath: Pipeline.scratchPath(for: package.root)))
        let store = try IndexStore(libraryPath: IndexStore.toolchainLibraryPath(), storePath: storePath)
        let reader = package.root.appendingPathComponent("Sources/Control/Reader.swift").resolvingSymlinksInPath().path
        let unit = try #require(
            store.units().filter { !$0.isSystem && $0.mainFile == reader }.max { $0.written < $1.written },
            "no unit names \(reader)",
        )
        let record = try #require(unit.ownRecords.first)
        let records = storePath.appendingPathComponent("v5/records", isDirectory: true)
        let file = try #require(
            FileManager.default.enumerator(at: records, includingPropertiesForKeys: nil)?
                .compactMap { $0 as? URL }.first { $0.lastPathComponent == record },
            "the record \(record) is not under \(records.path)",
        )
        try FileManager.default.removeItem(at: file)

        let after = try await package.run(Self.checked)
        let fresh = try await package.run(Self.checked + ["--no-cache"])
        #expect(after.filesReplayed == 1, "a verdict was replayed under a record no read can open")
        #expect(after.findings == fresh.findings)
    }

    /* A standalone script of ours (#13nbb1s): top-level code no target compiles, holding the empty-count shape. */
    static let script = "let values = [1, 2]\nif values.count == 0 {\n    print(\"empty\")\n}\n"

    /*
     22. A standalone script is a file of ours like any other: its own target, kind `executable`, checked alone by
     `ScriptTypesPhase`, which writes its index unit under `<scratch>/scripts`. So its typed verdict comes from a record
     there, and is kept and replayed. An edit to the script, which moves a file rule's verdict and a typed one, misses,
     and the run after it says what a run with nothing kept says. Red under: the file key without the source's hash.
     */
    @Test func aScriptsVerdictIsKeptAndMissesAfterAnEdit() async throws {
        let package = try Package(files: [
            ".git/info/exclude": "",
            "Sources/Control/Control.swift": PipelineControlTests.cleanSource,
            "Tools/Probe.swift": Self.script,
        ])
        let checked = try await package.run(Self.checked)
        #expect(
            checked.findings(of: Self.emptyCount).map { $0.split(separator: " ")[1] } == ["Tools/Probe.swift:2:4"],
            "the control: the script is checked and its typed rule judges it: \(checked.findings)",
        )
        let warm = try await package.run(Self.checked)
        #expect(warm.filesReplayed == 2, "the script and the target's file are both replayed")
        let probe = try #require(try Self.entries(package).first { $0.key.hasSuffix("/Tools/Probe.swift") }?.value)
        #expect(probe.typed != nil, "the script's typed verdict was not kept: it was not read from an index record")
        #expect(warm.findings == checked.findings)

        try package.write(
            "let values = [1, 2]\nif values.count == 0 || 0 == values.count {\n    print(values.first!)\n}\n",
            to: "Tools/Probe.swift",
        )
        let edited = try await package.run(Self.checked)
        let fresh = try await package.run(Self.checked + ["--no-cache"])
        #expect(edited.findings(of: Self.emptyCount).count == 2, "\(edited.findings)")
        #expect(edited.findings(of: Self.forceUnwrap).count == 1, "the edit's file-rule finding: \(edited.findings)")
        #expect(edited.filesReplayed == 1, "only the target's file is replayed after the script's edit")
        #expect(edited.findings == fresh.findings)
    }

    /*
     23. A standalone script's platform. The same bytes at the same path, read as a script or as a target's file,
     are judged apart (`consistency-require-matching-file-name` passes a script over, as it does `main.swift`), so they are keyed
     apart. Red under: the file key without the script's platform.
     */
    @Test func aScriptsPlatformIsKeyed() {
        let source = "struct Helper {}\nprint(Helper())\n"
        var asTarget = ParsedFile(
            url: URL(fileURLWithPath: "/package/Tools/Probe.swift"),
            targetName: "Probe",
            targetKind: StandaloneScript.targetKind,
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 1,
            packageRoot: URL(fileURLWithPath: "/package"),
        )
        var asScript = asTarget
        asScript.scriptPlatform = .macOS
        #expect(
            ConsistencyRequireMatchingFileName().applies(to: asTarget)
                && !ConsistencyRequireMatchingFileName().applies(to: asScript),
            "the control: the script's platform decides whether the rule judges the file",
        )
        #expect(LintVerdictCache.fileKey(of: asTarget) != LintVerdictCache.fileKey(of: asScript))
        asTarget.scriptPlatform = .iOS
        #expect(LintVerdictCache.fileKey(of: asTarget) != LintVerdictCache.fileKey(of: asScript))
    }

    /* 11. The modules the package owns. The same record under other owned modules misses. Red under: the typed key without the owned modules. */
    @Test func otherOwnedModulesAreAnotherKey() {
        let record = IndexStore.FreshRecord.Identity(storePath: "/scratch/index-store", record: "Control.swift-1ABC")
        #expect(
            LintVerdictCache.typedKey(fileKey: "key", record: record, ownedModules: ["Control"])
                != LintVerdictCache.typedKey(fileKey: "key", record: record, ownedModules: ["Control", "Status"])
        )
        #expect(
            LintVerdictCache.typedKey(fileKey: "key", record: record, ownedModules: ["Control", "Status"])
                == LintVerdictCache.typedKey(fileKey: "key", record: record, ownedModules: ["Status", "Control"]),
            "the control: the set, not its order",
        )
    }

    /*
     12. A body edit the index does not describe. The file is asked of sourcekitd and judged as it stands, and no typed
     verdict is kept for it, since no record stands for what sourcekitd said. Red under: `freshRecord` without its
     `written >= modified` filter, which answers from the record of the text before the edit.
     */
    @Test func aFileTheIndexNoLongerDescribesIsNeverReplayedFromARecord() async throws {
        let package = try Package(files: [
            "Sources/Control/Holder.swift": Self.holder, "Sources/Control/Reader.swift": Self.reader,
        ])
        _ = try await package.run(Self.checked)
        try package.write(
            "func isEmpty(_ holder: Holder) -> Bool {\n    let none = holder.values.count != 0\n    return none || holder.values.count == 0\n}\n",
            to: "Sources/Control/Reader.swift",
        )
        let edited = try await package.run(Self.checked)
        let build = edited.records.first { $0["kind"] as? String == "types" }?["build"] as? String ?? ""
        #expect(
            build.contains(TypesPhase.checkedInProcessWords),
            "the edit was built, so no file was left to sourcekitd: \(build)",
        )
        #expect(
            edited.findings(of: Self.emptyCount).map { $0.split(separator: " ")[1] } == [
                "Sources/Control/Reader.swift:2:16", "Sources/Control/Reader.swift:3:20",
            ],
            "\(edited.findings)",
        )
        let stored = try Self.entries(package)
        let reader = try #require(stored.first { $0.key.hasSuffix("/Reader.swift") }?.value)
        #expect(reader.typed == nil, "a verdict sourcekitd answered was kept")
    }

    /*
     13. A file no source of symbols described, a lint crash, keeps no typed verdict, and a later run with a check
     judges it. Red under: keeping a typed verdict for every file whose file rules are kept. That mutant cannot
     produce a wrong verdict, since a typed key needs a record, which a crashed file has none of; the test pins the
     refusal.
     */
    @Test func aFileTheTypedRulesCouldNotJudgeKeepsNoTypedVerdict() async throws {
        let package = try Package(
            manifest: PipelineControlTests.unresolvableManifest,
            files: ["Sources/Control/Reader.swift": Self.holder + "\n" + Self.reader],
        )
        let crashed = try await package.run(Self.lintOnly)
        #expect((crashed.lint?["crashes"] as? [Any])?.count == 1, "the control: the typed rules could not judge it")
        let stored = try Self.entries(package)
        let reader = try #require(
            stored.first { $0.key.hasSuffix("/Reader.swift") }?.value,
            "its file rules' verdict was kept",
        )
        #expect(reader.typed == nil)

        try package.write(PipelineControlTests.manifest, to: "Package.swift")
        let judged = try await package.run(Self.checked)
        #expect(judged.findings(of: Self.emptyCount).count == 1, "\(judged.findings)")
    }

    // MARK: Refusals and modes

    /* 14. A verdict holding a rule's failure is not kept, so the rule is tried again. Red under: keeping it. */
    @Test func aVerdictHoldingARulesFailureIsNotKept() {
        let cache = LintVerdictCache(
            scratchPath: CheckFixture.scratch(),
            context: "context",
            enabledFileRules: [SwiftFormatRule.lowerCamelCaseRuleName],
        )
        let failed = FindingRecord(
            source: .rule,
            file: "/package/Control.swift",
            line: 1,
            column: 1,
            severity: .error,
            rule: SwiftFormatRule.lowerCamelCaseRuleName,
            messageId: LintVerdictCache.failureMessageId,
            message: "the rule could not run",
        )
        let verdict = Linter.Verdict(
            fileRules: [SwiftFormatRule.lowerCamelCaseRuleName: [failed]],
            typedRules: [],
            typedFindings: [:],
        )
        #expect(cache.entry(fileKey: "key", verdict: verdict, typedKey: nil) == nil)
        var clean = verdict
        clean.fileRules = [SwiftFormatRule.lowerCamelCaseRuleName: []]
        #expect(
            cache.entry(fileKey: "key", verdict: clean, typedKey: nil) != nil,
            "the control: a clean verdict is kept",
        )
    }

    /*
     15. A run that stopped before lint keeps nothing: a type error in one file must not leave the other file's verdict
     kept as nothing. Red under: storing every file's verdict as empty when lint is cut off.
     */
    @Test func aRunCutOffBeforeLintKeepsNothing() async throws {
        let package = try Package(files: [
            "Sources/Control/First.swift": "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n",
            "Sources/Control/Broken.swift": "func broken() -> Int {\n    1\n}\n",
        ])
        _ = try await package.run(Self.checked)
        try package.write("func broken() -> Int {\n    \"one\"\n}\n", to: "Sources/Control/Broken.swift")
        let cutOff = try await package.run(Self.checked)
        #expect(cutOff.lintOutcome == "notReached", "the control: the type error stops lint")
        try package.write("func broken() -> Int {\n    2\n}\n", to: "Sources/Control/Broken.swift")
        let after = try await package.run(Self.checked)
        #expect(after.findings(of: Self.forceUnwrap).count == 1, "\(after.findings)")
    }

    /*
     16. A file whose text moved after it was linted keeps no verdict. Red under: no reading again before keeping. That
     mutant cannot produce a wrong verdict, since the entry is keyed by the text linted and the next run hashes the
     text it reads; the test pins the refusal.
     */
    @Test func aFileThatMovedDuringTheRunKeepsNoVerdict() throws {
        let root = CheckFixture.scratch()
        let url = root.appendingPathComponent("Control.swift")
        let source = "struct Control {}\n"
        try CheckFixture.write(source, to: "Control.swift", in: root)
        let file = ParsedFile(
            url: url,
            targetName: "Control",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 1,
        )
        let cache = LintVerdictCache(scratchPath: root, context: "context", enabledFileRules: [Self.forceUnwrap])
        let verdict = Linter.Verdict(fileRules: [Self.forceUnwrap: []], typedRules: [], typedFindings: [:])
        func entries() -> [String: LintVerdictCache.Entry] {
            cache.entries(
                of: [file],
                verdicts: [url.path: verdict],
                keys: [url.path: LintVerdictCache.fileKey(of: file)],
                replayed: [],
                typedKeys: [:],
            )
        }
        #expect(entries()[url.path] != nil, "the control: an unmoved file is kept")
        try CheckFixture.write("struct Control {\n    let value: Int\n}\n", to: "Control.swift", in: root)
        #expect(entries()[url.path] == nil)
    }

    /*
     17. `--no-cache` reads no verdict. A kept finding's message is changed in the store: a run that reads the store
     says the changed message, the control, and a `--no-cache` run says the rule's own. Red under: reading the
     project's store under `--no-cache`.
     */
    @Test func aRunWithNoCacheReadsNoVerdict() async throws {
        let package = try Package(files: [
            "Sources/Control/Control.swift": "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n"
        ])
        let honest = try await package.run(Self.lintOnly)
        let message = try #require(honest.findings(of: Self.forceUnwrap).first)
        let stored = try String(contentsOf: package.storeFile, encoding: .utf8)
        let words = try #require(
            honest.records.first { $0["rule"] as? String == Self.forceUnwrap }?["message"] as? String
        )
        try stored.replacingOccurrences(of: Self.jsonEscaped(words), with: "planted").write(
            to: package.storeFile,
            atomically: true,
            encoding: .utf8,
        )
        let replayed = try await package.run(Self.lintOnly)
        #expect(
            replayed.findings(of: Self.forceUnwrap).first?.hasSuffix(" planted") == true,
            "the control: the store is read",
        )
        let fresh = try await package.run(Self.lintOnly + ["--no-cache"])
        #expect(fresh.findings(of: Self.forceUnwrap).first == message)
    }

    /* 18. A kept verdict that proposes a fix is still fixed by a run that writes. Red under: skipping the fixer on every kept file. */
    @Test func aKeptFixIsStillApplied() async throws {
        let package = try Package(files: ["Sources/Control/Control.swift": "fileprivate let limit = 3\n"])
        let proposed = try await package.run(Self.lintOnly)
        #expect(
            !proposed.findings(of: PrivateOverFileprivate().name).isEmpty,
            "the control: the rule proposes its fix",
        )
        _ = try await package.run(["--fix"])
        let text = try String(
            contentsOf: package.root.appendingPathComponent("Sources/Control/Control.swift"),
            encoding: .utf8,
        )
        #expect(text == "private let limit = 3\n")
    }

    /*
     19. The lint record of a replayed run is the record of a walked one but for elapsed time and the replay count:
     rules that do not apply stay unasked (`consistency-no-print` in an executable is silent). Red under: keeping every
     enabled file rule as applying.
     */
    @Test func aReplayedRunAccountsAsAWalkedOne() async throws {
        let package = try Package(
            manifest: Self.manifest(kind: "executableTarget"),
            files: ["Sources/Control/main.swift": "let values = [1, 2]\nlet first = values.first!\n"],
        )
        let walked = try await package.run(Self.lintOnly)
        let replayed = try await package.run(Self.lintOnly)
        #expect(walked.filesReplayed == 0)
        #expect(replayed.filesReplayed == 1)
        #expect(
            (walked.lint?["rulesSilent"] as? [String])?.contains(Self.noPrint) == true,
            "the control: a rule that never applied",
        )
        #expect(replayed.lintAccounting == walked.lintAccounting)
        #expect(replayed.findings == walked.findings)
    }

    /*
     20. Identity: after each edit and its revert, a body edit and an interface edit, a run with the store says what a
     `--no-cache` run says. Red under: the file key without the source's hash.
     */
    @Test func everyRunSaysWhatARunWithNothingKeptSays() async throws {
        let package = try Package(files: [
            "Sources/Control/Holder.swift": Self.holder,
            "Sources/Control/Reader.swift": Self.reader,
            "Sources/Control/First.swift": "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n",
        ])
        let body = "func first(_ values: [Int]) -> Int {\n    values.last! + values.first!\n}\n"
        let interface = Self.holder + "\nstruct Tally {\n    var count: Int\n}\n"
        let steps: [(String, String, String)] = [
            (
                "unchanged", "Sources/Control/First.swift",
                "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n",
            ),
            ("body edit", "Sources/Control/First.swift", body),
            (
                "body revert", "Sources/Control/First.swift",
                "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n",
            ),
            ("interface edit", "Sources/Control/Holder.swift", interface),
            ("interface revert", "Sources/Control/Holder.swift", Self.holder),
        ]
        _ = try await package.run(Self.checked)
        for (step, path, text) in steps {
            try package.write(text, to: path)
            let kept = try await package.run(Self.checked)
            let fresh = try await package.run(Self.checked + ["--no-cache"])
            #expect(kept.findings == fresh.findings, "\(step): \(kept.findings) against \(fresh.findings)")
        }
    }

    /* The entries the package's store holds, by path. */
    static func entries(_ package: Package) throws -> [String: LintVerdictCache.Entry] {
        struct Store: Decodable {
            var entries: [String: LintVerdictCache.Entry]
        }
        return try JSONDecoder().decode(Store.self, from: Data(contentsOf: package.storeFile)).entries
    }

    /* A message as `JSONEncoder` writes it inside the store, so it can be found there. */
    static func jsonEscaped(_ text: String) -> String {
        let encoded = (try? JSONEncoder().encode(text)).map { String(decoding: $0, as: UTF8.self) } ?? "\"\(text)\""
        return String(encoded.dropFirst().dropLast())
    }
}
