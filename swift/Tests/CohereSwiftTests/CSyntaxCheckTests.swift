import Foundation
import Testing

@testable import CohereSwift

/*
 A C target is checked with `clang -fsyntax-only` and its own arguments from the build server, so a C error a build
 reports is reported without compiling anything. Serialized, because the toolchain test starts a build server.
 */
@Suite(.serialized)
struct CSyntaxCheckTests {
    static func options(arguments: [String]) -> BuildServerClient.TargetOptions {
        BuildServerClient.TargetOptions(
            target: BuildServerClient.Target(identifier: "target", displayName: "CCore", tags: [], dependencies: []),
            language: "c",
            askedFile: URL(fileURLWithPath: "/package/Sources/CCore/answer.c"),
            arguments: arguments,
        )
    }

    /* The server's arguments as one file's check: the file swapped in, every output dropped, the record added. */
    @Test func theServersArgumentsBecomeOneFilesSyntaxCheck() throws {
        let options = Self.options(arguments: [
            "-x", "c", "-isysroot", "/sdk", "-index-store-path", "/scratch/out", "-fsyntax-only",
            "/package/Sources/CCore/answer.c", "-o", "/scratch/answer.o", "-MMD", "-MF", "/scratch/answer.d",
            "-index-unit-output-path", "/answer.o", "-working-directory=/package",
        ])
        let source = CSyntaxCheck.Source(
            target: "CCore",
            file: URL(fileURLWithPath: "/package/Sources/CCore/broken.c"),
            options: options,
        )
        #expect(
            try CSyntaxCheck.arguments(for: source, diagnostics: URL(fileURLWithPath: "/check/broken.dia")) == [
                "-x", "c", "-isysroot", "/sdk", "/package/Sources/CCore/broken.c", "-working-directory=/package",
                "-fsyntax-only", "--serialize-diagnostics", "/check/broken.dia",
            ]
        )
    }

    /* Arguments that do not name the file they were asked for cannot be moved to another file, so they fail by target. */
    @Test func argumentsWithoutTheAskedFileFail() {
        let source = CSyntaxCheck.Source(
            target: "CCore",
            file: URL(fileURLWithPath: "/package/Sources/CCore/broken.c"),
            options: Self.options(arguments: ["-x", "c", "-fsyntax-only"]),
        )
        #expect(throws: CSyntaxCheck.Failure.self) {
            try CSyntaxCheck.arguments(for: source, diagnostics: URL(fileURLWithPath: "/check/broken.dia"))
        }
    }

    /* Only C-family files that are not headers are checked, each with its target's options for its language. */
    @Test func onlyCFamilySourcesAreChecked() {
        let target = BuildServerClient.Target(
            identifier: "target",
            displayName: "CCore",
            tags: [],
            dependencies: [],
            sources: [
                .init(url: URL(fileURLWithPath: "/package/answer.c"), language: "c", isHeader: false),
                .init(url: URL(fileURLWithPath: "/package/CCore.h"), language: "", isHeader: true),
                .init(url: URL(fileURLWithPath: "/package/fast.s"), language: "", isHeader: false),
                .init(url: URL(fileURLWithPath: "/package/main.swift"), language: "swift", isHeader: false),
            ],
        )
        var option = Self.options(arguments: [])
        option.target = target
        let sources = CSyntaxCheck.sources(of: [target], options: [option])
        #expect(sources.map(\.file.lastPathComponent) == ["answer.c"])
    }

    /*
     On the fixture: a planted C error comes back from the check's record, at its line and column, the clean C file's
     record holds nothing, and the check of the C target writes nothing but those records.
     */
    @Test func aCErrorIsReportedAndNothingIsCompiled() throws {
        let root = try CheckFixture.package(plants: [CheckFixture.cPlant])
        let scratch = CheckFixture.scratch()
        let checked = try CheckFixture.check(root, scratch: scratch)
        #expect(
            checked.cFamily.records.map { URL(fileURLWithPath: $0.file).lastPathComponent }.sorted() == [
                "answer.c", "broken.c",
            ]
        )
        #expect(
            checked.cFamily.failedJobs.map { URL(fileURLWithPath: $0.primaries.joined()).lastPathComponent } == [
                "broken.c"
            ]
        )
        #expect(checked.cFamily.crashes.isEmpty)

        let diagnostics = try CheckFixture.diagnostics(root, directories: checked.cFamily.targetDirectories)
        #expect(diagnostics.byFile["Sources/CCore/broken.c"] == ["2:12: error: \(CheckFixture.cPlant.message)"])
        #expect(diagnostics.byFile["Sources/CCore/answer.c"] == [])
        #expect(CheckFixture.walk([scratch, root]).compiled == [])
    }
}
