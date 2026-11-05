import Foundation

/*
 The toolchain rules' fix at an xcodegen root: each target's missing build settings written into project.yml,
 under the target's base settings (`XcodeGenSpecification`), and nowhere else. The project generated from the spec
 is the spec's output and is made again from it; a committed .xcodeproj is never written, so its findings carry no
 fix.

 A Package.swift has no counterpart: the same rules offer no fix there. Here the edit is a plain line of YAML in a
 file a person keeps, and it is offered only where it is provably what was meant. The spec is written, the project
 is generated from it again, and Xcode is asked again for every configuration of every target the fix touched
 (`XcodeBuildSettings`). Only when each rule then finds nothing missing on each of those targets does the edit stay.
 Otherwise project.yml is put back byte for byte, the project is generated from it again, and the findings stand
 with the refusal counted in the fix record, so a fix that would not have taken is never reported as one.
 */
struct XcodeSpecificationFix {
    /* What the fix did, for the fix record, and the project and model the rest of the run reads afterwards. */
    struct Outcome {
        var applied = 0
        var appliedByRule: [String: Int] = [:]
        var refusals: [String: Int] = [:]
        var project: XcodeProject.Loaded
        var package: PackageModel
        /* Whether project.yml now holds the fix. */
        var changed = false
    }

    /* A target's settings to write, and the rules each answers. */
    struct Planned {
        var target: String
        var settings: [XcodeGenSpecification.Setting]
        var rules: [String]
    }

    static let notRegenerated =
        "the fixed project.yml did not regenerate with every setting it wrote on in every configuration, so it was put back as it was"

    /* The base settings already say it, so a later layer (a configuration's settings, an xcconfig) is what turns it off. */
    static let overridden =
        "the target's base settings in project.yml already hold the value, and a configuration's settings or an xcconfig resolved after them overrides it, so the fix had nothing to write"

    let specification: URL
    let rules: [any XcodeSettingRule]
    /* Generates the project from the spec as it now stands and reads its settings into the model, as the run does. */
    let reload: () throws -> (project: XcodeProject.Loaded, package: PackageModel)

    func run(project: XcodeProject.Loaded, package: PackageModel) -> Outcome {
        var outcome = Outcome(project: project, package: package)
        let wanted = package.toolchainTargets.compactMap { target -> Planned? in
            var settings: [XcodeGenSpecification.Setting] = []
            var answered: [String] = []
            for rule in rules {
                let missing = rule.xcodeSettingsMissing(for: target)
                if !missing.isEmpty {
                    settings += missing
                    answered.append(rule.name)
                }
            }
            return settings.isEmpty ? nil : Planned(target: target.name, settings: settings, rules: answered)
        }
        guard !wanted.isEmpty, let original = try? Data(contentsOf: specification),
            let originalText = String(data: original, encoding: .utf8)
        else { return outcome }

        var text = originalText
        var planned: [Planned] = []
        for want in wanted {
            switch XcodeGenSpecification(text: text).adding(want.settings, to: want.target) {
                case let .success(edited) where edited == text:
                    outcome.refusals[Self.overridden, default: 0] += want.rules.count
                case let .success(edited):
                    text = edited
                    planned.append(want)
                case let .failure(refusal):
                    outcome.refusals[refusal.reason, default: 0] += want.rules.count
            }
        }
        guard !planned.isEmpty, text != originalText else { return outcome }

        let fixedCount = planned.map(\.rules.count).reduce(0, +)
        do {
            try Data(text.utf8).write(to: specification, options: .atomic)
            let reloaded = try reload()
            let fixed = planned.allSatisfy { want in
                guard let target = reloaded.package.toolchainTargets.first(where: { $0.name == want.target }) else {
                    return false
                }
                return rules.allSatisfy { $0.xcodeSettingsMissing(for: target).isEmpty }
            }
            if fixed {
                outcome.project = reloaded.project
                outcome.package = reloaded.package
                outcome.applied = fixedCount
                for want in planned {
                    for rule in want.rules {
                        outcome.appliedByRule[rule, default: 0] += 1
                    }
                }
                outcome.changed = true
                return outcome
            }
        }
        catch {
            /* A spec that would not regenerate is put back below, the same as one that regenerated without the settings on. */
        }
        outcome.refusals[Self.notRegenerated, default: 0] += fixedCount
        do {
            try original.write(to: specification, options: .atomic)
            let restored = try reload()
            outcome.project = restored.project
            outcome.package = restored.package
        }
        catch {
            /*
             Reached only when the original bytes could not be written back or the project would not generate from
             them again. The run goes on with the project it loaded before the fix, and its findings stand.
             */
        }
        return outcome
    }
}
