import Foundation

/*
 How the fix phase writes a source file, the one place the engine writes one (#723rzxm). A file is reached by the path
 the file set gave it, which may run through a symbolic link, a linked file or a linked directory, so the write goes to
 its real path, every link resolved. A link inside the run's root is written through to its target and stays a link,
 and a file whose real path lies outside the root is never written: it is checked, read only, and named in the fix
 record's `notWritten`. A temporary file and a rename at the path as reached would replace a linked file with a plain
 one and leave its target as it was, and follow a linked directory out of the root into someone else's files.
 */
enum SourceWrite {
    /* Where a file the phase would write really is, and whether the run may write there. */
    enum Destination: Equatable {
        case inside(realPath: String)
        case outside(realPath: String)
    }

    /* The path with every link resolved, as realpath(3) gives it, or nil when it does not resolve. */
    static func realPath(of url: URL) -> String? {
        guard let resolved = unsafe realpath(url.path, nil) else { return nil }
        defer { unsafe free(resolved) }
        return unsafe String(cString: resolved)
    }

    /*
     Where the file at this URL really is, against the root's real path. A file that no longer resolves is outside:
     it was read a moment ago, and writing nothing is the side a doubt falls on, so it is named and left as it is.
     */
    static func destination(of url: URL, root realRoot: String) -> Destination {
        guard let realPath = realPath(of: url) else { return .outside(realPath: url.path) }
        let prefix = realRoot.hasSuffix("/") ? realRoot : realRoot + "/"
        return realPath.hasPrefix(prefix) ? .inside(realPath: realPath) : .outside(realPath: realPath)
    }

    /*
     The text written to a real path: a temporary file in the path's own directory, given the file's permissions, then
     renamed onto it. The rename never crosses a filesystem and never replaces a link, since the path is the target
     itself, and an interrupted run leaves the old text or the new one, never half of either. The temporary file's name
     starts with a dot, so a walk that finds it after a crash skips it as it skips every hidden file.
     */
    static func write(_ text: String, toRealPath realPath: String) throws {
        let target = URL(fileURLWithPath: realPath)
        let permissions = try FileManager.default.attributesOfItem(atPath: realPath)[.posixPermissions]
        let temporary = target.deletingLastPathComponent()
            .appendingPathComponent(".\(target.lastPathComponent).cohere-\(UUID().uuidString)")
        try Data(text.utf8).write(to: temporary, options: .withoutOverwriting)
        do {
            if let permissions {
                try FileManager.default.setAttributes([.posixPermissions: permissions], ofItemAtPath: temporary.path)
            }
            guard unsafe rename(temporary.path, realPath) == 0 else {
                throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
            }
        }
        catch {
            do {
                try FileManager.default.removeItem(at: temporary)
            }
            catch {
                /* A hidden temporary file left beside the target; the write's own failure is what the run reports. */
            }
            throw error
        }
    }
}
