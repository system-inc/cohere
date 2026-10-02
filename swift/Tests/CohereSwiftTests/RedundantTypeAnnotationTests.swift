import Foundation
import SwiftParser
import SwiftSyntax
import Testing

@testable import CohereSwift

/*
 `redundant-type-annotation` both ways. The unit cases parse a source string and hand the rule symbols built by
 position, as the index records them: every name in `types` or `members` resolves at every place it is written,
 and an initializer from `initializers` is recorded where a call builds its type (the type's name, the `init` of
 `URL.init`, the bracket of `[Int]()`), keyed by the type's name or by `Array` and `Dictionary` for the sugar.
 A type left out of `initializers` records none, as a literal the compiler coerces does. The triggering cases
 are SwiftLint's documented examples for `redundant_type_annotation` in its default configuration, each
 asserting its line and column at the annotation's colon; the non-triggering cases are SwiftLint's own and the
 look-alikes the declarations tell apart. The end-to-end case runs a real package, so the symbols come from the
 index the build wrote.
 */
@Suite(.serialized)
struct RedundantTypeAnnotationTests {
    static let url = "s:10Foundation3URLV"
    static let urlInitializer = "s:10Foundation3URLVACycfc"
    static let urlStringInitializer = "s:10Foundation3URLV6stringACSgSSh_tcfc"
    static let integer = "s:Si"
    static let set = "s:Sh"
    static let setInitializer = "s:ShyShyxGqd__nc7ElementQyd__RszSTRd__lufc"
    static let arrayInitializer = "s:S2ayxGycfc"
    static let dictionaryInitializer = "s:S2Dyxq_Gycfc"
    static let characterSet = "s:10Foundation12CharacterSetV"
    static let alphanumerics = "s:10Foundation12CharacterSetV13alphanumericsACvpZ"
    static let random = "s:s17FixedWidthIntegerPsE6random2inxSnyxG_tFZ"
    static let direction = "s:7Control9DirectionO"
    static let up = "s:7Control9DirectionO2upyA2CmF"
    static let moved = "s:7Control9DirectionO5movedyACSi_tcACmF"
    static let shared = "s:7Control9DirectionO6sharedACvpZ"
    static let outer = "s:7Control1AV"
    static let inner = "s:7Control1AV1BV"
    static let innerInitializer = "s:7Control1AV1BVAEycfc"
    static let base = "s:7Control4BaseC"
    static let baseInitializer = "s:7Control4BaseCACycfc"
    static let derived = "s:7Control7DerivedC"
    static let derivedInitializer = "s:7Control7DerivedCACycfc"

    /* SwiftLint's examples name `URL`, `Int`, `Set`, `CharacterSet`, `Direction` and `A.B`, resolved as the index resolves them. */
    static let incumbentTypes = ["URL": url, "Int": integer, "Set": set, "CharacterSet": characterSet, "Direction": direction, "A": outer, "B": inner]
    static let incumbentInitializers = ["URL": urlInitializer, "Set": setInitializer, "Array": arrayInitializer, "B": innerInitializer]
    static let incumbentMembers = ["alphanumerics": alphanumerics, "random": random, "up": up, "moved": moved, "shared": shared]

