import Foundation
import SwiftSyntax

/*
 `unused-import`: an `import` in a file of ours that nothing in the file uses, read from the build's index store.

 The question is narrow because `MemberImportVisibility` is on for every target we check: each file must
 import the modules whose members it uses, so a file's imports are its own and an import is used exactly when
 something written in that file resolves into the module or into a module it re-exports. The index answers
 both halves. Each file's unit lists the modules the file imports; each module's records declare its symbols,
 so a symbol a file refers to is placed in the module that declares it.

 An import counts as used when a reference in the file:
   - resolves to a declaration of that module, or of a module it re-exports (a system module re-exports what
     it imports, as a superset; a module of ours re-exports what its files `@_exported import`);
   - or names that module anywhere in its signature. The index does not record every member reference: on
     Presence, `$0.rawValue` on a MediaPipe `BlendShape` reached through a closure parameter has no occurrence
     at all, yet the file needs MediaPipe's import for it. The closure's type is in the signature of the
     function the file does call, and crediting every module a referenced signature names closes that gap.
 A reference to a module that is also imported directly credits only that module, so `import AppKit` beside
 `import Foundation` is not kept alive by `URL`.

 Zero false positives is the bar, so a file is left unchecked, and counted, when the index cannot vouch for
 it: no unit newer than the file, a reference no module claims, or any `#if` in it, since the index describes
 the configuration the build compiled and an inactive branch may be the only user of an import.
 `@_exported` imports are API and never reported. Measured before it shipped: every finding on both proving
 grounds was removed on a `git archive` copy, and `swift build --build-tests` stayed clean (83 on ahraos-macos,
 376 on Presence, 459 of 459).

 Not yet never-worse than SwiftLint's analyzer `unused_import`. On ahraos-macos SwiftLint 0.65.1 reports 123 to
 this rule's 83, 81 of them the same; of its 42 others, at least 24 are real (the compiler builds without them)
 and at least 10 are SwiftLint's false positives. The real ones are pairs such as `import AppKit` beside `import
 SwiftUI` in a file that uses `CGFloat`: each import's closure covers the reference, so both are credited, though
 either alone would do. Closing it needs the real re-export graph (each `.swiftinterface`'s `@_exported` lines,
 each Clang module map's exports) in place of the system superset above, and then a joint check that removes
 imports one at a time while the rest still cover every reference, keeping the most specific.
 */
struct UnusedImports {
    static let ruleName = "cohere-swift/unused-import"

    /* Modules every file imports without saying so. */
    static let implicitModules: Set<String> = ["Swift", "_Concurrency", "_StringProcessing", "_SwiftConcurrencyShims", "SwiftOnoneSupport"]

    struct Result {
        /* Each unused import, with the declaration as written for the report's line. */
        var findings: [(finding: FindingRecord, subject: String)] = []
        var filesChecked = 0
        /* Files the index could not vouch for, by path, with why. */
        var filesUnchecked: [String: String] = [:]
        /* Imports judged, and imports never reported by design (re-exported API), in the checked files. */
        var importsChecked = 0
        var importsExported = 0
    }

    static let notCompiled = "the build has not compiled it as it stands"
    static let conditional = "it has #if, and the index describes only the configuration the build compiled"
    static let unresolvedReference = "a reference in it resolves to no module the index declares"

    struct Source {
        let store: IndexStore
        let unit: IndexStore.Unit
    }

    let stores: [IndexStore]
    let demangler: SwiftDemangler?

