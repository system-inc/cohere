import Foundation
import IndexStoreShim

/*
 The index store every debug `swift build` writes beside its products (`<scratch>/out/v5`), read through the
 toolchain's own `libIndexStore.dylib`: for each file the build compiled, every name in it and the
 declaration it resolves to.

 It is the same compiler's verdict the `.dia` records hold, kept for names instead of diagnostics, and it
 costs nothing extra: the build already wrote it. A unit describes one compile of one file and names the
 record holding that file's occurrences. A unit is trusted for a file only when it is newer than the file as
 it stands, the rule the types phase applies to `.dia` records, because an older one describes text that is
 no longer there.

 Measured on Presence: 2,531 units, listed in 9ms.
 */
final class IndexStore {
    /* The library or the store could not be opened. */
    struct Failure: Error, CustomStringConvertible {
        var description: String
    }

    /* `INDEXSTORE_UNIT_DEPENDENCY_UNIT`: a module the unit's file imports, named by its module name. */
    private static let unitDependency: Int32 = 1
    /* `INDEXSTORE_UNIT_DEPENDENCY_RECORD`: the dependency that names the record of the unit's own file. */
    private static let recordDependency: Int32 = 2
    /* `INDEXSTORE_UNIT_DEPENDENCY_FILE`: a file the compile read, such as a Clang module's headers and the module maps it consulted. */
    private static let fileDependency: Int32 = 3
    /* `INDEXSTORE_SYMBOL_ROLE_DECLARATION` and `INDEXSTORE_SYMBOL_ROLE_DEFINITION`. */
    static let declarationRoles: UInt64 = 1 << 0 | 1 << 1
    /* `INDEXSTORE_SYMBOL_ROLE_REFERENCE`. */
    static let referenceRole: UInt64 = 1 << 2
    /* `INDEXSTORE_SYMBOL_ROLE_IMPLICIT`. */
    static let implicitRole: UInt64 = 1 << 8
    /* `INDEXSTORE_SYMBOL_KIND_MODULE`: a module's own name, as an `import` line or a qualified name spells it. */
    static let moduleKind: Int32 = 1

    /*
     One unit: one compile of one file of ours, or one module the build imported (a `.swiftinterface` or a
     Clang `.pcm`, marked system). Its unit dependencies are what the file imports, the implicit standard
     library modules among them; for a system module they are the modules it imports in turn.
     */
    struct Unit: Sendable {
        var name: String
        /* The compiled file, resolved. Empty for a module the build imported. */
        var mainFile: String
        var module: String
        /* What the compile wrote, as the compiler names it: for a Swift module the build imported, the `.swiftinterface` it read; for a Clang module, its `.pcm`. */
        var outputFile: String
        var isSystem: Bool
        var written: Date
        var importedModules: [String]
        /* The records holding the unit's own file's occurrences. */
        var ownRecords: [String]
        /* Every record the unit names: for a system module, the records declaring what it exports. */
        var allRecords: [String]
        /* The file each record describes: for a Clang module, the header that declares what the record holds. */
        var recordFiles: [String: String]
        /* The files the compile read without recording them: for a Clang module, its headers and the module maps it consulted. */
        var files: [String]
    }

    /* One occurrence as the record holds it, roles and kind kept whole for questions `FileSymbols` does not ask. */
    struct RecordOccurrence: Sendable {
        var line: Int
        var column: Int
        var symbol: String
        var name: String
        var roles: UInt64
        var kind: Int32

        var isReference: Bool { roles & IndexStore.referenceRole != 0 }
        var isDeclaration: Bool { roles & IndexStore.declarationRoles != 0 }
    }

