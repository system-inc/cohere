import Foundation

/*
 cohere's cache directory in a project, `<root>/.cache/cohere`, made so that it ignores itself: the call that creates
 it writes a `.gitignore` in it holding `*`, so a run in a repository whose own rules say nothing of `.cache/` leaves
 `git status` as it found it (#6vhp0vd). The front door's `internal/cache_directory` makes it the same way, with the
 same bytes, for its table and the Go engine's store; whichever of them creates the directory first writes the file.
 Only the call that created the directory writes it, and never over a file that is there.
 */
enum CacheDirectory {
    /* The ignore file's bytes, the same as `cache_directory.IgnoreFileContents`. */
    static let ignoreFileContents = "# cohere's cache, made by cohere and ignored whole, this file with it.\n*\n"

    /* root's cache directory, the parent of the engine's scratch (`Pipeline.scratchPath(for:)`). */
    static func of(root: URL) -> URL {
        root.appendingPathComponent(".cache/cohere", isDirectory: true)
    }

    /*
     Makes directory and every parent it needs, and writes the ignore file in it when this call is the one that
     created it. Its parents are made with intermediate directories, and directory itself without, since only then
     does the call fail when the directory is there, which is how it knows it did not create it. The file is written
     without overwriting (`O_EXCL`), so one put there in between is never written over.
     */
    static func make(_ directory: URL) throws {
        let manager = FileManager.default
        try manager.createDirectory(at: directory.deletingLastPathComponent(), withIntermediateDirectories: true)
        do {
            try manager.createDirectory(at: directory, withIntermediateDirectories: false)
        }
        catch CocoaError.fileWriteFileExists {
            if (try? directory.resourceValues(forKeys: [.isDirectoryKey]))?.isDirectory == true {
                return
            }
            throw CocoaError(.fileWriteFileExists, userInfo: [NSFilePathErrorKey: directory.path])
        }
        do {
            try Data(ignoreFileContents.utf8).write(
                to: directory.appendingPathComponent(".gitignore"),
                options: .withoutOverwriting,
            )
        }
        catch CocoaError.fileWriteFileExists {
            /* A file put there since the directory was made is the project's, and is kept as it is. */
        }
    }
}
