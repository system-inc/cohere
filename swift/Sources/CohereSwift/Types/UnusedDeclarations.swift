import Foundation
import SwiftSyntax

/*
 `unused-declaration`: a declaration of ours that nothing refers to, read from the build's index store.

 The first kinds judged are the ones a single file can prove: a declaration `private` or `fileprivate`, or
 inside a type or extension that is, can be named only in its own file, so the file's own index record holds
 every reference it will ever have. A declaration is used when that record holds a reference to it from outside
 its own text (a function calling itself, or a type naming itself in its body, is not a use), or when another
 declaration in the file overrides or witnesses it, which removing it would break. A property wrapped by an
 attribute is also used when its `$name` or `_name` is, since `$flag` passed as a binding names the projection
 and never the property.

 What the index cannot see is treated as used, and counted by reason rather than reported, after Periphery's
 list: protocol witnesses and overrides; anything the Objective-C runtime reaches (`@objc`, `@IBAction`,
 `@IBOutlet`, `@NSManaged`, `dynamic`); entry points and previews; members the compiler calls by name
 (a property wrapper's `wrappedValue`, a result builder's `build` methods, `callAsFunction`, dynamic member
 lookup); a declaration carrying an attribute that may register it, a macro such as `@Test`; Codable's coding
 keys. Some are left alone because removing them changes what the program does without breaking the build:
 an initializer, which may exist to keep a type from being built; a stored property of a type whose
 conformances may read every stored property (Codable); an instance's stored property whose initializer runs
 code, which may be held for what it does or keeps alive (a subscription, an observer token); a field of a
 struct made only of numbers and SIMD vectors, whose bytes a shader or C may read whole, so an unread field is
 padding that holds the layout (found on Presence, where LockerBloom's `unused: Int32` pads the constants its
 Metal shader reads); a case of an enum that may be reached through its conformances or raw values
 (`CaseIterable`, `init(rawValue:)`).

 A file is left unchecked, and counted, when the index cannot vouch for it: no unit newer than the file, or any
 `#if` in it, since an inactive branch may be the only user of a declaration. Only the outermost declaration
 is reported when an unused one holds others, so every declaration reported can go together.
 */
struct UnusedDeclarations {
    static let ruleName = "cohere-swift/unused-declaration"

    struct Result {
        /* Each unused declaration, with its keyword and name for the report's line. */
        var findings: [(finding: FindingRecord, subject: String)] = []
        var filesChecked = 0
        /* Files the index could not vouch for, by path, with why. */
        var filesUnchecked: [String: String] = [:]
        /* Declarations judged, and declarations never reported, by why. */
        var declarationsChecked = 0
        var skipped: [String: Int] = [:]
    }

    static let overrides = "an override or a protocol witness, reached through what it overrides"
    static let objectiveC = "reached by the Objective-C runtime (@objc, @IBAction, @IBOutlet, @NSManaged, dynamic)"
    static let entryPoint = "an entry point or a preview (@main, PreviewProvider), reached from outside the program"
    static let registered = "an attribute that may register it, a macro such as @Test"
    static let compilerCalled = "called by the compiler by name: a property wrapper's or result builder's members, callAsFunction, dynamic member lookup"
    static let codingKeys = "Codable's coding keys, read by the synthesized conformance"
    static let initializer = "an initializer, which may exist to keep its type from being built another way"
    static let reflectedStorage = "a stored property of a type whose conformances may read every stored property (Codable)"
    static let lifetime = "an instance's stored property whose initializer runs code, which may be kept for what it does or keeps alive"
    static let layout = "a field of a struct of plain numbers, whose bytes may be read whole (a shader's constants, a C struct), padding included"
    static let reachableCase = "a case of an enum with raw values or conformances that may reach it (CaseIterable, Codable, init(rawValue:))"
    static let sharedDeclaration = "declared together with others (`let a = 1, b = 2`, `case a, b`), which the report cannot remove apart"
    static let unplaced = "the index records no definition at its name"

    let stores: [IndexStore]

