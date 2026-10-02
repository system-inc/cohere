import SwiftSyntax

/*
 No `default:` in a switch over an enum this package declares. Listing every case lets the compiler check the
 switch: add a case to the enum and every switch that does not handle it stops compiling, which is how the new
 case reaches each place that must decide what it means. A `default` answers for every case at once, the ones
 added later included, so the switch keeps compiling and quietly gives the new case whatever the default does.
 The repair is to list the remaining cases. An enum of a dependency, the standard library's or the SDK's may
 gain cases we cannot see coming and do not own, so its `default` is left alone.

 There is no SwiftLint rule for this, so there are no shapes to match. The shape is ours: a `switch` with a plain
 `default:` clause, whose `case` patterns name enum elements that all belong to one enum, declared in a module
 this package owns. The finding is at the `default` keyword, and the message names the enum.

 The subject's type is read from the patterns, as the compiler resolved them in the index the build wrote. Every
 pattern's leading element is looked up at its name token: `.running`, `Status.done`, `.failed(let reason)`,
 `let .failed(reason)`, `.running?`, and the elements nested in a payload (`.leaf(.red)`). An element's symbol
 starts with its enum's: `s:7Control6StatusO7runningyA2CmF` is `running` in `s:7Control6StatusO`, the enum
 `Status` (`O`) in the module `Control`, and a nested enum carries its parents (`s:7Control6HolderC4ModeO...`).
 The element symbol is read name by name from the front until the enum's own name, as the compiler's demangler
 reads it, word substitutions included: `s:7Control0A5StateO` is `ControlState`, the `A` standing for the word
 `Control` already spelled. A private type's name is followed by its file's discriminator and `LL`, which is read
 past. What follows the element's name must be a constructor's type, ending in a metatype (`mF`, or `lF` after a
 generic signature), so a static member of the enum (`.preferred`, `vpZ`) is not taken for an element.

 A pattern resolving to an element is not alone proof of the subject's type. A custom `~=` lets
 `switch number { case Status.running: ... }` compile on an `Int`, which needs its `default`. So the rule asks
 for one leading-dot element, `.running` with no type written, at which the index records no `~=`: a leading
 dot is looked up in the subject's own type (or the type an optional wraps), and the compiler matched it as an
 enum element, not as a value compared by `~=`, which the index records as an implicit `~=` at the name. With
 that one anchor, the subject is the enum or an optional of it, and every other pattern (a static member, a
 `let` binding, a `where` guard) is still a pattern on it.

 Misses, accepted. A switch whose patterns name no element: only `default`, values, ranges, `let` bindings,
 `where`-only clauses. A switch whose every element is written with its type (`Status.running`), which no
 leading dot anchors. Tuple subjects (`switch (left, right)`), where a `default` stands for the combinations
 left out and is often what is meant. Elements from more than one enum, `.leaf(.red)` and `.some(.running)`
 among them, where the default may stand for either. `@unknown default`, which is a different statement: it
 still warns on a case not listed. An `@objc` enum, whose elements are Clang's names (`c:@M@Control@E@...`). An
 enum declared inside an extension of another module's type (`s:10Foundation4DateV7ControlE4KindO`) or inside
 a function, which this reading of the symbol does not follow. An element whose symbol does not read as above is
 not flagged.
 */
public struct NoDefaultForOwnedEnum: TypedFileRule {
    public let name = "cohere-swift/no-default-for-owned-enum"

    public init() {}

    /* A `switch` and a `default` anywhere in the file: nothing else can hold this shape. */
    public func applies(to file: ParsedFile) -> Bool {
        file.source.contains("switch") && file.source.contains("default")
    }

