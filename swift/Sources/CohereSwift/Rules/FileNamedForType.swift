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
 - Two extensions of another type stay beside the file's type, because moving them would cost what the
   ruling is for. Both were raised by @system_cohere_swift_ahraos_macos on real code:
   - A `private` or `fileprivate` extension. It is used only in this file, and moving it out would force
     it to `internal`, widening access to satisfy a naming rule. This holds in a `Type+Purpose.swift` file
     too (the Circle's second question, on `Terminal+AuthoritativeCheckpoint.swift`'s `VtColor` helper), as
     long as the file has a visible face of its own: a declared type or a non-private extension it is
     named for. A file of nothing but private extensions answers to the plain naming rule.
   - An extension whose every member names the file's type. `extension View { func delayWidthUntilIdle()
     }` beside `struct DelayWidthUntilIdle: ViewModifier` is the modifier's public face, and putting it in
     `View+Something.swift` separates the modifier from its only entry point, the opposite of findability.
 - A `Type+Purpose.swift` file may hold helper types beside the visible extension it is named for: private or
   fileprivate of any size, or a struct or enum of at most 30 lines, the same helpers one-type-per-file lets
   sit beside a file's own type (Kirk's ruling of 2026-10-03, #r3hfpe8). Found by
   @system_cohere_swift_ahraos_presence un-nesting a probe file's helpers and Prune's `BinarySlice`: the
   ruling said they need not nest, and this rule then told the file to be renamed after the helper. A file
   whose only face is a helper still answers to the plain naming rule.
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
        /* A visible extension the file is named for: the face of a `Type+Purpose.swift` file. */
        let hasPurposeFace = declarations.extensions.contains { other in
            Self.isPurposeFile(stem: stem, for: other.name) && !(other.extensionDeclaration.map(Self.isFilePrivate) ?? false)
        }
        /* Every type here is a helper to that face, so the file is named for the face, not for a helper. */
        let helpersOnly = hasPurposeFace && !declarations.types.isEmpty
            && declarations.types.allSatisfy { OneTypePerFile.isHelper($0, declarations: declarations, in: file) }
        if let declared = primary, declared.name != stem, !helpersOnly {
            findings.append(file.finding(
                at: declared.token,
                rule: name,
                messageId: "fileNotNamedForType",
                message: "This file is \(stem).swift and declares \(declared.name). Name it \(declared.name).swift, so a reader looking for \(declared.name) opens it without a search."
            ))
        }

        let declaredName = helpersOnly ? nil : primary?.name
        let helperNames = helpersOnly ? Set(declarations.types.map(\.name)) : []
        /* The file has a public face: a type it declares, or a visible extension it is named for. A file-private helper may sit beside either. */
        let anchored = declaredName != nil || hasPurposeFace
        for extended in declarations.extensions where extended.name != declaredName && !helperNames.contains(extended.name) {
            if Self.isPurposeFile(stem: stem, for: extended.name) {
                continue
            }
            if let declaration = extended.extensionDeclaration, anchored, Self.isFilePrivate(declaration) {
                continue
            }
            if let declaredName, let declaration = extended.extensionDeclaration, Self.everyMemberNames(declaredName, in: declaration) {
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

    static func isFilePrivate(_ declaration: ExtensionDeclSyntax) -> Bool {
        declaration.modifiers.contains { $0.name.tokenKind == .keyword(.private) || $0.name.tokenKind == .keyword(.fileprivate) }
    }

    /* Every member mentions the type by name, in its signature or its body: the extension exists to expose that type. */
    static func everyMemberNames(_ typeName: String, in declaration: ExtensionDeclSyntax) -> Bool {
        let members = declaration.memberBlock.members
        guard !members.isEmpty else { return false }
        return members.allSatisfy { member in
            member.decl.tokens(viewMode: .sourceAccurate).contains { $0.tokenKind == .identifier(typeName) }
        }
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
