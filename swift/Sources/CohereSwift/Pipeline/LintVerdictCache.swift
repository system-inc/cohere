import CryptoKit
import Foundation
import Synchronization

/*
 What the rules said last time about each file, kept in the engine's scratch so a run after a one-file edit walks
 the one file rather than every file to learn that nothing else moved. Lint was the floor of a body edit on
 ahraos-presence (#da5fx6d, 2026-11-06): 2.45s of a 3.6s run, for 1,340 files of which one had changed.

 A verdict is what a rule returned for one file, and it is replayed only when everything that answer read still
 holds. What each rule reads was taken from every registered rule (the census on #da5fx6d), and falls in two classes
 with a key each, under one context for the whole store:

 - the context: this format, the engine (`engineIdentity`, the executable's sha256, which stands for every rule's
   code and every vocabulary, word list, brand and swift-format configuration compiled into it), and the enabled
   rule set with each rule's options (`ruleSet(fileRules:typedRules:options:)`). A different context makes every
   stored verdict none;
 - a file rule's verdict (`fileKey(of:)`): the file's bytes, its path and the path with links resolved, its package's
   root likewise, its target and its target's kind, and the platform a standalone script is checked for, which is
   every field of `ParsedFile` its tree does not come from. A rule reads the file name, the folders inside the package
   (resolving links), the target kind, or whether the file is a script beside the tree;
 - a typed rule's verdict (`typedKey(fileKey:record:ownedModules:)`): the file key, and the index record that
   describes the file as it stands, by its store and its name, with the modules the package owns. A record's name is
   a content address of every name it resolves (`IndexStore.FreshRecord`), so a check that changed what any name in
   the file resolves to, after an edit to another file, wrote a record of another name.

 Never kept, so computed every run: the package rules, which read the package; a typed verdict from symbols
 sourcekitd gave, since no record stands for what it would say; a typed verdict for a file no source of symbols
 described (a lint crash); any verdict holding a rule's failure (`incumbentFailed`), so the failure is tried and said
 again; and a verdict for a file whose text on disk is no longer the text linted. A run that stopped before lint
 keeps nothing, since only lint stores.

 The store is one file, replaced whole after each run that linted, as `FormatVerdictCache`'s is. A file this run did
 not lint keeps its verdict while it is still on disk. A store that does not decode reads as empty: the run walks
 every file, slower and still right. Under `--no-cache` there is none.
 */
struct LintVerdictCache: Sendable {
    /* One file's kept verdict, as `Entry.init` builds it from what `Linter` returned. */
    struct Entry: Codable, Equatable, Sendable {
        /* The key the file rules' verdict was given under (`fileKey(of:)`). */
        var key: String
        /* The enabled file rules that do not apply to the file; every other enabled file rule does. */
        var fileRulesNotApplying: [String]
        /* What each file rule that applies found, for those that found anything. */
        var fileFindings: [String: [FindingRecord]]
        /* The enabled typed rules that apply, which the bytes alone decide. */
        var typedRules: [String]
        /* The typed rules' verdict, when one was kept. */
        var typed: Typed?
    }

    /* The typed rules' verdict and the key it was given under (`typedKey(fileKey:record:ownedModules:)`). */
    struct Typed: Codable, Equatable, Sendable {
        var key: String
        /* What each typed rule that applies found, for those that found anything. */
        var findings: [String: [FindingRecord]]
    }

    private struct Store: Codable {
        var context: String
        var entries: [String: Entry]
    }

    /* The message id a rule's finding carries when the rule itself failed on the file (`SwiftFormatRule`). */
    static let failureMessageId = "incumbentFailed"

    let file: URL
    let context: String
    /* The enabled file rules, in the registry's order, which an entry's file-rule verdict is read against. */
    let enabledFileRules: [String]
    /* The verdicts the store held for this context, by path. */
    let stored: [String: Entry]

    init(scratchPath: URL, context: String, enabledFileRules: [String]) {
        file = Self.storeFile(scratchPath: scratchPath)
        self.context = context
        self.enabledFileRules = enabledFileRules
        guard let data = try? Data(contentsOf: file), let store = try? JSONDecoder().decode(Store.self, from: data),
            store.context == context
        else {
            stored = [:]
            return
        }
        stored = store.entries
    }

    static func storeFile(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("lint-verdicts.json")
    }

