import Foundation

/*
 Which of our files this run is about: all of them, the ones named on the command line, or the ones git
 says changed.

 A narrowed scope narrows fix, format and lint. It never narrows types: in Swift a change in one file
 changes what the rest of its module means, and the compiler checks the whole module regardless, so the
 types phase always reports the whole package and the coverage line says so.

 Git failing is an error, never an empty change set. "Nothing changed" is a clean answer over zero files,
 and only a git that actually answered is allowed to give it.
 */
public struct FileScope: Equatable, Sendable {
    public var files: [FileSet.OwnedFile]
    public var everything: Bool
    /* What was asked for, in words, printed in the parenthesis of `N in scope (…)`. Empty for the whole package. */
    public var description: String
    /* Set when `--changed` found nothing of ours changed: the reason, which becomes the summary's `nothingToCheck`. */
    public var nothingToCheck: String

    public init(files: [FileSet.OwnedFile], everything: Bool, description: String, nothingToCheck: String = "") {
        self.files = files
        self.everything = everything
        self.description = description
        self.nothingToCheck = nothingToCheck
    }

    public static func resolve(
        options: CommandOptions,
        fileSet: FileSet,
        root: URL,
        workingDirectory: URL,
        runner: ProcessRunner = ProcessRunner()
    ) throws -> FileScope {
        if !options.paths.isEmpty {
            return try named(options.paths, fileSet: fileSet, workingDirectory: workingDirectory)
        }
        if options.changedOnly {
            return try changed(fileSet: fileSet, root: root, runner: runner)
        }
        return FileScope(files: fileSet.owned, everything: true, description: "")
    }

    static func named(_ paths: [String], fileSet: FileSet, workingDirectory: URL) throws -> FileScope {
        var prefixes: [String] = []
        for path in paths {
            let url = URL(fileURLWithPath: path, relativeTo: workingDirectory).standardizedFileURL.resolvingSymlinksInPath()
            guard FileManager.default.fileExists(atPath: url.path) else {
                throw CommandOptions.UsageFailure(description: "\(path) does not exist, so there is nothing named to check")
            }
            prefixes.append(url.path)
        }
        let files = fileSet.owned.filter { owned in
            let path = owned.url.resolvingSymlinksInPath().path
            return prefixes.contains { path == $0 || path.hasPrefix($0 + "/") }
        }
        return FileScope(files: files, everything: false, description: paths.joined(separator: ", "))
    }

    static func changed(fileSet: FileSet, root: URL, runner: ProcessRunner) throws -> FileScope {
        let status = try runner.run("git", ["status", "--porcelain=v1", "-z", "--untracked-files=all"], in: root)
        guard status.succeeded else {
            throw ProcessRunner.LaunchFailure(command: "git status", underlying: "could not determine what changed: \(status.standardError)")
        }
        let topLevel = try runner.run("git", ["rev-parse", "--show-toplevel"], in: root)
        let repository = URL(
            fileURLWithPath: String(decoding: topLevel.standardOutput, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines),
            isDirectory: true
        )
        let changedPaths = changedPaths(fromPorcelain: status.standardOutput).map {
            repository.appendingPathComponent($0).resolvingSymlinksInPath().path
        }
        let changedSet = Set(changedPaths)
        let files = fileSet.owned.filter { changedSet.contains($0.url.resolvingSymlinksInPath().path) }
        let description = "changed files (working tree against HEAD): \(changedPaths.count)"
        if files.isEmpty {
            let reason = changedPaths.isEmpty
                ? "nothing changed against HEAD"
                : "none of the \(changedPaths.count) changed files are Swift files a target compiles"
            return FileScope(files: [], everything: false, description: description, nothingToCheck: reason)
        }
        return FileScope(files: files, everything: false, description: description)
    }

    /*
     The paths in `git status --porcelain=v1 -z`: entries are `XY path`, NUL-terminated, and a rename or copy
     carries its original path as one more NUL-terminated field after it, which is not a changed file of its own.
     */
    static func changedPaths(fromPorcelain data: Data) -> [String] {
        var fields = data.split(separator: 0).map { String(decoding: $0, as: UTF8.self) }[...]
        var paths: [String] = []
        while let entry = fields.popFirst() {
            guard entry.count > 3 else { continue }
            let statusCode = entry.prefix(2)
            paths.append(String(entry.dropFirst(3)))
            if statusCode.contains("R") || statusCode.contains("C") {
                _ = fields.popFirst()
            }
        }
        return paths
    }
}
