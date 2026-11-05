import Foundation
import SwiftSyntax

/*
 Every target we own enables the compiler checks the house rules ride on, as upcoming features in its
 `swiftSettings`:

 - `ExistentialAny`: a protocol used as a type is spelled `any P`. The Swift form of our TypeScript
   no-explicit-any discipline in reverse: the dynamic dispatch and boxing an existential costs are written
   where they happen, rather than hiding behind a name that reads like a concrete type.
 - `MemberImportVisibility`: a member is visible only from a module this file imports. Without it, an
   extension method from a module imported anywhere in the target leaks into every file, so a file can
   compile only because of an import it does not have.

 Neither is on by default in Swift 6 language mode. Both are read from SwiftPM's own answer
 (`dump-package`), so a target that enables them any way the manifest can spell is seen as enabled.

 Strict memory safety (SE-0458) is a setting rather than an upcoming feature, so its own rule requires it
 (`toolchain-require-strict-memory-safety`).

 Plugin and macro targets are skipped: their settings serve the build tool, not the code being judged. So
 is a target with no Swift sources (a C or C++ target like Presence's `MotionCorrection`): Swift features
 mean nothing to it.

 At an Xcode root a feature is on when every configuration turns it on, by its build setting or by
 `-enable-upcoming-feature` in `OTHER_SWIFT_FLAGS` (`XcodeBuildSettings`), and the advice is the build setting.
 */
public struct ToolchainRequireUpcomingFeatures: XcodeSettingRule {
    public let name = "cohere-swift/toolchain-require-upcoming-features"
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    /* The features required, with why each matters, said in the finding. */
    static let required: [(feature: String, reason: String)] = [
        (
            "ExistentialAny",
            "a protocol used as a type is spelled `any P`, so the cost of an existential is written where it is paid",
        ),
        (
            "MemberImportVisibility",
            "a member is visible only from a module the file imports, so no file compiles on an import it does not have",
        ),
    ]

    public init() {}

    /* Whether the target's settings are this rule's to judge; the reasons are in the comment above. */
    static func judges(_ target: PackageModel.Target) -> Bool {
        target.kind != "plugin" && target.kind != "macro" && !target.sources.isEmpty
    }

    func xcodeSettingsMissing(for target: PackageModel.Target) -> [XcodeGenSpecification.Setting] {
        guard Self.judges(target) else { return [] }
        return Self.required.filter { !target.upcomingFeatures.contains($0.feature) }.compactMap { required in
            XcodeBuildSettings.featureSettings.first { $0.value == required.feature }.map {
                XcodeGenSpecification.Setting(key: $0.key, value: "YES")
            }
        }
    }

    public func findings(in package: PackageModel, manifest: ParsedFile?) -> [FindingRecord] {
        if let settings = package.xcodeBuildSettings {
            return package.toolchainTargets.filter { !xcodeSettingsMissing(for: $0).isEmpty }.map { target in
                let missing = Self.required.filter { !target.upcomingFeatures.contains($0.feature) }
                /* Each feature named with the configurations it is missing from, once any of them is missing from only some. */
                let partial = missing.map { required in
                    settings.configurations(
                        of: target.name,
                        where: { !$0.upcomingFeatures.contains(required.feature) },
                    )
                }
                let features = zip(missing, partial).map { required, configurations in
                    guard partial.contains(where: { $0 != nil }) else { return required.feature }
                    return
                        "\(required.feature) in \(configurations.map(XcodeBuildSettings.listed) ?? "every configuration")"
                }
                let keys = xcodeSettingsMissing(for: target).map(\.key)
                return settings.finding(rule: name, target: target.name) { lineFound in
                    RuleMessages.ToolchainRequireUpcomingFeatures.upcomingFeatureMissingInXcode(
                        target: target.name,
                        features: features.joined(separator: " and "),
                        settings: keys.joined(separator: " and "),
                        reasons: missing.map { "\($0.feature): \($0.reason)" }.joined(separator: "; "),
                        located: lineFound ? .found : .notFound(definition: settings.definitionName),
                    )
                }
            }
        }
        return package.targets
            .filter(Self.judges)
            .compactMap { target -> FindingRecord? in
                let missing = Self.required.filter { !target.upcomingFeatures.contains($0.feature) }
                guard !missing.isEmpty else { return nil }
                let settings = missing.map { ".enableUpcomingFeature(\"\($0.feature)\")" }.joined(separator: ", ")
                let reasons = missing.map { "\($0.feature): \($0.reason)" }.joined(separator: "; ")
                let message = RuleMessages.ToolchainRequireUpcomingFeatures.upcomingFeatureMissing(
                    target: target.name,
                    features: missing.map(\.feature).joined(separator: " and "),
                    settings: settings,
                    reasons: reasons,
                )
                if let manifest, let declaration = TargetNameFinder.find(target.name, in: manifest.tree) {
                    return manifest.finding(
                        at: declaration,
                        rule: name,
                        message: message,
                    )
                }
                /* No declaration found (a target named by a computed string): point at the manifest's first line rather than drop the finding. */
                return FindingRecord(
                    source: .rule,
                    file: package.root.appendingPathComponent("Package.swift").path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: name,
                    messageId: message.id,
                    message: message.text,
                )
            }
    }
}
