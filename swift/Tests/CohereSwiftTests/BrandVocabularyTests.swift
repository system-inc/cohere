import Foundation
import Testing

@testable import CohereSwift

/*
 The brand vocabulary's loader, held to the Go loader's own vectors (`policy/brands_test.go`), so a file one engine
 refuses the other refuses too, and both read the same brand at the front of the same name.
 */
struct BrandVocabularyTests {
    static let minimal =
        #"{"about": "a", "brands": ["#
        + #"{"brand": "iMessage", "owner": "Apple", "citation": "https://example.com/imessage"}, "#
        + #"{"brand": "iMessageKit", "owner": "Apple", "citation": "https://example.com/imessagekit"}]}"#

    @Test(
        arguments: [
            ("iMessageThing", "iMessage"),
            ("iMessage", "iMessage"),
            ("iMessageKitThing", "iMessageKit"),
            ("IMessageThing", nil),
            ("imessageThing", nil),
            ("SendiMessage", nil),
        ] as [(String, String?)]
    )
    func theLongestLeadingBrandIsRead(name: String, brand: String?) throws {
        let vocabulary = try BrandVocabulary.load(data: Data(Self.minimal.utf8))
        #expect(vocabulary.leadingBrand(of: name) == brand)
    }

    @Test(arguments: [
        ("an unknown top-level key", #""about": "a""#, #""about": "a", "aliases": []"#),
        (
            "an unknown key on an entry", #""owner": "Apple", "citation": "https://example.com/imessage""#,
            #""owner": "Apple", "note": "n", "citation": "https://example.com/imessage""#,
        ),
        ("an empty about", #""about": "a""#, #""about": " ""#),
        ("a duplicate", #""brand": "iMessageKit""#, #""brand": "iMessage""#),
        ("a brand opening with a capital", #""brand": "iMessage""#, #""brand": "IMessage""#),
        ("a brand opening with a digit", #""brand": "iMessage""#, #""brand": "1Message""#),
        ("an empty brand", #""brand": "iMessage""#, #""brand": """#),
        ("a brand with a space", #""brand": "iMessage""#, #""brand": "i Message""#),
        ("a brand with a hyphen", #""brand": "iMessage""#, #""brand": "i-Message""#),
        (
            "an entry with no owner", #""owner": "Apple", "citation": "https://example.com/imessage""#,
            #""citation": "https://example.com/imessage""#,
        ),
        ("an entry with no citation", #", "citation": "https://example.com/imessage""#, ""),
        ("an empty citation", #""https://example.com/imessage""#, #""""#),
        ("a citation that is not an address", #""https://example.com/imessage""#, #""Apple's trademark list""#),
        ("a citation over plain http", #""https://example.com/imessage""#, #""http://example.com/imessage""#),
    ])
    func whatWouldReadAsSomethingElseIsRefused(name: String, original: String, replacement: String) throws {
        /* The minimal file loads, so each refusal below is the change's doing. */
        _ = try BrandVocabulary.load(data: Data(Self.minimal.utf8))
        let range = try #require(Self.minimal.range(of: original), "\(name): the change does not apply")
        let changed = Self.minimal.replacingCharacters(in: range, with: replacement)
        #expect(throws: BrandVocabulary.LoadFailure.self, "\(name) is accepted: \(changed)") {
            try BrandVocabulary.load(data: Data(changed.utf8))
        }
    }

    @Test func theCompiledInBrandsHoldIMessage() throws {
        #expect(try BrandVocabulary.compiledIn().leadingBrand(of: "iMessageCommandLineInterface") == "iMessage")
    }
}
