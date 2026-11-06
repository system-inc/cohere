import Foundation
import SwiftFormat
import SwiftParser
import Testing

@testable import CohereSwift

/*
 A `--no-fix` run replays the formatter's last verdict on a file only when nothing that verdict read has moved:
 the file's bytes at its path, the house format's configuration, and the swift-format version. Each test plants a
 verdict the formatter would never give, so a replay is visible, and then changes one input and expects the
 formatter's own answer.
 */
struct FormatVerdictCacheTests {
    /* Formatted the house way. */
    static let formatted = "let value = 1\n"

    /* 97 characters on one line: inside the house's 120, past a line length of 80, where the formatter breaks it. */
    static let longLine = "let sentence = \"" + String(repeating: "a", count: 80) + "\"\n"

    static let narrow: @Sendable () -> Configuration = {
        var configuration = HouseSwiftFormat.configuration
        configuration.lineLength = 80
        return configuration
    }

    static func scratch() throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-format-verdicts-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    /* A file on disk with this text, parsed. */
    static func file(_ name: String, source: String, in directory: URL) throws -> ParsedFile {
        let url = directory.appendingPathComponent(name)
        try source.write(to: url, atomically: true, encoding: .utf8)
        return FormatPhaseTests.parsed(url, source: source)
    }

    /* Stores a verdict no formatter gave, line 7 moved, for this text at this path under the house format. */
    static func plant(path: String, source: String, scratch: URL) {
        FormatVerdictCache(scratchPath: scratch, configuration: HouseSwiftFormat.configuration).store([
            path: .init(source: FormatVerdictCache.fingerprint(of: source), firstChangedLine: 7)
        ])
    }

    /* The first line each outcome says would move, nil for a file that would not move, or a word for anything else. */
    static func lines(_ result: FormatPhase.Result) -> [String] {
        result.outcomes.map(\.outcome).map { outcome in
            switch outcome {
                case .unchanged: "unchanged"
                case let .wouldChange(line): "line \(line)"
                case .changed: "rewritten"
                case let .failed(reason): "failed: \(reason)"
            }
        }
    }

    /* The configuration is part of the key: a verdict given under the house's line length is not the verdict under another. */
    @Test func aNewConfigurationGetsANewVerdict() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.longLine, in: scratch)
        guard case .changed = FormatPhase.format(file, configuration: Self.narrow()) else {
            Issue.record("the narrow configuration leaves the long line as it is, so this test would prove nothing")
            return
        }
        #expect(Self.lines(await FormatPhase().check([file], scratchPath: scratch)) == ["unchanged"])
        #expect(
            Self.lines(await FormatPhase(configuration: Self.narrow).check([file], scratchPath: scratch)) == ["line 1"]
        )
    }

    @Test func aVerdictIsReplayedForTheSameBytesAtTheSamePathUnderTheSameFormatter() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        #expect(Self.lines(await FormatPhase().check([file], scratchPath: scratch)) == ["line 7"])
    }

    @Test func editedBytesGetTheFormattersOwnVerdict() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        let edited = try Self.file("Example.swift", source: "let value = 2\n", in: scratch)
        #expect(Self.lines(await FormatPhase().check([edited], scratchPath: scratch)) == ["unchanged"])
    }

    @Test func theSameBytesAtAnotherPathGetTheFormattersOwnVerdict() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        let other = try Self.file("Other.swift", source: Self.formatted, in: scratch)
        #expect(Self.lines(await FormatPhase().check([other], scratchPath: scratch)) == ["unchanged"])
    }

    @Test func anotherConfigurationGetsTheFormattersOwnVerdict() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        #expect(
            Self.lines(await FormatPhase(configuration: Self.narrow).check([file], scratchPath: scratch))
                == ["unchanged"]
        )
    }

    @Test func anotherSwiftFormatGetsTheFormattersOwnVerdict() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        #expect(
            Self.lines(await FormatPhase().check([file], scratchPath: scratch, swiftFormatVersion: "605.0.0"))
                == ["unchanged"]
        )
    }

    /* Under `--no-cache` there is no scratch: nothing planted is read and nothing is stored. */
    @Test func withNoScratchNothingIsReadOrStored() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        let before = try Data(contentsOf: FormatVerdictCache.storeFile(scratchPath: scratch))
        #expect(Self.lines(await FormatPhase().check([file], scratchPath: nil)) == ["unchanged"])
        #expect(try Data(contentsOf: FormatVerdictCache.storeFile(scratchPath: scratch)) == before)
    }

    /* A run that writes formats from the text every time and keeps no verdict. */
    @Test func aRewritingRunStoresNothing() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.longLine, in: scratch)
        _ = await FormatPhase().run([file])
        #expect(!FileManager.default.fileExists(atPath: FormatVerdictCache.storeFile(scratchPath: scratch).path))
    }

    /* A file this run did not check keeps its verdict while it is on disk; a deleted file's goes. */
    @Test func theStoreKeepsVerdictsOfFilesStillOnDisk() async throws {
        let scratch = try Self.scratch()
        let kept = try Self.file("Kept.swift", source: Self.formatted, in: scratch)
        let deleted = try Self.file("Deleted.swift", source: Self.formatted, in: scratch)
        let checked = try Self.file("Checked.swift", source: Self.longLine, in: scratch)
        _ = await FormatPhase().check([kept, deleted], scratchPath: scratch)
        try FileManager.default.removeItem(at: deleted.url)
        _ = await FormatPhase().check([checked], scratchPath: scratch)
        let stored = FormatVerdictCache(scratchPath: scratch, configuration: HouseSwiftFormat.configuration).stored
        #expect(Set(stored.keys) == [kept.url.path, checked.url.path])
    }
}
