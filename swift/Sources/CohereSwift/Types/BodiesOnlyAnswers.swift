import CryptoKit
import Foundation

/*
 What sourcekitd answered for each file the types phase checked in process (`TypesPhase.checkBodiesOnly`),
 kept beside the build's snapshot so a run asks again only about the files that changed since it last asked.
 Measured on cohere's package before this: every file edited since the last build was asked about on every
 run, so a run took 1.55s, 3.94s and 6.65s after 2, 10 and 19 files were touched, about 0.3s a file.

 An answer is replayed only under the key it was given under, which holds everything the answer read:

 - the file's bytes, which are also the text sourcekitd is handed, so the key and the check can never
   describe two different texts;
 - the compile arguments, the build's command for the file's module;
 - the build snapshot the file was checked against, whose fingerprint covers every input of that build (the
   toolchain, and so sourcekitd, among them), and through it every other file of the module as it compiled;
 - every other edited file whose interface is not the one that build compiled, with the interface it has
   now, since another file's declarations change what the compiler says about this one. A sibling whose
   interface the build compiled is already covered by the snapshot, and keying it anyway would ask again
   about every edited file whenever one more was edited. An interface edit builds today rather than
   reaching the in-process check, so this is empty whenever an answer is kept; it is keyed so a kept answer
   cannot outlive a widening of that rule;
 - the engine binary that turned sourcekitd's answer into findings, by path, size and time.

 The names in the file come from the same check (`Sourcekitd.diagnostics(file:arguments:text:indexingInto:unitOutputPath:)`),
 so they are kept beside its findings under the same key, which holds everything they read too, and lint reads them
 instead of asking sourcekitd to check the file a second time. An answer whose names could not be written keeps
 none, and lint asks for them as it asks for any file's.

 Only the files edited now are kept, and the store is written only after the whole check succeeded, so a run
 that fell back to building leaves the last good answers, which that build's new snapshot no longer matches.
 */
struct BodiesOnlyAnswers: Codable, Equatable {
    struct Answer: Codable, Equatable {
        var key: String
        var findings: [FindingRecord]
        /* The file's names as the same check recorded them, nil when they could not be. */
        var names: [FileSymbols.Occurrence]?
    }

    /* Each checked file's answer, by resolved path. */
    var answers: [String: Answer] = [:]

    /* Every part of a key, encoded with its names so no two different sets of parts hash alike. */
    private struct KeyParts: Encodable {
        var contents: String
        var arguments: [String]
        var build: String
        var siblingInterfaces: [String: String]
        var engine: String
    }

    static func key(
        contents: Data,
        arguments: [String],
        build: String,
        siblingInterfaces: [String: String],
        engine: String = engineIdentity,
    ) throws -> String {
        let parts = KeyParts(
            contents: SHA256.hash(data: contents).map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }
                .joined(),
            arguments: arguments,
            build: build,
            siblingInterfaces: siblingInterfaces,
            engine: engine,
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return SHA256.hash(data: try encoder.encode(parts)).map {
            String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16)
        }.joined()
    }

    /* The other files, of those whose interfaces are known now, whose interface is not the one the build compiled, with the one they have now. */
    static func siblingInterfaces(
        of path: String,
        current: [String: String],
        built: [String: String],
    ) -> [String: String] {
        current.filter { $0.key != path && built[$0.key] != $0.value }
    }

    /*
     One file's answer: replayed from `earlier` when it holds an answer under this key, otherwise asked. Either
     way the answer is kept here under the key, for the run after this one.
     */
    mutating func answer(
        path: String,
        key: String,
        replaying earlier: BodiesOnlyAnswers,
        ask: () throws -> (findings: [FindingRecord], names: [FileSymbols.Occurrence]?),
    ) rethrows -> (answer: Answer, replayed: Bool) {
        if let kept = earlier.answers[path], kept.key == key {
            answers[path] = kept
            return (kept, true)
        }
        let asked = try ask()
        let answer = Answer(key: key, findings: asked.findings, names: asked.names)
        answers[path] = answer
        return (answer, false)
    }

    /*
     The running engine, by path, size and modification time, so answers one engine turned into findings are
     never replayed by another. Unknown when the executable cannot be read, which still keys: two runs that
     cannot read it share answers only with each other.
     */
    static let engineIdentity: String = {
        guard let executable = Bundle.main.executableURL?.resolvingSymlinksInPath(),
            let values = try? executable.resourceValues(forKeys: [.fileSizeKey, .contentModificationDateKey])
        else { return "unknown" }
        let modified = values.contentModificationDate?.timeIntervalSinceReferenceDate ?? -1
        return "\(executable.path)\t\(values.fileSize ?? -1)\t\(modified)"
    }()

    /* Where the answers are kept, beside the build snapshot they were checked against. */
    static func storeFile(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("bodies-only-answers.json")
    }

    /* The answers the last successful in-process check kept, or none when there are none or they cannot be read: every edited file is then asked. */
    static func stored(scratchPath: URL) -> BodiesOnlyAnswers {
        guard let data = try? Data(contentsOf: storeFile(scratchPath: scratchPath)),
            let stored = try? JSONDecoder().decode(BodiesOnlyAnswers.self, from: data)
        else { return BodiesOnlyAnswers() }
        return stored
    }

    func store(scratchPath: URL) {
        do {
            try FileManager.default.createDirectory(at: scratchPath, withIntermediateDirectories: true)
            try JSONEncoder().encode(self).write(to: Self.storeFile(scratchPath: scratchPath), options: .atomic)
        }
        catch {
            /* Not kept, so the next run asks sourcekitd again: slower, and still right. */
        }
    }
}
