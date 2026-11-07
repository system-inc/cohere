import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 Which symbols are the standard library's, on symbols the index recorded on our proving grounds and in the
 sorted-first-last rule's tests. The trap is the extension: a member another module adds to a standard library
 protocol starts with the same `s:S` as the standard library's own.
 */
struct FileSymbolsTests {
    static func isStandardLibrary(_ symbol: String) -> Bool {
        FileSymbols.Occurrence(line: 1, column: 1, symbol: symbol, name: "", isReference: true).isStandardLibrary
    }

    @Test func theStandardLibrarysOwnMembersAre() {
        #expect(Self.isStandardLibrary("s:Sa5countSivp"))
        #expect(Self.isStandardLibrary("s:SD5countSivp"))
        #expect(Self.isStandardLibrary("s:Sh8containsySbxF"))
        #expect(Self.isStandardLibrary("s:STsE5first5where7ElementQzSgSbADKXE_tKF"))
        #expect(Self.isStandardLibrary("s:SlsE7isEmptySbvp"))
        #expect(Self.isStandardLibrary("s:s14_ArrayProtocolPsE6filterySay7ElementQzGSbAEqd__YKXEqd__YKs5ErrorRd__lF"))
    }

    /* Concurrency's three-character substitutions, and its module's extensions, are the standard library's. */
    @Test func concurrencyIsTheStandardLibrarys() {
        #expect(Self.isStandardLibrary("s:ScT"))
        #expect(Self.isStandardLibrary("s:Sci12_ConcurrencyE6filteryScFyxGSbxYacF"))
        #expect(!Self.isStandardLibrary("s:Sci7ControlE6filteryScFyxGSbxYacF"))
    }

    @Test func anotherModulesExtensionIsNot() {
        #expect(!Self.isStandardLibrary("s:ST7ControlE6sorted2bySay7ElementQzGs7KeyPathCyADqd__G_tSLRd__lF"))
        #expect(
            !Self.isStandardLibrary(
                "s:ST10FoundationE6sorted5usingSay7ElementQzGqd___tAA14SortComparatorRd__AEQyd__AERSlF"
            )
        )
    }

    /*
     A member of a constrained extension of ours on a standard library type is ours (#awb2ykk). Its symbol puts the
     extension's generic signature between our module and the `E` that marks the extension (`SiRsz` is
     `Element == Int`), which the demangler reads and a reading of the mangling did not. The standard library's own
     members of the same types still are its own. Red under the reading that expected the `E` right after the module.
     */
    @Test func aConstrainedExtensionOfOursIsNot() {
        #expect(!Self.isStandardLibrary("s:Sa7ControlSiRszlE5countSivp"), "Array where Element == Int")
        #expect(!Self.isStandardLibrary("s:SD7ControlSSRszrlE5countSivp"), "Dictionary where Key == String")
        #expect(!Self.isStandardLibrary("s:Sh7ControlSiRszlE5countSivp"), "Set where Element == Int")
        #expect(Self.isStandardLibrary("s:Sa5countSivp"))
        #expect(Self.isStandardLibrary("s:SD5countSivp"))
        #expect(Self.isStandardLibrary("s:Sh5countSivp"))
    }

    @Test func ourOwnDeclarationsAreNot() {
        #expect(!Self.isStandardLibrary("s:7Control5TallyV5countSivp"))
        #expect(!Self.isStandardLibrary("c:objc(cs)NSArray(py)count"))
        /* Found by the is-disjoint rule on presence: CoreGraphics' `CGRect.intersection`, a type imported from C. */
        #expect(!Self.isStandardLibrary("s:So6CGRectV12CoreGraphicsE12intersectionyA2BF"))
    }

    @Test func aDeclarationOfOursKnowsItsModule() {
        let element = FileSymbols.Occurrence(
            line: 1,
            column: 1,
            symbol: "s:7Control6StatusO7runningyA2CmF",
            name: "running",
            isReference: true,
        )
        #expect(element.declaringModule == "Control")
        #expect(FileSymbols([element], ownedModules: ["Control"]).isOwned(element))
        #expect(!FileSymbols([element], ownedModules: ["Other"]).isOwned(element))
        let standard = FileSymbols.Occurrence(
            line: 1,
            column: 1,
            symbol: "s:Sa5countSivp",
            name: "count",
            isReference: true,
        )
        #expect(standard.declaringModule == nil)
    }
    /*
     A symbol that names a standard library type and that the demangler places in no module (a hand-made one here) is
     of unknown origin. empty-count, which asks of it, says nothing of the comparison it decides, and the file is
     reported unchecked by that rule, naming the symbol: never clean and never flagged. The comparison beside it on
     the standard library's own `count` is still flagged. Red under a reading that takes an unplaced symbol for one
     not the standard library's without saying so.
     */
    @Test func aSymbolNobodyCanPlaceIsUncheckedNeverClean() async throws {
        let source = "func checks(items: [Int], tally: [Int]) -> Bool {\n    items.count == 0 || tally.count == 0\n}\n"
        let url = URL(fileURLWithPath: "/package/Sources/A/Checks.swift")
        let file = ParsedFile(
            url: url,
            targetName: "A",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 1,
        )
        let unplaceable = "s:Sa_unplaceable"
        #expect(
            FileSymbols.Occurrence(line: 2, column: 11, symbol: unplaceable, name: "count", isReference: true).origin
                == .unplaced,
            "the control: the demangler places this symbol nowhere",
        )
        var linter = Linter(configuration: RuleConfiguration(severities: [:], note: ""), fileRules: [])
        linter.typedRules = [EmptyCount()]
        linter.symbols = SymbolProvider.Result(
            symbols: [
                url.path: FileSymbols([
                    FileSymbols.Occurrence(line: 2, column: 11, symbol: unplaceable, name: "count", isReference: true),
                    FileSymbols.Occurrence(
                        line: 2,
                        column: 31,
                        symbol: "s:Sa5countSivp",
                        name: "count",
                        isReference: true,
                    ),
                ])
            ],
            unavailable: [:],
            fromIndex: 1,
            fromSourcekitd: 0,
        )
        let package = try PackageModel(
            root: PackageModelTests.root,
            describeJson: PackageModelTests.describe(targets: ["A"]),
            dumpPackageJson: PackageModelTests.dump(tools: "6.4.0"),
        )
        let result = await linter.run(package: package, manifests: [:], files: [file])
        let flagged = result.findings.filter { $0.rule == EmptyCount().name }.map { "\($0.line):\($0.column)" }
        #expect(flagged == ["2:25"], "only the standard library's count is flagged: \(flagged)")
        let crash = try #require(result.record.crashes.first { $0.file == url.path }, "the file was reported clean")
        #expect(crash.error.contains(EmptyCount().name) && crash.error.contains(unplaceable), "\(crash.error)")
        #expect(result.verdicts[url.path]?.typedUnplaced == [EmptyCount().name: [unplaceable]])
    }
}
