import Foundation
import SwiftFormat

/*
 The fix phase's formatting half: swift-format, as a library inside this process, applying the
 `.swift-format` nearest each file.

 It formats from source text rather than from the tree the engine already parsed. The source-text entry
 point is the one measured byte-identical to `xcrun swift-format` (803 Presence files, 149 ahraos-macos
 files, 117 of them actually rewritten). The tree entry point needs the tree folded with swift-format's
 operator table first, and nothing has measured it. The cost is a second parse inside the formatter,
 about a millisecond per file across cores, and the parity is worth more than that.

 A file with no `.swift-format` at or above it, up to the repository root, is not formatted, and that is
 said with its reason. A formatter that declined a file and a formatter that found it already formatted
 would otherwise print the same zero. The lookup stops at the repository root so a config belonging to
 some unrelated directory above the checkout can never format our code.
 */
struct FormatPhase {
    /* What formatting did to each file. */
    enum Outcome: Sendable {
        case unchanged
        case changed(formatted: String)
        case declined(reason: String)
        case failed(reason: String)
    }

    /* Per-file outcomes, in input order. */
    struct Result: Sendable {
        var outcomes: [(file: ParsedFile, outcome: Outcome)]
    }

    let boundary: URL

    func run(_ files: [ParsedFile]) async -> Result {
        let boundary = boundary
        let outcomes = await withTaskGroup(of: (Int, Outcome).self) { group in
            for (index, file) in files.enumerated() {
                group.addTask { (index, Self.format(file, boundary: boundary)) }
            }
            var collected = [Outcome?](repeating: nil, count: files.count)
            for await (index, outcome) in group {
                collected[index] = outcome
            }
            return collected
        }
        return Result(outcomes: zip(files, outcomes).map { ($0, $1 ?? .failed(reason: "the formatter never answered")) })
    }

    static func format(_ file: ParsedFile, boundary: URL) -> Outcome {
        guard let configurationFile = configurationFile(for: file.url, boundary: boundary) else {
            return .declined(reason: "no .swift-format at or above it")
        }
        do {
            let configuration = try Configuration(contentsOf: configurationFile)
            var output = ""
            try SwiftFormatter(configuration: configuration).format(
                source: file.source,
                assumingFileURL: file.url,
                selection: .infinite,
                to: &output
            )
            return output == file.source ? .unchanged : .changed(formatted: output)
        } catch {
            return .failed(reason: "\(error)")
        }
    }

    /* The nearest `.swift-format` from the file's directory up to and including the boundary, swift-format's own rule with an upper bound. */
    static func configurationFile(for file: URL, boundary: URL) -> URL? {
        let boundaryPath = boundary.resolvingSymlinksInPath().path
        var directory = file.resolvingSymlinksInPath().deletingLastPathComponent()
        while true {
            let candidate = directory.appendingPathComponent(".swift-format")
            if FileManager.default.isReadableFile(atPath: candidate.path) {
                return candidate
            }
            if directory.path == boundaryPath || directory.path == "/" || !directory.path.hasPrefix(boundaryPath) {
                return nil
            }
            directory.deleteLastPathComponent()
        }
    }

    /* The first line where the formatted text differs, for the finding a `--no-fix` run reports instead of writing. */
    static func firstDifferingLine(_ original: String, _ formatted: String) -> Int {
        let originalLines = original.split(separator: "\n", omittingEmptySubsequences: false)
        let formattedLines = formatted.split(separator: "\n", omittingEmptySubsequences: false)
        for index in 0..<max(originalLines.count, formattedLines.count) {
            let before = index < originalLines.count ? originalLines[index] : nil
            let after = index < formattedLines.count ? formattedLines[index] : nil
            if before != after {
                return index + 1
            }
        }
        return 1
    }
}
