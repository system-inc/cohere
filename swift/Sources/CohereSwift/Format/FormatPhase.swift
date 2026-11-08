import Foundation
import SwiftFormat

/*
 The fix phase's formatting half: swift-format, as a library inside this process, applying the house
 format (`HouseSwiftFormat`) to every file it is handed, but one its store already holds as formatted
 (`FormatVerdictCache`).

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

    /*
     The formatted text of every file, for a run that writes it. With a scratch, a file whose kept verdict
     (`FormatVerdictCache`) says these very bytes are formatted is not formatted again, since formatting it would hand
     back the same text, unless the fixer rewrote it (`fixed`, by path): a file the fixer touched is always formatted,
     so the formatter has the last word on every fix.

     What it learns is kept for the next run, and only what the formatter said. A file it left as it was is kept as
     formatted under its bytes. A file it rewrote is formatted once more and kept under the rewritten bytes with that
     answer, since those are the bytes the run writes and the next run reads; one extra format per rewritten file, the
     one the next run would otherwise pay. A file it failed on, or did not format, adds nothing.
     */
    func run(
        _ files: [ParsedFile],
        fixed: Set<String> = [],
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
        let (result, verdicts) = await outcomes(of: files) { file in
            if !fixed.contains(file.url.path), let kept = cache?.verdict(path: file.url.path, source: file.source),
                kept.firstChangedLine == nil
            {
                return (.unchanged, nil)
            }
            let outcome = Self.format(source: file.source, url: file.url, configuration: configuration())
            switch outcome {
                case .unchanged:
                    return (
                        outcome, .init(source: FormatVerdictCache.fingerprint(of: file.source), firstChangedLine: nil),
                    )
                case let .changed(formatted):
                    return (outcome, Self.verdict(of: formatted, url: file.url, configuration: configuration()))
                case .wouldChange, .failed:
                    return (outcome, nil)
            }
        }
        cache?.store(verdicts)
        return result
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
        let (result, verdicts) = await outcomes(of: files) { file in
            if let kept = cache?.verdict(path: file.url.path, source: file.source) {
                return (kept.firstChangedLine.map { .wouldChange(firstChangedLine: $0) } ?? .unchanged, nil)
            }
            let source = FormatVerdictCache.fingerprint(of: file.source)
            switch Self.format(source: file.source, url: file.url, configuration: configuration()) {
                case .unchanged:
                    return (.unchanged, .init(source: source, firstChangedLine: nil))
                case let .changed(formatted):
                    let line = Self.firstDifferingLine(file.source, formatted)
                    return (.wouldChange(firstChangedLine: line), .init(source: source, firstChangedLine: line))
                case let outcome:
                    return (outcome, nil)
            }
        }
        cache?.store(verdicts)
        return result
    }

    /* One file's outcome, with the verdict it adds to the store, or nil when it adds none. */
    private typealias Answer = (outcome: Outcome, verdict: FormatVerdictCache.Verdict?)

    /*
     Each file's outcome, files side by side, gathered back in input order, with the new verdict each one gave for the
     store, by path. A file that gave none, replayed or failed, has no entry.
     */
    private func outcomes(
        of files: [ParsedFile],
        _ answer: @escaping @Sendable (ParsedFile) -> Answer,
    ) async -> (Result, [String: FormatVerdictCache.Verdict]) {
        let answers = await withTaskGroup(of: (Int, Answer).self) { group in
            for (index, file) in files.enumerated() {
                group.addTask { (index, answer(file)) }
            }
            var collected = [Answer?](repeating: nil, count: files.count)
            for await (index, answered) in group {
                collected[index] = answered
            }
            return collected
        }
        var verdicts: [String: FormatVerdictCache.Verdict] = [:]
        for (file, answer) in zip(files, answers) {
            if let verdict = answer?.verdict {
                verdicts[file.url.path] = verdict
            }
        }
        return (
            Result(
                outcomes: zip(files, answers).map {
                    ($0, $1?.outcome ?? .failed(reason: "the formatter never answered"))
                }
            ),
            verdicts,
        )
    }

    static func format(_ file: ParsedFile, configuration: Configuration = HouseSwiftFormat.configuration) -> Outcome {
        format(source: file.source, url: file.url, configuration: configuration)
    }

    /* The formatter's verdict on one text as the file at this URL, for the store, or nil when it failed on it. */
    static func verdict(of source: String, url: URL, configuration: Configuration) -> FormatVerdictCache.Verdict? {
        let firstChangedLine: Int?
        switch format(source: source, url: url, configuration: configuration) {
            case .unchanged:
                firstChangedLine = nil
            case let .changed(formatted):
                firstChangedLine = firstDifferingLine(source, formatted)
            case .wouldChange, .failed:
                return nil
        }
        return .init(source: FormatVerdictCache.fingerprint(of: source), firstChangedLine: firstChangedLine)
    }

    /* One text as the file at this URL, so a text the formatter just returned can be asked again without a parse. */
    static func format(source: String, url: URL, configuration: Configuration) -> Outcome {
        do {
            var output = ""
            try SwiftFormatter(configuration: configuration).format(
                source: source,
                assumingFileURL: url,
                selection: .infinite,
                to: &output,
            )
            return output == source ? .unchanged : .changed(formatted: output)
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
