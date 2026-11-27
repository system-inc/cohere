import Foundation

/*
 SwiftPM's resolved dependencies, laid ahead of a run that has no network, named by `COHERE_SWIFT_RESOLVED`
 (#je1r32a). The integrate job runs cohere inside `sandbox-exec` with the network denied, and names a read-only
 directory holding `Package.resolved` (the bytes the set was resolved from), `scratch/` (SwiftPM's scratch path after
 `swift package resolve`: `checkouts/`, `repositories/`, `workspace-state.json` and the rest) and `cache/`.

 Every `swift package` call cohere-swift makes (`PackageModel`'s describe and dump-package, `BuildServerClient`'s
 server) asks this for the global options it adds before its subcommand:
 - unset, none, so every call is exactly what it was before the set existed;
 - set, `--disable-sandbox`, since SwiftPM starts a sandbox of its own for manifests and plugins and a sandbox cannot
   start inside the job's ("sandbox-exec: sandbox_apply: Operation not permitted");
 - set, and the package root's own `Package.resolved` holds the set's bytes exactly, `--skip-update` as well, after the
   set's scratch is cloned into the engine's scratch for that root, so SwiftPM finds every pin checked out and fetches
   nothing. A root that resolves to other versions, or has no Package.resolved, is never handed the set's checkouts,
   which would check it against dependencies it does not pin.

 A set named but not there, or holding no Package.resolved, fails every call that asks, naming the path: a run told to
 work offline that quietly went online would fail later and further from the cause, or pass where the job cannot.

 The variable is read once for the process (`process`); a test hands its own environment to `init(environment:)`.
 */
struct LaidResolvedSet: Sendable {
    static let variable = "COHERE_SWIFT_RESOLVED"

    /* Why the set named cannot be used, or could not be cloned in, with the path that says where. */
    struct Failure: Error, CustomStringConvertible, Equatable {
        var description: String
    }

    /* A set the variable named, read: its directory and the bytes of its Package.resolved. */
    private struct Laid: Sendable {
        var directory: URL
        var packageResolved: Data
    }

    /* Nil when the variable is unset or empty. */
    private let reading: Swift.Result<Laid?, Failure>

    /* This process's set, read from its environment the first time any SwiftPM call asks. */
    static let process = LaidResolvedSet(environment: ProcessInfo.processInfo.environment)

    init(environment: [String: String]) {
        reading = Swift.Result { () throws(Failure) -> Laid? in try Self.read(environment) }
    }