    /*
     The file rules' verdict kept for this file under this key: every enabled file rule that applies, with what it
     found, a rule absent being one that does not apply, as `Linter` takes a reusable walk. Also which enabled typed
     rules apply, which the same bytes decide.
     */
    func fileVerdict(path: String, key: String) -> (fileRules: [String: [FindingRecord]], typedRules: [String])? {
        guard let entry = stored[path], entry.key == key else { return nil }
        let notApplying = Set(entry.fileRulesNotApplying)
        var fileRules: [String: [FindingRecord]] = [:]
        for name in enabledFileRules where !notApplying.contains(name) {
            fileRules[name] = entry.fileFindings[name] ?? []
        }
        return (fileRules, entry.typedRules)
    }

    /* The typed rules' verdict kept for this file under this key, each typed rule that applies with what it found. */
    func typedVerdict(path: String, key: String) -> [String: [FindingRecord]]? {
        guard let entry = stored[path], let typed = entry.typed, typed.key == key else { return nil }
        return Dictionary(
            entry.typedRules.map { ($0, typed.findings[$0] ?? []) },
            uniquingKeysWith: { first, _ in first },
        )
    }

    /*
     The entry to keep for a file `Linter` judged, or nil when its file rules' verdict may not be kept. `typedKey` is
     the key its typed verdict was given under, nil when that verdict may not be kept: symbols from sourcekitd, or
     none at all.
     */
    func entry(fileKey: String, verdict: Linter.Verdict, typedKey: String?) -> Entry? {
        guard Self.isKeepable(verdict.fileRules) else { return nil }
        var typed: Typed?
        if let typedKey, let found = verdict.typedFindings, Self.isKeepable(found) {
            typed = Typed(key: typedKey, findings: found.filter { !$0.value.isEmpty })
        }
        return Entry(
            key: fileKey,
            fileRulesNotApplying: enabledFileRules.filter { verdict.fileRules[$0] == nil },
            fileFindings: verdict.fileRules.filter { !$0.value.isEmpty },
            typedRules: verdict.typedRules,
            typed: typed,
        )
    }

    /*
     The entries to keep from one run's verdicts, by path. `replayed` names the files the store answered for whole,
     whose entries are kept as they were. A file whose rules ran is kept only while its text on disk is still the text
     linted, read side by side: an entry for text that has since moved could never be read, and keeping none says so
     plainly. `typedKeys` holds the key of each file whose typed verdict came from an index record, read this run or
     replayed; a file without one keeps no typed verdict.
     */
    func entries(
        of files: [ParsedFile],
        verdicts: [String: Linter.Verdict],
        keys: [String: String],
        replayed: Set<String>,
        typedKeys: [String: String],
    ) -> [String: Entry] {
        let judged = files.filter { !replayed.contains($0.url.path) }
        let stillOnDisk = Mutex(Set<String>())
        DispatchQueue.concurrentPerform(iterations: judged.count) { index in
            if Self.isStillOnDisk(judged[index]) {
                stillOnDisk.withLock { _ = $0.insert(judged[index].url.path) }
            }
        }
        let unmoved = stillOnDisk.withLock { $0 }
        var kept: [String: Entry] = [:]
        for file in files {
            let path = file.url.path
            guard let verdict = verdicts[path], let key = keys[path], replayed.contains(path) || unmoved.contains(path),
                let entry = entry(fileKey: key, verdict: verdict, typedKey: typedKeys[path])
            else { continue }
            kept[path] = entry
        }
        return kept
    }

    /* Whether no rule failed on the file, so what the rules said is what they would say again. */
    static func isKeepable(_ findings: [String: [FindingRecord]]) -> Bool {
        !findings.values.contains { $0.contains { $0.messageId == failureMessageId } }
    }

    /* This run's entries, by path, over the stored ones whose file is still on disk. */
    func store(_ entries: [String: Entry]) {
        var kept = entries
        for (path, entry) in stored where kept[path] == nil && FileManager.default.fileExists(atPath: path) {
            kept[path] = entry
        }
        do {
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try JSONEncoder().encode(Store(context: context, entries: kept)).write(to: file, options: .atomic)
        }
        catch {
            /* Not stored, so the next run walks every file: slower, and still right. */
        }
    }

