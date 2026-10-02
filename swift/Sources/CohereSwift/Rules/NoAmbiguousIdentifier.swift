import Foundation
import SwiftSyntax

/*
 No single-letter names: `let n = items.count`, `for i in rows`, `{ e in report(e) }`. The Swift form of
 `nexus/consistency-no-ambiguous-identifier`, with its judgment and its messages carried over.

 A name is read everywhere it is used and declared only once, so the keystrokes a single letter saves are
 saved at the declaration and paid for at every read. The repair is the word the letter stands for:
 `count`, `rowIndex`, `error`.

 Which names: declared names only, exactly the set `cohere-swift/no-abbreviated-identifier` judges, found by
 its own visitor (`NoAbbreviatedIdentifier.Visitor`). A reference is spelled by whoever declared it, and our
 own single-letter declaration is reported once, where it is written. That visitor already skips an
 `override`'s name and its single-name labels (the superclass chose them), a label that is not also the
 parameter's name (API another type may dictate), and `if let value` with no initializer (it re-binds a name
 declared elsewhere, where the finding belongs).

 Only a single lowercase ASCII letter is in scope, which is the Go rule's line. An uppercase single letter is
 a type parameter by convention (`<T>`), and the Go rule leaves it alone, so this rule does too, even though
 the house spells type parameters as words (`<Element>`). A lowercase generic parameter, `<t>`, is judged
 like any other name. One place the shared visitor is wider than the Go rule: a protocol's property
 requirement is judged here, where the Go rule skips a TypeScript interface's property signature as a name
 somebody else may have chosen. A protocol is declared by this file, so its names are ours to rename.

 Exemptions, each the Go rule's:
 - `x`, `y` and `z`, the coordinate names, where the single letter is the conventional spelling and a longer
   one reads worse.
 - `a` and `b` in a sort comparator, because that is the shape everyone reads and naming them is noise. The
   Go rule exempts an `a` or `b` whose nearest enclosing function is the first argument of a `.sort(...)`
   call on some object. The Swift comparators are `sort(by:)`, `sorted(by:)`, `min(by:)` and `max(by:)`, so
   here the nearest enclosing closure must be the only argument of a call to one of those four names on some
   base (`items.sorted { a, b in ... }` or `items.sorted(by: { a, b in ... })`): a trailing closure with no
   other argument, or the single argument labeled `by`. A bare `sorted { ... }` with no base is not a
   comparator the Go rule would recognize, and a nested closure inside the comparator is judged on its own,
   both as the Go rule does.

 `_` is not judged, which is where Swift parts from the Go rule's `noUnderscore`. In Swift `_` is the discard
 pattern and the empty label, not a name anyone has to track.

 `e` gets its own message, because it is the ambiguous one: it could be an error or an event, and which one
 decides the right name. The rule infers from context and says what it inferred, the Go rule's inference
 carried to Swift's shapes:
 - error: `catch let e`, or `catch let e as SomeError`, binds the thrown error itself. A payload bound in a
   catch pattern, `catch Failure.denied(let e)`, is not the error and falls through to the checks below.
 - event: an argument or trailing closure labeled `onSomething:` (the Go rule's `{ onClick: (e) => ... }` and
   `onClick={(e) => ...}`), a binding named like a handler whose initializer holds the `e`
   (`let handleClick = { e in ... }`), or a function named like a handler (`func handleKey(_ e: Key)`).
   "Named like a handler" is the Go rule's case-insensitive `handle|on[A-Z]|event`, which under case folding
   matches "on" followed by any letter.
 - otherwise "event", marked "context unclear", as the Go rule does.

 No fix, for the Go rule's reason: renaming a binding without following its references through scope leaves
 every other use pointing at a name that no longer exists. The suggested name travels in the message.
 */
public struct NoAmbiguousIdentifier: FileRule {
    public let name = "cohere-swift/no-ambiguous-identifier"

    public init() {}

    /* The coordinate and math names, where the single letter is the conventional spelling. */
    static let alwaysAllowedSingleLetters: Set<String> = ["x", "y", "z"]

