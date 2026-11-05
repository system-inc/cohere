import Foundation

/*
 What each native target of an Xcode project compiles with, as far as the toolchain rules judge it: its language
 mode, the upcoming features it enables and whether strict memory safety is on. The counterpart of what
 `dump-package` tells the same rules about a package's targets.

 Read from Xcode's own answer, never from the layers it is resolved through. A target's settings come from the
 project, the target, the xcconfig files each names and Xcode's defaults, separately for every configuration, and
 restating that resolution here would be a second answer that drifts from the first. So each target that owns a
 file of ours is asked `xcodebuild -showBuildSettings -json` once per configuration it has, through the very
 invocation its build uses (`XcodeTypesPhase.invocation`): the same project or scratch workspace, derived data,
 cloned packages and resolution rules.

 Measured 2026-11-05 with Xcode 27.2, on a probe fixture and on a copy of ahraos-presence's CapturePhone:

 - It resolves the project's packages, into the `-clonedSourcePackagesDirPath` it is given, as a build does. It
   writes into the derived data it is given, and nothing into `~/Library/Developer/Xcode/DerivedData`, while
   `-alltargets` with no scheme made a folder there, which is why each target's scheme is named. Like a build, it
   makes an empty `.swiftpm/xcode` in each local package and empty `xcshareddata/swiftpm` folders in the bundle it
   is given; the first are removed again (`removeFoldersMadeInLocalPackages`), and the bundle is the generated one
   in the scratch, or under `--no-fix` a committed one is reached through the scratch workspace, which left it
   byte-identical.
 - It costs 4.9s on the probe and 8.4s on CapturePhone the first time, which resolves, then 1.4s and 2.1 to 2.3s a
   configuration. CapturePhone's two configurations are 4.5s on every warm run, most of a run that reuses its
   build, so the answer is kept against the same kind of fingerprint the build's reuse uses (`BuildInputSnapshot`)
   and asked again only when something it reads changed.
 - The keys, from Xcode's own specification of them (`Swift.xcspec`) and printed on a fixture that sets each:
   `SWIFT_VERSION`; `SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY` and `SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY`,
   which pass `-enable-upcoming-feature <Name>` only for `YES` (`MIGRATE` passes `<Name>:migrate`, which is not the
   feature on); `SWIFT_STRICT_MEMORY_SAFETY`, which passes `-strict-memory-safety` only for `YES`; and
   `OTHER_SWIFT_FLAGS`, where the same flags can be written by hand. A key nobody set is absent from the answer,
   and its default is `NO`, so absent is off.

 A setting is on for a target only when it is on in every configuration: a feature enabled for Debug alone is not
 the target's setting, since Release ships without it. A target whose settings could not be read in any
 configuration is unchecked, with why, and never judged on or off.
 */
public struct XcodeBuildSettings: Equatable, Sendable {
    /* What one configuration of one target compiles with. */
    public struct Configuration: Equatable, Sendable, Codable {
        /* `SWIFT_VERSION` as the language modes SwiftPM spells (`XcodeProject.languageMode`). */
        public var languageMode: String
        public var upcomingFeatures: Set<String>
        public var strictMemorySafety: Bool

        public init(languageMode: String, upcomingFeatures: Set<String>, strictMemorySafety: Bool) {
            self.languageMode = languageMode
            self.upcomingFeatures = upcomingFeatures
            self.strictMemorySafety = strictMemorySafety
        }
    }

    /* The file a finding about a target points into: the spec at an xcodegen root, else the committed project file. */
    public var definition: URL
    /* Each target's line in the definition, where it was found; a finding about a target with none points at line 1 and says so. */
    public var targetLines: [String: Int]
    /* Each target whose settings were read, to its configurations by name. These are the targets the toolchain rules judge. */
    public var targets: [String: [String: Configuration]]
    /* Each target of ours whose settings could not be read, to why. */
    public var unchecked: [String: String]
    /*
     The spec's text at an xcodegen root, which a finding's suggestion edits (`XcodeGenSpecification`). Nil for a
     committed project, whose project file is never offered an edit.
     */
    public var specification: String?

