import Foundation
import Testing

@testable import CohereSwift

/*
 A plain run's formatting through the whole pipeline (#gqw7z1w): the fixer's output is formatted whatever the format
 store says, and kept under the bytes the run wrote, and an unchanged run skips both the fixer and the formatter for
 a file and lints it from its kept verdict. Serialized, as the lint cache's tests are, since a run that checks a
 package starts a build server and a pool of frontends.
 */
@Suite(.serialized)
struct PlainRunFormatTests {
    static let control = "Sources/Control/Control.swift"

    /* The format store's verdict for one file of the package, by its path from the root. */
    static func verdict(_ package: LintVerdictCacheTests.Package, _ path: String) -> FormatVerdictCache.Verdict? {
        FormatVerdictCache(
            scratchPath: Pipeline.scratchPath(for: package.root),
            configuration: HouseSwiftFormat.configuration,
        )
        .stored[package.root.appendingPathComponent(path).path]
    }

    /* A verdict of formatted, which no formatter gives these texts, planted for each path from the root. */
    static func plantFormatted(_ package: LintVerdictCacheTests.Package, _ texts: [String: String]) {
        var verdicts: [String: FormatVerdictCache.Verdict] = [:]
        for (path, text) in texts {
            verdicts[package.root.appendingPathComponent(path).path] = .init(
                source: FormatVerdictCache.fingerprint(of: text),
                firstChangedLine: nil,
            )
        }
        FormatVerdictCache(
            scratchPath: Pipeline.scratchPath(for: package.root),
            configuration: HouseSwiftFormat.configuration,
        )
        .store(verdicts)
    }

    static func text(_ package: LintVerdictCacheTests.Package, _ path: String) throws -> String {
        try String(contentsOf: package.root.appendingPathComponent(path), encoding: .utf8)
    }

    /*
     The fixer turns `fileprivate` into `private` and leaves the space after it, which the formatter takes out. The
     store holds the fixed text as formatted, and the formatter still runs, since the fixer moved the bytes. A file
     beside it under the same planted verdict is left as it is, the control that the plant is read at that path. Red
     under: Pipeline handing the format phase no `fixed` paths (the double space is written), and the format phase
     keeping a rewritten file under the bytes it was handed.
     */
    @Test func aFileTheFixerRewroteIsFormattedAndKeptUnderTheBytesWritten() async throws {
        let other = "Sources/Control/Other.swift"
        let package = try LintVerdictCacheTests.Package(files: [
            Self.control: "fileprivate  let limit = 3\n",
            other: "let  other = 1\n",
        ])
        Self.plantFormatted(package, [Self.control: "private  let limit = 3\n", other: "let  other = 1\n"])
        let run = try await package.run(["--fix"])
        #expect(
            run.records.first { $0["kind"] as? String == "fix" }?["fixesApplied"] as? Int == 1,
            "the control: the fixer moved the file",
        )
        #expect(try Self.text(package, other) == "let  other = 1\n", "the control: the planted verdict is read")
        let written = "private let limit = 3\n"
        #expect(try Self.text(package, Self.control) == written)
        #expect(
            Self.verdict(package, Self.control)
                == .init(source: FormatVerdictCache.fingerprint(of: written), firstChangedLine: nil)
        )
    }

    /*
     `--format-all` formats every file, a file the store holds as formatted among them, as TypeScript's flag does. A run
     without it leaves the file as it is first, the control that the planted verdict is read. Red under: the format
     phase reading the store under `--format-all`, and Pipeline not handing the flag on.
     */
    @Test func formatAllFormatsAFileHeldAsFormatted() async throws {
        let package = try LintVerdictCacheTests.Package(files: [Self.control: "let  value = 1\n"])
        Self.plantFormatted(package, [Self.control: "let  value = 1\n"])
        _ = try await package.run(["--fix"])
        #expect(
            try Self.text(package, Self.control) == "let  value = 1\n",
            "the control: the planted verdict is read",
        )
        _ = try await package.run(["--fix", "--format-all"])
        #expect(try Self.text(package, Self.control) == "let value = 1\n")
    }

    /*
     The two stores compose. An unchanged plain run skips the fixer on the file's kept lint verdict and the formatter
     on its kept format verdict, writes no file and leaves the format store as it is, and lint reads the kept verdict,
     with the findings the first run's walk gave. Red under: the fixer walking a file whose kept verdict proposes no
     fix, which hands lint the fixer's walk instead of the kept verdict (nothing replayed).
     */
    @Test func anUnchangedPlainRunIsLintedFromItsKeptVerdict() async throws {
        let source = "func first(_ values: [Int]) -> Int {\n    values.first!\n}\n"
        let package = try LintVerdictCacheTests.Package(files: [Self.control: source])
        let first = try await package.run([])
        let store = try FormatVerdictCacheTests.storeIdentity(scratch: Pipeline.scratchPath(for: package.root))
        let second = try await package.run([])
        #expect(!first.findings.isEmpty, "the control: the kept verdict holds a finding")
        #expect(first.filesReplayed == 0)
        #expect(second.filesReplayed == 1)
        #expect(second.findings == first.findings)
        let fix = second.records.first { $0["kind"] as? String == "fix" }
        #expect(fix?["filesRewritten"] as? Int == 0)
        #expect(fix?["filesReformatted"] as? Int == 0)
        #expect(try Self.text(package, Self.control) == source)
        #expect(try FormatVerdictCacheTests.storeIdentity(scratch: Pipeline.scratchPath(for: package.root)) == store)
    }
}
