/*
 What a type conforms to and inherits from, followed to the end through the index's base-of relations: the
 question `unused-declaration` asks before it reports a stored property, since a conformance that reaches
 `Encodable` or `Decodable` reads every stored property whether or not the program names it.

 The index records an inheritance clause as a reference to each protocol or superclass, related base-of to the
 type, protocol or extension that names it. `Codable` arrives as two implicit references, to `Encodable`
 (`s:SE`) and `Decodable` (`s:Se`), and a protocol of ours that refines it carries the same two. A conformance
 an extension adds is related to the extension, and the extension's reference to the type it extends, related
 extended-by, brings it home. Every record of ours is read, so an extension anywhere in the module counts.

 A Swift module the build imported is read only when a path reaches one of its protocols or classes, and then
 whole, once, with the Swift modules it imports when it does not declare the symbol itself: `Hashable` leads into
 the standard library's record, `View` into SwiftUI's and then SwiftUICore's, which declares it. What cannot be followed
 is never taken as clear: a C or Objective-C type (its inheritance lives in headers, outside what this reads), a
 type alias (`Codable & Sendable` behind a name), or a protocol or class whose declaring record the store does
 not hold.
 */
struct ConformanceGraph {
    enum Reach: Equatable {
        /* Some path reaches `Encodable` or `Decodable`. */
        case codable
        /* No path does, but some path reaches something the index cannot see into. */
        case unseen
        case clear
    }

    static let encodable = "s:SE"
    static let decodable = "s:Se"

    /* What each type, protocol or extension names in its inheritance clause. */
    private var parents: [String: Set<String>] = [:]
    /* Each extension of a type, by the type. */
    private var extensions: [String: Set<String>] = [:]
    /* The kind of every symbol a record read so far declares. */
    private var kinds: [String: Int32] = [:]
    /* Each imported Swift module's records, read on first need, and the modules it imports. */
    private var moduleRecords: [String: [(store: IndexStore, record: String)]] = [:]
    private var moduleImports: [String: Set<String>] = [:]
    private var loadedModules: Set<String> = []

    init(stores: [IndexStore]) {
        var seen: Set<String> = []
        for store in stores {
            for unit in store.units() where unit.isSystem && !unit.module.isEmpty && unit.outputFile.hasSuffix(".swiftinterface") {
                moduleImports[unit.module, default: []].formUnion(unit.importedModules)
                for record in unit.allRecords where seen.insert(record).inserted {
                    moduleRecords[unit.module, default: []].append((store, record))
                }
            }
        }
    }

    /* One record's declarations and inheritance, read with its relations. */
    mutating func add(_ occurrences: [IndexStore.RecordOccurrence]) {
        for occurrence in occurrences {
            if occurrence.isDeclaration {
                kinds[occurrence.symbol] = occurrence.kind
            }
            for relation in occurrence.relations {
                if relation.roles & IndexStore.baseOfRole != 0 {
                    parents[relation.symbol, default: []].insert(occurrence.symbol)
                }
                if relation.roles & IndexStore.extendedByRole != 0 {
                    extensions[occurrence.symbol, default: []].insert(relation.symbol)
                }
            }
        }
    }

    /* Where the type's conformances and superclasses lead, its extensions' included. */
    mutating func reach(of type: String) -> Reach {
        var visited: Set<String> = [type]
        var queue = [type]
        var unseen = false
        while let node = queue.popLast() {
            let named = (parents[node] ?? []).union((extensions[node] ?? []).flatMap { parents[$0] ?? [] })
            for parent in named where visited.insert(parent).inserted {
                if parent == Self.encodable || parent == Self.decodable {
                    return .codable
                }
                switch kind(of: parent) {
                case IndexStore.protocolKind, IndexStore.classKind:
                    queue.append(parent)
                case nil, IndexStore.typeAliasKind:
                    unseen = true
                default:
                    /* A struct or an enum, which an inheritance clause names only as an enum's raw type: it adds no conformance of the type's. */
                    break
                }
            }
        }
        return unseen ? .unseen : .clear
    }

    /* The symbol's kind, reading the Swift module that declares it if no record read so far does. Nil for a C or Objective-C symbol, or one no record declares. */
    private mutating func kind(of symbol: String) -> Int32? {
        if let known = kinds[symbol] {
            return known
        }
        guard let module = Self.swiftModule(of: symbol) else { return nil }
        /*
         The module a name spells, then the Swift modules it imports: a declaration moved to another module keeps the
         name it was first declared under, so `s:7SwiftUI4ViewP` is declared in SwiftUICore. The standard library's
         substitutions name its companion modules' declarations the same way: `s:Sci` is `_Concurrency`'s AsyncSequence.
         */
        load(module)
        for imported in (moduleImports[module] ?? []).sorted() where kinds[symbol] == nil {
            load(imported)
        }
        return kinds[symbol]
    }

    private mutating func load(_ module: String) {
        guard loadedModules.insert(module).inserted else { return }
        for (store, record) in moduleRecords[module] ?? [] {
            add(store.occurrences(inRecord: record, relations: true) ?? [])
        }
    }

    /* The Swift module a symbol's name places it in: `s:7SwiftUI4ViewP` is SwiftUI's, `s:SH` the standard library's. Nil for a C or Objective-C symbol, including one Swift imports (`s:So`, `s:SC`). */
    static func swiftModule(of symbol: String) -> String? {
        guard symbol.hasPrefix("s:"), !symbol.hasPrefix("s:So"), !symbol.hasPrefix("s:SC") else { return nil }
        if symbol.hasPrefix("s:s") || symbol.hasPrefix("s:S") {
            return "Swift"
        }
        return FileSymbols.Occurrence(line: 0, column: 0, symbol: symbol, name: "", isReference: false).declaringModule
    }
}
