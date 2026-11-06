import Foundation
import SwiftFormat

/*
 The fix phase's formatting half: swift-format, as a library inside this process, applying the house
 format (`HouseSwiftFormat`) to every file it is handed.

 It formats from source text rather than from the tree the engine already parsed. The source-text entry
 point is the one measured byte-identical to `xcrun swift-format` (803 Presence files, 149 ahraos-macos
 files, 117 of them actually rewritten). The tree entry point needs the tree folded with swift-format's
 operator table first, and nothing has measured it. The cost is a second parse inside the formatter,
 about a millisecond per file across cores, and the parity is worth more than that.
 */
struct FormatPhase {
    /* What formatting did to each file, or under `--no-fix` would do. */
    enum Outcome: Sendable {
        case unchanged
        case changed(formatted: String)
        /* A `--no-fix` verdict: formatting would move this file, first at this line. The text is not kept. */
        case wouldChange(firstChangedLine: Int)
        case failed(reason: String)
    }

    /* Per-file outcomes, in input order. */
    struct Result: Sendable {
        var outcomes: [(file: ParsedFile, outcome: Outcome)]
    }

    /* The house format, made fresh for each use, since swift-format's `Configuration` does not cross tasks. A test passes another. */
    var configuration: @Sendable () -> Configuration = { HouseSwiftFormat.configuration }

    /* The formatted text of every file, for a run that writes it. */
    func run(_ files: [ParsedFile]) async -> Result {
        await outcomes(of: files) { file in Self.format(file, configuration: configuration()) }
    }

    /*
     A `--no-fix` run's verdicts: whether formatting would move each file, and its first line that would move. With
     a scratch, each verdict comes from `FormatVerdictCache` when it was given for the same bytes under the same
     configuration and formatter, and every new verdict is stored for the next run.
     */
    func check(
        _ files: [ParsedFile],
        scratchPath: URL?,
        swiftFormatVersion: String = EngineVersion.swiftFormat,
    ) async -> Result {
        let cache = scratchPath.map {
            FormatVerdictCache(
                scratchPath: $0,
                configuration: configuration(),
                swiftFormatVersion: swiftFormatVersion,
            )
        }
        let result = await outcomes(of: files) { file in
            if let kept = cache?.verdict(path: file.url.path, source: file.source) {
                return kept.firstChangedLine.map { .wouldChange(firstChangedLine: $0) } ?? .unchanged
            }
            switch Self.format(file, configuration: configuration()) {
                case let .changed(formatted):
                    return .wouldChange(firstChangedLine: Self.firstDifferingLine(file.source, formatted))
                case let outcome:
                    return outcome
            }
        }
        if let cache {
            var verdicts: [String: FormatVerdictCache.Verdict] = [:]
            for (file, outcome) in result.outcomes {
                let source = FormatVerdictCache.fingerprint(of: file.source)
                switch outcome {
                    case .unchanged:
                        verdicts[file.url.path] = .init(source: source, firstChangedLine: nil)
                    case let .wouldChange(line):
                        verdicts[file.url.path] = .init(source: source, firstChangedLine: line)
                    case .changed, .failed:
                        break
                }
            }
            cache.store(verdicts)
        }
        return result
    }

    /* Each file's outcome, files side by side, gathered back in input order. */
    private func outcomes(
        of files: [ParsedFile],
        _ outcome: @escaping @Sendable (ParsedFile) -> Outcome,
    ) async -> Result {
        let outcomes = await withTaskGroup(of: (Int, Outcome).self) { group in
            for (index, file) in files.enumerated() {
                group.addTask { (index, outcome(file)) }
            }
            var collected = [Outcome?](repeating: nil, count: files.count)
            for await (index, answered) in group {
                collected[index] = answered
            }
            return collected
        }
        return Result(
            outcomes: zip(files, outcomes).map { ($0, $1 ?? .failed(reason: "the formatter never answered")) }
        )
    }

    static func format(_ file: ParsedFile, configuration: Configuration = HouseSwiftFormat.configuration) -> Outcome {
        do {
            var output = ""
            try SwiftFormatter(configuration: configuration).format(
                source: file.source,
                assumingFileURL: file.url,
                selection: .infinite,
                to: &output,
            )
            return output == file.source ? .unchanged : .changed(formatted: output)
        }
        catch {
            return .failed(reason: "\(error)")
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
