import Foundation

/*
 No folder named `util` or `utils` inside the package, in any casing and with any leading underscores: a
 folder's name is judged lowercased with its leading underscores removed, the verdict every engine gives in the
 text rules corpus (#4b337jk, family `utils-folder`; #4spf9wc). The repair is to rename it `utilities`, keeping
 the folder's own casing and underscores: `Utils` becomes `Utilities`, `_util` becomes `_utilities`.

 The abbreviation is the smaller half of the problem. A folder named for nothing in particular collects
 everything, and "utilities" is not meaningfully better at that, but the spelling is what the kingdom
 settled on and one spelling searched is worth two spellings guessed. The Swift form of our TypeScript
 `nexus/consistency-no-utils-folder`, with its two messages carried over.

 It reads only the path, never the tree, so there is no syntax visitor.

 - A folder, never a file. `Utils.swift` is a file name.
 - The whole name, never a piece of it. `utilities`, `myutils` and `utilsomething` all pass, because a rule
   that fires on the spelling it is asking for is worse than no rule.
 - `Helpers` and `Utility` are not banned.
 - One finding per file, for the outermost banned folder, at line 1, column 1, zero width, because the file as a
   whole is what is misplaced. Each file moved clears its own finding, which suits a per-file cache.
 - The rule applies to every file and answers with nothing for most of them, rather than declining them,
   so a clean package reads as watched and quiet, not as a rule that never looked.

 Only folders inside the package count, judged from `ParsedFile.packageRoot`, the root of the package whose
 target compiles the file. A checkout under `~/utils/` is not a finding anybody inside the package can act on
 (uf06). Every folder from the root down is judged, the target's own directory included. A file whose package
 root is unknown is not judged at all, rather than judged against a guess.
 */
public struct ConsistencyNoUtilsFolder: FileRule {
    public let name = "cohere-swift/consistency-no-utils-folder"
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        guard let folder = Self.foldersInsidePackage(of: file).first(where: Self.isBanned) else { return [] }
        let replacement = Self.replacement(for: folder)
        let message =
            folder.hasPrefix("_")
            ? RuleMessages.ConsistencyNoUtilsFolder.noUnderscoreUtils(folder: folder, replacement: replacement)
            : RuleMessages.ConsistencyNoUtilsFolder.noUtils(folder: folder, replacement: replacement)
        return [finding(in: file, message: message)]
    }

    /* Whether a folder's name, lowercased with its leading underscores removed, is `util` or `utils`. */
    static func isBanned(_ folder: String) -> Bool {
        ["util", "utils"].contains(String(folder.drop { $0 == "_" }).lowercased())
    }

    /*
     The name to use instead: the same leading underscores, then `utilities` in the casing the folder's first
     letter has, or all capitals when every letter is one.
     */
    static func replacement(for folder: String) -> String {
        let underscores = folder.prefix { $0 == "_" }
        let name = folder.dropFirst(underscores.count)
        if name.allSatisfy(\.isUppercase) {
            return underscores + "UTILITIES"
        }
        return underscores + (name.first?.isUppercase == true ? "Utilities" : "utilities")
    }

    /* The directories between the package root and the file, outermost first; empty when the root is unknown or the file is not under it. */
    static func foldersInsidePackage(of file: ParsedFile) -> ArraySlice<String> {
        guard let root = file.packageRoot else { return [] }
        let rootComponents = root.resolvingSymlinksInPath().standardizedFileURL.pathComponents
        let directories = file.url.resolvingSymlinksInPath().standardizedFileURL.pathComponents.dropLast()
        guard directories.starts(with: rootComponents) else { return [] }
        return directories.dropFirst(rootComponents.count)
    }

    /* A zero-width finding at the very start of the file, the place the Go rule reports a misplaced file. */
    private func finding(in file: ParsedFile, message: RuleMessages.Message) -> FindingRecord {
        FindingRecord(
            source: .rule,
            file: file.url.path,
            line: 1,
            column: 1,
            endLine: 1,
            endColumn: 1,
            severity: .error,
            rule: name,
            messageId: message.id,
            message: message.text,
        )
    }
}
