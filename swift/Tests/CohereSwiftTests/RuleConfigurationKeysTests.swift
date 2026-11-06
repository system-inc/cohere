import Foundation
import Testing

@testable import CohereSwift

/*
 What the swift block may hold, refused by name when it holds anything else (#naazhm0, from
 @system_cohere_tools' read of the parser): a key other than `rules`, or a rule name cohere-swift does not
 have, once loaded clean and configured nothing.
 */
struct RuleConfigurationKeysTests {
    static func refusal(_ document: String) -> String? {
        do {
            _ = try RuleConfiguration.parse(Data(document.utf8), path: "/project/CohereSettings.json")
            return nil
        }
        catch let failure as RuleConfiguration.ReadFailure {
            return failure.reason
        }
        catch {
            return "refused, but not as a settings failure: \(error)"
        }
    }

    @Test func aKeyBesideRulesIsRefusedByName() {
        #expect(
            Self.refusal(#"{"swift":{"rules":{},"severity":"warning"}}"#)
                == #"the swift block's "severity" is not read by cohere-swift, whose block holds only rules; remove it"#
        )
        #expect(Self.refusal(#"{"swift":{"rule":{"cohere-swift/force-cast":"off"}}}"#)?.contains(#""rule""#) == true)
        #expect(Self.refusal(#"{"swift":["rules"]}"#) == "the swift block is not an object")
    }

    @Test func aRuleNameCohereSwiftDoesNotHaveIsRefusedByName() {
        #expect(
            Self.refusal(#"{"swift":{"rules":{"cohere-swift/force-casts":"off"}}}"#)
                == "cohere-swift/force-casts is not a rule cohere-swift has (cohere --rules lists them); correct or remove it"
        )
        #expect(Self.refusal(#"{"swift":{"rules":{"cohere-swift/force-cast":"off"}}}"#) == nil)
    }
}