    public func findings(in file: ParsedFile, symbols: FileSymbols) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.found.compactMap { candidate in
            var enums: [String: Element] = [:]
            var isOwned = false
            var isAnchored = false
            for head in candidate.heads {
                let location = file.locations.location(for: head.name.positionAfterSkippingLeadingTrivia)
                let named = symbols.occurrences(line: location.line, column: location.column).filter { $0.isReference && !$0.isImplicit && Self.element($0.symbol) != nil }
                guard named.count == 1, let occurrence = named.first, let element = Self.element(occurrence.symbol) else { continue }
                enums[element.enumSymbol] = element
                isOwned = symbols.isOwned(occurrence)
                if head.isLeadingDot, !Self.matchedByPatternOperator(head.pattern, file: file, symbols: symbols) {
                    isAnchored = true
                }
            }
            guard isAnchored, isOwned, enums.count == 1, let element = enums.values.first else { return nil }
            return file.finding(
                at: candidate.defaultKeyword,
                rule: name,
                messageId: "noDefaultForOwnedEnum",
                message: "This default answers for every case of \(element.enumName), including any added later, so the compiler can no longer say this switch does not handle a new one. List the remaining cases instead."
            )
        }
    }

    /* The enum an element symbol names: the enum's own symbol, and its name with its parents'. */
    struct Element: Equatable {
        var enumSymbol: String
        var enumName: String
    }

    /* Whether the index records an implicit `~=` at any token of the pattern: the compiler compared a value, it did not match an element. */
    static func matchedByPatternOperator(_ pattern: PatternSyntax, file: ParsedFile, symbols: FileSymbols) -> Bool {
        pattern.tokens(viewMode: .sourceAccurate).contains { token in
            let location = file.locations.location(for: token.positionAfterSkippingLeadingTrivia)
            return symbols.occurrences(line: location.line, column: location.column).contains { $0.name.hasPrefix("~=") }
        }
    }

    /*
     The enum an element symbol belongs to, read from the front: `s:`, the module's name, then each enclosing
     type's name and kind letter (`O` enum, `V` struct, `C` class), until a name followed by none of them, which is
     the element's. The last enclosing type must be an enum, and what follows the element's name must be a
     constructor's type. Anything else, a substitution, a function's local context, punycode, reads as nil.
     */
    static func element(_ symbol: String) -> Element? {
        guard symbol.hasPrefix("s:") else { return nil }
        var reader = MangledNameReader(Array(symbol.utf8.dropFirst(2)))
        guard reader.identifier() != nil else { return nil }
        var enclosing: [String] = []
        var lastKind: UInt8 = 0
        var enumEnd = 0
        while true {
            guard let identifier = reader.identifier() else { return nil }
            /* A private type's name is followed by its file's discriminator and `LL`: `14HandProbePhase33_B31C...LLO`. */
            if let next = reader.peek, MangledNameReader.isDigit(next) {
                guard reader.identifier() != nil, reader.bytes[reader.position...].starts(with: "LL".utf8) else { return nil }
                reader.position += 2
            }
            guard let kind = reader.peek, [UInt8(ascii: "O"), UInt8(ascii: "V"), UInt8(ascii: "C")].contains(kind) else { break }
            reader.position += 1
            enclosing.append(identifier)
            lastKind = kind
            enumEnd = reader.position
        }
        let remainder = reader.bytes[reader.position...]
        let isConstructorType = remainder.reversed().starts(with: "Fm".utf8) || (remainder.reversed().starts(with: "Fl".utf8) && remainder.contains(UInt8(ascii: "m")))
        guard lastKind == UInt8(ascii: "O"), isConstructorType else { return nil }
        let enumSymbol = "s:" + String(decoding: reader.bytes[..<enumEnd], as: UTF8.self)
        return Element(enumSymbol: enumSymbol, enumName: enclosing.joined(separator: "."))
    }

    /*
     Reads identifiers from a mangled name the way the compiler's demangler does. A plain identifier is its length
     and its characters. One starting `0` uses word substitutions: a lowercase letter is a word spelled earlier in
     the name, an uppercase letter is the last such word, a length and characters are spelled out, and a `0` ends
     it. Every spelled-out part adds its words to the list the letters index.
     */
    struct MangledNameReader {
        let bytes: [UInt8]
        var position = 0
        var words: [String] = []

        init(_ bytes: [UInt8]) {
            self.bytes = bytes
        }

        var peek: UInt8? {
            position < bytes.count ? bytes[position] : nil
        }

        mutating func identifier() -> String? {
            guard let first = peek, Self.isDigit(first) else { return nil }
            var hasWordSubstitutions = false
            if first == UInt8(ascii: "0") {
                position += 1
                /* `00` is punycode, a name with characters outside ASCII. */
                guard peek != UInt8(ascii: "0") else { return nil }
                hasWordSubstitutions = true
            }
            var text = ""
            repeat {
                while hasWordSubstitutions, let letter = peek, Self.isLetter(letter) {
                    position += 1
                    let isLast = letter < UInt8(ascii: "a")
                    let index = Int(letter - (isLast ? UInt8(ascii: "A") : UInt8(ascii: "a")))
                    guard index < words.count else { return nil }
                    text += words[index]
                    if isLast {
                        hasWordSubstitutions = false
                    }
                }
                if peek == UInt8(ascii: "0") {
                    position += 1
                    break
                }
                guard let length = natural(), length > 0, position + length <= bytes.count else { return nil }
                let part = bytes[position..<(position + length)]
                position += length
                addWords(Array(part))
                text += String(decoding: part, as: UTF8.self)
            } while hasWordSubstitutions
            return text
        }

        mutating func natural() -> Int? {
            let start = position
            while let digit = peek, Self.isDigit(digit) {
                position += 1
            }
            return position > start ? Int(String(decoding: bytes[start..<position], as: UTF8.self)) : nil
        }

        /* A word starts at any character but a digit or `_`, and ends before `_`, at the end, or where a lowercase letter meets an uppercase one. Only words of two characters or more count, at most 26. */
        mutating func addWords(_ part: [UInt8]) {
            var wordStart: Int?
            for index in 0...part.count {
                let character = index < part.count ? part[index] : 0
                if let start = wordStart, character == UInt8(ascii: "_") || character == 0 || (!Self.isUppercase(part[index - 1]) && Self.isUppercase(character)) {
                    if index - start >= 2, words.count < 26 {
                        words.append(String(decoding: part[start..<index], as: UTF8.self))
                    }
                    wordStart = nil
                }
                if wordStart == nil, character != 0, character != UInt8(ascii: "_"), !Self.isDigit(character) {
                    wordStart = index
                }
            }
        }

        static func isDigit(_ character: UInt8) -> Bool {
            character >= UInt8(ascii: "0") && character <= UInt8(ascii: "9")
        }

        static func isUppercase(_ character: UInt8) -> Bool {
            character >= UInt8(ascii: "A") && character <= UInt8(ascii: "Z")
        }

        static func isLetter(_ character: UInt8) -> Bool {
            isUppercase(character) || (character >= UInt8(ascii: "a") && character <= UInt8(ascii: "z"))
        }
    }

    /* One element a pattern names: its name token, whether it is the pattern's own leading-dot element, and the whole pattern it heads. */
    struct Head {
        var name: TokenSyntax
        var isLeadingDot: Bool
        var pattern: PatternSyntax
    }

    /* A switch with a plain `default:`, its keyword, and every element its patterns name. */
    struct Candidate {
        var defaultKeyword: TokenSyntax
        var heads: [Head]
    }

    final class Visitor: SyntaxVisitor {
        private(set) var found: [Candidate] = []

        override func visit(_ node: SwitchExprSyntax) -> SyntaxVisitorContinueKind {
            if let tuple = node.subject.as(TupleExprSyntax.self), tuple.elements.count > 1 {
                return .visitChildren
            }
            var defaultKeyword: TokenSyntax?
            var heads: [Head] = []
            for switchCase in Self.cases(node.cases) {
                switch switchCase.label {
                case .default(let label):
                    if switchCase.attribute == nil {
                        defaultKeyword = label.defaultKeyword
                    }
                case .case(let label):
                    for item in label.caseItems {
                        heads += Self.heads(item.pattern, isTop: true, top: item.pattern)
                    }
                }
            }
            if let defaultKeyword {
                found.append(Candidate(defaultKeyword: defaultKeyword, heads: heads))
            }
            return .visitChildren
        }

        /* The switch's own cases, through `#if` clauses, and none of a nested switch's. */
        static func cases(_ list: SwitchCaseListSyntax) -> [SwitchCaseSyntax] {
            list.flatMap { element -> [SwitchCaseSyntax] in
                switch element {
                case .switchCase(let switchCase):
                    return [switchCase]
                case .conditionalBlockDecl(let conditionalBlock):
                    return conditionalBlock.clauses.flatMap { clause -> [SwitchCaseSyntax] in
                        guard case .switchCases(let nested) = clause.elements else { return [] }
                        return cases(nested)
                    }
                }
            }
        }

        /* The element a pattern leads with, through `let`, a call's payload and a trailing `?`, and those its payload names. */
        static func heads(_ pattern: PatternSyntax, isTop: Bool, top: PatternSyntax) -> [Head] {
            if let binding = pattern.as(ValueBindingPatternSyntax.self) {
                return heads(binding.pattern, isTop: isTop, top: top)
            }
            guard let expression = pattern.as(ExpressionPatternSyntax.self) else { return [] }
            return heads(expression.expression, isTop: isTop, top: top)
        }

        static func heads(_ expression: ExprSyntax, isTop: Bool, top: PatternSyntax) -> [Head] {
            if let optional = expression.as(OptionalChainingExprSyntax.self) {
                return heads(optional.expression, isTop: isTop, top: top)
            }
            if let pattern = expression.as(PatternExprSyntax.self) {
                return heads(pattern.pattern, isTop: false, top: top)
            }
            var payload: [Head] = []
            var called = expression
            if let call = expression.as(FunctionCallExprSyntax.self) {
                called = call.calledExpression
                payload = call.arguments.flatMap { heads($0.expression, isTop: false, top: top) }
            }
            guard let member = called.as(MemberAccessExprSyntax.self) else { return payload }
            return [Head(name: member.declName.baseName, isLeadingDot: isTop && member.base == nil, pattern: top)] + payload
        }
    }
}
