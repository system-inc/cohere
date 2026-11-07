import Foundation

/*
 Which of the package's Swift files this run treats as ours, and why each of the others is not.

 A file is ours when a target of the package, or of a local package inside it that is not vendored,
 compiles it, and git does not ignore it. A generated file is ours too: Kirk's convention (2026-10-03,
 shared with TypeScript) names it `*.generated.swift` and gives it every rule and the formatter, exempting
 it only from file-length, by that name. What git ignores is read from the ignore files themselves
 (`IgnoreRules`), with no git process, and it includes untracked files that are not ignored. A new file someone is still writing is exactly the one a gate must see, and TypeScript's program
 already includes it, because a tsconfig reads the disk rather than the index.

 Every file a target compiles and this run does not check is listed with a reason, so the coverage line
 can say what was left out rather than letting a smaller number pass for a complete one.
 */
public struct FileSet: Equatable, Sendable {
    /* A file this run checks, with the target that compiles it. */
    public struct OwnedFile: Equatable, Sendable {
        public var url: URL
        public var targetName: String
        public var targetKind: String
        /* The root of the package whose target compiles it. */
        public var packageRoot: URL?
        /*
         The platform a standalone script is checked for, nil for a file a target compiles. A script's target is its own,
         named for the module `swift <file>` compiles it into (`StandaloneScript`).
         */
        var scriptPlatform: StandaloneScript.Platform?

        public init(url: URL, targetName: String, targetKind: String, packageRoot: URL? = nil) {
            self.packageRoot = packageRoot
            self.url = url
            self.targetName = targetName
            self.targetKind = targetKind
        }
    }

    public var filesInPackage: [URL]
    public var owned: [OwnedFile]
    public var excluded: [ProjectRecord.ExcludedFile]
    /* Empty inside a git repository. Otherwise why every target source was treated as ours. */
    public var note: String
    /*
     What deciding the set read beyond the targets' files, for the run's inputs record (`RunInputs`): every ignore file
     and git config file consulted, there or not, and every directory the walk for files no target compiles listed.
     Outside a git repository that walk reads the package's own ignore files, so these are its.
     */
    public var ignoreFilesConsulted: [String] = []
    public var directoriesListed: [String] = []

    /* The files of ours a target compiles, which the package's or the Xcode project's types phase checks. */
    public var targetFiles: [OwnedFile] { owned.filter { $0.scriptPlatform == nil } }

    /* The standalone scripts of ours, each checked alone (`ScriptTypesPhase`). */
    public var scripts: [OwnedFile] { owned.filter { $0.scriptPlatform != nil } }

    public init(filesInPackage: [URL], owned: [OwnedFile], excluded: [ProjectRecord.ExcludedFile], note: String) {
        self.filesInPackage = filesInPackage
        self.owned = owned
        self.excluded = excluded
        self.note = note
    }

