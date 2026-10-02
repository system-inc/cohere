import Foundation

/*
 Which of the package's Swift files this run treats as ours, and why each of the others is not.

 A file is ours when a target of the package, or of a local package inside it that is not vendored,
 compiles it, git does not ignore it, and it is not generated. Git's
 answer includes untracked files that are not ignored. A new file someone is still writing is exactly
 the one a gate must see, and TypeScript's program already includes it, because a tsconfig reads the
 disk rather than the index.

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

        public init(url: URL, targetName: String, targetKind: String, packageRoot: URL? = nil) {
            self.packageRoot = packageRoot
            self.url = url
            self.targetName = targetName
            self.targetKind = targetKind
        }
    }

    /* The leading bytes searched for a generated-code marker. A marker below this point is not a file header. */
    static let generatedMarkerWindow = 1024

    public var filesInPackage: [URL]
    public var owned: [OwnedFile]
    public var excluded: [ProjectRecord.ExcludedFile]
    /* Empty when git answered. Otherwise why every target source was treated as ours. */
    public var note: String

    public init(filesInPackage: [URL], owned: [OwnedFile], excluded: [ProjectRecord.ExcludedFile], note: String) {
        self.filesInPackage = filesInPackage
        self.owned = owned
        self.excluded = excluded
        self.note = note
    }

    public static func build(package: PackageModel, runner: ProcessRunner = ProcessRunner()) throws -> FileSet {
        let visible = try gitVisibleFiles(root: package.root, runner: runner)
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
                    /* Compared resolved on both sides: git reports real paths, and a root reached through a symlink (`/tmp` is `/private/tmp`) would otherwise match nothing and exclude everything. */
                    if let visible, !visible.contains(source.resolvingSymlinksInPath().path) {
                        excluded.append(.init(file: source.path, reason: "ignored by git"))
                        continue
                    }
                    if try isGenerated(source) {
                        excluded.append(.init(file: source.path, reason: "marked @generated"))
                        continue
                    }
                    owned.append(OwnedFile(url: source, targetName: target.name, targetKind: kind, packageRoot: member.root))
                }
            }
        }

        /*
         Tracked Swift files under the package that no target compiles: standalone scripts, the manifests, an
         app built by an Xcode project, a separate package beside this one. None is checked by this run, and
         each is named, so a clean verdict over the targets cannot read as a clean verdict over the directory.
         Measured on Presence: 20 such files, holding 56 force unwraps the target files do not.
         */
        if let visible {
            let rootPath = package.root.resolvingSymlinksInPath().path + "/"
            let compiled = Set(filesInPackage.map { $0.resolvingSymlinksInPath().path })
            for path in visible.sorted() where path.hasPrefix(rootPath) && path.hasSuffix(".swift") && !compiled.contains(path) {
                let underVendor = path.dropFirst(rootPath.count).split(separator: "/").dropLast().contains("Vendor")
                let reason = underVendor
                    ? "vendored under Vendor/"
                    : path.hasSuffix("/Package.swift") ? "a package manifest" : "no target of this package compiles it"
                excluded.append(.init(file: path, reason: reason))
            }
        }

        let note = visible == nil
            ? "not a git repository, so every file a target compiles was treated as ours"
            : ""
        return FileSet(filesInPackage: filesInPackage, owned: owned, excluded: excluded, note: note)
    }

    /*
     Every file git does not ignore, tracked or not, as absolute paths. Nil when the package is not in a git
     repository at all, which is an answer ("there is no index to consult") rather than an empty set.
     */
    static func gitVisibleFiles(root: URL, runner: ProcessRunner) throws -> Set<String>? {
        let topLevel = try runner.run("git", ["rev-parse", "--show-toplevel"], in: root)
        guard topLevel.succeeded else {
            return nil
        }
        let listing = try runner.run("git", ["ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name"], in: root)
        guard listing.succeeded else {
            throw ProcessRunner.LaunchFailure(command: "git ls-files", underlying: listing.standardError)
        }
        let repositoryRoot = String(decoding: topLevel.standardOutput, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        let repository = URL(fileURLWithPath: repositoryRoot, isDirectory: true)
        let names = listing.standardOutput.split(separator: 0).map { String(decoding: $0, as: UTF8.self) }
        return Set(names.map { repository.appendingPathComponent($0).resolvingSymlinksInPath().path })
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
            if text.range(of: #"(?m)^\s*(@\w+\s+)*import\s+(SwiftUI|AppKit|UIKit)\b"#, options: .regularExpression) != nil {
                return true
            }
        }
        return false
    }

    /* A file that declares itself generated in its header: `// @generated`, the marker Apollo and most generators write. */
    static func isGenerated(_ url: URL) throws -> Bool {
        let handle = try FileHandle(forReadingFrom: url)
        defer { handle.closeFile() }
        let head = handle.readData(ofLength: generatedMarkerWindow)
        return String(decoding: head, as: UTF8.self).contains("@generated")
    }
}