    func run(files: [ParsedFile]) -> Result {
        var result = Result()
        let fresh = IndexStore.freshUnits(of: files, in: stores)
        for file in files {
            guard let described = fresh[file.url.path] else {
                result.filesUnchecked[file.url.path] = UnusedImports.notCompiled
                continue
            }
            if UnusedImports.hasConditionalCompilation(file.tree) {
                result.filesUnchecked[file.url.path] = UnusedImports.conditional
                continue
            }
            let occurrences = described.unit.ownRecords.flatMap { described.store.occurrences(inRecord: $0, relations: true) ?? [] }
            result.filesChecked += 1
            judge(file, occurrences: occurrences, into: &result)
        }
        return result
    }

    /* One file's declarations against its own record. */
    private func judge(_ file: ParsedFile, occurrences: [IndexStore.RecordOccurrence], into result: inout Result) {
        var definitions: [Place: [IndexStore.RecordOccurrence]] = [:]
        var references: [String: [Place]] = [:]
        var referencesByName: [String: [Place]] = [:]
        var overridden: Set<String> = []
        for occurrence in occurrences {
            let place = Place(line: occurrence.line, column: occurrence.column)
            if occurrence.isDeclaration && !occurrence.isImplicit {
                definitions[place, default: []].append(occurrence)
            }
            if occurrence.isReference {
                references[occurrence.symbol, default: []].append(place)
                /* Reading a property records its getter too; either names the property. */
                if let base = UnusedImports.ModuleResolver.accessorBase(occurrence.symbol) {
                    references[base, default: []].append(place)
                }
                referencesByName[occurrence.name, default: []].append(place)
            }
            overridden.formUnion(occurrence.overridden)
        }

        var collector = Collector(tree: file.tree)
        collector.collect()
        var unused: [(candidate: Candidate, symbol: IndexStore.RecordOccurrence, range: ClosedRange<Place>)] = []
        for candidate in collector.candidates {
            if let exemption = candidate.exemption {
                result.skipped[exemption, default: 0] += 1
                continue
            }
            let location = file.locations.location(for: candidate.name.positionAfterSkippingLeadingTrivia)
            let placed = definitions[Place(line: location.line, column: location.column)] ?? []
            guard let definition = placed.first else {
                result.skipped[Self.unplaced, default: 0] += 1
                continue
            }
            if placed.contains(where: { $0.roles & IndexStore.overrideOfRole != 0 }) || candidate.isOverride {
                result.skipped[Self.overrides, default: 0] += 1
                continue
            }
            result.declarationsChecked += 1
            let start = file.locations.location(for: candidate.node.positionAfterSkippingLeadingTrivia)
            let end = file.locations.location(for: candidate.node.endPositionBeforeTrailingTrivia)
            let range = Place(line: start.line, column: start.column)...Place(line: end.line, column: end.column)
            let symbols = Set(placed.map(\.symbol))
            if !symbols.isDisjoint(with: overridden) {
                continue
            }
            var places = symbols.flatMap { references[$0] ?? [] }
            if candidate.isWrapped {
                places += (referencesByName["$" + candidate.name.text] ?? []) + (referencesByName["_" + candidate.name.text] ?? [])
            }
            if places.contains(where: { !range.contains($0) }) {
                continue
            }
            unused.append((candidate, definition, range))
        }

        /* The outermost only: a declaration inside another one reported goes with it. */
        let outermost = unused.filter { inner in
            !unused.contains { outer in outer.range != inner.range && outer.range.contains(inner.range.lowerBound) && outer.range.contains(inner.range.upperBound) }
        }
        for entry in outermost {
            let name = entry.candidate.keyword == "func" || entry.candidate.keyword == "subscript" ? entry.symbol.name : entry.candidate.name.text
            let subject = "\(entry.candidate.keyword) \(name)"
            let finding = file.finding(
                at: entry.candidate.name,
                rule: Self.ruleName,
                messageId: "unusedDeclaration",
                message: "Nothing refers to \(name), and it is private to this file, so nothing outside the file can. Remove it.",
                suggestions: [FindingRecord.Suggestion(message: "Remove `\(subject)`", fixes: [Self.removal(of: entry.candidate.node, in: file)])]
            )
            result.findings.append((finding, subject))
        }
    }