    private static func read(_ environment: [String: String]) throws(Failure) -> Laid? {
        let named = (environment[variable] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !named.isEmpty else { return nil }
        /* A relative path would be read against wherever SwiftPM happens to run, which is a different folder per call. */
        guard named.hasPrefix("/") else {
            throw Failure(description: "\(variable)=\(named) is not an absolute path, so no laid set could be read")
        }
        let directory = URL(fileURLWithPath: named, isDirectory: true)
        guard (try? directory.resourceValues(forKeys: [.isDirectoryKey]))?.isDirectory == true else {
            throw Failure(
                description: "\(variable) names \(directory.path), which is not a directory, so nothing was checked"
            )
        }
        let resolved = directory.appendingPathComponent("Package.resolved")
        guard let bytes = FileManager.default.contents(atPath: resolved.path) else {
            throw Failure(
                description:
                    "\(variable) names \(directory.path), which holds no readable Package.resolved at \(resolved.path), so nothing was checked"
            )
        }
        return Laid(directory: directory, packageResolved: bytes)
    }

    /*
     The global options a `swift package` call on `packageRoot`, writing into `scratchPath`, adds before its subcommand,
     having first cloned the set's scratch into `scratchPath` when the root pins exactly the set's versions.
     */
    func swiftPackageOptions(packageRoot: URL, scratchPath: URL) throws(Failure) -> [String] {
        guard let laid = try reading.get() else { return [] }
        let rootResolved = FileManager.default.contents(
            atPath: packageRoot.appendingPathComponent("Package.resolved").path
        )
        guard rootResolved == laid.packageResolved else { return ["--disable-sandbox"] }
        try Self.seed(scratchPath, from: laid.directory.appendingPathComponent("scratch", isDirectory: true))
        return ["--disable-sandbox", "--skip-update"]
    }

    /*
     Clones the set's scratch into `scratchPath` unless it already holds `checkouts/`, the folder SwiftPM reads a pin's
     sources from, which is cloned last, so its presence means the rest is there. Under an exclusive `flock` on a file in
     the scratch, which the kernel holds per open file, so the describe and the build server of one run, or two runs on
     one root, never clone at once, and the second finds the first's checkouts and leaves them.

     Cloned, never moved: the set is read-only, and its checkouts' `.git/objects/info/alternates` name its
     `repositories/` by absolute path, which a clone keeps pointing at the set. `clonefile` clones a whole tree on APFS
     in one call, sharing every block; across volumes, where it cannot, `FileManager.copyItem` copies. Either keeps
     each file's mode, so a set laid without write permission would clone into a scratch SwiftPM cannot write. Measured
     2026-11-26 with the set's modes made read-only: the build server's SwiftPM failed with "invalid access to
     <scratch>/workspace-state.json" and listed no target. So the clone is owned by this process's user
     (`CLONE_NOOWNERCOPY`) and made writable by it, file by file. An entry the scratch already holds from a run before
     the set (a `workspace-state.json` that pins nothing) is replaced, so SwiftPM reads the set's state beside the set's
     checkouts.
     */
    private static func seed(_ scratchPath: URL, from laidScratch: URL) throws(Failure) {
        let checkouts = scratchPath.appendingPathComponent("checkouts", isDirectory: true)
        let laidCheckouts = laidScratch.appendingPathComponent("checkouts", isDirectory: true)
        guard FileManager.default.fileExists(atPath: laidCheckouts.path) else {
            throw Failure(
                description:
                    "the laid set's scratch holds no checkouts at \(laidCheckouts.path), so SwiftPM would fetch them, and the network is not there"
            )
        }
        do {
            try FileManager.default.createDirectory(at: scratchPath, withIntermediateDirectories: true)
        }
        catch {
            throw Failure(description: "the scratch \(scratchPath.path) could not be made for the laid set: \(error)")
        }
        let lockPath = scratchPath.appendingPathComponent("laid-resolved-set.lock").path
        let lock = unsafe open(lockPath, O_CREAT | O_RDWR, 0o644)
        guard lock >= 0 else {
            throw Failure(description: "the lock \(lockPath) could not be opened: \(Self.lastSystemError())")
        }
        defer { close(lock) }
        guard flock(lock, LOCK_EX) == 0 else {
            throw Failure(description: "the lock \(lockPath) could not be taken: \(Self.lastSystemError())")
        }
        guard !FileManager.default.fileExists(atPath: checkouts.path) else { return }
        do {
            let entries = try FileManager.default.contentsOfDirectory(atPath: laidScratch.path)
            for name in entries.sorted() where name != "checkouts" {
                let target = scratchPath.appendingPathComponent(name)
                if FileManager.default.fileExists(atPath: target.path) {
                    try FileManager.default.removeItem(at: target)
                }
                try clone(laidScratch.appendingPathComponent(name), to: target)
                try makeWritable(target)
            }
            /* Under a name of its own and renamed in whole, so a run killed mid-clone leaves no half `checkouts/` that reads as seeded. */
            let staged = scratchPath.appendingPathComponent(".checkouts-\(UUID().uuidString)", isDirectory: true)
            try clone(laidCheckouts, to: staged)
            try makeWritable(staged)
            guard unsafe rename(staged.path, checkouts.path) == 0 else {
                throw Failure(
                    description:
                        "the laid checkouts cloned to \(staged.path) could not be renamed to \(checkouts.path): \(Self.lastSystemError())"
                )
            }
        }
        catch let failure as Failure {
            throw failure
        }
        catch {
            throw Failure(
                description:
                    "the laid set's scratch \(laidScratch.path) could not be cloned into \(scratchPath.path): \(error)"
            )
        }
    }

    /* What the system call that just failed said, by its `errno`. */
    private static func lastSystemError() -> String {
        let code = errno
        return POSIXErrorCode(rawValue: code).map { POSIXError($0).localizedDescription } ?? "errno \(code)"
    }

    private static func clone(_ source: URL, to target: URL) throws {
        guard unsafe clonefile(source.path, target.path, UInt32(CLONE_NOOWNERCOPY)) == 0 else {
            try FileManager.default.copyItem(at: source, to: target)
            return
        }
    }

    /* Gives this process's user write permission on `root` and everything under it; a symbolic link is left, since a mode change would follow it. */
    private static func makeWritable(_ root: URL) throws {
        var paths = [root.path]
        if let walk = FileManager.default.enumerator(atPath: root.path) {
            for case let relative as String in walk {
                paths.append(root.appendingPathComponent(relative).path)
            }
        }
        for path in paths {
            let attributes = try FileManager.default.attributesOfItem(atPath: path)
            guard attributes[.type] as? FileAttributeType != .typeSymbolicLink,
                let mode = (attributes[.posixPermissions] as? NSNumber)?.intValue,
                mode & 0o200 == 0
            else { continue }
            try FileManager.default.setAttributes([.posixPermissions: mode | 0o200], ofItemAtPath: path)
        }
    }
}
