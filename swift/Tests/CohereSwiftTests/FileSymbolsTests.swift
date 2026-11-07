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
}
