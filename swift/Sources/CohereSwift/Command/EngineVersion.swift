import Foundation

/*
 What this binary is and what it was built from, for the provenance record.

 The swift-syntax and swift-format versions are written down here and in `Package.swift`, and a test
 reads `Package.resolved` and fails if they disagree, because a provenance line that names the wrong
 parser is worse than none: it sends a reader to the wrong source when a finding looks wrong.

 The commit is `dev` until the release build stamps it. Go stamps the TypeScript engine at link time,
 and Swift has no equivalent flag, so stamping waits for @system_cohere_release. Until then a reader
 sees `dev`, which is true.
 */
public enum EngineVersion {
    public static let contract = 1
    public static let engine = "cohere-swift"
    public static let version = "0.1.0"
    public static let commit = "dev"
    public static let swiftSyntax = "604.0.0"
    public static let swiftFormat = "604.0.0"

    public static var sourceTreeModified: Bool {
        #if COHERE_SOURCE_TREE_MODIFIED
            true
        #else
            false
        #endif
    }

    /* The toolchain, as `swift --version` names it: `swiftlang-6.4.0.34.1`. Asked at run time, because it is also the toolchain that will build the checked package. */
    public static func toolchain(runner: ProcessRunner = ProcessRunner()) -> String {
        do {
            let result = try runner.run("swift", ["--version"], in: URL(fileURLWithPath: "/"))
            let text = String(decoding: result.standardOutput, as: UTF8.self) + result.standardError
            if let range = text.range(of: #"swiftlang-[0-9.]+"#, options: .regularExpression) {
                return String(text[range])
            }
            return "unknown (swift --version said: \(text.split(separator: "\n").first ?? ""))"
        } catch {
            return "unknown (\(error))"
        }
    }

    public static func provenance(toolchain: String) -> ProvenanceRecord {
        ProvenanceRecord(
            contract: contract,
            engine: engine,
            version: version,
            commit: commit,
            sourceTreeModified: sourceTreeModified,
            toolchain: toolchain,
            swiftSyntax: swiftSyntax,
            swiftFormat: swiftFormat
        )
    }
}
