import Foundation
import SwiftSyntax

/*
 One top-level type per file, whatever its access level.

 Kirk's ruling, strict, matching his TypeScript ruling of one component per file: a file holds one type,
 and a helper type becomes a nested type or moves to its own file. A file is then the unit a reader opens
 to understand one thing, and a type is found by its name without a search.

 Counts struct, class, enum, actor and protocol declarations at top level, including inside a top-level
 `#if` (see TopLevelDeclarations). Every type other than the file's own (the one its name matches, else
 the first) is reported at its name, so the finding points at the code that has to move.
 */
public struct OneTypePerFile: FileRule {
    public let name = "cohere-swift/one-type-per-file"

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let declarations = TopLevelDeclarations(file.tree)
        guard let first = declarations.primary(stem: file.url.deletingPathExtension().lastPathComponent) else { return [] }
        return declarations.types.filter { $0.name != first.name }.map { extra in
            file.finding(
                at: extra.token,
                rule: name,
                messageId: "moreThanOneType",
                message: "\(extra.name) is a second top-level type in a file that already holds \(first.name). One type per file: nest \(extra.name) inside the type that uses it, or move it to \(extra.name).swift."
            )
        }
    }
}
