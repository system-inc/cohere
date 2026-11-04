import Testing

@testable import CohereSwift

/* The toolchain's demangler, called through the one unsafe call in `SwiftDemangler.demangle`. */
struct SwiftDemanglerTests {
    /* The example in SwiftDemangler's own header: a mangled name in, the declaration it spells out. */
    @Test func aMangledNameReadsAsItsDeclaration() throws {
        let demangler = try SwiftDemangler.shared()
        #expect(
            demangler.demangle("$sSTsE7forEachyyy7ElementQzKXEKF")
                == "(extension in Swift):Swift.Sequence.forEach((A.Element) throws -> ()) throws -> ()"
        )
        #expect(
            demangler.declaration(ofSymbol: "s:STsE7forEachyyy7ElementQzKXEKF")
                == demangler.demangle("$sSTsE7forEachyyy7ElementQzKXEKF")
        )
        #expect(demangler.declaration(ofSymbol: "c:objc(cs)NSObject") == nil)
    }
}
