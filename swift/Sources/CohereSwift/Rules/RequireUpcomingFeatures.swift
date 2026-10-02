import SwiftSyntax
import Foundation

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

 Strict memory safety (SE-0458) is not required here: it lights up unsafe-pointer code like ByteRing
 throughout, and whether the house requires it is Kirk's decision, put to him with counts.

 Plugin and macro targets are skipped: their settings serve the build tool, not the code being judged. So
 is a target with no Swift sources (a C or C++ target like Presence's `MotionCorrection`): Swift features
 mean nothing to it.
 */
public struct RequireUpcomingFeatures: PackageRule {
    public let name = "cohere-swift/require-upcoming-features"

    /* The features required, with why each matters, said in the finding. */
    static let required: [(feature: String, reason: String)] = [
        ("ExistentialAny", "a protocol used as a type is spelled `any P`, so the cost of an existential is written where it is paid"),
        ("MemberImportVisibility", "a member is visible only from a module the file imports, so no file compiles on an import it does not have"),
    ]

    public init() {}

    public func findings(in package: PackageModel, manifest: ParsedFile?) -> [FindingRecord] {
        package.targets
            .filter { $0.kind != "plugin" && $0.kind != "macro" && !$0.sources.isEmpty }
            .compactMap { target -> FindingRecord? in
                let missing = Self.required.filter { !target.upcomingFeatures.contains($0.feature) }
                guard !missing.isEmpty else { return nil }
                let settings = missing.map { ".enableUpcomingFeature(\"\($0.feature)\")" }.joined(separator: ", ")
                let reasons = missing.map { "\($0.feature): \($0.reason)" }.joined(separator: "; ")
                let message = "\(target.name) does not enable \(missing.map(\.feature).joined(separator: " and ")). Add \(settings) to its swiftSettings. \(reasons)."
                if let manifest, let declaration = TargetNameFinder.find(target.name, in: manifest.tree) {
                    return manifest.finding(at: declaration, rule: name, messageId: "upcomingFeatureMissing", message: message)
                }
                /* No declaration found (a target named by a computed string): point at the manifest's first line rather than drop the finding. */
                return FindingRecord(
                    source: .rule,
                    file: package.root.appendingPathComponent("Package.swift").path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: name,
                    messageId: "upcomingFeatureMissing",
                    message: message
                )
            }
    }
}
