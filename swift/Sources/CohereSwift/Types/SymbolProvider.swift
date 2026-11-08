import Foundation

/*
 Symbols for the files typed rules need, from the cheapest source that can vouch for each file as it stands.

 First the build's index store, which costs a record read, every file's read side by side: it holds every file
 the last build compiled, and a unit newer than the file vouches for it. Then the names the types phase's
 in-process check recorded for a file it answered for (`answered`), from the check that gave its findings. Then
 sourcekitd in process, about half a second a file, for a file none of those vouches for: one edited since, in a
 run whose types phase did not answer for it. A file none can describe gets a reason instead, which the linter
 reports as unchecked.

 The package's local packages build into their own scratch paths, so each has its own store and its own
 compile commands; every one is consulted. An Xcode root has one layout, its xcodebuild's derived data, read
 the same way.
 */
struct SymbolProvider {
    struct Result: Sendable {
        var symbols: [String: FileSymbols]
        /* Files no source could describe, by path, with why. */
        var unavailable: [String: String]
        var fromIndex: Int
        var fromSourcekitd: Int
        /* The record each file the index described was read from, by path; a file sourcekitd described has none. */
        var records: [String: IndexStore.FreshRecord.Identity] = [:]
        /*
         The files whose verdict the caller already holds under the record that describes them as they stand, by path,
         with that record. Their records were not read, and they have no symbols here.
         */
        var replayed: [String: IndexStore.FreshRecord.Identity] = [:]
    }

    /* Every build whose index and compile commands may describe a file (`Pipeline.symbolLayouts`). */
    let layouts: [BuildLayout]
    let runner: ProcessRunner
    /* Handed to every file's symbols, so a rule can tell our declarations from a dependency's. */
    var ownedModules: Set<String> = []
    /*
     The names the types phase's in-process check recorded, by resolved path (`TypesPhase.Result.symbols`). Like
     `indexsource`'s, they come with no record of the build's, so no verdict is ever kept under them.
     */
    var answered: [String: FileSymbols] = [:]
    /* How the demangler is loaded: the process's own, or a test's loader that fails, to drive the path where none loads. */
    var loadDemangler: (ProcessRunner) throws -> SwiftDemangler = { try SwiftDemangler.shared(runner: $0) }

    /*
     `replaying` is asked, for each file the index can vouch for, whether the caller holds the file's verdict under the
     record that would be read (`LintVerdictCache`); a file it answers yes for is not read. A file only sourcekitd can
     describe is never offered, since no record stands for what sourcekitd would say.
     */
    func symbols(
        for files: [ParsedFile],
        replaying: (ParsedFile, IndexStore.FreshRecord.Identity) -> Bool = { _, _ in false },
    ) -> Result {
        var result = Result(symbols: [:], unavailable: [:], fromIndex: 0, fromSourcekitd: 0)
        guard !files.isEmpty else { return result }
        /*
         The demangler tells the standard library's declarations from an extension of ours on its types
         (`FileSymbols.Occurrence.isStandardLibrary`), so a file is judged only once it is loaded. Without it, no file
         is: each says why, as a file no source could describe does.
         */
        let demangler: SwiftDemangler
        do {
            demangler = try loadDemangler(runner)
        }
        catch {
            for file in files {
                result.unavailable[file.url.path] =
                    "the demangler that tells the standard library's declarations from ours could not load: \(error)"
            }
            return result
        }
        var stores: [IndexStore] = []
        if let library = try? IndexStore.toolchainLibraryPath(runner: runner) {
            stores = layouts.compactMap {
                try? IndexStore(libraryPath: library, storePath: IndexStore.storePath(layout: $0))
            }
        }
        var toRead: [(file: ParsedFile, candidates: [IndexStore.FreshRecord])] = []
        /*
         A kept verdict must name the record a read would answer from: the first fresh record whose reader opens, not
         merely the first, since a read passes over a record deleted or damaged under a unit that still names it. Only
         the files the caller holds a verdict for under one of their records are opened to find it.
         */
        var offered: [(file: ParsedFile, candidates: [IndexStore.FreshRecord])] = []
        for (file, candidates) in zip(files, IndexStore.freshRecords(of: files.map(\.url), in: stores)) {
            if candidates.contains(where: { replaying(file, $0.identity) }) {
                offered.append((file, candidates))
            }
            else {
                toRead.append((file, candidates))
            }
        }
        for ((file, candidates), chosen) in zip(offered, IndexStore.readableRecords(in: offered.map(\.candidates))) {
            if let chosen, replaying(file, chosen.identity) {
                result.replayed[file.url.path] = chosen.identity
            }
            else {
                toRead.append((file, candidates))
            }
        }
        var needSourcekitd: [ParsedFile] = []
        for (file, read) in zip(toRead.map(\.file), IndexStore.symbols(in: toRead.map(\.candidates))) {
            if let read {
                var found = read.symbols
                found.ownedModules = ownedModules
                found.demangler = demangler
                result.symbols[file.url.path] = found
                result.records[file.url.path] = read.record.identity
                result.fromIndex += 1
            }
            else if var found = answered[file.url.resolvingSymlinksInPath().path] {
                found.ownedModules = ownedModules
                found.demangler = demangler
                result.symbols[file.url.path] = found
                result.fromSourcekitd += 1
            }
            else {
                needSourcekitd.append(file)
            }
        }
        guard !needSourcekitd.isEmpty else { return result }

        let tables = layouts.compactMap { try? CompileCommands.read(layout: $0) }
        let session: Sourcekitd
        do {
            session = try Sourcekitd.shared(runner: runner)
        }
        catch {
            for file in needSourcekitd {
                result.unavailable[file.url.path] =
                    "the index does not describe it as it stands, and sourcekitd could not load: \(error)"
            }
            return result
        }
        for file in needSourcekitd {
            guard let command = tables.lazy.compactMap({ $0.command(for: file.url) }).first else {
                result.unavailable[file.url.path] =
                    "the index does not describe it as it stands, and no check or build kept how to compile it"
                continue
            }
            /*
             A command naming a file that no longer exists compiled a module that is not this one: a file was
             renamed, moved or deleted since that build. sourcekitd would still answer from it, from a module
             missing the file's new name and holding the old one, after printing "error opening input file" for
             it. So the command vouches for nothing, and a run with types builds the module as it is now.
             Presence, 2026-11-04 (#zggj9xy): `cohere --lint` after renames with plain mv.
             */
            if let gone = command.files.first(where: { !FileManager.default.fileExists(atPath: $0) }) {
                result.unavailable[file.url.path] =
                    "the last check or build compiled its module with \(gone), which no longer exists, so its arguments describe another module"
                continue
            }
            do {
                var found = try session.symbols(
                    file: file.url.resolvingSymlinksInPath().path,
                    arguments: command.arguments,
                )
                found.ownedModules = ownedModules
                found.demangler = demangler
                result.symbols[file.url.path] = found
                result.fromSourcekitd += 1
            }
            catch {
                result.unavailable[file.url.path] =
                    "the index does not describe it as it stands, and sourcekitd could not index it: \(error)"
            }
        }
        return result
    }
}
