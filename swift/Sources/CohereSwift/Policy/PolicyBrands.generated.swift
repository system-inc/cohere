/*
 Generated from cohere's policy/Brands.json by `go run ./policy/tools/generate`. Do not edit it here:
 edit the JSON and run that again. While the two differ, a test in each engine fails.
 */
enum PolicyBrands {
    /* policy/Brands.json, byte for byte. */
    static let file = #"""
        {
            "about": "The brands whose owners spell them opening lowercase, read by the casing rules. A name a casing rule judges as UpperCamelCase, a type's above all, that opens with a listed brand spelled exactly as listed, followed by nothing or by an UpperCamelCase rest, counts as UpperCamelCase: `iMessageCommandLineInterface` passes, and `iMessagecommandLineInterface` does not, since its rest is not UpperCamelCase. A brand inside a name, `SendIMessage`, is spelled by the ordinary rule. This is vocabulary, like the shouting and abbreviation lists, and not a carve-out. The bar for entry is binding: a brand joins the list only when its owner's own spelling opens lowercase, never for a project's taste, and each entry names its owner and cites a page where the owner spells it so. The list grows by reviewed additions. Read by nexus/consistency-require-identifier-casing through go:embed, and compiled into the Swift engine as PolicyBrands.",
            "brands": [
                {
                    "brand": "iMessage",
                    "owner": "Apple",
                    "citation": "https://www.apple.com/legal/intellectual-property/trademark/appletmlist.html"
                }
            ]
        }

        """#
}
