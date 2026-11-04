import CohereSwift
import Foundation
import Testing

/* A flag accepted and ignored reads as a run that did what was asked, so every flag either means something or is refused. */
struct CommandOptionsTests {
    static let here = URL(fileURLWithPath: "/work", isDirectory: true)

    @Test func aBareRunRunsEveryPhaseAndMutates() throws {
        let options = try CommandOptions.parse(
            ["--contract", "\(EngineVersion.contract)", "--root", "/work"],
            workingDirectory: Self.here,
        )
        #expect(options.runFix && options.runTypes && options.runLint && options.mutate)
    }

    @Test func oneDashIsTheSameAsTwo() throws {
        let options = try CommandOptions.parse(["-lint", "-no-fix"], workingDirectory: Self.here)
        #expect(options.runLint && !options.runTypes && !options.runFix && options.noFix)
    }

    @Test(arguments: [
        ["--timing"], ["--explain", "A.swift"], ["--tsconfig", "x"], ["--bogus"], ["--changed"], ["--fix", "--no-fix"],
        ["--contract", "1"], ["--fix-passes", "0"],
        ["--format-only", "--fix"], ["--format-only", "--types"], ["--format-only", "--lint"],
        ["--format-only", "--unused"], ["--no-format", "--format"], ["--no-format", "--format-all"],
        ["--no-format", "--format-only"],
    ])
    func refusedCommandLines(arguments: [String]) {
        #expect(throws: CommandOptions.UsageFailure.self) {
            try CommandOptions.parse(arguments, workingDirectory: Self.here)
        }
    }

    /* `--format-only` is the fix phase narrowed to formatting, as in TypeScript: no fix proposed, no other phase. */
    @Test func formatOnlyFormatsAndNothingElse() throws {
        let writing = try CommandOptions.parse(["--format-only"], workingDirectory: Self.here)
        #expect(writing.runFix && writing.runFormat && writing.mutate && !writing.proposeFixes)
        #expect(!writing.runTypes && !writing.runLint)
        let gate = try CommandOptions.parse(
            ["--format-only", "--no-fix", "--format-all"],
            workingDirectory: Self.here,
        )
        #expect(gate.runFix && gate.runFormat && !gate.mutate && !gate.proposeFixes && !gate.runTypes && !gate.runLint)
    }

    /* `--no-format` keeps every phase and leaves the formatter out, with `--fix` and with `--no-fix` too. */
    @Test func noFormatKeepsThePhasesAndLeavesFormattingOut() throws {
        let bare = try CommandOptions.parse(["--no-format"], workingDirectory: Self.here)
        #expect(bare.runFix && bare.runTypes && bare.runLint && bare.proposeFixes && !bare.runFormat)
        let fixing = try CommandOptions.parse(["--no-format", "--fix"], workingDirectory: Self.here)
        #expect(fixing.proposeFixes && !fixing.runFormat && !fixing.runTypes)
        let checking = try CommandOptions.parse(["--no-format", "--no-fix"], workingDirectory: Self.here)
        #expect(checking.runFix && !checking.runFormat && !checking.proposeFixes && checking.runLint)
    }

    @Test func pathsAreCollected() throws {
        let options = try CommandOptions.parse(["Sources/A", "--no-fix", "B.swift"], workingDirectory: Self.here)
        #expect(options.paths == ["Sources/A", "B.swift"])
    }
}