    public init(
        definition: URL,
        targetLines: [String: Int],
        targets: [String: [String: Configuration]],
        unchecked: [String: String],
        specification: String? = nil,
    ) {
        self.definition = definition
        self.targetLines = targetLines
        self.targets = targets
        self.unchecked = unchecked
        self.specification = specification
    }

    /* The upcoming features the rules judge, by the build setting Xcode turns each on with. */
    static let featureSettings = [
        "SWIFT_UPCOMING_FEATURE_EXISTENTIAL_ANY": "ExistentialAny",
        "SWIFT_UPCOMING_FEATURE_MEMBER_IMPORT_VISIBILITY": "MemberImportVisibility",
    ]

    /*
     One configuration's answer, from the settings `-showBuildSettings` printed for it, or nil when it names no Swift
     version: Xcode will not compile Swift without one, so there is no mode to judge. The build settings turn features
     on first and `OTHER_SWIFT_FLAGS` comes after them on the command line, so a flag there has the last word, a
     `-disable-upcoming-feature` included. `-Xfrontend` only hands the next word to the frontend, which reads these
     flags the same way, so it is passed over.
     */
    static func configuration(from settings: [String: String]) -> Configuration? {
        guard let version = settings["SWIFT_VERSION"]?.trimmingCharacters(in: .whitespaces), !version.isEmpty else {
            return nil
        }
        var features = Set(featureSettings.filter { settings[$0.key] == "YES" }.map(\.value))
        var strictMemorySafety = settings["SWIFT_STRICT_MEMORY_SAFETY"] == "YES"
        let flags = words(of: settings["OTHER_SWIFT_FLAGS"] ?? "").filter { $0 != "-Xfrontend" }
        for (index, flag) in flags.enumerated() {
            let next = flags.indices.contains(index + 1) ? flags[index + 1] : nil
            switch flag {
                case "-enable-upcoming-feature":
                    if let next {
                        features.insert(next)
                    }
                case "-disable-upcoming-feature":
                    if let next {
                        features.remove(next)
                    }
                case "-strict-memory-safety":
                    strictMemorySafety = true
                default:
                    break
            }
        }
        return Configuration(
            languageMode: XcodeProject.languageMode(of: version),
            upcomingFeatures: features,
            strictMemorySafety: strictMemorySafety,
        )
    }

    /* A build setting's value as the words Xcode splits it into: on white space, each word's quotes taken off. */
    static func words(of value: String) -> [String] {
        value.split(whereSeparator: \.isWhitespace).map { word in
            var text = String(word)
            for quote in ["\"", "'"] where text.count >= 2 && text.hasPrefix(quote) && text.hasSuffix(quote) {
                text = String(text.dropFirst().dropLast())
            }
            return text
        }
    }

    /*
     The model with each read target's settings as the rules judge them: the oldest language mode any configuration
     compiles in, the features every configuration enables, and strict memory safety only when every configuration
     has it.
     */
    func applied(to model: PackageModel) -> PackageModel {
        var applied = model
        applied.xcodeBuildSettings = self
        applied.targets = model.targets.map { target in
            guard let configurations = targets[target.name], let first = configurations.values.first else {
                return target
            }
            var judged = target
            judged.languageMode = configurations.values.map(\.languageMode).reduce(first.languageMode) {
                PackageModel.isOlderLanguageMode($1, $0) ? $1 : $0
            }
            judged.upcomingFeatures = configurations.values.map(\.upcomingFeatures).reduce(first.upcomingFeatures) {
                $0.intersection($1)
            }
            judged.strictMemorySafety = configurations.values.allSatisfy(\.strictMemorySafety)
            return judged
        }
        return applied
    }

    /*
     The configurations of `target` where `lacking` holds, by name, or nil when it holds in all of them, so a finding
     names a configuration only when the others are fine.
     */
    func configurations(of target: String, where lacking: (Configuration) -> Bool) -> [String]? {
        guard let configurations = targets[target] else { return nil }
        let lackingNames = configurations.filter { lacking($0.value) }.keys.sorted()
        return lackingNames.count == configurations.count ? nil : lackingNames
    }

    /* The definition's name in a finding's words: `project.yml`, or the committed bundle's `project.pbxproj`. */
    var definitionName: String {
        definition.lastPathComponent
    }

