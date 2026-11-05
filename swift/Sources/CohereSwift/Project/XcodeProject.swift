import Foundation

/*
 An Xcode project as this engine checks it: found at a root that holds no Package.swift, read from its project
 file, and handed to the rest of the run as a `PackageModel`, so fix, format and lint need nothing new to check
 its files.

 Two kinds of root. A `project.yml` is an xcodegen spec, and the project is generated from it into the engine's
 scratch (`XcodeGeneration`), because a repository that keeps a spec ignores the project made from it. A
 committed `.xcodeproj` is read where it stands. The spec wins when both are there, since a project beside its
 spec is the spec's output and may be stale.

 Targets and files come from the project file (`XcodeProjectFile`), never from the spec's globs: xcodegen's rules
 for what a source path includes are xcodegen's, and a second reading of them would drift from the first.

 What the run takes from each native target: its name, its product type spelled as SwiftPM's kinds, its Swift
 files and its `SWIFT_VERSION`. The settings the package rules judge (upcoming features, strict memory safety,
 the language mode as Xcode resolves it through configuration files) are block D's (#6vjs9n7). Until then those
 rules are not run against an Xcode project (`Linter`), because a judgment made from half the settings would be
 a guess.
 */
public enum XcodeProject {
    /* Where an Xcode root's project comes from. */
    public enum Source: Equatable, Sendable {
        case xcodeGenSpecification(URL)
        case projectBundle(URL)
    }

    /* The root holds Xcode files this engine cannot choose a project from. */
    public struct AmbiguousRoot: Error, CustomStringConvertible {
        public var description: String
    }

    /* The Xcode project at `root`, or nil when it holds none. A Package.swift at the root is the caller's to look for first. */
    public static func source(at root: URL) throws -> Source? {
        let specification = root.appendingPathComponent("project.yml")
        if FileManager.default.fileExists(atPath: specification.path) {
            return .xcodeGenSpecification(specification)
        }
        let entries = try FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)
        let bundles = entries.filter { $0.pathExtension == "xcodeproj" }.sorted { $0.path < $1.path }
        if bundles.count > 1 {
            throw AmbiguousRoot(
                description:
                    "\(root.path) holds \(bundles.count) Xcode projects (\(bundles.map(\.lastPathComponent).joined(separator: ", "))), and a run checks one project per root, so nothing was checked"
            )
        }
        if let bundle = bundles.first {
            return .projectBundle(bundle)
        }
        if let workspace = entries.first(where: { $0.pathExtension == "xcworkspace" }) {
            throw AmbiguousRoot(
                description:
                    "\(root.path) holds the workspace \(workspace.lastPathComponent) and no project, and this engine does not read workspaces yet, so nothing was checked"
            )
        }
        return nil
    }

    /* A project as the run reads it: the model fix, format and lint check, and the bundle and project file the types phase builds. */
    public struct Loaded: Sendable {
        public var model: PackageModel
        /* The .xcodeproj xcodebuild is given: the generated one in the scratch, or the committed one where it stands. */
        public var bundle: URL
        public var projectFile: XcodeProjectFile
        /* The file a person edits to change the project, where a finding about the project as a whole points: the spec, or the committed project file. */
        public var definition: URL
    }

    /* The project as a package model: generated first when it is a spec, then read. */
    public static func load(source: Source, root: URL, scratchPath: URL, runner: ProcessRunner) throws -> Loaded {
        let bundle: URL
        let kind: PackageModel.ProjectKind
        let definition: URL
        switch source {
            case let .xcodeGenSpecification(specification):
                bundle = try XcodeGeneration.generate(
                    specFile: specification,
                    root: root,
                    into: scratchPath.appendingPathComponent("xcode", isDirectory: true),
                    runner: runner,
                )
                kind = .xcodeGen
                definition = specification
            case let .projectBundle(projectBundle):
                bundle = projectBundle
                kind = .xcodeProject
                definition = projectBundle.appendingPathComponent("project.pbxproj")
        }
        let projectFile = try XcodeProjectFile.read(bundle)
        var model = PackageModel(
            name: bundle.deletingPathExtension().lastPathComponent,
            root: root,
            toolsVersion: "",
            targets: projectFile.targets.map { target in
                PackageModel.Target(
                    name: target.name,
                    kind: Self.kind(of: target.productType),
                    /* Only the package description's cache reads a target's directory, and an Xcode project is never cached there. */
                    directory: root,
                    sources: target.sources,
                    languageMode: Self.languageMode(of: target.swiftVersion),
                )
            },
        )
        model.projectKind = kind
        return Loaded(model: model, bundle: bundle, projectFile: projectFile, definition: definition)
    }

    /*
     Xcode's product type in SwiftPM's words, so the rules that ask what a target is (print, where stdout means
     something in a tool and nothing in an app) read an Xcode target the way they read a package's.
     */
    static func kind(of productType: String) -> String {
        let prefix = "com.apple.product-type."
        let name = productType.hasPrefix(prefix) ? String(productType.dropFirst(prefix.count)) : productType
        if name.hasPrefix("application") || name.contains("extension") {
            return "application"
        }
        if name.hasPrefix("bundle.unit-test") || name.hasPrefix("bundle.ui-testing") {
            return "test"
        }
        if name == "tool" {
            return "executable"
        }
        if name.hasPrefix("framework") || name.hasPrefix("library") {
            return "library"
        }
        return name
    }

    /*
     `SWIFT_VERSION` as the language modes SwiftPM spells: "5.0" is "5" and "6.0" is "6", while "4.2" stays. An
     unset one compiles in the compiler's default mode, which is Swift 5 for every Swift 6 compiler.
     */
    static func languageMode(of swiftVersion: String?) -> String {
        guard var mode = swiftVersion?.trimmingCharacters(in: .whitespaces), !mode.isEmpty else { return "5" }
        while mode.hasSuffix(".0") {
            mode.removeLast(2)
        }
        return mode
    }
}
