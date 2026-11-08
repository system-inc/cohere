import Synchronization

/*
 Which declaration each name in one file resolves to, as the compiler decided it: the input a typed rule reads
 instead of guessing from a name's spelling.

 An occurrence is placed the way the contract places findings, 1-based line and 1-based UTF-8 byte column, at
 the first character of the name. Its symbol is a unified symbol name, the compiler's stable identifier for a
 declaration: `s:Sa5countSivp` is `Array.count`, and a type of ours carries its module in its name. Standard
 library declarations are spelled `s:S` (a standard substitution: `Sa` Array, `SD` Dictionary, `Sh` Set, `SS`
 String, `Sl` Collection, `ST` Sequence) or `s:s` and a length-prefixed name (`s:s14_ArrayProtocol`).

 Every source fills the same shape: the build's index store for a file the build compiled as it stands, the record
 sourcekitd wrote beside the types phase's in-process check of an edited file (`BodiesOnlyAnswers`), and
 sourcekitd's `source.request.indexsource` for a file neither describes.
 */
public struct FileSymbols: Sendable {
    public struct Occurrence: Sendable, Equatable, Codable {
        public var line: Int
        public var column: Int
        public var symbol: String
        public var name: String
        public var isReference: Bool
        /*
         Not written in the source but implied by it: reading `items.count` also records a call of `count`'s
         getter at the same place. A rule asks about what the source names, so implicit occurrences never answer.
         */
        public var isImplicit: Bool
        /*
         The demangler that places the declaration's module, and the log a symbol it cannot place goes to, from the
         `FileSymbols` that handed this occurrence out (`occurrences(line:column:)`). Nil for one made elsewhere, which
         asks the process's demangler and logs nowhere.
         */
        var context: Context?

        /* What an occurrence needs from the `FileSymbols` it came from. */
        final class Context: Sendable {
            let demangler: SwiftDemangler?
            let unplaced: UnplacedSymbols?

            init(demangler: SwiftDemangler?, unplaced: UnplacedSymbols?) {
                self.demangler = demangler
                self.unplaced = unplaced
            }
        }

        /* Where a declaration comes from, as far as a typed rule asks: the standard library, anywhere else, or nobody can say. */
        enum Origin: Equatable, Sendable {
            case standardLibrary
            case elsewhere
            /* The symbol names a standard library type, and the demangler places its declaration in no module. */
            case unplaced
        }

        /* Kept without its context, which belongs to the run that hands the occurrence out. */
        enum CodingKeys: String, CodingKey {
            case line
            case column
            case symbol
            case name
            case isReference
            case isImplicit
        }

        public static func == (left: Occurrence, right: Occurrence) -> Bool {
            left.line == right.line && left.column == right.column && left.symbol == right.symbol
                && left.name == right.name && left.isReference == right.isReference
                && left.isImplicit == right.isImplicit
        }

        public init(line: Int, column: Int, symbol: String, name: String, isReference: Bool, isImplicit: Bool = false) {
            self.line = line
            self.column = column
            self.symbol = symbol
            self.name = name
            self.isReference = isReference
            self.isImplicit = isImplicit
        }

        /*
         The module a declaration of a module's own type lives in, read from the symbol's leading length-prefixed
         name: `s:7Control6StatusO7runningyA2CmF` is `Control`'s. Nil for anything that does not start that way:
         the standard library's, an extension's member on another module's type, an Objective-C name.
         */
        public var declaringModule: String? {
            guard symbol.hasPrefix("s:") else { return nil }
            let rest = symbol.utf8.dropFirst(2)
            let digits = rest.prefix { $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }
            guard let length = Int(String(decoding: digits, as: UTF8.self)), length > 0 else { return nil }
            let name = rest.dropFirst(digits.count).prefix(length)
            return name.count == length ? String(decoding: name, as: UTF8.self) : nil
        }