    func run(files: [ParsedFile]) -> Result {
        var result = Result()
        let units = stores.flatMap { store in store.units().map { Source(store: store, unit: $0) } }

        /* The unit that describes each file as it stands: the newest one written after the file was. */
        var fresh: [String: Source] = [:]
        for source in units where !source.unit.isSystem && !source.unit.mainFile.isEmpty && !source.unit.ownRecords.isEmpty {
            if let held = fresh[source.unit.mainFile], held.unit.written >= source.unit.written {
                continue
            }
            fresh[source.unit.mainFile] = source
        }
        var checked: [(file: ParsedFile, source: Source, references: [IndexStore.RecordOccurrence])] = []
        for file in files {
            let path = file.url.resolvingSymlinksInPath().path
            let modified = (try? file.url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantFuture
            guard let source = fresh[path], source.unit.written >= modified else {
                result.filesUnchecked[file.url.path] = Self.notCompiled
                continue
            }
            if Self.hasConditionalCompilation(file.tree) {
                result.filesUnchecked[file.url.path] = Self.conditional
                continue
            }
            let references = source.unit.ownRecords.flatMap { source.store.occurrences(inRecord: $0) ?? [] }
                .filter { $0.isReference && $0.kind != IndexStore.moduleKind }
            checked.append((file, source, references))
        }

        let referenced = Set(checked.flatMap { $0.references.map(\.symbol) })
        let resolver = ModuleResolver(units: units, referenced: referenced, demangler: demangler)
        let reexports = Self.reexports(units: units.map(\.unit), files: files)

        for entry in checked {
            let imports = Self.imports(in: entry.file.tree)
            let imported = Set(imports.map(\.module))
            var used: Set<String> = []
            var unresolved = 0
            for reference in entry.references {
                guard var modules = resolver.declaringModules(of: reference) else {
                    unresolved += 1
                    continue
                }
                modules.formUnion(resolver.modulesNamed(inSignatureOf: reference.symbol))
                let direct = modules.intersection(imported)
                if !direct.isEmpty {
                    used.formUnion(direct)
                    continue
                }
                /*
                 The standard library is every file's without an import, and every system module imports it, so
                 a reference into it credits no import through a re-export. Found by the fixture: `Int` kept
                 `import Foundation` alive in a file that used nothing of Foundation's.
                 */
                let reexported = modules.subtracting(Self.implicitModules)
                for module in imported where !reexported.isDisjoint(with: reexports.closure(of: module)) {
                    used.insert(module)
                }
            }
            if unresolved > 0 {
                result.filesUnchecked[entry.file.url.path] = Self.unresolvedReference
                continue
            }
            result.filesChecked += 1
            for declaration in imports where !Self.implicitModules.contains(declaration.module) {
                if declaration.isExported {
                    result.importsExported += 1
                    continue
                }
                result.importsChecked += 1
                guard !used.contains(declaration.module) else { continue }
                let finding = entry.file.finding(
                    at: declaration.node,
                    rule: Self.ruleName,
                    messageId: "unusedImport",
                    message: "Nothing in this file uses \(declaration.module): no name here resolves into it or into a module it re-exports. Remove the import.",
                    suggestions: [FindingRecord.Suggestion(message: "Remove `import \(declaration.module)`", fixes: [Self.removal(of: declaration.node, in: entry.file)])]
                )
                result.findings.append((finding, declaration.node.trimmedDescription))
            }
        }
        return result
    }

    struct ImportDeclaration {
        var module: String
        var isExported: Bool
        var node: ImportDeclSyntax
    }

    /* The file's top-level imports. One inside `#if` is never reached here: such a file is left unchecked. */
    static func imports(in tree: SourceFileSyntax) -> [ImportDeclaration] {
        tree.statements.compactMap { statement in
            guard let declaration = statement.item.as(ImportDeclSyntax.self), let first = declaration.path.first else { return nil }
            let isExported = declaration.attributes.contains { attribute in
                attribute.as(AttributeSyntax.self)?.attributeName.trimmedDescription == "_exported"
            }
            return ImportDeclaration(module: first.name.text, isExported: isExported, node: declaration)
        }
    }

    static func hasConditionalCompilation(_ tree: SourceFileSyntax) -> Bool {
        final class Finder: SyntaxVisitor {
            var found = false
            override func visit(_ node: IfConfigDeclSyntax) -> SyntaxVisitorContinueKind {
                found = true
                return .skipChildren
            }
        }
        let finder = Finder(viewMode: .sourceAccurate)
        finder.walk(tree)
        return finder.found
    }

    /* The edit that removes the import's whole line: the declaration, the newline that ends it, and its leading trivia. */
    static func removal(of node: ImportDeclSyntax, in file: ParsedFile) -> FindingRecord.Edit {
        let utf8 = Array(file.source.utf8)
        var start = node.position.utf8Offset
        while start > 0 && utf8[start - 1] != UInt8(ascii: "\n") {
            start -= 1
        }
        var end = node.endPositionBeforeTrailingTrivia.utf8Offset
        while end < utf8.count && utf8[end] != UInt8(ascii: "\n") {
            end += 1
        }
        if end < utf8.count {
            end += 1
        }
        return FindingRecord.Edit(start: start, end: end, text: "")
    }

    /*
     What each module makes visible beyond itself. A system module is taken to re-export everything it imports,
     a superset, so an import is never reported for lack of an edge the interface would have shown. A module of
     ours re-exports exactly what its files `@_exported import`.
     */
    struct Reexports {
        var edges: [String: Set<String>]

        func closure(of module: String) -> Set<String> {
            var seen: Set<String> = []
            var pending = [module]
            while let current = pending.popLast() {
                for next in edges[current, default: []] where seen.insert(next).inserted {
                    pending.append(next)
                }
            }
            return seen
        }
    }

    static func reexports(units: [IndexStore.Unit], files: [ParsedFile]) -> Reexports {
        var edges: [String: Set<String>] = [:]
        for unit in units where unit.isSystem {
            edges[unit.module, default: []].formUnion(unit.importedModules)
        }
        let moduleOfFile = Dictionary(units.filter { !$0.isSystem }.map { ($0.mainFile, $0.module) }, uniquingKeysWith: { first, _ in first })
        for file in files {
            guard let module = moduleOfFile[file.url.resolvingSymlinksInPath().path] else { continue }
            for declaration in imports(in: file.tree) where declaration.isExported {
                edges[module, default: []].insert(declaration.module)
            }
        }
        return Reexports(edges: edges)
    }

    /*
     Which module declares a symbol, from every record the store holds. Only the symbols the checked files refer to
     are kept, so the table stays the size of the question rather than the size of the SDK.
     */
    struct ModuleResolver {
        private var declaring: [String: Set<String>] = [:]
        /* Top-level names Clang modules declare, for symbols Swift spells by name (`s:So11sockaddr_unV`, `s:SC7AF_UNIX`). */
        private var clangNames: [String: Set<String>] = [:]
        private let knownModules: Set<String>
        private let demangler: SwiftDemangler?

        init(units: [Source], referenced: Set<String>, demangler: SwiftDemangler?) {
            self.demangler = demangler
            knownModules = Set(units.map(\.unit.module).filter { !$0.isEmpty })
            var wanted = referenced
            var wantedClangNames: Set<String> = []
            for symbol in referenced {
                if let base = Self.accessorBase(symbol) {
                    wanted.insert(base)
                }
                if let objectiveCType = Self.objectiveCContainer(symbol) {
                    wanted.insert(objectiveCType)
                }
                if let name = Self.clangImportedName(symbol) {
                    wantedClangNames.insert(name)
                }
            }
            /*
             A record can be named by more than one unit, and each unit's module declares what it holds: read once,
             credited to every module whose unit names it. Crediting only the first unit read put Accelerate's
             vDSP in another module on Presence, and `import Accelerate` read as unused where every call needs it.
             */
            var modulesOfRecord: [String: (store: IndexStore, modules: Set<String>)] = [:]
            for source in units {
                for record in source.unit.allRecords {
                    if modulesOfRecord[record] == nil {
                        modulesOfRecord[record] = (source.store, [])
                    }
                    modulesOfRecord[record]?.modules.insert(source.unit.module)
                }
            }
            for (record, holder) in modulesOfRecord {
                for occurrence in holder.store.occurrences(inRecord: record) ?? [] where occurrence.isDeclaration {
                    if wanted.contains(occurrence.symbol) {
                        declaring[occurrence.symbol, default: []].formUnion(holder.modules)
                    }
                    if occurrence.symbol.hasPrefix("c:"), wantedClangNames.contains(occurrence.name) {
                        clangNames[occurrence.name, default: []].formUnion(holder.modules)
                    }
                }
            }
        }

        /* The modules that declare what the occurrence refers to, or nil when nothing the store holds says. */
        func declaringModules(of occurrence: IndexStore.RecordOccurrence) -> Set<String>? {
            let symbol = occurrence.symbol
            if let modules = declaring[symbol] {
                return modules
            }
            if let base = Self.accessorBase(symbol), let modules = declaring[base] {
                return modules
            }
            if let module = Self.categoryModule(symbol) {
                return [module]
            }
            if let container = Self.objectiveCContainer(symbol), let modules = declaring[container] {
                return modules
            }
            if let module = FileSymbols.Occurrence(line: 0, column: 0, symbol: symbol, name: occurrence.name, isReference: true).declaringModule {
                return [module]
            }
            if symbol.hasPrefix("s:s") || (symbol.hasPrefix("s:S") && !symbol.hasPrefix("s:So") && !symbol.hasPrefix("s:SC")) {
                return ["Swift"]
            }
            if let name = Self.clangImportedName(symbol), let modules = clangNames[name] {
                return modules
            }
            return nil
        }

        /* Every module of the build the declaration's demangled signature names, `CoreFoundation.CGFloat` and the like. */
        func modulesNamed(inSignatureOf symbol: String) -> Set<String> {
            guard let demangler, let declaration = demangler.declaration(ofSymbol: symbol) else { return [] }
            var modules: Set<String> = []
            var identifier = ""
            for character in declaration {
                if character.isLetter || character.isNumber || character == "_" {
                    identifier.append(character)
                    continue
                }
                if character == ".", knownModules.contains(identifier) {
                    modules.insert(identifier)
                }
                identifier = ""
            }
            return modules
        }

        /* An accessor's property or subscript: `...vg` (getter) to `...vp`, `...ig` to `...ip`. */
        static func accessorBase(_ symbol: String) -> String? {
            guard symbol.hasPrefix("s:"), symbol.count > 4 else { return nil }
            let characters = Array(symbol)
            let accessor = characters[characters.count - 1]
            let kind = characters[characters.count - 2]
            guard (kind == "v" || kind == "i"), "gsmrwWMaAlux".contains(accessor) else { return nil }
            return String(characters.dropLast()) + "p"
        }

        /* The module a category member carries in its own name: `c:@CM@Foundation@@objc(cs)NSString(im)...`. */
        static func categoryModule(_ symbol: String) -> String? {
            guard symbol.hasPrefix("c:@CM@") else { return nil }
            let rest = symbol.dropFirst("c:@CM@".count)
            guard let end = rest.firstIndex(of: "@") else { return nil }
            return String(rest[..<end])
        }

        /* The class or protocol an Objective-C member belongs to: `c:objc(cs)NSView(im)setIdentifier:` to `c:objc(cs)NSView`. */
        static func objectiveCContainer(_ symbol: String) -> String? {
            for prefix in ["c:objc(cs)", "c:objc(pl)"] where symbol.hasPrefix(prefix) {
                let rest = symbol.dropFirst(prefix.count)
                guard let end = rest.firstIndex(of: "(") else { return nil }
                return prefix + rest[..<end]
            }
            return nil
        }

        /* The C name Swift wraps in `s:So<length><name>` or `s:SC<length><name>`. */
        static func clangImportedName(_ symbol: String) -> String? {
            guard symbol.hasPrefix("s:So") || symbol.hasPrefix("s:SC") else { return nil }
            let rest = symbol.utf8.dropFirst(4)
            let digits = rest.prefix { $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }
            guard let length = Int(String(decoding: digits, as: UTF8.self)), length > 0 else { return nil }
            let name = rest.dropFirst(digits.count).prefix(length)
            return name.count == length ? String(decoding: name, as: UTF8.self) : nil
        }
    }
}
