import Foundation
import SwiftFormat
import SwiftParser
import Synchronization
import Testing

@testable import CohereSwift

/*
 A run replays the formatter's last verdict on a file only when nothing that verdict read has moved: the file's
 bytes at its path, the house format's configuration, and the swift-format version. Each test plants a verdict the
 formatter would never give, so a replay is visible, and then changes one input and expects the formatter's own
 answer. A `--no-fix` run replays every verdict; a plain run replays only formatted, the one verdict that leaves it
 nothing to write, and never for a file the fixer rewrote. Each plain-run test names the mutant it is red under.
 */
struct FormatVerdictCacheTests {
    /* Formatted the house way. */
    static let formatted = "let value = 1\n"

    /* Formatted the house way but for one space too many, which the formatter takes out. */
    static let dirty = "let  value = 1\n"

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

    /* Stores a verdict no formatter gave, line 7 moved unless told otherwise, for this text at this path under the house format. */
    static func plant(path: String, source: String, scratch: URL, firstChangedLine: Int? = 7) {
        FormatVerdictCache(scratchPath: scratch, configuration: HouseSwiftFormat.configuration).store([
            path: .init(source: FormatVerdictCache.fingerprint(of: source), firstChangedLine: firstChangedLine)
        ])
    }

    /* The store file's identity on disk, which a write replaces, since the store is written atomically. */
    static func storeIdentity(scratch: URL) throws -> Int? {
        try FileManager.default.attributesOfItem(atPath: FormatVerdictCache.storeFile(scratchPath: scratch).path)[
            .systemFileNumber
        ] as? Int
    }

    /* The house format, counting each configuration made: one for the store's key, then one for each text formatted. */
    final class CountedConfiguration: Sendable {
        private let count = Mutex(0)

        var made: Int { count.withLock { $0 } }

        func make() -> Configuration {
            count.withLock { $0 += 1 }
            return HouseSwiftFormat.configuration
        }
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

    /*
     A plain run does not format a file held as formatted for its bytes, and an unchanged run leaves the store as it
     is. Red under: `run` not reading the store (rewritten), and `store` writing what it already holds (the file moves).
     */
    @Test func aPlainRunFormatsNoFileHeldAsFormattedAndWritesNothing() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.dirty, in: scratch)
        guard case .changed = FormatPhase.format(file) else {
            Issue.record("the house leaves the dirty text as it is, so this test would prove nothing")
            return
        }
        Self.plant(path: file.url.path, source: Self.dirty, scratch: scratch, firstChangedLine: nil)
        let before = try Self.storeIdentity(scratch: scratch)
        #expect(Self.lines(await FormatPhase().run([file], scratchPath: scratch)) == ["unchanged"])
        #expect(try Self.storeIdentity(scratch: scratch) == before)
    }

    /* A file whose bytes moved since its verdict is formatted. Red under: `verdict` not comparing the bytes' fingerprint. */
    @Test func aPlainRunFormatsAFileWhoseBytesMoved() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.dirty, in: scratch)
        Self.plant(path: file.url.path, source: Self.dirty, scratch: scratch, firstChangedLine: nil)
        let edited = try Self.file("Example.swift", source: "let  value = 2\n", in: scratch)
        #expect(Self.lines(await FormatPhase().run([edited], scratchPath: scratch)) == ["rewritten"])
    }

    /* A verdict that the bytes would move gives a plain run no text to write, so the file is formatted. */
    @Test func aPlainRunFormatsAFileHeldAsUnformatted() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.formatted, in: scratch)
        Self.plant(path: file.url.path, source: Self.formatted, scratch: scratch)
        let configuration = CountedConfiguration()
        #expect(
            Self.lines(await FormatPhase(configuration: { configuration.make() }).run([file], scratchPath: scratch))
                == ["unchanged"]
        )
        #expect(configuration.made == 2, "the store's key and one format")
    }

    /* A file the fixer rewrote is formatted whatever its verdict says. Red under: `run` not reading `fixed`. */
    @Test func aPlainRunFormatsAFileTheFixerRewrote() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.dirty, in: scratch)
        Self.plant(path: file.url.path, source: Self.dirty, scratch: scratch, firstChangedLine: nil)
        #expect(
            Self.lines(await FormatPhase().run([file], fixed: [file.url.path], scratchPath: scratch)) == ["rewritten"]
        )
    }

    /*
     A plain run keeps what the formatter said, so the next run over what it wrote formats nothing: a file left as it
     was is kept under its bytes, and a file rewritten under the bytes it was rewritten to. Red under: `run` storing
     nothing, keeping nothing for a file left as it was, and keeping a rewritten file under the bytes it was handed.
     */
    @Test func aPlainRunKeepsWhatItLearnsSoTheNextRunFormatsNothing() async throws {
        let scratch = try Self.scratch()
        let clean = try Self.file("Clean.swift", source: Self.formatted, in: scratch)
        let dirty = try Self.file("Dirty.swift", source: Self.dirty, in: scratch)
        #expect(
            Self.lines(await FormatPhase().run([clean, dirty], scratchPath: scratch)) == ["unchanged", "rewritten"]
        )
        let written = try Self.file("Dirty.swift", source: Self.formatted, in: scratch)
        let configuration = CountedConfiguration()
        #expect(
            Self.lines(
                await FormatPhase(configuration: { configuration.make() }).run([clean, written], scratchPath: scratch)
            )
                == ["unchanged", "unchanged"]
        )
        #expect(configuration.made == 1, "only the store's key: no file was formatted")
    }

    /* Under `--no-cache` a plain run has no scratch: a planted verdict is not read, and nothing is stored. */
    @Test func aPlainRunWithNoScratchReadsAndStoresNothing() async throws {
        let scratch = try Self.scratch()
        let file = try Self.file("Example.swift", source: Self.dirty, in: scratch)
        Self.plant(path: file.url.path, source: Self.dirty, scratch: scratch, firstChangedLine: nil)
        let before = try Data(contentsOf: FormatVerdictCache.storeFile(scratchPath: scratch))
        #expect(Self.lines(await FormatPhase().run([file], scratchPath: nil)) == ["rewritten"])
        #expect(try Data(contentsOf: FormatVerdictCache.storeFile(scratchPath: scratch)) == before)
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