    /* A place in a file as the index gives it: 1-based line, 1-based UTF-8 column. */
    struct Place: Hashable, Comparable {
        var line: Int
        var column: Int

        static func < (first: Place, second: Place) -> Bool {
            (first.line, first.column) < (second.line, second.column)
        }
    }

    /* One declaration the file alone can judge, or one it could but never reports, with why. */
    struct Candidate {
        /* The whole declaration, attributes and body, which a removal takes. */
        var node: Syntax
        /* The name the index places the declaration at. */
        var name: TokenSyntax
        var keyword: String
        var exemption: String?
        /* A property with an attribute, whose projection (`$name`) and storage (`_name`) are its uses too. */
        var isWrapped = false
        var isOverride = false
    }

    /*
     Every declaration in the file and what encloses it. Types come first, in a pass of their own, so an extension
     knows whether the type it extends is private to the file and what conformances the type has, wherever in the
     file each is written.
     */
    struct Collector {
        /* What a declaration sits inside: whether that keeps it in this file, and the type it belongs to. */
        struct Scope {
            var fileScoped = false
            var typeName: String?
            /* A struct whose stored properties are all plain numbers or SIMD vectors: its bytes may be its meaning. */
            var isLayout = false
            var conformances: Set<String> = []
            var attributes: Set<String> = []
        }

        let tree: SourceFileSyntax
        private(set) var candidates: [Candidate] = []
        /* Types declared in the file by qualified name (`Outer.Inner`), and whether each is private to it. */
        private var fileScopedTypes: [String: Bool] = [:]
        /* What each type conforms to or inherits from, by qualified name: its declaration's clause and every extension's in the file. */
        private var conformances: [String: Set<String>] = [:]

        init(tree: SourceFileSyntax) {
            self.tree = tree
        }

        mutating func collect() {
            for statement in tree.statements {
                if let declaration = statement.item.as(DeclSyntax.self) {
                    survey(declaration, enclosing: nil, fileScoped: false)
                }
            }
            for statement in tree.statements {
                if let declaration = statement.item.as(DeclSyntax.self) {
                    visit(declaration, in: Scope())
                }
            }
        }

        /* The first pass: each type's qualified name, whether it is private to the file, and its conformances. */
        private mutating func survey(_ declaration: DeclSyntax, enclosing: String?, fileScoped: Bool) {
            if let extended = declaration.as(ExtensionDeclSyntax.self) {
                let name = extended.extendedType.trimmedDescription
                conformances[name, default: []].formUnion(Self.inherited(extended.inheritanceClause))
                for member in extended.memberBlock.members {
                    survey(member.decl, enclosing: name, fileScoped: fileScoped || Self.isFileScoped(extended.modifiers))
                }
                return
            }
            guard let group = declaration.asProtocol((any DeclGroupSyntax).self), let named = declaration.asProtocol((any NamedDeclSyntax).self) else { return }
            let name = enclosing.map { "\($0).\(named.name.text)" } ?? named.name.text
            let scoped = fileScoped || Self.isFileScoped(group.modifiers)
            fileScopedTypes[name] = (fileScopedTypes[name] ?? true) && scoped
            conformances[name, default: []].formUnion(Self.inherited(group.inheritanceClause))
            for member in group.memberBlock.members {
                survey(member.decl, enclosing: name, fileScoped: scoped)
            }
        }