    /* `environment` is where the global git config and excludes file are found; tests pass their own. */
    public static func build(
        package: PackageModel,
        environment: [String: String] = ProcessInfo.processInfo.environment,
    ) throws -> FileSet {
        /* Nil outside a git repository, which is an answer ("there are no ignore rules to apply") rather than an empty set. */
        let ignoreRules = IgnoreRules.Repository.containing(package.root).map {
            IgnoreRules(repository: $0, environment: environment)
        }
        var filesInPackage: [URL] = []
        var owned: [OwnedFile] = []
        var excluded: [ProjectRecord.ExcludedFile] = []
        var seen = Set<String>()

        for member in package.allPackages {
            let vendored = member.root != package.root && package.isVendored(member)
            for target in member.targets {
                let kind = try Self.isApplication(target) ? "application" : target.kind
                for source in target.sources where seen.insert(source.path).inserted {
                    filesInPackage.append(source)
                    if vendored {
                        excluded.append(.init(file: source.path, reason: "vendored under Vendor/"))
                        continue
                    }
                    /* Resolved, as the repository root is: a root reached through a symlink (`/tmp` is `/private/tmp`) would otherwise sit outside the repository and exclude everything. */
                    if let ignoreRules, !ignoreRules.isVisible(source.resolvingSymlinksInPath().path) {
                        excluded.append(.init(file: source.path, reason: "ignored by git"))
                        continue
                    }
                    owned.append(
                        OwnedFile(url: source, targetName: target.name, targetKind: kind, packageRoot: member.root)
                    )
                }
            }
        }

        /*
         Swift files git would list under the package that no target compiles: standalone scripts, the manifests, an
         app built by an Xcode project, a separate package beside this one. Measured on Presence: 20 such files,
         holding 56 force unwraps the target files do not. A script whose nearest project is this run's is ours,
         checked alone (`StandaloneScript`, #13nbb1s). Every other is named, so a clean verdict over the targets cannot
         read as a clean verdict over the directory, and a file below a project of its own is left to that project's run.

         Outside a git repository, a `git archive` copy say, the walk reads the package's own ignore files as git would
         were the package's root a repository's, which is what keeps its build output and dependencies out: before
         this, no walk ran there, and a copy of ahraos-presence checked neither Glyph script nor named it, 1,342 files
         where its checkout checks 1,343 (the presence Circle, 2026-11-07). A file a target compiles is still ours there,
         ignored or not, as the note says.
         */
        let walkRules =
            ignoreRules
            ?? IgnoreRules(
                repository: IgnoreRules.Repository(root: package.root.resolvingSymlinksInPath(), commonDirectory: nil),
                environment: environment,
            )
        do {
            let resolvedRoot = package.root.resolvingSymlinksInPath().path
            let rootPath = resolvedRoot + "/"
            let compiled = Set(filesInPackage.map { $0.resolvingSymlinksInPath().path })
            let visible = Set(
                walkRules.visibleFiles(
                    under: resolvedRoot,
                    skipping: Self.buildScratches(of: package),
                    skippingWhere: Self.isCohereCache,
                )
            )
            let coveredRoots = Dictionary(
                package.allPackages.filter { member in
                    member.root == package.root
                        || (PackageModel.isInside(member.root, package.root) && !package.isVendored(member))
                }
                .map { ($0.root.resolvingSymlinksInPath().path, $0.root) },
                uniquingKeysWith: { first, _ in first },
            )
            var projectDirectories: [String: Bool] = [:]
            var scriptModules = Set<String>()
            for path in visible.sorted()
            where path.hasPrefix(rootPath) && path.hasSuffix(".swift") && !compiled.contains(path) {
                let underVendor = path.dropFirst(rootPath.count).split(separator: "/").dropLast().contains("Vendor")
                if underVendor || path.hasSuffix("/Package.swift") {
                    excluded.append(
                        .init(file: path, reason: underVendor ? "vendored under Vendor/" : "a package manifest")
                    )
                    continue
                }
                let url = URL(fileURLWithPath: path)
                guard
                    let packageRoot = Self.coveringRoot(
                        of: path,
                        resolvedRoot: resolvedRoot,
                        coveredRoots: coveredRoots,
                        projectDirectories: &projectDirectories,
                    )
                else {
                    excluded.append(.init(file: path, reason: Self.notCompiledReason))
                    continue
                }
                guard let source = try? String(contentsOf: url, encoding: .utf8) else {
                    excluded.append(.init(file: path, reason: Self.unreadableReason))
                    continue
                }
                switch StandaloneScript.read(source: source) {
                    case .declarationsOnly:
                        excluded.append(.init(file: path, reason: Self.declarationsOnlyReason))
                    case .platformsDisagree(let words):
                        excluded.append(.init(file: path, reason: Self.platformsDisagreeReason(words)))
                    case .script(let platform):
                        /* Two scripts of one name would share a module's records and index units, so a later one takes a number. */
                        let base = StandaloneScript.moduleName(of: url)
                        var module = base
                        var suffix = 2
                        while !scriptModules.insert(module).inserted {
                            module = "\(base)_\(suffix)"
                            suffix += 1
                        }
                        var script = OwnedFile(
                            url: url,
                            targetName: module,
                            targetKind: StandaloneScript.targetKind,
                            packageRoot: packageRoot,
                        )
                        script.scriptPlatform = platform
                        owned.append(script)
                }
            }
        }

        let note =
            ignoreRules == nil
            ? "not a git repository, so every file a target compiles was treated as ours, and the Swift files no target compiles were found by the package's own ignore files"
            : ""
        var fileSet = FileSet(filesInPackage: filesInPackage, owned: owned, excluded: excluded, note: note)
        fileSet.ignoreFilesConsulted = walkRules.filesConsulted.sorted()
        fileSet.directoriesListed = walkRules.directoriesListed
        return fileSet
    }

    /*
     Why a file no target compiles is not checked, in the words the project record gives. The front door expects none
     of them (`ExpectedExclusionReasons` in command/cohere), so each is the run's gap but the first, which only a file
     below a project of its own is given, and that project's run accounts for it.
     */
    static let notCompiledReason = "no target of this package compiles it"
    static let unreadableReason =
        "no target of this package compiles it, and it could not be read as UTF-8 to tell whether it is a script"
    static let declarationsOnlyReason =
        "no target of this package compiles it, and it holds only declarations, so it is no script to check alone"

    static func platformsDisagreeReason(_ words: String) -> String {
        "a script whose imports do not settle its platform: \(words), so neither SDK was guessed at"
    }