    /* Every part of a key, encoded with its names so no two different sets of parts hash alike. */
    private struct ContextParts: Encodable {
        var format: String
        var engine: String
        var ruleSet: [String]
    }

    private struct FileKeyParts: Encodable {
        var contents: String
        var path: String
        var resolvedPath: String
        var packageRoot: String?
        var resolvedPackageRoot: String?
        var targetName: String
        var targetKind: String
        /* A standalone script's platform (`StandaloneScript`), nil for a file a target compiles: `consistency-require-matching-file-name` judges a script as it judges `main.swift`. */
        var scriptPlatform: String?
    }

    private struct TypedKeyParts: Encodable {
        var fileKey: String
        var storePath: String
        var record: String
        var ownedModules: [String]
    }

    /* The store's own format, so a store another layout of these entries wrote is never read. */
    static let format = "lint-verdicts 1"

    static func context(engine: String, ruleSet: [String]) -> String {
        digest(ContextParts(format: format, engine: engine, ruleSet: ruleSet))
    }

    /* Each enabled rule as its name, or its name and its options as JSON with sorted keys, sorted. */
    static func ruleSet(fileRules: [String], typedRules: [String], options: [String: [String: Int]]) -> [String] {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return (fileRules + typedRules).map { name in
            guard let given = options[name], let encoded = try? encoder.encode(given) else { return name }
            return "\(name) \(String(decoding: encoded, as: UTF8.self))"
        }
        .sorted()
    }

    /* The key a file rule's verdict on this file is given under: its bytes and every field of it that is not its tree. */
    static func fileKey(of file: ParsedFile) -> String {
        digest(
            FileKeyParts(
                contents: hex(SHA256.hash(data: Data(file.source.utf8))),
                path: file.url.path,
                resolvedPath: file.url.resolvingSymlinksInPath().path,
                packageRoot: file.packageRoot?.path,
                resolvedPackageRoot: file.packageRoot?.resolvingSymlinksInPath().path,
                targetName: file.targetName,
                targetKind: file.targetKind,
                scriptPlatform: file.scriptPlatform.map { "\($0)" },
            )
        )
    }

    /* Every file's key, by path, hashed side by side. */
    static func fileKeys(of files: [ParsedFile]) -> [String: String] {
        let keys = Mutex<[String: String]>([:])
        DispatchQueue.concurrentPerform(iterations: files.count) { index in
            let key = fileKey(of: files[index])
            keys.withLock { $0[files[index].url.path] = key }
        }
        return keys.withLock { $0 }
    }

    /* The key a typed rule's verdict on a file is given under: its file key, the record describing it, and the modules the package owns. */
    static func typedKey(fileKey: String, record: IndexStore.FreshRecord.Identity, ownedModules: Set<String>) -> String
    {
        digest(
            TypedKeyParts(
                fileKey: fileKey,
                storePath: record.storePath,
                record: record.record,
                ownedModules: ownedModules.sorted(),
            )
        )
    }

    /* Whether the file's text on disk is still the text that was linted, read as the parser reads it. */
    static func isStillOnDisk(_ file: ParsedFile) -> Bool {
        (try? String(contentsOf: file.url, encoding: .utf8)) == file.source
    }

    /*
     The running engine's sha256, hashed once per process: it stands for every rule's code and everything compiled
     into it, as the Go engine's `ExecutableIdentity` does. Measured 2026-11-06 on the 31.5 MB release engine: 12 to
     16ms a run, 53ms the first time after a build, so it is not kept between runs. Nil when the executable cannot be
     read: nothing would then tell two engines' verdicts apart, so no verdict is kept or replayed.
     */
    static let engineIdentity: String? = {
        guard let executable = Bundle.main.executableURL?.resolvingSymlinksInPath(),
            let data = try? Data(contentsOf: executable, options: .mappedIfSafe)
        else { return nil }
        return hex(SHA256.hash(data: data))
    }()

    private static func digest(_ parts: some Encodable) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        /* Parts of strings and arrays always encode; a failure would key nothing, so it keys a value no store holds. */
        guard let encoded = try? encoder.encode(parts) else { return "unencodable \(UUID().uuidString)" }
        return hex(SHA256.hash(data: encoded))
    }

    private static func hex(_ digest: SHA256.Digest) -> String {
        digest.map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }.joined()
    }
}