        private mutating func visit(_ declaration: DeclSyntax, in scope: Scope) {
            let attributes = Self.attributeNames(declaration.asProtocol((any WithAttributesSyntax).self)?.attributes)
            let modifiers = declaration.asProtocol((any WithModifiersSyntax).self)?.modifiers ?? []
            let fileScoped = scope.fileScoped || Self.isFileScoped(modifiers)
            let runtime = !attributes.isDisjoint(with: Self.objectiveCAttributes) || modifiers.contains { ["dynamic", "optional"].contains($0.name.text) }
                || scope.attributes.contains("objcMembers")
            let entry = scope.attributes.contains("main") || scope.conformances.contains("PreviewProvider")
            let isOverride = modifiers.contains { $0.name.text == "override" }

            if let extended = declaration.as(ExtensionDeclSyntax.self) {
                let name = extended.extendedType.trimmedDescription
                let inner = Scope(
                    fileScoped: Self.isFileScoped(extended.modifiers) || fileScopedTypes[name] == true,
                    typeName: name,
                    conformances: conformances[name] ?? [],
                    attributes: []
                )
                for member in extended.memberBlock.members {
                    visit(member.decl, in: inner)
                }
                return
            }

            if let group = declaration.asProtocol((any DeclGroupSyntax).self), let named = declaration.asProtocol((any NamedDeclSyntax).self) {
                let name = scope.typeName.map { "\($0).\(named.name.text)" } ?? named.name.text
                let typeConformances = conformances[name] ?? []
                if fileScoped {
                    var exemption: String?
                    if runtime {
                        exemption = UnusedDeclarations.objectiveC
                    } else if entry || attributes.contains("main") || typeConformances.contains("PreviewProvider") {
                        exemption = UnusedDeclarations.entryPoint
                    } else if !attributes.subtracting(Self.knownAttributes).isEmpty {
                        exemption = UnusedDeclarations.registered
                    } else if named.name.text == "CodingKeys" {
                        exemption = UnusedDeclarations.codingKeys
                    }
                    candidates.append(Candidate(node: Syntax(declaration), name: named.name, keyword: group.introducer.text, exemption: exemption))
                }
                let inner = Scope(
                    fileScoped: fileScoped,
                    typeName: name,
                    isLayout: declaration.as(StructDeclSyntax.self).map(Self.isLayout) ?? false,
                    conformances: typeConformances,
                    attributes: attributes.union(entry ? ["main"] : [])
                )
                for member in group.memberBlock.members {
                    visit(member.decl, in: inner)
                }
                return
            }

            guard fileScoped else { return }
            var exemption: String?
            if runtime {
                exemption = UnusedDeclarations.objectiveC
            } else if entry {
                exemption = UnusedDeclarations.entryPoint
            }

            if let function = declaration.as(FunctionDeclSyntax.self) {
                let name = function.name.text
                if exemption == nil {
                    if !attributes.subtracting(Self.knownAttributes).isEmpty {
                        exemption = UnusedDeclarations.registered
                    } else if name == "callAsFunction" || (scope.attributes.contains("resultBuilder") && name.hasPrefix("build"))
                        || (scope.attributes.contains("dynamicCallable") && name.hasPrefix("dynamicallyCall"))
                    {
                        exemption = UnusedDeclarations.compilerCalled
                    }
                }
                candidates.append(Candidate(node: Syntax(function), name: function.name, keyword: "func", exemption: exemption, isOverride: isOverride))
            } else if let variable = declaration.as(VariableDeclSyntax.self) {
                guard variable.bindings.count == 1, let binding = variable.bindings.first, let pattern = binding.pattern.as(IdentifierPatternSyntax.self) else {
                    if let first = variable.bindings.first?.pattern.firstToken(viewMode: .sourceAccurate) {
                        candidates.append(Candidate(node: Syntax(variable), name: first, keyword: variable.bindingSpecifier.text, exemption: UnusedDeclarations.sharedDeclaration))
                    }
                    return
                }
                let name = pattern.identifier.text
                let isStored = Self.isStored(binding)
                let isStatic = modifiers.contains { ["static", "class"].contains($0.name.text) }
                if exemption == nil {
                    if scope.attributes.contains("propertyWrapper") && ["wrappedValue", "projectedValue"].contains(name) {
                        exemption = UnusedDeclarations.compilerCalled
                    } else if isStored, scope.typeName != nil, !isStatic, !scope.conformances.isSubset(of: Self.storageBlindConformances) {
                        exemption = UnusedDeclarations.reflectedStorage
                    } else if isStored, scope.typeName != nil, !isStatic, let value = binding.initializer?.value, !Self.isPlainValue(value) {
                        exemption = UnusedDeclarations.lifetime
                    } else if isStored, scope.isLayout, !isStatic {
                        exemption = UnusedDeclarations.layout
                    }
                }
                candidates.append(Candidate(
                    node: Syntax(variable),
                    name: pattern.identifier,
                    keyword: variable.bindingSpecifier.text,
                    exemption: exemption,
                    isWrapped: !attributes.subtracting(Self.knownAttributes).isEmpty,
                    isOverride: isOverride
                ))
            } else if let alias = declaration.as(TypeAliasDeclSyntax.self) {
                candidates.append(Candidate(node: Syntax(alias), name: alias.name, keyword: "typealias", exemption: exemption))
            } else if let cases = declaration.as(EnumCaseDeclSyntax.self) {
                guard let element = cases.elements.first else { return }
                if exemption == nil {
                    if cases.elements.count > 1 {
                        exemption = UnusedDeclarations.sharedDeclaration
                    } else if scope.typeName?.hasSuffix("CodingKeys") == true {
                        exemption = UnusedDeclarations.codingKeys
                    } else if !scope.conformances.isSubset(of: Self.caseBlindConformances) {
                        exemption = UnusedDeclarations.reachableCase
                    }
                }
                candidates.append(Candidate(node: Syntax(cases), name: element.name, keyword: "case", exemption: exemption))
            } else if let subscriptDeclaration = declaration.as(SubscriptDeclSyntax.self) {
                let dynamicMember = subscriptDeclaration.parameterClause.parameters.first?.firstName.text == "dynamicMember"
                if exemption == nil && dynamicMember {
                    exemption = UnusedDeclarations.compilerCalled
                }
                candidates.append(Candidate(
                    node: Syntax(subscriptDeclaration),
                    name: subscriptDeclaration.subscriptKeyword,
                    keyword: "subscript",
                    exemption: exemption,
                    isOverride: isOverride
                ))
            } else if let initializer = declaration.as(InitializerDeclSyntax.self) {
                candidates.append(Candidate(node: Syntax(initializer), name: initializer.initKeyword, keyword: "init", exemption: exemption ?? UnusedDeclarations.initializer))
            }
        }

