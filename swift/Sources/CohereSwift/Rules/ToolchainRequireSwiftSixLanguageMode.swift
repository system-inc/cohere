import Foundation
import SwiftSyntax

/*
 Every root target compiles in Swift 6 language mode.

 Kirk's ruling: Swift 6 is required, with each project migrated as it comes under `c`. Swift 6 mode is
 what makes strict concurrency checking an error rather than a suggestion, so a target in Swift 5 mode is
 a target where the compiler, the first gate, is quietly running weaker.

 The mode is SwiftPM's resolved answer (the target's own setting, else the package's list, else the
 tools version's default), so a target that says nothing in a 6.x manifest is correctly in Swift 6.

 At an Xcode root the mode is the `SWIFT_VERSION` Xcode resolves for the target, the oldest of its configurations
 (`XcodeBuildSettings`), and the advice is that build setting, with the edit to project.yml that sets it offered as
 a suggestion and never applied: turning it on is migration work, and a fix would leave a build that may not
 compile.
 */
public struct ToolchainRequireSwiftSixLanguageMode: PackageRule {
    public let name = "cohere-swift/toolchain-require-swift-6-language-mode"
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    public init() {}

    /* The build settings `target` lacks, as project.yml spells them; empty when it passes or is not this rule's to judge. */
    func xcodeSettingsMissing(for target: PackageModel.Target) -> [XcodeGenSpecification.Setting] {
        PackageModel.isOlderLanguageMode(target.languageMode, "6")
            ? [XcodeGenSpecification.Setting(key: "SWIFT_VERSION", value: "\"6.0\"")] : []
    }

    public func findings(in package: PackageModel, manifest: ParsedFile?) -> [FindingRecord] {
        if let settings = package.xcodeBuildSettings {
            return package.toolchainTargets.filter { !xcodeSettingsMissing(for: $0).isEmpty }.map { target in
                let below = settings.configurations(
                    of: target.name,
                    where: { PackageModel.isOlderLanguageMode($0.languageMode, "6") },
                )
                return settings.finding(rule: name, target: target.name, missing: xcodeSettingsMissing(for: target)) {
                    lineFound in
                    RuleMessages.ToolchainRequireSwift6LanguageMode.languageModeBelowSixInXcode(
                        target: target.name,
                        languageMode: target.languageMode,
                        configurations: below.map { .some(configurations: XcodeBuildSettings.listed($0)) } ?? .every,
                        located: lineFound ? .found : .notFound(definition: settings.definitionName),
                    )
                }
            }
        }
        return package.targets
            .filter { PackageModel.isOlderLanguageMode($0.languageMode, "6") }
            .map { target in
                let message = RuleMessages.ToolchainRequireSwift6LanguageMode.languageModeBelowSix(
                    target: target.name,
                    languageMode: target.languageMode,
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
