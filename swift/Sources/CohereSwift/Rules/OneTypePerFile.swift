import Foundation
import SwiftSyntax

/*
 A file holds its main type, plus the small types only it uses.

 Kirk's ruling (2026-10-03, #r3hfpe8), loosening the strict one-type-per-file of before. A file is the
 unit a reader opens to understand one thing, and a type is found by its name without a search. The strict
 form paid for that with files of fifteen lines and with helpers nested inside a type only to satisfy the
 rule, which made the outer type harder to read without making anything easier to find. So beside the
 file's own type (the one its name matches, else the first; see `TopLevelDeclarations.primary`) a
 top-level type may stay when it is:
 - `private` or `fileprivate`, of any kind and size. It cannot be used outside this file, so nobody looks
   for it anywhere else, and moving it out would force it to `internal`, widening access to satisfy a
   placement rule.
 - A struct or enum of at most 30 lines: a small value type, such as a message's schema or a row of a
   table. The lines run from its first token to its closing brace, counting its attributes and the comment
   directly above it (no blank line between), and counting every extension of it in this file too, so a
   ten-line struct with a three-hundred-line extension beside it is not small. A class, actor or protocol
   is never small this way: each is a thing with identity or a contract other code conforms to, which a
   reader looks for by name.
 Everything else is reported at its name, so the finding points at the code that has to move. Nothing is
 ever asked to nest: a nested type is the right shape when it belongs to its outer type's meaning, never as
 a way past this rule.

 "Only it uses" is guaranteed for a private helper by its access level. It is not checked for a small
 internal value type: proving no other file names it takes the whole package's references, which a rule
 that sees one file does not have. That half of the ruling is the author's to keep.

 The size of the file itself is a separate check (`MaxFileLines`). Counting types never caught the real
 problem, a file of thousands of lines that declares one type.

 Counts struct, class, enum, actor and protocol declarations at top level, including inside a top-level
 `#if` (see TopLevelDeclarations). A type declared in two branches is judged by its first declaration.
 */
public struct OneTypePerFile: FileRule {
    public let name = "cohere-swift/one-type-per-file"

    /* The most lines a struct or enum may have and still stay beside the file's own type. */
    static let smallValueTypeLines = 30

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let declarations = TopLevelDeclarations(file.tree)
        guard let primary = declarations.primary(stem: file.url.deletingPathExtension().lastPathComponent) else { return [] }
        return declarations.types.filter { $0.name != primary.name }.compactMap { extra in
            guard let declaration = extra.token.parent.flatMap({ DeclSyntax($0) }) else { return nil }
            if Self.isHelper(extra, declarations: declarations, in: file) {
                return nil
            }
            let isValueType = declaration.is(StructDeclSyntax.self) || declaration.is(EnumDeclSyntax.self)
            let lines = Self.lines(of: extra, declaration: declaration, declarations: declarations, in: file)
            let why = isValueType
                ? "it is \(lines) lines, over the \(Self.smallValueTypeLines) a small value type may have"
                : "it is \(Self.kindPhrase(of: declaration)) that is not private"
            return file.finding(
                at: extra.token,
                rule: name,
                messageId: "moreThanOneType",
                message: "\(extra.name) is a second top-level type in the file for \(primary.name), and \(why). Move it to \(extra.name).swift, or keep it here only if it is a helper this file alone uses (mark it private) or a small value type (a struct or enum of at most \(Self.smallValueTypeLines) lines)."
            )
        }
    }

    /*
     A type that may sit beside a file's own type, or, in a `Type+Purpose.swift` file, beside the extension the
     file is named for (FileNamedForType asks the same question): private or fileprivate of any size, or a struct
     or enum of at most `smallValueTypeLines`.
     */
    static func isHelper(_ type: TopLevelDeclarations.Entry, declarations: TopLevelDeclarations, in file: ParsedFile) -> Bool {
        guard let declaration = type.token.parent.flatMap({ DeclSyntax($0) }) else { return false }
        if isFilePrivate(declaration) {
            return true
        }
        let isValueType = declaration.is(StructDeclSyntax.self) || declaration.is(EnumDeclSyntax.self)
        return isValueType && lines(of: type, declaration: declaration, declarations: declarations, in: file) <= smallValueTypeLines
    }

    /* The type's own lines plus those of every extension of it in this file. */
    static func lines(of type: TopLevelDeclarations.Entry, declaration: DeclSyntax, declarations: TopLevelDeclarations, in file: ParsedFile) -> Int {
        lineCount(of: declaration, in: file) + declarations.extensions
            .filter { $0.name == type.name }
            .compactMap(\.extensionDeclaration)
            .map { lineCount(of: DeclSyntax($0), in: file) }
            .reduce(0, +)
    }

    /* What a type that can never be small is called in the message: a class, an actor or a protocol. */
    static func kindPhrase(of declaration: DeclSyntax) -> String {
        if declaration.is(ActorDeclSyntax.self) { return "an actor" }
        if declaration.is(ProtocolDeclSyntax.self) { return "a protocol" }
        return "a class"
    }

    static func isFilePrivate(_ declaration: DeclSyntax) -> Bool {
        guard let modifiers = declaration.asProtocol((any WithModifiersSyntax).self)?.modifiers else { return false }
        return modifiers.contains { $0.name.tokenKind == .keyword(.private) || $0.name.tokenKind == .keyword(.fileprivate) }
    }

    /*
     The lines a declaration occupies, from the comment directly above it (or its first attribute or keyword)
     to its closing brace. A comment counts when no blank line separates it from the declaration, so a file
     header above the first type is never billed to it.
     */
    static func lineCount(of declaration: DeclSyntax, in file: ParsedFile) -> Int {
        guard let firstToken = declaration.firstToken(viewMode: .sourceAccurate) else { return 0 }
        var start = firstToken.positionAfterSkippingLeadingTrivia
        var position = firstToken.position
        var attachedComment: AbsolutePosition?
        /* Line breaks since the last comment; two of them, with only spaces between, make a blank line that detaches every comment above it. */
        var lineBreaks = 0
        for piece in firstToken.leadingTrivia {
            switch piece {
            case .newlines(let count), .carriageReturns(let count), .carriageReturnLineFeeds(let count):
                lineBreaks += count
                if lineBreaks > 1 {
                    attachedComment = nil
                }
            default:
                if piece.isComment {
                    attachedComment = attachedComment ?? position
                    lineBreaks = 0
                }
            }
            position = position.advanced(by: piece.sourceLength.utf8Length)
        }
        if let attachedComment {
            start = attachedComment
        }
        let startLine = file.locations.location(for: start).line
        let endLine = declaration.endLocation(converter: file.locations).line
        return endLine - startLine + 1
    }
}
