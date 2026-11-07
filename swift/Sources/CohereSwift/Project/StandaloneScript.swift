import Foundation
import SwiftParser
import SwiftSyntax

/*
 A standalone Swift script: a file run as `swift <file>`, which no package target and no Xcode target compiles, so the
 run checks it on its own (#13nbb1s, @system_cohere's rulings of 2026-11-06). ahraos-presence's two `Glyph.swift`
 files, which `GenerateIcon.sh` runs to draw an icon's glyph, were the first: named as checked by nothing until then.

 - A file no target compiles is a script when it has a `#!` line or a statement at its top level, the code `swift`
   runs. A file of declarations only runs nothing, so it is a file that belongs in a target, and it stays the named
   gap it was.
 - A script is checked as `swift` runs it, alone, so one that needs companion files to compile (one swiftc'd with an
   app's sources) does not compile alone. That is true, and the fix is giving it a package, so no companion is ever
   guessed at.
 - Its platform is its imports': AppKit or Cocoa means macOS, UIKit means iOS, and anything else (SwiftUI,
   Foundation, nothing at all) the macOS host. Imports naming both are never guessed between: the script is left
   unchecked, and the reason says which imports disagree.
 - Its kind is a command-line tool's, whatever it imports: `swift <file>` runs it as a process whose standard output
   goes to the shell that ran it, as `GenerateIcon.sh` runs `Glyph.swift`, so it prints as a tool prints, AppKit or
   not (@system_cohere's ruling of 2026-11-07). Reading an AppKit script as an app was a false `print` finding.
 */
enum StandaloneScript {
    /* The SDK a script is checked against. */
    enum Platform: Hashable, Sendable {
        case macOS
        case iOS
    }

    /* What a file no target compiles is to the run, read from its text. */
    enum Reading: Equatable {
        case script(Platform)
        case declarationsOnly
        /* A script whose imports name both platforms, with the words that say which. */
        case platformsDisagree(String)
    }

    /* The imports that name macOS, and the one that names iOS. */
    static let macOSImports: Set<String> = ["AppKit", "Cocoa"]
    static let iOSImports: Set<String> = ["UIKit"]
    /* A script's target kind, SwiftPM's word for a command-line tool, which `consistency-no-print` lets print. */
    static let targetKind = "executable"

    static func read(source: String) -> Reading {
        let tree = Parser.parse(source: source)
        guard tree.shebang != nil || hasTopLevelCode(tree.statements) else { return .declarationsOnly }
        /* Every branch of every `#if`, as the types phase reads imports, so a branch this check would not compile still counts. */
        let imports = TypesPhase.importedModules(of: tree)
        let macOS = imports.intersection(macOSImports).sorted()
        let iOS = imports.intersection(iOSImports).sorted()
        if !macOS.isEmpty, !iOS.isEmpty {
            return .platformsDisagree(
                "it imports \(macOS.joined(separator: " and ")) for macOS and \(iOS.joined(separator: " and ")) for iOS"
            )
        }
        return .script(iOS.isEmpty ? .macOS : .iOS)
    }

    /*
     Whether these statements, or those in any branch of an `#if` among them, hold one that is not a declaration. A
     top-level `let` is a declaration, a global in any file, so a file of constants is not a script.
     */
    static func hasTopLevelCode(_ statements: CodeBlockItemListSyntax) -> Bool {
        statements.contains { statement in
            switch statement.item {
                case .stmt, .expr:
                    return true
                case .decl(let declaration):
                    guard let condition = declaration.as(IfConfigDeclSyntax.self) else { return false }
                    return condition.clauses.contains { clause in
                        guard case .statements(let nested) = clause.elements else { return false }
                        return hasTopLevelCode(nested)
                    }
            }
        }
    }

    /*
     The module `swift <file>` compiles a script into: the file's stem, or `main` when the stem is no identifier, as the
     driver falls back for a script it runs (measured 2026-11-06: `print(#fileID)` in `Which.swift` prints
     `Which/Which.swift`).
     */
    static func moduleName(of file: URL) -> String {
        let stem = file.deletingPathExtension().lastPathComponent
        guard let first = stem.first, first.isLetter || first == "_",
            stem.allSatisfy({ $0.isLetter || $0.isNumber || $0 == "_" })
        else { return "main" }
        return stem
    }

    /*
     The arguments one script is checked with, as the driver takes them: the house's settings, as every package target
     of ours is held to them (swift/Package.swift's `houseSettings`), in one shape for every script. Swift 6 language
     mode, the upcoming features `toolchain-require-upcoming-features` requires, read from that rule, and strict memory
     safety with its warnings as errors. Then the SDK, and the triple when the script is not checked for the host.
     */
    static func arguments(module: String, file: URL, sdk: String, target: String?) -> [String] {
        var arguments = ["-module-name", module, "-swift-version", "6"]
        for required in ToolchainRequireUpcomingFeatures.required {
            arguments += ["-enable-upcoming-feature", required.feature]
        }
        arguments += ["-strict-memory-safety", "-Werror", "StrictMemorySafety", "-sdk", sdk]
        if let target {
            arguments += ["-target", target]
        }
        return arguments + [file.resolvingSymlinksInPath().path]
    }
}
