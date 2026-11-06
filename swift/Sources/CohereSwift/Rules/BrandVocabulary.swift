import Foundation

/*
 The brand vocabulary, cohere's `policy/Brands.json`, the same file the TypeScript casing rules embed, so Swift and
 TypeScript read one list of the brands whose owners spell them opening lowercase (#vf4ytzg, corpus case us12):
 `iMessage`, as Apple spells it. The type casing in `always-use-lower-camel-case` reads it, so `iMessageThing` is an
 UpperCamelCase type name, as it is in TypeScript.

 The words are compiled in (`PolicyBrands`, generated from the file), so a released engine carries them with no
 checkout and nothing handed to it at run time. A test fails while the compiled-in copy and the file differ. A copy
 that would not load refuses the run with exit 2 (`Pipeline`), never a rule that quietly reports every brand.
 */
public struct BrandVocabulary: Sendable {
    struct LoadFailure: Error, CustomStringConvertible {
        var description: String
    }

    /* Every brand's spelling, the longest first, so a name that opens with two listed brands is read by the longer, as Go's `LeadingBrand` reads it. */
    private(set) var longestFirst: [String] = []

    /* An empty vocabulary lists no brand, which is right only where nothing is judged: listing rule names, or a run with the rule off. */
    public init() {}

    /* The compiled-in brands. The reason alone, in words: the caller says what failed to load, once, in the sentence a person reads. */
    public static func compiledIn() throws -> BrandVocabulary {
        do {
            return try load(data: Data(PolicyBrands.file.utf8))
        }
        catch let failure as LoadFailure {
            throw failure
        }
        catch {
            throw LoadFailure(description: "it is not valid JSON (\((error as NSError).localizedDescription))")
        }
    }

    /*
     Parsed by hand from JSON objects rather than decoded, because an unknown key must be refused, as Go's
     `DisallowUnknownFields` refuses it. Each check is the Go loader's (`policy/brands.go`), so a file one engine
     refuses the other refuses too: a file that does not state its bar, a brand that does not open with a lowercase
     ASCII letter or holds anything but ASCII letters and digits after it, an entry with no owner, one whose citation is
     not an https page, and a brand listed twice.
     */
    static func load(data: Data) throws -> BrandVocabulary {
        guard let file = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LoadFailure(description: "the file is not a JSON object")
        }
        try refuseUnknownKeys(in: file, allowed: ["about", "brands"], context: "the file")
        guard !(file["about"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw LoadFailure(description: "about is empty, and the file states the bar every entry meets")
        }
        var vocabulary = BrandVocabulary()
        var seen: Set<String> = []
        for (index, entry) in (file["brands"] as? [[String: Any]] ?? []).enumerated() {
            try refuseUnknownKeys(in: entry, allowed: ["brand", "owner", "citation"], context: "brand \(index)")
            let brand = entry["brand"] as? String ?? ""
            if let refusal = refusal(
                brand: brand,
                owner: entry["owner"] as? String ?? "",
                citation: entry["citation"] as? String ?? "",
            ) {
                throw LoadFailure(description: "\"\(brand)\" \(refusal)")
            }
            guard seen.insert(brand).inserted else {
                throw LoadFailure(description: "\"\(brand)\" appears twice")
            }
            vocabulary.longestFirst.append(brand)
        }
        /* Stable, as Go's `sort.SliceStable`, so two brands of one length keep the file's order. */
        vocabulary.longestFirst = vocabulary.longestFirst.enumerated()
            .sorted { ($1.element.utf8.count, $0.offset) < ($0.element.utf8.count, $1.offset) }
            .map(\.element)
        return vocabulary
    }

    /* Why an entry is refused, or nil when it is not: Go's `brandRefusal`, check for check. */
    static func refusal(brand: String, owner: String, citation: String) -> String? {
        let bytes = Array(brand.utf8)
        guard let first = bytes.first, first >= UInt8(ascii: "a"), first <= UInt8(ascii: "z") else {
            return "does not open with a lowercase letter, and the list holds only brands whose owners spell them so"
        }
        let isLetterOrDigit: (UInt8) -> Bool = { byte in
            (byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z"))
                || (byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z"))
                || (byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9"))
        }
        guard bytes.dropFirst().allSatisfy(isLetterOrDigit) else {
            return "holds something other than letters and digits, so no identifier can open with it"
        }
        guard !owner.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return "names no owner, and the bar is the owner's own spelling"
        }
        guard let page = URL(string: citation), page.scheme == "https", let host = page.host, !host.isEmpty else {
            return "cites no https page where its owner spells it, and every entry cites one"
        }
        return nil
    }

    private static func refuseUnknownKeys(in object: [String: Any], allowed: Set<String>, context: String) throws {
        if let unknown = object.keys.sorted().first(where: { !allowed.contains($0) }) {
            throw LoadFailure(description: "\(context) has an unknown field \"\(unknown)\"")
        }
    }

    /* The listed brand a name opens with, spelled exactly as listed, the longest when more than one is. What follows it is the caller's to judge. */
    func leadingBrand(of name: String) -> String? {
        longestFirst.first { name.hasPrefix($0) }
    }
}
