import Foundation
import Testing

@testable import CohereSwift

/*
 `unused-import` end to end on a real package of three targets, through the `--unused` report. `Base` declares
 the types, `Bridge` re-exports `Base`, and `Control`'s files each hold one case: an import that is used, one
 used only through a re-export, two that nothing uses, one kept alive by nothing but a sibling's re-export of a
 module the file imports directly, a file with `#if` that must be left unchecked, and an `@_exported` import
 that is API and never reported. The records are a report, so the summary counts no findings.
 */
@Suite(.serialized)
struct UnusedImportsTests {
    static let manifest = """
        // swift-tools-version:6.0
        import PackageDescription

        let features: [SwiftSetting] = [.enableUpcomingFeature("ExistentialAny"), .enableUpcomingFeature("MemberImportVisibility")]

        let package = Package(
            name: "Control",
            targets: [
                .target(name: "Base", swiftSettings: features),
                .target(name: "Bridge", dependencies: ["Base"], swiftSettings: features),
                .target(name: "Control", dependencies: ["Base", "Bridge"], swiftSettings: features),
            ]
        )

        """

    static let files: [String: String] = [
        "Base/Item.swift": """
            public struct Item {
                public init() {}
            }

            """,
        "Bridge/Exports.swift": """
            @_exported import Base

            """,
        "Control/UsesBase.swift": """
            import Base

            func make() -> Item { Item() }

            """,
        "Control/ViaBridge.swift": """
            import Bridge

            func makeThroughTheBridge() -> Item { Item() }

            """,
        "Control/Unused.swift": """
            import Foundation
            import Base

            func plain() -> Int { 1 }

            """,
        "Control/Direct.swift": """
            import Base
            import Bridge

            func direct() -> Item { Item() }

            """,
        "Control/Conditional.swift": """
            import Foundation

            #if DEBUG
            func stamp() -> Date { Date() }
            #endif

            """,
        "Control/Exported.swift": """
            @_exported import Base

            """,
    ]

    /* One `--no-fix --unused` run: each unused record as `file:line subject`, the coverage record, the phase, and how many findings carried the rule. */
    static func run() async throws -> (items: [String], coverage: [String: Any], phase: String, findings: Int) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-unused-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        try manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        try PipelineControlTests.configuration.write(to: root.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
        for (path, source) in files {
            let url = root.appendingPathComponent("Sources/\(path)")
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try source.write(to: url, atomically: true, encoding: .utf8)
        }
        let options = try CommandOptions.parse(["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix", "--unused"], workingDirectory: root)
        var stream = Data()
        let writer = ContractWriter { stream.append($0) }
        _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
        var items: [String] = []
        var coverage: [String: Any] = [:]
        var phase = ""
        var findings = 0
        for line in stream.split(separator: UInt8(ascii: "\n")) {
            let record = try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
            switch record["kind"] as? String {
            case "unused":
                let file = (record["file"] as? String ?? "").components(separatedBy: "/Sources/").last ?? ""
                items.append("\(file):\(record["line"] as? Int ?? 0) \(record["subject"] as? String ?? "")")
            case "unusedCoverage":
                coverage = record
            case "phase" where record["name"] as? String == "unused":
                phase = record["outcome"] as? String ?? ""
            case "finding" where record["rule"] as? String == UnusedImports.ruleName:
                findings += 1
            default:
                break
            }
        }
        return (items, coverage, phase, findings)
    }

    @Test func reportsExactlyTheImportsNothingUses() async throws {
        let run = try await Self.run()
        #expect(run.phase == "ran")
        #expect(
            run.items.sorted() == [
                "Control/Direct.swift:2 import Bridge",
                "Control/Unused.swift:1 import Foundation",
                "Control/Unused.swift:2 import Base",
            ],
            "the used, re-exported, conditional and @_exported imports must not be reported: \(run.items)"
        )
        #expect(run.coverage["found"] as? Int == 3)
        #expect(run.coverage["rule"] as? String == UnusedImports.ruleName)
        let notChecked = run.coverage["filesNotChecked"] as? [String: Int] ?? [:]
        #expect(notChecked == [UnusedImports.conditional: 1], "only the #if file is left unchecked: \(notChecked)")
        let skipped = run.coverage["skipped"] as? [String: Int] ?? [:]
        #expect(skipped.values.reduce(0, +) == 2, "Bridge's and Control's @_exported imports are counted as never reported: \(skipped)")
        /* A report, not a gate: nothing it found is a finding. */
        #expect(run.findings == 0, "unused-import findings must be unused records, never finding records")
    }
}
