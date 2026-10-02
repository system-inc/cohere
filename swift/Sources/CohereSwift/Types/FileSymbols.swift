/*
 Which declaration each name in one file resolves to, as the compiler decided it: the input a typed rule reads
 instead of guessing from a name's spelling.

 An occurrence is placed the way the contract places findings, 1-based line and 1-based UTF-8 byte column, at
 the first character of the name. Its symbol is a unified symbol name, the compiler's stable identifier for a
 declaration: `s:Sa5countSivp` is `Array.count`, and a type of ours carries its module in its name. Standard
 library declarations are spelled `s:S` (a standard substitution: `Sa` Array, `SD` Dictionary, `Sh` Set, `SS`
 String, `Sl` Collection, `ST` Sequence) or `s:s` and a length-prefixed name (`s:s14_ArrayProtocol`).

 Both sources fill the same shape: the build's index store for a file the build compiled as it stands, and
 sourcekitd's `source.request.indexsource` for one it did not.
 */
public struct FileSymbols: Sendable {
    public struct Occurrence: Sendable, Equatable {
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

        public init(line: Int, column: Int, symbol: String, name: String, isReference: Bool, isImplicit: Bool = false) {
            self.line = line
            self.column = column
            self.symbol = symbol
            self.name = name
            self.isReference = isReference
            self.isImplicit = isImplicit
        }

        /* Declared by the Swift standard library, by the spelling every standard library symbol name has. */
        public var isStandardLibrary: Bool {
            symbol.hasPrefix("s:S") || symbol.hasPrefix("s:s")
        }
    }

    private struct Position: Hashable {
        var line: Int
        var column: Int
    }

    private let byPosition: [Position: [Occurrence]]
    public let count: Int

    public init(_ occurrences: [Occurrence]) {
        byPosition = Dictionary(grouping: occurrences) { Position(line: $0.line, column: $0.column) }
        count = occurrences.count
    }

    /* Every occurrence that starts at this place: usually one, more where one name both declares and refers. */
    public func occurrences(line: Int, column: Int) -> [Occurrence] {
        byPosition[Position(line: line, column: column)] ?? []
    }

    /* The declaration a name at this place refers to, when the compiler recorded exactly one written reference there. */
    public func reference(line: Int, column: Int) -> Occurrence? {
        let references = occurrences(line: line, column: column).filter { $0.isReference && !$0.isImplicit }
        return references.count == 1 ? references.first : nil
    }
}
