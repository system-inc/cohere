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

    /* `INDEXSTORE_UNIT_DEPENDENCY_RECORD`: the dependency that names the record of the unit's own file. */
    private static let recordDependency: Int32 = 2
    /* `INDEXSTORE_SYMBOL_ROLE_REFERENCE`. */
    private static let referenceRole: UInt64 = 1 << 2
    /* `INDEXSTORE_SYMBOL_ROLE_IMPLICIT`. */
    private static let implicitRole: UInt64 = 1 << 8

    private let store: CohereIndexStore
    private let storePath: URL
    private let dispose: CohereIndexStoreDispose
    private let unitsApply: CohereIndexStoreUnitsApply
    private let unitReaderCreate: CohereIndexUnitReaderCreate
    private let unitReaderDispose: CohereIndexUnitReaderDispose
    private let unitMainFile: CohereIndexUnitReaderGetMainFile
    private let dependenciesApply: CohereIndexUnitReaderDependenciesApply
    private let dependencyKind: CohereIndexUnitDependencyGetKind
    private let dependencyName: CohereIndexUnitDependencyGetName
    private let dependencyFilePath: CohereIndexUnitDependencyGetFilePath
    private let recordReaderCreate: CohereIndexRecordReaderCreate
    private let recordReaderDispose: CohereIndexRecordReaderDispose
    private let occurrencesApply: CohereIndexRecordReaderOccurrencesApply
    private let occurrenceSymbol: CohereIndexOccurrenceGetSymbol
    private let occurrenceRoles: CohereIndexOccurrenceGetRoles
    private let occurrenceLineColumn: CohereIndexOccurrenceGetLineColumn
    private let symbolName: CohereIndexSymbolGetString
    private let symbolIdentifier: CohereIndexSymbolGetString

    /* Each file's records, by resolved path, with when the unit naming them was written. Read once, on first use. */
    private var recordsByFile: [String: [(record: String, written: Date)]]?

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
        dependenciesApply = try symbol("indexstore_unit_reader_dependencies_apply_f", as: CohereIndexUnitReaderDependenciesApply.self)
        dependencyKind = try symbol("indexstore_unit_dependency_get_kind", as: CohereIndexUnitDependencyGetKind.self)
        dependencyName = try symbol("indexstore_unit_dependency_get_name", as: CohereIndexUnitDependencyGetName.self)
        dependencyFilePath = try symbol("indexstore_unit_dependency_get_filepath", as: CohereIndexUnitDependencyGetFilePath.self)
        recordReaderCreate = try symbol("indexstore_record_reader_create", as: CohereIndexRecordReaderCreate.self)
        recordReaderDispose = try symbol("indexstore_record_reader_dispose", as: CohereIndexRecordReaderDispose.self)
        occurrencesApply = try symbol("indexstore_record_reader_occurrences_apply_f", as: CohereIndexRecordReaderOccurrencesApply.self)
        occurrenceSymbol = try symbol("indexstore_occurrence_get_symbol", as: CohereIndexOccurrenceGetSymbol.self)
        occurrenceRoles = try symbol("indexstore_occurrence_get_roles", as: CohereIndexOccurrenceGetRoles.self)
        occurrenceLineColumn = try symbol("indexstore_occurrence_get_line_col", as: CohereIndexOccurrenceGetLineColumn.self)
        symbolName = try symbol("indexstore_symbol_get_name", as: CohereIndexSymbolGetString.self)
        symbolIdentifier = try symbol("indexstore_symbol_get_usr", as: CohereIndexSymbolGetString.self)
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

    private func records() -> [String: [(record: String, written: Date)]] {
        if let recordsByFile {
            return recordsByFile
        }
        final class Names {
            var values: [String] = []
        }
        let units = Names()
        _ = unitsApply(store, 0, Unmanaged.passUnretained(units).toOpaque()) { context, name in
            guard let context else { return false }
            Unmanaged<Names>.fromOpaque(context).takeUnretainedValue().values.append(IndexStore.text(name))
            return true
        }
        let unitsDirectory = storePath.appendingPathComponent("v5/units", isDirectory: true)
        var byFile: [String: [(record: String, written: Date)]] = [:]
        for unit in units.values {
            guard let reader = unitReaderCreate(store, unit, nil) else { continue }
            defer { unitReaderDispose(reader) }
            let mainFile = Self.text(unitMainFile(reader))
            guard !mainFile.isEmpty else { continue }
            let written = (try? unitsDirectory.appendingPathComponent(unit).resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
            /* The record of the unit's own file, matched by path: a unit's other records belong to files it only read. */
            final class Dependencies {
                let store: IndexStore
                let mainFile: String
                var records: [String] = []
                init(store: IndexStore, mainFile: String) {
                    self.store = store
                    self.mainFile = mainFile
                }
            }
            let dependencies = Dependencies(store: self, mainFile: mainFile)
            _ = dependenciesApply(reader, Unmanaged.passUnretained(dependencies).toOpaque()) { context, dependency in
                guard let context, let dependency else { return true }
                let dependencies = Unmanaged<Dependencies>.fromOpaque(context).takeUnretainedValue()
                let store = dependencies.store
                if store.dependencyKind(dependency) == IndexStore.recordDependency,
                    IndexStore.text(store.dependencyFilePath(dependency)) == dependencies.mainFile
                {
                    dependencies.records.append(IndexStore.text(store.dependencyName(dependency)))
                }
                return true
            }
            let path = URL(fileURLWithPath: mainFile).resolvingSymlinksInPath().path
            byFile[path, default: []].append(contentsOf: dependencies.records.map { ($0, written) })
        }
        recordsByFile = byFile
        return byFile
    }

    static func text(_ string: CohereIndexString) -> String {
        guard let data = string.data else { return "" }
        return String(decoding: UnsafeRawBufferPointer(start: data, count: string.length), as: UTF8.self)
    }
}