    private let store: CohereIndexStore
    private let storePath: URL
    private let dispose: CohereIndexStoreDispose
    private let unitsApply: CohereIndexStoreUnitsApply
    private let unitReaderCreate: CohereIndexUnitReaderCreate
    private let unitReaderDispose: CohereIndexUnitReaderDispose
    private let unitMainFile: CohereIndexUnitReaderGetMainFile
    private let unitModuleName: CohereIndexUnitReaderGetModuleName
    private let unitOutputFile: CohereIndexUnitReaderGetOutputFile
    private let unitIsSystem: CohereIndexUnitReaderIsSystemUnit
    private let dependenciesApply: CohereIndexUnitReaderDependenciesApply
    private let dependencyKind: CohereIndexUnitDependencyGetKind
    private let dependencyName: CohereIndexUnitDependencyGetName
    private let dependencyFilePath: CohereIndexUnitDependencyGetFilePath
    private let dependencyModuleName: CohereIndexUnitDependencyGetModuleName
    private let recordReaderCreate: CohereIndexRecordReaderCreate
    private let recordReaderDispose: CohereIndexRecordReaderDispose
    private let occurrencesApply: CohereIndexRecordReaderOccurrencesApply
    private let occurrenceSymbol: CohereIndexOccurrenceGetSymbol
    private let occurrenceRoles: CohereIndexOccurrenceGetRoles
    private let occurrenceLineColumn: CohereIndexOccurrenceGetLineColumn
    private let symbolName: CohereIndexSymbolGetString
    private let symbolIdentifier: CohereIndexSymbolGetString
    private let symbolKind: CohereIndexSymbolGetKind

    /* Every unit in the store, read once, on first use. */
    private var cachedUnits: [Unit]?

    init(libraryPath: String, storePath: URL) throws {
        guard let handle = dlopen(libraryPath, RTLD_NOW | RTLD_LOCAL) else {
            let reason = dlerror().map { String(cString: $0) } ?? "no reason given"
            throw Failure(description: "could not load the index store library at \(libraryPath): \(reason)")
        }
        func symbol<Function>(_ name: String, as type: Function.Type) throws -> Function {
            guard let address = dlsym(handle, name) else {
                throw Failure(description: "the index store library at \(libraryPath) has no \(name)")
            }
            return unsafeBitCast(address, to: type)
        }
        let create = try symbol("indexstore_store_create", as: CohereIndexStoreCreate.self)
        dispose = try symbol("indexstore_store_dispose", as: CohereIndexStoreDispose.self)
        unitsApply = try symbol("indexstore_store_units_apply_f", as: CohereIndexStoreUnitsApply.self)
        unitReaderCreate = try symbol("indexstore_unit_reader_create", as: CohereIndexUnitReaderCreate.self)
        unitReaderDispose = try symbol("indexstore_unit_reader_dispose", as: CohereIndexUnitReaderDispose.self)
        unitMainFile = try symbol("indexstore_unit_reader_get_main_file", as: CohereIndexUnitReaderGetMainFile.self)
        unitModuleName = try symbol("indexstore_unit_reader_get_module_name", as: CohereIndexUnitReaderGetModuleName.self)
        unitOutputFile = try symbol("indexstore_unit_reader_get_output_file", as: CohereIndexUnitReaderGetOutputFile.self)
        unitIsSystem = try symbol("indexstore_unit_reader_is_system_unit", as: CohereIndexUnitReaderIsSystemUnit.self)
        dependenciesApply = try symbol("indexstore_unit_reader_dependencies_apply_f", as: CohereIndexUnitReaderDependenciesApply.self)
        dependencyKind = try symbol("indexstore_unit_dependency_get_kind", as: CohereIndexUnitDependencyGetKind.self)
        dependencyName = try symbol("indexstore_unit_dependency_get_name", as: CohereIndexUnitDependencyGetName.self)
        dependencyFilePath = try symbol("indexstore_unit_dependency_get_filepath", as: CohereIndexUnitDependencyGetFilePath.self)
        dependencyModuleName = try symbol("indexstore_unit_dependency_get_modulename", as: CohereIndexUnitDependencyGetModuleName.self)
        recordReaderCreate = try symbol("indexstore_record_reader_create", as: CohereIndexRecordReaderCreate.self)
        recordReaderDispose = try symbol("indexstore_record_reader_dispose", as: CohereIndexRecordReaderDispose.self)
        occurrencesApply = try symbol("indexstore_record_reader_occurrences_apply_f", as: CohereIndexRecordReaderOccurrencesApply.self)
        occurrenceSymbol = try symbol("indexstore_occurrence_get_symbol", as: CohereIndexOccurrenceGetSymbol.self)
        occurrenceRoles = try symbol("indexstore_occurrence_get_roles", as: CohereIndexOccurrenceGetRoles.self)
        occurrenceLineColumn = try symbol("indexstore_occurrence_get_line_col", as: CohereIndexOccurrenceGetLineColumn.self)
        symbolName = try symbol("indexstore_symbol_get_name", as: CohereIndexSymbolGetString.self)
        symbolIdentifier = try symbol("indexstore_symbol_get_usr", as: CohereIndexSymbolGetString.self)
        symbolKind = try symbol("indexstore_symbol_get_kind", as: CohereIndexSymbolGetKind.self)
        guard FileManager.default.fileExists(atPath: storePath.appendingPathComponent("v5/units").path), let store = create(storePath.path, nil) else {
            throw Failure(description: "no index store at \(storePath.path): the build wrote none, or wrote it where this engine does not look")
        }
        self.store = store
        self.storePath = storePath
    }