    /*
     A finding about `target` at its line in the definition, or at line 1 when that line was not found, which `message`
     is told so it can say so. At an xcodegen root it carries, as a suggestion and never a fix, the edit to project.yml
     that writes `missing` into the target's base settings, when that edit is one `XcodeGenSpecification` can make.
     */
    func finding(
        rule: String,
        target: String,
        missing: [XcodeGenSpecification.Setting],
        message: (_ lineFound: Bool) -> RuleMessages.Message,
    ) -> FindingRecord {
        let line = targetLines[target]
        let said = message(line != nil)
        var suggestions: [FindingRecord.Suggestion] = []
        if let specification,
            case let .success(edit) = XcodeGenSpecification(text: specification).suggestedEdit(missing, to: target)
        {
            let written = missing.map { "\($0.key): \($0.value)" }.joined(separator: ", ")
            suggestions.append(
                FindingRecord.Suggestion(
                    message: "Write \(written) into \(target)'s base settings in project.yml",
                    fixes: [edit],
                )
            )
        }
        return FindingRecord(
            source: .rule,
            file: definition.path,
            line: line ?? 1,
            column: 1,
            severity: .error,
            rule: rule,
            messageId: said.id,
            message: said.text,
            suggestions: suggestions,
        )
    }

    /* Names as a sentence lists them: `Release`, `Debug and Release`, `Debug, Profile and Release`. */
    static func listed(_ names: [String]) -> String {
        guard let last = names.last, names.count > 1 else { return names.first ?? "" }
        return names.dropLast().joined(separator: ", ") + " and " + last
    }

