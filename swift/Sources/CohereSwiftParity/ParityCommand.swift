import CohereSwift
import Foundation

/*
 cohere-swift-parity: "never worse than the incumbent" as a measurement rather than a belief. The Swift
 version of cohere's differential against oxlint.

     cohere-swift-parity [--package <dir>] [--swiftlint <path>] [--examples <n>]

 The file set is the engine's own (`FileSet`), so the incumbents read exactly the files cohere checks: the
 same vendored, git-ignored and generated exclusions, with no second list to drift. Nothing is written to
 the package. Run it on a worktree or a copy anyway; the standing rule keeps measurement off the live
 checkouts.
 */
@main
struct ParityCommand {
    static let defaultExamples = 20

    /* The pinned SwiftLint, found from this source file: `swift/Sources/CohereSwiftParity/` up to cohere's root. */
    static let pinnedSwiftLint = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent(".cache/cohere/swiftlint-0.65.1/swiftlint")

    static func main() async {
        do {
            try await run(arguments: Array(CommandLine.arguments.dropFirst()))
        } catch {
            FileHandle.standardError.write(Data("cohere-swift-parity: \(error)\n".utf8))
            exit(2)
        }
    }

    struct UsageFailure: Error, CustomStringConvertible {
        var description: String
    }

    static func run(arguments: [String]) async throws {
        let workingDirectory = URL(fileURLWithPath: FileManager.default.currentDirectoryPath, isDirectory: true)
        var root = workingDirectory
        var swiftLint = pinnedSwiftLint
        var examples = defaultExamples
        var remaining = arguments[...]
        while let argument = remaining.popFirst() {
            guard let value = remaining.popFirst() else {
                throw UsageFailure(description: "\(argument) needs a value")
            }
            switch argument {
            case "--package": root = URL(fileURLWithPath: value, relativeTo: workingDirectory).standardizedFileURL
            case "--swiftlint": swiftLint = URL(fileURLWithPath: value, relativeTo: workingDirectory).standardizedFileURL
            case "--examples":
                guard let count = Int(value), count >= 0 else {
                    throw UsageFailure(description: "--examples takes a number, not \(value)")
                }
                examples = count
            default:
                throw UsageFailure(description: "unknown argument \(argument)")
            }
        }
        guard FileManager.default.isExecutableFile(atPath: swiftLint.path) else {
            throw UsageFailure(description: "no SwiftLint at \(swiftLint.path); the parity task (#w9hkcza) installs 0.65.1 there")
        }

        let runner = ProcessRunner()
        let package = try PackageModel.load(root: root, scratchPath: Pipeline.scratchPath(for: root), runner: runner)
        let files = try FileSet.build(package: package, runner: runner).owned.map(\.url)
        guard !files.isEmpty else {
            throw UsageFailure(description: "the engine owns no files in \(root.path), so there is nothing to compare")
        }

        let swiftLintRun = SwiftLintRun(executable: swiftLint, root: root, files: files, runner: runner)
        let cohere = try await EngineRun(root: root).findings()
        let swiftLintFindings = try swiftLintRun.findings()
        let swiftFormatFindings = try SwiftFormatLintRun(root: root, files: files, runner: runner).findings()

        var reports: [ParityReport] = []
        for mapping in RuleMapping.all {
            let incumbentFindings = mapping.incumbent == .swiftLint ? swiftLintFindings : swiftFormatFindings
            reports.append(ParityReport(
                mapping: mapping,
                cohere: cohere.filter { mapping.rules.contains($0.rule) },
                other: incumbentFindings.filter { $0.rule == mapping.incumbentRule }
            ))
        }

        /* Coverage first, so a comparison over nothing cannot read as agreement. */
        print("parity over \(files.count) files in \(root.path)")
        print("incumbents: SwiftLint \(try swiftLintRun.version()), swift-format from xcrun; \(swiftLintFindings.count) and \(swiftFormatFindings.count) findings under the mapped rules")
        print("")
        for report in reports {
            print(report.summaryLine())
        }
        for report in reports where !report.cohereOnly.isEmpty || !report.incumbentOnly.isEmpty {
            print("\n\(report.mapping.incumbent.rawValue) \(report.mapping.incumbentRule) against \(report.mapping.rules.joined(separator: " + ")), compared by \(report.mapping.comparison):")
            print(report.differences(root: root, examples: examples), terminator: "")
        }
    }
}
