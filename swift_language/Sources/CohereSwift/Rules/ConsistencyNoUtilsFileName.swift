import Foundation

/*
 No `util` or `utils` as a word of a file's name, as `ConsistencyNoUtilsFolder` allows neither as a folder's:
 `Utils.swift`, `ViewUtil.swift` and `String+Utils.swift` are each reported once (#2xt6v5r, the text rules family
 `utils-file-name`). A file named for nothing in particular collects whatever has no other home, which is what a
 folder of that name does, one level down. The Swift form of `nexus/consistency-no-utils-file-name` and
 `cohere-go/consistency-no-utils-file-name`, which read cohere's `internal/file_name_words`.

 The name's words are `FileNameWords`', each held whole to `ConsistencyNoUtilsFolder.isBanned`, so `Utilities.swift`
 and `Myutils.swift` pass, as `Utilities` and `myutils` do as folders. A generated file, `Package.swift` and
 `main.swift` are not judged, and nor is a file under a folder whose files are someone else's.

 It reads only the path, never the tree, so there is no syntax visitor. One finding per file, at line 1, column 1,
 zero width, as the folder rules report a misplaced file.
 */
public struct ConsistencyNoUtilsFileName: FileRule {
    public let name = "cohere-swift/consistency-no-utils-file-name"
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        guard let words = FileNameWords.words(of: file), let word = FileNameWords.utilsWord(in: words) else {
            return []
        }
        let message = RuleMessages.ConsistencyNoUtilsFileName.utilsFileName(
            fileName: file.url.lastPathComponent,
            word: word,
            replacement: FileNameWords.utilitiesReplacement(for: word),
        )
        return [FileNameWords.finding(in: file, rule: name, message: message)]
    }
}