    /*
     Each target's line in a committed project file: the line that opens its object, its identifier, then its name in
     a comment, then `= {`. The identifier also appears in every list that refers to the target, but only that line
     opens it. A target whose line is not found is absent, and its findings point at line 1 and say so.
     */
    static func targetLines(inProjectFile text: String, targets: [XcodeProjectFile.Target]) -> [String: Int] {
        let identifiers = Dictionary(
            targets.filter { !$0.identifier.isEmpty }.map { ($0.identifier, $0.name) },
            uniquingKeysWith: { first, _ in first },
        )
        var found: [String: Int] = [:]
        for (index, line) in text.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
            let content = line.trimmingCharacters(in: .whitespaces)
            guard content.hasSuffix("= {"),
                let identifier = content.split(separator: " ", maxSplits: 1).first,
                let name = identifiers[String(identifier)], found[name] == nil
            else { continue }
            found[name] = index + 1
        }
        return found
    }

    /* The answer kept against the fingerprint of everything it was read from. */
    struct Stored: Codable {
        var fingerprint: String
        var targets: [String: [String: Configuration]]
    }

    static func storeFile(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("xcode-settings.json")
    }

    /*
     Each owned target's settings, read through `phase`'s invocation, in every configuration the project file gives
     it, or the reason it is unchecked. A target the build plan does not build (no platform) is unchecked for the
     reason the types record gives. `definition` and `targetLines` are where findings point, and `specification` is
     the spec's text a suggestion edits (`Pipeline`).
     */
    static func read(
        phase: XcodeTypesPhase,
        definition: URL,
        targetLines: [String: Int],
        specification: String?,
    ) -> XcodeBuildSettings {
        let plan = phase.buildPlan
        var unchecked = Dictionary(
            plan.unbuilt.map { ($0.target, $0.reason) },
            uniquingKeysWith: { first, _ in first },
        )
        var answered = XcodeBuildSettings(
            definition: definition,
            targetLines: targetLines,
            targets: [:],
            unchecked: [:],
            specification: specification,
        )
        var builds: [XcodeTypesPhase.Build] = []
        for build in plan.builds where !builds.contains(where: { $0.target == build.target }) {
            builds.append(build)
        }
        let configurationsByTarget = Dictionary(
            phase.project.projectFile.targets.map { ($0.name, $0.configurations) },
            uniquingKeysWith: { first, _ in first },
        )
        for build in builds where (configurationsByTarget[build.target] ?? []).isEmpty {
            unchecked[build.target] =
                "the project file gives it no build configuration, so there is none to ask Xcode about"
        }
        let asked = builds.filter { !(configurationsByTarget[$0.target] ?? []).isEmpty }
        if phase.buildsThroughWorkspace {
            do {
                try phase.writeWorkspace()
            }
            catch {
                for build in asked {
                    unchecked[build.target] =
                        "the workspace its settings are read through could not be written (\(error.localizedDescription))"
                }
                answered.unchecked = unchecked
                return answered
            }
        }
        phase.restoreResolved()
        let invocations = asked.flatMap { build in
            (configurationsByTarget[build.target] ?? []).map { (build, $0) }
        }
        let snapshot =
            phase.toolchain.hasPrefix("unknown")
            ? nil
            : BuildInputSnapshot.take(
                roots: phase.inputRoots,
                resolved: phase.resolvedInBuild,
                toolchain: phase.toolchain,
                arguments: invocations.flatMap {
                    phase.invocation(of: $0.0, configuration: $0.1) + ["-showBuildSettings"]
                },
                contents: [phase.project.bundle.appendingPathComponent("project.pbxproj")],
            )
        if let snapshot,
            let data = try? Data(contentsOf: storeFile(scratchPath: phase.scratchPath)),
            let stored = try? JSONDecoder().decode(Stored.self, from: data),
            stored.fingerprint == snapshot.fingerprint
        {
            answered.targets = stored.targets
            answered.unchecked = unchecked
            return answered
        }

        let start = Date()
        for build in asked {
            var configurations: [String: Configuration] = [:]
            for configuration in configurationsByTarget[build.target] ?? [] {
                switch answer(phase: phase, build: build, configuration: configuration) {
                    case let .success(read):
                        configurations[configuration] = read
                    case let .failure(reason):
                        unchecked[build.target] = reason.description
                }
                if unchecked[build.target] != nil {
                    break
                }
            }
            if unchecked[build.target] == nil {
                answered.targets[build.target] = configurations
            }
        }
        phase.removeFoldersMadeInLocalPackages(since: start)
        if phase.package.projectKind == .xcodeGen {
            phase.keepResolved()
        }
        answered.unchecked = unchecked
        /* Kept only when every target asked was answered: an unread target is asked again next run, never remembered as unread. */
        if let snapshot, answered.targets.count == asked.count {
            do {
                try JSONEncoder().encode(Stored(fingerprint: snapshot.fingerprint, targets: answered.targets))
                    .write(to: storeFile(scratchPath: phase.scratchPath), options: .atomic)
            }
            catch {
                /* Not kept, so the next run asks Xcode again: slower, and still right. */
            }
        }
        return answered
    }

    /* Why one configuration of one target could not be read, in the words the lint record gives. */
    struct Unread: Error, CustomStringConvertible {
        var description: String
    }

    /* One configuration of one target, asked of Xcode. */
    static func answer(
        phase: XcodeTypesPhase,
        build: XcodeTypesPhase.Build,
        configuration: String,
    ) -> Result<Configuration, Unread> {
        let asking = "xcodebuild -showBuildSettings for its \(configuration) configuration"
        let result: ProcessRunner.Result
        do {
            result = try phase.runner.run(
                "xcodebuild",
                phase.invocation(of: build, configuration: configuration) + ["-showBuildSettings", "-json"],
                in: phase.package.root,
            )
        }
        catch {
            return .failure(Unread(description: "\(asking) could not run: \(error)"))
        }
        guard result.succeeded else {
            return .failure(Unread(description: "\(asking) failed: \(phase.failedStep(of: result))"))
        }
        guard let listed = try? JSONSerialization.jsonObject(with: result.standardOutput) as? [[String: Any]] else {
            return .failure(Unread(description: "\(asking) did not answer with a JSON list"))
        }
        guard
            let settings = listed.first(where: { $0["target"] as? String == build.target })?["buildSettings"]
                as? [String: String]
        else {
            return .failure(Unread(description: "\(asking) did not list the target's settings"))
        }
        guard let read = Self.configuration(from: settings) else {
            return .failure(
                Unread(description: "\(asking) gave no SWIFT_VERSION, so there is no language mode to judge")
            )
        }
        return .success(read)
    }
}
