import CryptoKit
import Foundation
import SwiftFormat

/*
 What the formatter said last time about each file under `--no-fix`, kept in the engine's scratch so a warm run
 does not format every file again to learn that nothing moved. Formatting is most of a warm `--no-fix` run's
 fix phase: 1,548 samples of it on ahraos-presence, every file every run, for a verdict that changes only when
 the file does.

 A verdict is the formatter's answer to one text under one house format, so the entry holds a fingerprint of
 each thing that answer reads, and replays only when every one still holds:

 - the file's bytes, the text the formatter is handed, by content, under the file's path;
 - the house format's configuration (`HouseSwiftFormat`), every setting and rule, as swift-format encodes it;
 - the swift-format version linked into this engine (`EngineVersion.swiftFormat`), since a new formatter can
   lay out the same text under the same settings differently.

 Only the verdict is kept, formatted or not and the first line that would move, which is all a `--no-fix` run
 reports. A plain run's rewrite is never cached: it needs the formatted text itself, and it writes what it
 formats. A file the formatter failed on is not kept either, so it is asked again next time and says why.

 The store is one file, replaced whole after each run that checked formatting. A file this run did not check
 keeps its verdict while it is still on disk, so a run over a few paths does not cost the next whole run its
 answers, and a deleted file's verdict goes. A different configuration or formatter makes every stored verdict
 none, and the next store starts over. A store that does not decode reads as empty: the run formats cold,
 slower and still right.
 */
struct FormatVerdictCache: Sendable {
    /* One file's verdict, with the fingerprint of the bytes it was given for. */
    struct Verdict: Codable, Equatable, Sendable {
        var source: String
        /* The first line formatting would move, or nil when it would move nothing. */
        var firstChangedLine: Int?
    }

    struct Entry: Codable {
        var formatter: String
        var verdicts: [String: Verdict]
    }

    let file: URL
    /* The configuration and swift-format version every verdict here was given under, as one fingerprint. */
    let formatter: String
    /* The verdicts the store held for this formatter, by path. */
    let stored: [String: Verdict]

    init(scratchPath: URL, configuration: Configuration, swiftFormatVersion: String = EngineVersion.swiftFormat) {
        file = Self.storeFile(scratchPath: scratchPath)
        formatter = Self.formatterFingerprint(configuration: configuration, swiftFormatVersion: swiftFormatVersion)
        guard let data = try? Data(contentsOf: file), let entry = try? JSONDecoder().decode(Entry.self, from: data),
            entry.formatter == formatter
        else {
            stored = [:]
            return
        }
        stored = entry.verdicts
    }

    static func storeFile(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("format-verdicts.json")
    }

    /* The stored verdict for this text at this path, only if it was given for these very bytes. */
    func verdict(path: String, source: String) -> Verdict? {
        guard let kept = stored[path], kept.source == Self.fingerprint(of: source) else { return nil }
        return kept
    }

    /* This run's verdicts, by path, over the stored ones whose file is still on disk. */
    func store(_ verdicts: [String: Verdict]) {
        var kept = verdicts
        for (path, verdict) in stored where kept[path] == nil && FileManager.default.fileExists(atPath: path) {
            kept[path] = verdict
        }
        do {
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try JSONEncoder().encode(Entry(formatter: formatter, verdicts: kept)).write(to: file, options: .atomic)
        }
        catch {
            /* Not stored, so the next run formats cold: slower, and still right. */
        }
    }

    /* A text's content hash: the fingerprint a verdict's bytes are compared by. */
    static func fingerprint(of source: String) -> String {
        SHA256.hash(data: Data(source.utf8)).map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }
            .joined()
    }

    /*
     The configuration as swift-format encodes it, keys sorted so the same settings always read the same, with the
     version. A configuration that does not encode gets a fingerprint of its own that no store can hold, so nothing
     is replayed for it.
     */
    static func formatterFingerprint(configuration: Configuration, swiftFormatVersion: String) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        guard let encoded = try? encoder.encode(configuration) else {
            return "unencodable \(UUID().uuidString)"
        }
        let digest = SHA256.hash(data: encoded).map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }
        return "swift-format \(swiftFormatVersion)\t\(digest.joined())"
    }
}