        /* `private` or `fileprivate` on the declaration itself, and not on its setter alone (`private(set)`). */
        static func isFileScoped(_ modifiers: DeclModifierListSyntax) -> Bool {
            modifiers.contains { ["private", "fileprivate"].contains($0.name.text) && $0.detail == nil }
        }

        /* Each name an inheritance clause lists, as its last component: `Swift.Codable` and `@unchecked Sendable` give `Codable` and `Sendable`. */
        static func inherited(_ clause: InheritanceClauseSyntax?) -> Set<String> {
            var names: Set<String> = []
            func add(_ type: TypeSyntax) {
                if let attributed = type.as(AttributedTypeSyntax.self) {
                    add(attributed.baseType)
                } else if let composition = type.as(CompositionTypeSyntax.self) {
                    composition.elements.forEach { add($0.type) }
                } else if let member = type.as(MemberTypeSyntax.self) {
                    names.insert(member.name.text)
                } else if let identifier = type.as(IdentifierTypeSyntax.self) {
                    names.insert(identifier.name.text)
                } else {
                    names.insert(type.trimmedDescription)
                }
            }
            clause?.inheritedTypes.forEach { add($0.type) }
            return names
        }

        static func attributeNames(_ attributes: AttributeListSyntax?) -> Set<String> {
            Set((attributes ?? []).compactMap { $0.as(AttributeSyntax.self)?.attributeName.trimmedDescription })
        }

        /* A stored property: no accessors, or only observers. */
        static func isStored(_ binding: PatternBindingSyntax) -> Bool {
            guard let accessors = binding.accessorBlock?.accessors else { return true }
            guard let list = accessors.as(AccessorDeclListSyntax.self) else { return false }
            return list.allSatisfy { ["willSet", "didSet"].contains($0.accessorSpecifier.text) }
        }

