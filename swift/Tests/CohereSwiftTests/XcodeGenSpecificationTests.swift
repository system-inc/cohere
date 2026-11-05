import Foundation
import Testing

@testable import CohereSwift

/*
 Where a toolchain fix writes a build setting into an xcodegen spec, for each shape xcodegen reads a target's
 settings in (ProjectSpec.md, `Settings`), and the shapes it refuses rather than guess at. Every line it does not
 write is the line that was read, comments and order included.
 */
struct XcodeGenSpecificationTests {
    static let strict = [XcodeGenSpecification.Setting(key: "SWIFT_STRICT_MEMORY_SAFETY", value: "YES")]

    static func spec(target body: String) -> String {
        """
        # The spec's own comment stays.
        name: Fixture
        settings:
          base:
            SWIFT_VERSION: "5.0"
        targets:
          App:
            type: application
        \(body)
          Other:
            type: framework

        """
    }

    static func added(_ settings: [XcodeGenSpecification.Setting], to text: String, target: String = "App") -> String? {
        try? XcodeGenSpecification(text: text).adding(settings, to: target).get()
    }

    static func refusal(_ text: String, target: String = "App") -> XcodeGenSpecification.Refusal? {
        guard case let .failure(refusal) = XcodeGenSpecification(text: text).adding(Self.strict, to: target) else {
            return nil
        }
        return refusal
    }

