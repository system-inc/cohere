import Foundation
import Synchronization

/*
 What this binary is and what it was built from, for the provenance record.

 The swift-syntax and swift-format versions are written down here and in `Package.swift`, and a test
 reads `Package.resolved` and fails if they disagree, because a provenance line that names the wrong
 parser is worse than none: it sends a reader to the wrong source when a finding looks wrong.

 The commit is `dev` unless the release stamped it. Go stamps the TypeScript engine at link time, and
 Swift has no equivalent flag, and a manifest can pass only boolean defines, never a string. So the
 release writes `EngineReleaseStamp.generated.swift` beside this file, holding the 40-hex commit inside
 `#if COHERE_RELEASE_STAMP`, builds with `-Xswiftc -DCOHERE_RELEASE_STAMP`, and removes the file. The file
 is gitignored, so the stamp never marks the tree modified (measured: SwiftPM's `hasUncommittedChanges`
 skips ignored files). Neither half can fail quietly: a build without the define says `dev` even with a
 stale file lying there, and a build with the define and no file does not compile.
 */
public enum EngineVersion {
    public static let contract = 3
    public static let engine = "cohere-swift"
    public static let version = "0.1.0"
    #if COHERE_RELEASE_STAMP
        public static let commit = EngineReleaseStamp.commit
    #else
        public static let commit = "dev"
    #endif
    public static let swiftSyntax = "604.0.0"
    public static let swiftFormat = "604.0.0"

    public static var sourceTreeModified: Bool {
        #if COHERE_SOURCE_TREE_MODIFIED
            true
        #else
            false
        #endif
    }

    /*
     The environment value a caller sets to what `swift --version` printed, so a process tree that already asked
     does not ask again: the front door asks once and every engine below it reads the answer. The engine reads it
     exactly as it reads its own question's output, so a value given here names the same toolchain, and keys the
     same caches (`PackageDescriptionCache`, `BuildInputSnapshot`), as asking would. A value naming no toolchain
     is not trusted, and the engine asks. It is the caller's own fresh answer, set for the children of one run:
     a value exported by hand and left behind would outlive the toolchain it names.
     */
    public static let toolchainVariable = "COHERE_SWIFT_VERSION_OUTPUT"

    /* This process's `swift --version` output, kept after the first question: the toolchain does not change under a running engine. */
    private static let knownVersionOutput = Mutex<String?>(nil)

    /*
     The toolchain, as `swift --version` names it: `swiftlang-6.4.0.34.1`. Asked at run time, because it is also the
     toolchain that will build the checked package, and asked once per process, since every phase that keys on it
     asked again (0.11s each time, measured).
     */
    public static func toolchain(
        runner: ProcessRunner = ProcessRunner(),
        environment: [String: String] = ProcessInfo.processInfo.environment,
    ) -> String {
        let output = versionOutput(runner: runner, environment: environment)
        if let named = toolchainNamed(in: output) {
            return named
        }
        return output.hasPrefix("unknown (")
            ? output : "unknown (swift --version said: \(output.split(separator: "\n").first ?? ""))"
    }

    /*
     Everything `swift --version` printed, the caller's given answer when it names a toolchain, else this process's
     own question's: what a cache keyed on the whole toolchain keys on (`SDKIndexSeed`). Asked once per process, as
     `toolchain` is, and the same answer it reads.
     */
    public static func versionOutput(
        runner: ProcessRunner = ProcessRunner(),
        environment: [String: String] = ProcessInfo.processInfo.environment,
    ) -> String {
        knownVersionOutput.withLock { known in
            if let known {
                return known
            }
            let given = environment[toolchainVariable].flatMap { toolchainNamed(in: $0) == nil ? nil : $0 }
            let answer = given ?? askVersionOutput(runner: runner)
            known = answer
            return answer
        }
    }

    /* The toolchain the caller's environment names, or nil when it names none. */
    static func toolchainGiven(_ environment: [String: String]) -> String? {
        environment[toolchainVariable].flatMap(toolchainNamed(in:))
    }

    /* The `swiftlang-…` identity in what `swift --version` printed, or nil when it holds none. */
    static func toolchainNamed(in versionOutput: String) -> String? {
        versionOutput.range(of: #"swiftlang-[0-9.]+"#, options: .regularExpression).map { String(versionOutput[$0]) }
    }

    private static func askVersionOutput(runner: ProcessRunner) -> String {
        do {
            let result = try runner.run("swift", ["--version"], in: URL(fileURLWithPath: "/"))
            return String(decoding: result.standardOutput, as: UTF8.self) + result.standardError
        }
        catch {
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
            swiftFormat: swiftFormat,
        )
    }
}