        /*
         A struct made only of numbers: every instance stored property is typed as an integer, a float, a `Bool` or a
         SIMD vector or matrix, or untyped and given a number. Such a struct is how Swift spells a C struct or a
         shader's constants, and its fields are read by offset, not by name.
         */
        static func isLayout(_ structure: StructDeclSyntax) -> Bool {
            var fields = 0
            for member in structure.memberBlock.members {
                guard let variable = member.decl.as(VariableDeclSyntax.self), !variable.modifiers.contains(where: { $0.name.text == "static" }) else { continue }
                for binding in variable.bindings where isStored(binding) {
                    fields += 1
                    if let type = binding.typeAnnotation?.type.trimmedDescription {
                        guard numericTypes.contains(type) || numericGenerics.contains(where: { type.hasPrefix($0) }) else { return false }
                    } else if let value = binding.initializer?.value {
                        guard value.is(IntegerLiteralExprSyntax.self) || value.is(FloatLiteralExprSyntax.self) else { return false }
                    } else {
                        return false
                    }
                }
            }
            return fields > 0
        }

        static let numericTypes: Set<String> = [
            "Int", "Int8", "Int16", "Int32", "Int64", "UInt", "UInt8", "UInt16", "UInt32", "UInt64", "Float", "Float16", "Float32", "Float64", "Double",
            "CGFloat", "Bool",
        ]

        /* SIMD vectors and the simd module's vector and matrix types, by how their names begin: `SIMD4<Float>`, `simd_float4x4`. */
        static let numericGenerics = ["SIMD2<", "SIMD3<", "SIMD4<", "SIMD8<", "SIMD16<", "simd_", "matrix_", "packed_"]

        /* A value that runs no code of anyone's to make: a literal, a collection of them, a member like `.zero`. */
        static func isPlainValue(_ expression: ExprSyntax) -> Bool {
            if expression.is(IntegerLiteralExprSyntax.self) || expression.is(FloatLiteralExprSyntax.self) || expression.is(BooleanLiteralExprSyntax.self)
                || expression.is(NilLiteralExprSyntax.self)
            {
                return true
            }
            if let string = expression.as(StringLiteralExprSyntax.self) {
                return string.segments.allSatisfy { $0.is(StringSegmentSyntax.self) }
            }
            if let array = expression.as(ArrayExprSyntax.self) {
                return array.elements.allSatisfy { isPlainValue($0.expression) }
            }
            if let dictionary = expression.as(DictionaryExprSyntax.self) {
                guard case .elements(let elements) = dictionary.content else { return true }
                return elements.allSatisfy { isPlainValue($0.key) && isPlainValue($0.value) }
            }
            if let prefix = expression.as(PrefixOperatorExprSyntax.self) {
                return prefix.operator.text == "-" && isPlainValue(prefix.expression)
            }
            if let member = expression.as(MemberAccessExprSyntax.self) {
                return member.base.map { $0.is(DeclReferenceExprSyntax.self) } ?? true
            }
            return false
        }

        /* Attributes that register nothing and that the Objective-C runtime does not read. Any other names something that may reach the declaration, a macro or a framework, and keeps it. */
        static let knownAttributes: Set<String> = [
            "available", "discardableResult", "inline", "inlinable", "usableFromInline", "frozen", "MainActor", "Sendable", "preconcurrency",
            "nonobjc", "ViewBuilder", "ToolbarContentBuilder", "SceneBuilder", "CommandsBuilder", "warn_unqualified_access", "_disfavoredOverload",
            "backDeployed", "specialize", "_specialize", "_effects", "_optimize", "_semantics", "resultBuilder", "propertyWrapper", "dynamicCallable",
            "dynamicMemberLookup", "unchecked", "retroactive", "Observable", "ObservationIgnored", "ObservationTracked", "concurrent",
        ]

        static let objectiveCAttributes: Set<String> = [
            "objc", "objcMembers", "IBAction", "IBOutlet", "IBInspectable", "IBDesignable", "IBSegueAction", "NSManaged", "GKInspectable", "_cdecl",
            "_silgen_name", "_dynamicReplacement", "NSApplicationMain", "UIApplicationMain",
        ]

