import Foundation

/*
 Symbols for the files typed rules need, from the cheapest source that can vouch for each file as it stands.

 First the build's index store, which costs a record read, every file's read side by side: it holds every file
 the last build compiled, and a unit newer than the file vouches for it. Then sourcekitd in process, about half a
 second a file, for a file the index cannot vouch for: one the bodies-only path checked without building, or one
 edited since. A file neither can describe gets a reason instead, which the linter reports as unchecked.

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
    }

    /* Every build whose index and compile commands may describe a file (`Pipeline.symbolLayouts`). */
    let layouts: [BuildLayout]
    let runner: ProcessRunner
    /* Handed to every file's symbols, so a rule can tell our declarations from a dependency's. */
    var ownedModules: Set<String> = []

    func symbols(for files: [ParsedFile]) -> Result {
        var result = Result(symbols: [:], unavailable: [:], fromIndex: 0, fromSourcekitd: 0)
        guard !files.isEmpty else { return result }
        var stores: [IndexStore] = []
        if let library = try? IndexStore.toolchainLibraryPath(runner: runner) {
            stores = layouts.compactMap {
                try? IndexStore(libraryPath: library, storePath: IndexStore.storePath(layout: $0))
            }
        }
        var needSourcekitd: [ParsedFile] = []
        for (file, indexed) in zip(files, IndexStore.symbols(of: files.map(\.url), in: stores)) {
            if var found = indexed {
                found.ownedModules = ownedModules
                result.symbols[file.url.path] = found
                result.fromIndex += 1
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
                    "the build's index does not describe it as it stands, and sourcekitd could not load: \(error)"
            }
            return result
        }
        for file in needSourcekitd {
            guard let command = tables.lazy.compactMap({ $0.command(for: file.url) }).first else {
                result.unavailable[file.url.path] =
                    "the build's index does not describe it as it stands, and no build recorded how to compile it"
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
                    "the last build compiled its module with \(gone), which no longer exists, so that build's arguments describe another module"
                continue
            }
            do {
                var found = try session.symbols(
                    file: file.url.resolvingSymlinksInPath().path,
                    arguments: command.arguments,
                )
                found.ownedModules = ownedModules
                result.symbols[file.url.path] = found
                result.fromSourcekitd += 1
            }
            catch {
                result.unavailable[file.url.path] =
                    "the build's index does not describe it as it stands, and sourcekitd could not index it: \(error)"
            }
        }
        return result
    }
}