    deinit {
        dispose(store)
    }

    /* The library beside the `swift` that `xcrun` resolves. */
    static func toolchainLibraryPath(runner: ProcessRunner = ProcessRunner()) throws -> String {
        let library = try SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner)
        return URL(fileURLWithPath: library).deletingLastPathComponent().appendingPathComponent("libIndexStore.dylib").path
    }

    /* The store a build into this scratch path writes. */
    static func storePath(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("out", isDirectory: true)
    }

    /* Every name in the file and what it resolves to, or nil when no unit newer than the file describes it. */
    func symbols(of file: URL) -> FileSymbols? {
        let path = file.resolvingSymlinksInPath().path
        let sourceModified = (try? file.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantFuture
        guard let newest = records()[path]?.filter({ $0.written >= sourceModified }).max(by: { $0.written < $1.written }) else { return nil }
        guard let reader = recordReaderCreate(store, newest.record, nil) else { return nil }
        defer { recordReaderDispose(reader) }
        final class Collected {
            var occurrences: [FileSymbols.Occurrence] = []
            let store: IndexStore
            init(store: IndexStore) { self.store = store }
        }
        let collected = Collected(store: self)
        _ = occurrencesApply(reader, Unmanaged.passUnretained(collected).toOpaque()) { context, occurrence in
            guard let context, let occurrence else { return true }
            let collected = Unmanaged<Collected>.fromOpaque(context).takeUnretainedValue()
            collected.occurrences.append(collected.store.occurrence(occurrence))
            return true
        }
        return FileSymbols(collected.occurrences)
    }

    private func occurrence(_ occurrence: CohereIndexOccurrence) -> FileSymbols.Occurrence {
        var line: UInt32 = 0
        var column: UInt32 = 0
        occurrenceLineColumn(occurrence, &line, &column)
        let symbol = occurrenceSymbol(occurrence)
        let roles = occurrenceRoles(occurrence)
        return FileSymbols.Occurrence(
            line: Int(line),
            column: Int(column),
            symbol: Self.text(symbolIdentifier(symbol)),
            name: Self.text(symbolName(symbol)),
            isReference: roles & Self.referenceRole != 0,
            isImplicit: roles & Self.implicitRole != 0
        )
    }

    /* Each file's records, by resolved path, with when the unit naming them was written. */
    private func records() -> [String: [(record: String, written: Date)]] {
        var byFile: [String: [(record: String, written: Date)]] = [:]
        for unit in units() where !unit.mainFile.isEmpty {
            byFile[unit.mainFile, default: []].append(contentsOf: unit.ownRecords.map { ($0, unit.written) })
        }
        return byFile
    }

    /*
     One spelling for a path under an SDK. The index names the same header through `MacOSX.sdk` and through the
     versioned SDK it links to, so each SDK root is resolved once and every path under it is written through that.
     */
    final class SDKRoots {
        private var resolved: [String: String] = [:]

        func canonical(_ path: String) -> String {
            guard let range = path.range(of: ".sdk/") else { return path }
            let root = String(path[..<range.upperBound])
            if resolved[root] == nil {
                resolved[root] = URL(fileURLWithPath: root).resolvingSymlinksInPath().path + "/"
            }
            return (resolved[root] ?? root) + path[range.upperBound...]
        }
    }

    /* Every unit in the store, read once. Measured on Presence: 2,531 units, listed in 9ms. */
    func units() -> [Unit] {
        if let cachedUnits {
            return cachedUnits
        }
        final class Names {
            var values: [String] = []
        }
        let names = Names()
        _ = unitsApply(store, 0, Unmanaged.passUnretained(names).toOpaque()) { context, name in
            guard let context else { return false }
            Unmanaged<Names>.fromOpaque(context).takeUnretainedValue().values.append(IndexStore.text(name))
            return true
        }
        let unitsDirectory = storePath.appendingPathComponent("v5/units", isDirectory: true)
        var units: [Unit] = []
        let sdkRoots = SDKRoots()
        for name in names.values {
            guard let reader = unitReaderCreate(store, name, nil) else { continue }
            defer { unitReaderDispose(reader) }
            let mainFile = Self.text(unitMainFile(reader))
            let written = (try? unitsDirectory.appendingPathComponent(name).resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
            final class Dependencies {
                let store: IndexStore
                let mainFile: String
                let sdkRoots: SDKRoots
                var imported: [String] = []
                var own: [String] = []
                var all: [String] = []
                var recordFiles: [String: String] = [:]
                var files: [String] = []
                init(store: IndexStore, mainFile: String, sdkRoots: SDKRoots) {
                    self.store = store
                    self.mainFile = mainFile
                    self.sdkRoots = sdkRoots
                }
            }
            let dependencies = Dependencies(store: self, mainFile: mainFile, sdkRoots: sdkRoots)
            _ = dependenciesApply(reader, Unmanaged.passUnretained(dependencies).toOpaque()) { context, dependency in
                guard let context, let dependency else { return true }
                let dependencies = Unmanaged<Dependencies>.fromOpaque(context).takeUnretainedValue()
                let store = dependencies.store
                switch store.dependencyKind(dependency) {
                case IndexStore.unitDependency:
                    let module = IndexStore.text(store.dependencyModuleName(dependency))
                    if !module.isEmpty {
                        dependencies.imported.append(module)
                    }
                case IndexStore.recordDependency:
                    let record = IndexStore.text(store.dependencyName(dependency))
                    let file = IndexStore.text(store.dependencyFilePath(dependency))
                    dependencies.all.append(record)
                    dependencies.recordFiles[record] = dependencies.sdkRoots.canonical(file)
                    /* The record of the unit's own file, matched by path: a unit's other records belong to files it only read. */
                    if file == dependencies.mainFile {
                        dependencies.own.append(record)
                    }
                case IndexStore.fileDependency:
                    dependencies.files.append(dependencies.sdkRoots.canonical(IndexStore.text(store.dependencyFilePath(dependency))))
                default:
                    break
                }
                return true
            }
            units.append(Unit(
                name: name,
                mainFile: mainFile.isEmpty ? "" : URL(fileURLWithPath: mainFile).resolvingSymlinksInPath().path,
                module: Self.text(unitModuleName(reader)),
                outputFile: Self.text(unitOutputFile(reader)),
                isSystem: unitIsSystem(reader),
                written: written,
                importedModules: dependencies.imported,
                ownRecords: dependencies.own,
                allRecords: dependencies.all,
                recordFiles: dependencies.recordFiles,
                files: dependencies.files
            ))
        }
        cachedUnits = units
        return units
    }

    /* Every occurrence one record holds, or nil when the record cannot be read. */
    func occurrences(inRecord record: String) -> [RecordOccurrence]? {
        guard let reader = recordReaderCreate(store, record, nil) else { return nil }
        defer { recordReaderDispose(reader) }
        final class Collected {
            var occurrences: [RecordOccurrence] = []
            let store: IndexStore
            init(store: IndexStore) { self.store = store }
        }
        let collected = Collected(store: self)
        _ = occurrencesApply(reader, Unmanaged.passUnretained(collected).toOpaque()) { context, occurrence in
            guard let context, let occurrence else { return true }
            let collected = Unmanaged<Collected>.fromOpaque(context).takeUnretainedValue()
            let store = collected.store
            var line: UInt32 = 0
            var column: UInt32 = 0
            store.occurrenceLineColumn(occurrence, &line, &column)
            let symbol = store.occurrenceSymbol(occurrence)
            collected.occurrences.append(RecordOccurrence(
                line: Int(line),
                column: Int(column),
                symbol: IndexStore.text(store.symbolIdentifier(symbol)),
                name: IndexStore.text(store.symbolName(symbol)),
                roles: store.occurrenceRoles(occurrence),
                kind: Int32(store.symbolKind(symbol))
            ))
            return true
        }
        return collected.occurrences
    }

    static func text(_ string: CohereIndexString) -> String {
        guard let data = string.data else { return "" }
        return String(decoding: UnsafeRawBufferPointer(start: data, count: string.length), as: UTF8.self)
    }
}