        /* Conformances whose synthesized members never read a stored property the rest of the program does not. */
        static let storageBlindConformances: Set<String> = [
            "Equatable", "Hashable", "Comparable", "Sendable", "Identifiable", "View", "ViewModifier", "Shape", "CustomStringConvertible",
            "CustomDebugStringConvertible", "Error", "LocalizedError", "ObservableObject", "AnyObject", "Actor",
        ]

        /* Conformances that never reach a case the program does not name. */
        static let caseBlindConformances: Set<String> = [
            "Equatable", "Hashable", "Comparable", "Sendable", "Error", "LocalizedError", "CustomStringConvertible", "CustomDebugStringConvertible",
        ]
    }

    /*
     The edit that removes the declaration: its whole lines when it has them to itself, with the comments written
     directly above it and a comment trailing its last line, and otherwise only its own text. A `// MARK:` line
     above it stays, since it heads what follows rather than this one declaration. Removed from between two blank
     lines, it takes one of them, so no double gap is left where it was.
     */
    static func removal(of node: Syntax, in file: ParsedFile) -> FindingRecord.Edit {
        let utf8 = Array(file.source.utf8)
        let start = node.positionAfterSkippingLeadingTrivia.utf8Offset
        let end = node.endPositionBeforeTrailingTrivia.utf8Offset
        var lineStart = start
        while lineStart > 0 && utf8[lineStart - 1] != UInt8(ascii: "\n") {
            lineStart -= 1
        }
        var lineEnd = end
        while lineEnd < utf8.count && utf8[lineEnd] != UInt8(ascii: "\n") {
            lineEnd += 1
        }
        let blank: (UInt8) -> Bool = { $0 == UInt8(ascii: " ") || $0 == UInt8(ascii: "\t") }
        let rest = utf8[end..<lineEnd].drop(while: blank)
        guard utf8[lineStart..<start].allSatisfy(blank), rest.isEmpty || rest.starts(with: "//".utf8) else {
            return FindingRecord.Edit(start: start, end: end, text: "")
        }
        var removalStart = lineStart
        var cursor = start
        scan: for piece in node.leadingTrivia.pieces.reversed() {
            switch piece {
            case .spaces, .tabs:
                cursor -= piece.sourceLength.utf8Length
            case .newlines(let count), .carriageReturnLineFeeds(let count):
                guard count == 1 else { break scan }
                cursor -= piece.sourceLength.utf8Length
            case .lineComment(let text), .blockComment(let text), .docLineComment(let text), .docBlockComment(let text):
                guard !text.hasPrefix("// MARK") else { break scan }
                cursor -= piece.sourceLength.utf8Length
                removalStart = cursor
                while removalStart > 0 && utf8[removalStart - 1] != UInt8(ascii: "\n") {
                    removalStart -= 1
                }
            default:
                break scan
            }
        }
        var removalEnd = min(lineEnd + 1, utf8.count)
        if Self.isBlankLine(endingAt: removalStart, in: utf8), let next = Self.blankLineEnd(from: removalEnd, in: utf8) {
            removalEnd = next
        }
        return FindingRecord.Edit(start: removalStart, end: removalEnd, text: "")
    }

    /* Whether the line ending just before `offset` holds only spaces and tabs. */
    private static func isBlankLine(endingAt offset: Int, in utf8: [UInt8]) -> Bool {
        guard offset > 0 else { return false }
        var cursor = offset - 1
        while cursor > 0 && (utf8[cursor - 1] == UInt8(ascii: " ") || utf8[cursor - 1] == UInt8(ascii: "\t")) {
            cursor -= 1
        }
        return cursor == 0 || utf8[cursor - 1] == UInt8(ascii: "\n")
    }

    /* The offset past the line starting at `offset` when it holds only spaces and tabs, or nil. */
    private static func blankLineEnd(from offset: Int, in utf8: [UInt8]) -> Int? {
        var cursor = offset
        while cursor < utf8.count && (utf8[cursor] == UInt8(ascii: " ") || utf8[cursor] == UInt8(ascii: "\t")) {
            cursor += 1
        }
        guard cursor < utf8.count, utf8[cursor] == UInt8(ascii: "\n") else { return nil }
        return cursor + 1
    }
}
