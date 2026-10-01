import Foundation
import SwiftSyntax

/*
 A file is named for what it holds: `Type.swift` for the type it declares, and `Type+Purpose.swift` for
 extensions of a type it does not declare.

 Kirk's ruling, with one-type-per-file. A reader looking for `ScrollbackBuffer` opens
 `ScrollbackBuffer.swift`, and a reader looking for what was added to `String` finds every `String+…`
 file together. The Swift form of our TypeScript `require-matching-file-name`.

 - A file declaring a type is named for it. Extensions of that same type may sit beside it.
 - An extension of any other type belongs in that type's `+Purpose` file. A nested type can be named
   either way: `Outer.Inner+Purpose.swift` or `Outer+Purpose.swift`.
 - `main.swift` keeps its name, which SwiftPM gives meaning to (top-level code), so naming is not judged
   there. One-type-per-file still is.
 - A file with no types and no extensions (free functions, a script) has no name to match.
 */
public struct FileNamedForType: FileRule {
    public let name = "cohere-swift/file-named-for-type"

    public init() {}

    public func applies(to file: ParsedFile) -> Bool {
        file.url.lastPathComponent != "main.swift"
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let stem = file.url.deletingPathExtension().lastPathComponent
        let declarations = TopLevelDeclarations(file.tree)
        var findings: [FindingRecord] = []

        let primary = declarations.primary(stem: stem)
        if let declared = primary, declared.name != stem {
            findings.append(file.finding(
                at: declared.token,
                rule: name,
                messageId: "fileNotNamedForType",
                message: "This file is \(stem).swift and declares \(declared.name). Name it \(declared.name).swift, so a reader looking for \(declared.name) opens it without a search."
            ))
        }

        let declaredName = primary?.name
        for extended in declarations.extensions where extended.name != declaredName {
            if Self.isPurposeFile(stem: stem, for: extended.name) {
                continue
            }
            let suggestion = "\(extended.name.split(separator: ".").first.map(String.init) ?? extended.name)+Purpose.swift"
            let message = declaredName.map {
                "An extension of \(extended.name) in the file for \($0). Extensions of another type belong in that type's own file, such as \(suggestion), so everything added to \(extended.name) is found together."
            } ?? "This file is \(stem).swift and extends \(extended.name). Name it \(suggestion) after what it adds, so everything added to \(extended.name) is found together."
            findings.append(file.finding(at: extended.token, rule: name, messageId: "extensionOutsideItsFile", message: message))
        }
        return findings
    }

    /* `Type+Purpose`, or `Outer+Purpose` for a nested `Outer.Inner`, or exactly the extended name. */
    static func isPurposeFile(stem: String, for extended: String) -> Bool {
        if stem == extended || stem.hasPrefix(extended + "+") {
            return true
        }
        if let outer = extended.split(separator: ".").first, extended.contains(".") {
            return stem == String(outer) || stem.hasPrefix(outer + "+")
        }
        return false
    }
}