        /*
         Declared by the Swift standard library. Its symbols name their type first, as a standard substitution
         (`s:Sa`, `s:ST`, `s:ScT`) or `s` and a length-prefixed name (`s:s14_ArrayProtocolP`), and `So` and `SC` are
         types imported from C and Objective-C (`s:So6CGRectV`), not its own. The type alone is not enough: a member an
         extension adds is the extending module's, `s:ST7ControlE6sorted` for our own `extension Sequence` or
         `s:ST10FoundationE` for Foundation's (found by the sorted-first-last rule, whose key-path `sorted(by:)` of ours
         read as the standard library's). Which module declares a member is the demangler's answer
         (`SwiftDemangler.module(ofSymbol:)`), never a reading of the mangling of ours: a constrained extension's generic
         signature sits between the module and the `E` that marks it, `s:Sa7ControlSiRszlE5count` for our `count` in an
         `extension Array where Element == Int` (#awb2ykk), and an earlier reading took that for the standard library's.
         The standard library's underscored companion modules ship with it and are its own (`_Concurrency` adds
         AsyncSequence's `filter`).
         */
        var origin: Origin {
            guard symbol.hasPrefix("s:") else { return .elsewhere }
            let rest = symbol.utf8.dropFirst(2)
            guard !rest.starts(with: "So".utf8), !rest.starts(with: "SC".utf8),
                rest.first == UInt8(ascii: "S") || rest.first == UInt8(ascii: "s")
            else { return .elsewhere }
            let demangler = context.map(\.demangler) ?? (try? SwiftDemangler.shared())
            guard let module = demangler?.module(ofSymbol: symbol) else { return .unplaced }
            return module == "Swift" || module.hasPrefix("_") ? .standardLibrary : .elsewhere
        }

        /*
         Declared by the Swift standard library (`origin`), as a typed rule asks it. A symbol the demangler cannot place
         is not the standard library's, so the rule says nothing of it, and it is logged where the occurrence came from,
         so the linter reports what hinged on it as unchecked rather than clean (`Linter`).
         */
        public var isStandardLibrary: Bool {
            switch origin {
                case .standardLibrary:
                    return true
                case .elsewhere:
                    return false
                case .unplaced:
                    context?.unplaced?.record(symbol)
                    return false
            }
        }
    }

    private struct Position: Hashable {
        var line: Int
        var column: Int
    }

    /* The symbols the demangler placed in no module while one typed rule judged a file, each once. */
    final class UnplacedSymbols: Sendable {
        private let symbols = Mutex<Set<String>>([])

        func record(_ symbol: String) {
            symbols.withLock { _ = $0.insert(symbol) }
        }

        var recorded: [String] { symbols.withLock { $0.sorted() } }
    }

    private let byPosition: [Position: [Occurrence]]
    public let count: Int
    /*
     The demangler this file's occurrences are placed with: the one `SymbolProvider` loaded, or the process's when the
     symbols were made by hand. Nil only when none could load, and then nothing is standard library's to anyone.
     */
    var demangler: SwiftDemangler? = try? SwiftDemangler.shared()
    /* Where the occurrences handed out log a symbol the demangler cannot place, nil when nobody is listening. */
    private var unplaced: UnplacedSymbols?
    /*
     The modules the checked package owns, as the compiler names them (a target's name with anything that is not
     an identifier character made `_`). Vendored packages' modules are not in it. Lets a rule tell a declaration
     of ours from a dependency's.
     */
    public var ownedModules: Set<String> = []

    public init(_ occurrences: [Occurrence], ownedModules: Set<String> = []) {
        byPosition = Dictionary(grouping: occurrences) { Position(line: $0.line, column: $0.column) }
        count = occurrences.count
        self.ownedModules = ownedModules
    }

    /* Every occurrence, in the order of their places, so the same names are always kept as the same list. */
    var everyOccurrence: [Occurrence] {
        byPosition.values.flatMap(\.self).sorted {
            ($0.line, $0.column, $0.symbol, $0.name) < ($1.line, $1.column, $1.symbol, $1.name)
        }
    }

    /* Whether the declaration was written in a module this package owns. */
    public func isOwned(_ occurrence: Occurrence) -> Bool {
        occurrence.declaringModule.map(ownedModules.contains) ?? false
    }

    /* These symbols, logging into `log` every symbol the demangler cannot place for a rule that asks. */
    func watching(_ log: UnplacedSymbols) -> FileSymbols {
        var watched = self
        watched.unplaced = log
        return watched
    }

    /* Every occurrence that starts at this place: usually one, more where one name both declares and refers. */
    public func occurrences(line: Int, column: Int) -> [Occurrence] {
        let found = byPosition[Position(line: line, column: column)] ?? []
        guard !found.isEmpty else { return [] }
        let context = Occurrence.Context(demangler: demangler, unplaced: unplaced)
        return found.map { occurrence in
            var placed = occurrence
            placed.context = context
            return placed
        }
    }

    /* The declaration a name at this place refers to, when the compiler recorded exactly one written reference there. */
    public func reference(line: Int, column: Int) -> Occurrence? {
        let references = occurrences(line: line, column: column).filter { $0.isReference && !$0.isImplicit }
        return references.count == 1 ? references.first : nil
    }
}