    /* The project's own settings are not the target's, so a target with none gets `settings:` and `base:` under it. */
    @Test func aTargetWithNoSettingsGetsABaseMap() {
        let text = Self.spec(target: "    platform: iOS")
        #expect(
            Self.added(Self.strict, to: text)
                == text.replacingOccurrences(
                    of: "  App:\n",
                    with: "  App:\n    settings:\n      base:\n        SWIFT_STRICT_MEMORY_SAFETY: YES\n",
                )
        )
    }

    @Test func aStructuredMapGetsTheKeyUnderItsBase() {
        let text = Self.spec(
            target:
                "    settings:\n      base:\n        PRODUCT_NAME: Capture\n      configs:\n        Debug:\n          ONLY: YES"
        )
        #expect(
            Self.added(Self.strict, to: text)
                == text.replacingOccurrences(
                    of: "      base:\n",
                    with: "      base:\n        SWIFT_STRICT_MEMORY_SAFETY: YES\n",
                )
        )
    }

    /* A map with only `configs` is still the structured form, so `base:` is made for the key rather than the key put beside `configs`. */
    @Test func aConfigsOnlyMapGetsABase() {
        let text = Self.spec(target: "    settings:\n      configs:\n        Debug:\n          ONLY: YES")
        #expect(
            Self.added(Self.strict, to: text)
                == text.replacingOccurrences(
                    of: "    settings:\n",
                    with: "    settings:\n      base:\n        SWIFT_STRICT_MEMORY_SAFETY: YES\n",
                )
        )
    }

    /* A map with none of `base`, `configs` or `groups` is itself the base, so the key goes straight in; a `base:` there would be read as a setting. */
    @Test func aPlainMapIsItsOwnBase() {
        let text = Self.spec(target: "    settings:\n      PRODUCT_NAME: Capture")
        #expect(
            Self.added(Self.strict, to: text)
                == text.replacingOccurrences(
                    of: "    settings:\n",
                    with: "    settings:\n      SWIFT_STRICT_MEMORY_SAFETY: YES\n",
                )
        )
    }

    /* YAML refuses a key written twice, so a key already there has its value replaced, its comment kept. */
    @Test func aKeyAlreadyThereHasItsValueReplaced() {
        let text = Self.spec(
            target:
                "    settings:\n      base:\n        SWIFT_VERSION: \"5.0\" # the old mode\n        PRODUCT_NAME: Capture"
        )
        let version = [XcodeGenSpecification.Setting(key: "SWIFT_VERSION", value: "\"6.0\"")]
        #expect(
            Self.added(version, to: text)
                == text.replacingOccurrences(
                    of: "        SWIFT_VERSION: \"5.0\" # the old mode",
                    with: "        SWIFT_VERSION: \"6.0\" # the old mode",
                )
        )
        #expect(
            text.contains("  base:\n    SWIFT_VERSION: \"5.0\"\n"),
            "the project's own setting is not the target's",
        )
    }

    /* The indentation step is the target's own, here four spaces. */
    @Test func theIndentationIsTheSpecsOwn() {
        let text = "targets:\n    App:\n        type: application\n"
        #expect(
            Self.added(Self.strict, to: text)
                == "targets:\n    App:\n        settings:\n            base:\n                SWIFT_STRICT_MEMORY_SAFETY: YES\n        type: application\n"
        )
    }

    @Test(arguments: [
        ("    settings:\n      groups: [Shared]\n      base:\n        A: B", "a preset group"),
        ("    settings: *shared", "an alias"),
        ("    settings: &shared\n      A: B", "an anchor"),
        ("    settings: { A: B }", "a flow map"),
        ("    settings:\n      <<: *shared", "a merge key"),
        ("    settings:\n      base: *shared", "an aliased base"),
        ("    settings:\n      base:\n        <<: *shared", "a merge key in base"),
        (
            "    settings:\n      base:\n        SWIFT_STRICT_MEMORY_SAFETY:\n          - YES",
            "a key that is not a plain value",
        ),
    ])
    func aSettingsMapThatIsNotPlainIsRefused(body: String, shape: String) {
        #expect(Self.refusal(Self.spec(target: body)) == .notPlain, "\(shape)")
    }

    @Test func aTargetThatIsNotAPlainMapIsRefused() {
        let text = "targets:\n  App: *template\n"
        #expect(Self.refusal(text) == .notPlain)
    }

    /* A target from an included file is not under this file's `targets:`. */
    @Test func aTargetNotInTheFileIsRefused() {
        #expect(Self.refusal(Self.spec(target: "    platform: iOS"), target: "Included") == .targetNotFound)
        #expect(Self.refusal("include:\n  - shared.yml\n", target: "App") == .targetNotFound)
    }

    @Test func tabsAndCarriageReturnsAreRefused() {
        #expect(Self.refusal("targets:\n\tApp:\n\t\ttype: application\n") == .unreadable)
        #expect(Self.refusal("targets:\r\n  App:\r\n    type: application\r\n") == .unreadable)
    }

    @Test func eachTargetsLineIsFound() {
        let text = Self.spec(target: "    platform: iOS")
        #expect(XcodeGenSpecification(text: text).targetLines == ["App": 7, "Other": 10])
        #expect(XcodeGenSpecification(text: "targets:\n  \"My App\":\n    type: tool\n").targetLines == ["My App": 2])
    }

    /* Two targets written one after the other, as the fix does, each into its own block. */
    @Test func twoTargetsAreWrittenOneAfterTheOther() throws {
        let text = Self.spec(target: "    platform: iOS")
        let first = try #require(Self.added(Self.strict, to: text))
        let both = try #require(Self.added(Self.strict, to: first, target: "Other"))
        #expect(
            both.hasSuffix(
                "  Other:\n    settings:\n      base:\n        SWIFT_STRICT_MEMORY_SAFETY: YES\n    type: framework\n"
            )
        )
        #expect(both.components(separatedBy: "SWIFT_STRICT_MEMORY_SAFETY").count == 3)
    }

    /* The suggestion's edit, applied to the spec as read, is exactly the text `adding` writes, and touches only the line it adds. */
    @Test(arguments: [
        "    platform: iOS",
        "    settings:\n      base:\n        SWIFT_VERSION: \"5.0\" # the old mode\n        PRODUCT_NAME: Capture",
        "    settings:\n      PRODUCT_NAME: Capture",
        "    name: \"Café\"",
    ])
    func theSuggestedEditIsTheWrittenText(body: String) throws {
        let text = Self.spec(target: body)
        let settings = Self.strict + [XcodeGenSpecification.Setting(key: "SWIFT_VERSION", value: "\"6.0\"")]
        let written = try #require(Self.added(settings, to: text))
        let edit = try XcodeGenSpecification(text: text).suggestedEdit(settings, to: "App").get()
        let applied = FixApplier.apply([FixApplier.Proposal(rule: "suggestion", edit: edit)], to: text)
        #expect(applied.applied == 1)
        #expect(applied.text == written)
        #expect(edit.end - edit.start < 60, "the edit replaces the lines it changes, not the file: \(edit)")
    }

    /*
     A key one of the target's own configurations sets to another value is resolved after base, so a key written in
     base would change nothing there. One that sets the same value already agrees, and base is where the rest goes.
     */
    @Test func aKeyAConfigurationSetsOtherwiseIsRefused() {
        let configured =
            "    settings:\n      base:\n        A: B\n      configs:\n        Release:\n          SWIFT_STRICT_MEMORY_SAFETY: "
        #expect(Self.refusal(Self.spec(target: configured + "NO")) == .overriddenByConfiguration)
        #expect(Self.refusal(Self.spec(target: configured + "YES")) == nil)
    }

    /* Base already holds the value, so something resolved after it turns it off, and there is nothing to write. */
    @Test func aValueBaseAlreadyHoldsIsNoEdit() {
        let text = Self.spec(target: "    settings:\n      base:\n        SWIFT_STRICT_MEMORY_SAFETY: YES")
        guard case let .failure(refusal) = XcodeGenSpecification(text: text).suggestedEdit(Self.strict, to: "App")
        else {
            Issue.record("an edit that changes nothing was offered")
            return
        }
        #expect(refusal == .alreadyWritten)
    }
}