    /* The findings, as `line:column`, with the names resolved as the maps say. */
    static func findings(_ source: String, types: [String: String] = incumbentTypes, initializers: [String: String] = incumbentInitializers, members: [String: String] = incumbentMembers) -> [String] {
        let url = URL(fileURLWithPath: "/fixture/Subject.swift")
        let file = ParsedFile(url: url, targetName: "Fixture", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
        var occurrences: [FileSymbols.Occurrence] = []
        func record(_ token: TokenSyntax, _ symbol: String, _ name: String) {
            let location = file.locations.location(for: token.positionAfterSkippingLeadingTrivia)
            occurrences.append(FileSymbols.Occurrence(line: location.line, column: location.column, symbol: symbol, name: name, isReference: true))
        }
        for token in file.tree.tokens(viewMode: .sourceAccurate) {
            if let symbol = types[token.text] {
                record(token, symbol, token.text)
            }
            if let symbol = members[token.text] {
                record(token, symbol, token.text)
                record(token, symbol.replacingOccurrences(of: "vpZ", with: "vgZ"), "getter:\(token.text)")
                occurrences[occurrences.count - 1].isImplicit = true
            }
        }
        let calls = Calls(viewMode: .sourceAccurate)
        calls.walk(file.tree)
        for (key, token) in calls.found {
            if let symbol = initializers[key] {
                record(token, symbol, "init()")
            }
        }
        return RedundantTypeAnnotation().findings(in: file, symbols: FileSymbols(occurrences)).map { "\($0.line):\($0.column)" }
    }

    /* Where the index records the initializer of each call that builds a type, keyed by the type's name. */
    final class Calls: SyntaxVisitor {
        private(set) var found: [(String, TokenSyntax)] = []

        override func visit(_ node: FunctionCallExprSyntax) -> SyntaxVisitorContinueKind {
            if let member = node.calledExpression.as(MemberAccessExprSyntax.self), member.declName.baseName.tokenKind == .keyword(.`init`), let base = member.base, let name = Self.name(base) {
                found.append((name.text, member.declName.baseName))
            } else if let name = Self.name(node.calledExpression) {
                found.append((name.text, name))
            } else if let array = node.calledExpression.as(ArrayExprSyntax.self) {
                found.append(("Array", array.leftSquare))
            } else if let dictionary = node.calledExpression.as(DictionaryExprSyntax.self) {
                found.append(("Dictionary", dictionary.leftSquare))
            }
            return .visitChildren
        }

        static func name(_ expression: ExprSyntax) -> TokenSyntax? {
            if let reference = expression.as(DeclReferenceExprSyntax.self) {
                return reference.baseName
            }
            if let member = expression.as(MemberAccessExprSyntax.self), member.base != nil {
                return member.declName.baseName
            }
            return expression.as(GenericSpecializationExprSyntax.self).flatMap { name($0.expression) }
        }
    }

    /* SwiftLint's triggering examples for a call of the type, its `init`, and the bindings it reads them in, each at the annotation's colon. */
    @Test func incumbentConstructionExamplesAreFound() {
        let source = """
            var url:URL=URL()
            var url: URL = URL()
            let url: URL = URL()
            lazy var url: URL = URL()
            let url: URL = URL()!
            var one: Int = 1, two: Int = Int(5), three: Int
            guard let url: URL = URL() else { return }
            if let url: URL = URL() { return }
            let a: [Int] = [Int]()
            let a: A.B = A.B()

            """
        #expect(Self.findings(source) == ["1:8", "2:8", "3:8", "4:13", "5:8", "6:22", "7:14", "8:11", "9:6", "10:6"])
    }

    /* SwiftLint's triggering examples for a generic spelled in the initializer, with the annotation spelling it too or leaving it bare. */
    @Test func incumbentGenericExamplesAreFound() {
        let source = """
            var set: Set<Int> = Set<Int>([])
            var set: Set<Int> = Set<Int>.init([])
            var set: Set = Set<Int>([])
            var set: Set = Set<Int>.init([])
            guard var set: Set = Set<Int>([]) else { return }
            if var set: Set = Set<Int>.init([]) { return }
            guard var set: Set<Int> = Set<Int>([]) else { return }
            if var set: Set<Int> = Set<Int>.init([]) { return }
            var set: Set = Set<Int>([]), otherSet: Set<Int>

            """
        #expect(Self.findings(source) == ["1:8", "2:8", "3:8", "4:8", "5:14", "6:11", "7:14", "8:11", "9:8"])
    }

    /* SwiftLint's triggering examples inside a method, under an attribute it does not ignore, inside a computed property, and an enum case. */
    @Test func incumbentPlacementExamplesAreFound() {
        let source = """
            class ViewController: UIViewController {
              func someMethod() {
                let myVar: Int = Int(5)
              }
            }
            @DontIgnoreMe var a: Int = Int(5)
            @IgnoreMe
            var a: Int {
                let i: Int = Int(1)
                return i
            }
            var direction: Direction = Direction.up

            """
        #expect(Self.findings(source) == ["3:14", "6:20", "9:10", "12:14"])
    }

    /*
     SwiftLint's member-read examples the index cannot judge: a static member of the type may hold any type
     convertible to it, and a chain's type is its last member's. Misses, accepted.
     */
    @Test func incumbentMemberReadExamplesAreMisses() {
        let source = """
            let alphanumerics: CharacterSet = CharacterSet.alphanumerics
            var num: Int = Int.random(0..<10)
            let a: A = A.b.c.d
            let a: A = A.f().b
            let shared: Direction = Direction.shared

            """
        #expect(Self.findings(source).isEmpty)
    }

    /* SwiftLint's `URL(string: "")` example does not compile: the initializer is failable, so it binds only to an optional unless unwrapped. */
    @Test func aFailableInitializerIsFoundOnlyUnwrappedOrBound() {
        let source = """
            var url: URL = URL(string: "")
            let forced: URL = URL(string: "")!
            if let bound: URL = URL(string: "") { return }

            """
        #expect(Self.findings(source, initializers: ["URL": Self.urlStringInitializer]) == ["2:11", "3:13"])
    }

    /* SwiftLint's non-triggering examples, in its default configuration. */
    @Test func incumbentNonTriggeringExamplesAreNotFound() {
        let source = """
            var url = URL()
            var url: CustomStringConvertible = URL()
            var one: Int = 1, two: Int = 2, three: Int
            guard let url = URL() else { return }
            if let url = URL() { return }
            let alphanumerics = CharacterSet.alphanumerics
            var set: Set<Int> = Set([])
            var set: Set<Int> = Set.init([])
            var set = Set<Int>([])
            var set = Set<Int>.init([])
            guard var set: Set<Int> = Set([]) else { return }
            if var set: Set<Int> = Set.init([]) { return }
            guard var set = Set<Int>([]) else { return }
            if var set = Set<Int>.init([]) { return }
            var one: A<T> = B()
            var one: A = B<T>()
            var one: A<T> = B<T>()
            let a = A.b.c.d
            let a: B = A.b.c.d
            var direction: Direction = .up
            var direction = Direction.up
            var bol: Bool = true
            var dbl: Double = 0.0
            var int: Int = 0
            var str: String = "str"

            """
        #expect(Self.findings(source, types: Self.incumbentTypes.merging(["CustomStringConvertible": "s:s23CustomStringConvertibleP", "Bool": "s:Sb", "Double": "s:Sd", "String": "s:SS"]) { first, _ in first }).isEmpty)
    }

    /* The default `ignore_attributes`: an `@IBInspectable` property keeps the annotation Interface Builder reads. */
    @Test func anInspectablePropertyIsNotFound() {
        #expect(Self.findings("@IBInspectable var count: Int = Int(5)\n").isEmpty)
    }

    /* Properties are checked as well as locals, SwiftLint's default `ignore_properties: false`. */
    @Test func aStoredPropertyIsFound() {
        let source = """
            struct Foo {
                var url: URL = URL()
                static let shared: URL = URL()
            }

            """
        #expect(Self.findings(source) == ["2:12", "3:22"])
    }

    /* The reason the rule is typed: the same declaration on both sides, never a subclass, an existential, or an alias over another spelling. */
    @Test func differentDeclarationsAreNotFound() {
        let types = ["Base": Self.base, "Derived": Self.derived, "Shape": "s:7Control5ShapeP", "Circle": "s:7Control6CircleV", "Round": "s:7Control5Rounda"]
        let initializers = ["Base": Self.baseInitializer, "Derived": Self.derivedInitializer, "Circle": "s:7Control6CircleVACycfc", "Round": "s:7Control6CircleVACycfc"]
        let source = """
            let value: Base = Derived()
            let shape: any Shape = Circle()
            let some: some Shape = Circle()
            let circle: Circle = Round()
            let round: Round = Round()
            let same: Base = Base()

            """
        #expect(Self.findings(source, types: types, initializers: initializers) == ["5:10", "6:9"])
    }

    /* An optional, an implicitly unwrapped or an existential annotation is never the shape, nor an implicit `.init`, which needs the annotation. */
    @Test func annotationsThatAreNotTheTypeAreNotFound() {
        let source = """
            let optional: URL? = URL()
            let unwrapped: URL! = URL()
            let spelled: Optional<URL> = URL()
            let implicit: URL = .init()
            let implicitCase: Direction = .up
            let tuple: (URL, URL) = (URL(), URL())

            """
        #expect(Self.findings(source).isEmpty)
    }

    /* An initializer that may give nil binds to a non-optional annotation only as `init!`, so it is found only unwrapped or bound. */
    @Test func anInitializerThatMayGiveNilIsNotFoundUnlessUnwrapped() {
        let source = """
            let lenient: Lenient = Lenient(value: 1)
            let forced: Lenient = Lenient(value: 1)!
            if let bound: Lenient = Lenient(value: 1) { return }
            let takes: Takes = Takes(name: nil)

            """
        let types = ["Lenient": "s:7Control7LenientV", "Takes": "s:7Control5TakesV"]
        let initializers = ["Lenient": "s:7Control7LenientV5valueACSgSi_tcfc", "Takes": "s:7Control5TakesV4nameACSSSg_tcfc"]
        #expect(Self.findings(source, types: types, initializers: initializers) == ["2:11", "3:13"])
    }

    /* An Objective-C initializer's symbol does not say whether it can return nil: found only unwrapped or bound. */
    @Test func anObjectiveCInitializerIsFoundOnlyUnwrappedOrBound() {
        let source = """
            let view: NSView = NSView()
            let image: NSImage = NSImage(named: "icon")!
            guard let named: NSImage = NSImage(named: "icon") else { return }

            """
        let types = ["NSView": "c:objc(cs)NSView", "NSImage": "c:objc(cs)NSImage"]
        let initializers = ["NSView": "c:objc(cs)NSResponder(im)init", "NSImage": "c:objc(cs)NSImage(im)initWithName:"]
        #expect(Self.findings(source, types: types, initializers: initializers) == ["2:10", "3:16"])
    }

    /* A coerced literal records no initializer; any other call that records none is not judged. */
    @Test func aCoercedLiteralIsFoundAndAnUnrecordedCallIsNot() {
        let source = """
            let count: Int = Int(5)
            let text: String = String("text")
            let converted: Int = Int(number)
            let labeled: Int = Int(exactly: 5)
            let explicit: Int = Int.init(5)

            """
        #expect(Self.findings(source, types: Self.incumbentTypes.merging(["String": "s:SS"]) { first, _ in first }) == ["1:10", "2:9"])
    }

    /* `try` and `await` change no type, and `try?` in an optional binding is unwrapped by it; `try?` anywhere else is not the shape. */
    @Test func tryAndAwaitAreLookedThrough() {
        let source = """
            let thrown: URL = try URL()
            let awaited: URL = await URL()
            let both: URL = try await URL()
            if let optional: URL = try? URL() { return }
            let optional: URL = try? URL()

            """
        #expect(Self.findings(source) == ["1:11", "2:12", "3:9", "4:16"])
    }

    /* A case of the annotation's own enum, bare or with a payload, qualified, and of a generic enum spelling the same arguments. */
    @Test func enumCasesAreFound() {
        let source = """
            let up: Direction = Direction.up
            let moved: Direction = Direction.moved(by: 1)
            let qualified: Control.Direction = Control.Direction.up
            let wrapped: Wrapper<Int> = Wrapper<Int>.wrapped(1)
            let inferred: Wrapper<Int> = Wrapper.wrapped(1)

            """
        let types = Self.incumbentTypes.merging(["Wrapper": "s:7Control7WrapperO", "Control": "c:@M@Control"]) { first, _ in first }
        let members = Self.incumbentMembers.merging(["wrapped": "s:7Control7WrapperO7wrappedyACyxGxcAEmlF"]) { first, _ in first }
        #expect(Self.findings(source, types: types, members: members) == ["1:7", "2:10", "3:14", "4:12"])
    }

    /* A member read from the type that is not one of its own cases: another enum's case, a nested type, a static member. */
    @Test func membersThatAreNotTheEnumsCasesAreNotFound() {
        let source = """
            let other: Direction = Direction.left
            let nested: Direction = Direction.Inner
            let shared: Direction = Direction.shared

            """
        let members = Self.incumbentMembers.merging(["left": "s:7Control4SideO4leftyA2CmF", "Inner": "s:7Control9DirectionO5InnerV"]) { first, _ in first }
        #expect(Self.findings(source, members: members).isEmpty)
    }

    /* The sugar matches token for token, a dictionary as well as an array, and a different element type is not the shape. */
    @Test func theSugarIsComparedBySpelling() {
        let source = """
            let words: [String: Int] = [String: Int]()
            let numbers: [Int] = [Int].init()
            let other: [Int] = [Double]()
            let spelled: [Int] = Array<Int>()

            """
        let initializers = Self.incumbentInitializers.merging(["Dictionary": Self.dictionaryInitializer, "init": Self.arrayInitializer]) { first, _ in first }
        #expect(Self.findings(source, initializers: initializers) == ["1:10", "2:12"])
    }

    /* A qualified name on either side resolves to the same type; a nested type is not its parent. */
    @Test func qualifiedAndNestedNamesAreComparedByDeclaration() {
        let source = """
            let qualified: Foundation.URL = URL()
            let bare: URL = Foundation.URL()
            let nested: A.B = A.B.init()
            let parent: A = A.B()

            """
        #expect(Self.findings(source) == ["1:14", "2:9", "3:11"])
    }

    /* A name the compiler resolved to nothing is not judged. */
    @Test func unresolvedNamesAreNotFound() {
        #expect(Self.findings("let url: URL = URL()\n", types: [:]).isEmpty)
        #expect(Self.findings("let url: URL = URL()\n", initializers: [:]).isEmpty)
    }

    @Test func aFileWithNoAnnotationDoesNotApply() {
        let source = "let url = URL()\n"
        let file = ParsedFile(url: URL(fileURLWithPath: "/Plain.swift"), targetName: "Control", targetKind: "regular", source: source, tree: Parser.parse(source: source), nodeCount: 0)
        #expect(!RedundantTypeAnnotation().applies(to: file))
    }

    /*
     End to end on a real package, symbols from the index the build wrote: Foundation's `URL`, a struct of ours, an
     enum case and a coerced literal are found, and a subclass, an `init!`, an optional annotation and an implicit
     `.init` are not.
     */
    static let packageSource = """
        import Foundation

        class Base {
            init() {}
        }

        final class Derived: Base {
            override init() {}
        }

        struct Lenient {
            init!(value: Int) {
                if value < 0 { return nil }
            }
        }

        enum Direction {
            case up
            case down
        }

        func checks(path: String) -> [Any] {
            let url: URL = URL(fileURLWithPath: path)
            let base: Base = Derived()
            let same: Base = Base()
            let lenient: Lenient = Lenient(value: 1)
            let direction: Direction = Direction.up
            let optional: URL? = URL(string: path)
            let implicit: URL = .init(fileURLWithPath: path)
            let count: Int = Int(5)
            return [url, base, same, lenient, direction, optional as Any, implicit, count]
        }

        """

    @Test func aRepeatedTypeIsFlaggedAndASubclassIsNot() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-typed-\(UUID().uuidString)", isDirectory: true)
        let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try PipelineControlTests.manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        try PipelineControlTests.configuration.write(to: root.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
        try Self.packageSource.write(to: sources.appendingPathComponent("Control.swift"), atomically: true, encoding: .utf8)

        /* One run builds the package and writes its index. The rule is not registered here, so it is run by hand on what the run left. */
        let options = try CommandOptions.parse(["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix"], workingDirectory: root)
        _ = try await Pipeline(options: options, writer: ContractWriter { _ in }, workingDirectory: root).run()
        let package = try PackageModel.load(root: root, scratchPath: Pipeline.scratchPath(for: root), runner: ProcessRunner())
        let parsed = await SourceParser().parse(try FileSet.build(package: package).owned)
        let candidates = parsed.files.filter { RedundantTypeAnnotation().applies(to: $0) }
        let symbols = SymbolProvider(scratchPaths: Pipeline.symbolScratchPaths(package: package, root: root), runner: ProcessRunner()).symbols(for: candidates)
        #expect(symbols.fromIndex == 1, "the build's index should describe the file: \(symbols.unavailable)")
        let found = candidates.flatMap { file in
            RedundantTypeAnnotation().findings(in: file, symbols: symbols.symbols[file.url.path] ?? FileSymbols([])).map { "\($0.line)" }
        }
        #expect(found == ["23", "25", "27", "30"], "expected the URL, the struct of ours, the enum case and the coerced literal, and not the subclass, the init!, the optional or the implicit init: \(found)")
    }
}