    /* The Swift spellings of the Go rule's `.sort(...)`: every standard library method that takes a comparator closure. */
    static let comparatorMethodNames: Set<String> = ["sort", "sorted", "min", "max"]

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let visitor = NoAbbreviatedIdentifier.Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.declared.compactMap { token in
            let spelled = token.text.trimmingCharacters(in: CharacterSet(charactersIn: "`"))
            guard Self.isSingleLowercaseLetter(spelled), !Self.alwaysAllowedSingleLetters.contains(spelled) else { return nil }
            if spelled == "e" {
                let inference = Self.inferEventOrError(token)
                return file.finding(
                    at: token,
                    rule: name,
                    messageId: "noAmbiguousE",
                    message: "Variable named \"e\" is too ambiguous\(inference.contextHint). It is the one name that could be an error or an event, and a reader has to find the declaration to learn which. Use \"\(inference.suggestedName)\" or a more descriptive name."
                )
            }
            /* A sort comparator is the one place a and b read correctly. */
            if spelled == "a" || spelled == "b", Self.isInsideSortComparator(token) {
                return nil
            }
            return file.finding(
                at: token,
                rule: name,
                messageId: "noSingleLetter",
                message: "Single-letter identifier \"\(spelled)\" is not descriptive enough. The name is read everywhere it is used and declared only once, so the saving is at the declaration and the cost is at every call site."
            )
        }
    }

    /* The Go rule's `^[a-z]$`: one ASCII lowercase letter, nothing else. */
    static func isSingleLowercaseLetter(_ name: String) -> Bool {
        guard name.utf8.count == 1, let byte = name.utf8.first else { return false }
        return byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z")
    }

    /*
     The Go rule's case-insensitive `handle|on[A-Z]|event`. Case folding turns `[A-Z]` into any ASCII letter,
     so `on` followed by a letter anywhere matches, and so does `Handle` or `EVENT`. Written out by hand rather
     than as a regular expression so the folding is visible.
     */
    static func isNamedLikeHandler(_ name: String) -> Bool {
        let loweredName = name.lowercased()
        if loweredName.contains("handle") || loweredName.contains("event") {
            return true
        }
        let lowered = Array(loweredName.utf8)
        guard lowered.count >= 3 else { return false }
        for start in 0..<(lowered.count - 2) where lowered[start] == UInt8(ascii: "o") && lowered[start + 1] == UInt8(ascii: "n") {
            let following = lowered[start + 2]
            if following >= UInt8(ascii: "a") && following <= UInt8(ascii: "z") {
                return true
            }
        }
        return false
    }

    /* The Go rule's `^on[A-Z]`, case-sensitive: `onClick`, never `online`. */
    static func isEventHandlerKey(_ label: String) -> Bool {
        let bytes = Array(label.utf8)
        guard bytes.count >= 3, bytes[0] == UInt8(ascii: "o"), bytes[1] == UInt8(ascii: "n") else { return false }
        return bytes[2] >= UInt8(ascii: "A") && bytes[2] <= UInt8(ascii: "Z")
    }

    /*
     Whether the nearest closure around the token is the comparator handed to `sort`, `sorted`, `min` or `max`
     on some base: the call's trailing closure with no other argument, or its single argument, labeled `by`.
     The nearest closure decides, as the nearest function does in the Go rule, so an `a` in a closure nested
     inside a comparator is judged.
     */
    static func isInsideSortComparator(_ token: TokenSyntax) -> Bool {
        guard let closure = token.parent?.ancestorOrSelf(mapping: { $0.as(ClosureExprSyntax.self) }) else { return false }
        let call: FunctionCallExprSyntax
        if let argument = closure.parent?.as(LabeledExprSyntax.self) {
            guard argument.label?.text == "by", let list = argument.parent?.as(LabeledExprListSyntax.self), list.count == 1,
                let owner = list.parent?.as(FunctionCallExprSyntax.self), owner.trailingClosure == nil
            else { return false }
            call = owner
        } else if let owner = closure.parent?.as(FunctionCallExprSyntax.self), owner.trailingClosure?.id == closure.id {
            guard owner.arguments.isEmpty, owner.additionalTrailingClosures.isEmpty else { return false }
            call = owner
        } else {
            return false
        }
        guard let callee = call.calledExpression.as(MemberAccessExprSyntax.self), callee.base != nil else { return false }
        return comparatorMethodNames.contains(callee.declName.baseName.text)
    }

    /* The suggestion and the reason for it, said in the message so a reader can disagree with the reasoning and not only the verdict. */
    struct Inference {
        let suggestedName: String
        let contextHint: String
    }

    static func inferEventOrError(_ token: TokenSyntax) -> Inference {
        if isCaughtError(token) {
            return Inference(suggestedName: "error", contextHint: " (appears to be an error)")
        }
        let event = Inference(suggestedName: "event", contextHint: " (appears to be an event)")
        var current = token.parent
        while let node = current {
            if let parent = node.parent {
                /* A handler passed as `Button(onTap: { e in ... })`. */
                if let argument = parent.as(LabeledExprSyntax.self), argument.expression.id == node.id,
                    let label = argument.label, isEventHandlerKey(label.text)
                {
                    return event
                }
                /* A handler passed as a labeled trailing closure, `} onChange: { e in ... }`. */
                if let element = parent.as(MultipleTrailingClosureElementSyntax.self), element.closure.id == node.id,
                    isEventHandlerKey(element.label.text)
                {
                    return event
                }
                /* Assigned to something named like a handler, `let handleClick = { e in ... }`. */
                if let binding = parent.as(PatternBindingSyntax.self), binding.initializer?.id == node.id,
                    let identifier = binding.pattern.as(IdentifierPatternSyntax.self), isNamedLikeHandler(identifier.identifier.text)
                {
                    return event
                }
            }
            /* A function named like a handler, `func handleKey(_ e: Key)`. */
            if let function = node.as(FunctionDeclSyntax.self), isNamedLikeHandler(function.name.text) {
                return event
            }
            current = node.parent
        }
        return Inference(suggestedName: "event", contextHint: " (context unclear)")
    }

    /*
     Whether the token is the thrown error a `catch` binds: `catch let e`, `catch var e`, or `catch let e as
     SomeError`. The walk from the name to the catch item may pass only through the binding keyword and the
     cast, so a payload bound inside a pattern, `catch Failure.denied(let e)`, is not the error.
     */
    static func isCaughtError(_ token: TokenSyntax) -> Bool {
        guard let identifier = token.parent?.as(IdentifierPatternSyntax.self) else { return false }
        var current = Syntax(identifier)
        while let parent = current.parent {
            if parent.is(CatchItemSyntax.self) {
                return true
            }
            if parent.is(ValueBindingPatternSyntax.self) || parent.is(PatternExprSyntax.self) || parent.is(ExpressionPatternSyntax.self)
                || parent.is(AsExprSyntax.self)
            {
                current = parent
                continue
            }
            /* The unfolded `e as SomeError` is the sequence `e`, `as SomeError`; the name must lead it. */
            if let elements = parent.as(ExprListSyntax.self), elements.first?.id == current.id,
                elements.dropFirst().first?.is(UnresolvedAsExprSyntax.self) == true, elements.count == 3,
                let sequence = elements.parent?.as(SequenceExprSyntax.self)
            {
                current = Syntax(sequence)
                continue
            }
            return false
        }
        return false
    }
}