    /*
     The root of the package whose directory is the nearest project above the file at `path`, when that is this run's
     root or a local package inside it the run covers; nil when a project of its own is nearer, whose run checks the
     file. A project is what a run would check at a directory (`XcodeProject.rootProject`): a Package.swift, a
     project.yml or one .xcodeproj, and a directory a run would refuse counts too, since its files are not this run's
     either. Each directory is asked once, in `projectDirectories`.
     */
    static func coveringRoot(
        of path: String,
        resolvedRoot: String,
        coveredRoots: [String: URL],
        projectDirectories: inout [String: Bool],
    ) -> URL? {
        var directory = (path as NSString).deletingLastPathComponent
        while directory == resolvedRoot || directory.hasPrefix(resolvedRoot + "/") {
            if let covered = coveredRoots[directory] {
                return covered
            }
            let isProject =
                projectDirectories[directory]
                ?? {
                    do {
                        return try XcodeProject.rootProject(at: URL(fileURLWithPath: directory, isDirectory: true))
                            != nil
                    }
                    catch {
                        return true
                    }
                }()
            projectDirectories[directory] = isProject
            if isProject {
                return nil
            }
            directory = (directory as NSString).deletingLastPathComponent
        }
        return nil
    }

    /*
     The build scratches under each package's root that the walk for files no target compiles never enters, as
     absolute paths with symlinks resolved, the way the walk spells them. Each is told by structure, never by a
     directory's name:

     - cohere's cache directory, `.cache/cohere`, which holds the engine's own scratch, `Pipeline.scratchPath(for:)`,
       beside the front door's table, and which every run writes. A repository whose `.gitignore` does not list
       `.cache` otherwise had every run after the first build name the build's generated sources as Swift files
       nothing checked (ahraos-macos, 2026-11-06: six under `DerivedSources`) and walk some 9,800 entries of the
       scratch for them. The whole directory rather than the scratch alone, since the walk's listings are inputs of
       the run (`RunInputs`), and the table the front door writes there after every run would make the next one miss;
     - SwiftPM's own default scratch, the root's `.build`, only when it holds the cache directory tag SwiftPM
       writes into every scratch (`CACHEDIR.TAG`, with the signature the tag's specification fixes), so a
       `.build` that SwiftPM did not make is walked as before.
     */
    static func buildScratches(of package: PackageModel) -> Set<String> {
        var scratches: Set<String> = []
        for member in package.allPackages {
            let root = member.root.resolvingSymlinksInPath()
            scratches.insert(Pipeline.scratchPath(for: root).deletingLastPathComponent().standardizedFileURL.path)
            let swiftPackageScratch = root.appendingPathComponent(".build", isDirectory: true)
            if Self.isTaggedCache(swiftPackageScratch) {
                scratches.insert(swiftPackageScratch.standardizedFileURL.path)
            }
        }
        return scratches
    }

    /*
     Whether `directory` is cohere's cache directory for another project below the root: a `.cache/cohere` beside a
     project definition cohere checks, where the front door keeps that project's table and the engine its scratch
     (`cacheDirectory` in command/cohere). It is never source, and the run checking that project writes it, so a walk
     that listed it would name a build's generated sources as files nothing checked and hold a listing every one of
     that project's runs moves (ahraos-macos's `tools/scroll-probe`).
     */
    static func isCohereCache(_ directory: String) -> Bool {
        guard directory.hasSuffix("/.cache/cohere") else { return false }
        let project = URL(fileURLWithPath: directory).deletingLastPathComponent().deletingLastPathComponent()
        return ["Package.swift", "tsconfig.json", "go.mod", "go.work", "project.yml"].contains {
            FileManager.default.fileExists(atPath: project.appendingPathComponent($0).path)
        }
    }

    /* The first line every cache directory tag begins with, from its specification (bford.info/cachedir). */
    static let cacheDirectoryTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

    /* Whether a directory holds a cache directory tag: a `CACHEDIR.TAG` that begins with the signature. */
    static func isTaggedCache(_ directory: URL) -> Bool {
        guard let contents = FileManager.default.contents(atPath: directory.appendingPathComponent("CACHEDIR.TAG").path)
        else {
            return false
        }
        return contents.starts(with: Data(cacheDirectoryTagSignature.utf8))
    }

    /*
     Whether an executable target is an app rather than a command-line tool. SwiftPM spells both
     `executableTarget`, and the rules that care (print, where stdout means something) need to know which.
     An app is a target where any file imports SwiftUI, AppKit or UIKit. Decided per target, not per file:
     an app's model files import only Foundation and are still app code, and a per-file test would call
     their prints tool output.
     */
    static func isApplication(_ target: PackageModel.Target) throws -> Bool {
        guard target.kind == "executable" else { return false }
        for source in target.sources {
            let text = try String(contentsOf: source, encoding: .utf8)
            if text.range(of: #"(?m)^\s*(@\w+\s+)*import\s+(SwiftUI|AppKit|UIKit)\b"#, options: .regularExpression)
                != nil
            {
                return true
            }
        }
        return false
    }
}
