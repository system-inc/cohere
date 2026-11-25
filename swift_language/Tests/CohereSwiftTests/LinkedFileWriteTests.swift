import Foundation
import Testing

@testable import CohereSwift

/*
 The fix phase's writes across symbolic links (#723rzxm), shaped on the probe that found them on #hwz114g: a linked
 directory pointing outside the root had its file rewritten, and a linked file was replaced by a plain one. A file
 whose real path lies outside the run's root is checked and never written, and named in the fix record's
 `notWritten`; a link inside the root is written through to its target and stays a link; a plain file writes as it
 always has, its permissions kept. Each test is red with the write going to the path as reached
 (`final.write(to: file.url, atomically: true, encoding: .utf8)`) and nothing resolved. Serialized, as the pipeline's
 other runs are, since a run that lints a package starts a build server and a pool of frontends.
 */
@Suite(.serialized)
struct LinkedFileWriteTests {
    /* A text the fixer changes (`fileprivate` to `private`) and the formatter changes (the double space). */
    static let needsFixAndFormat = "fileprivate  let limit = 3\n"
    static let fixedAndFormatted = "private let limit = 3\n"

    /* A folder beside the package's, outside its root, holding these files by their paths from it. */
    static func outside(_ files: [String: String]) throws -> URL {
        let folder = FileManager.default.temporaryDirectory
            .appendingPathComponent("cohere-swift-link-outside-\(UUID().uuidString)", isDirectory: true)
            .resolvingSymlinksInPath()
        for (path, text) in files {
            try CheckFixture.write(text, to: path, in: folder)
        }
        return folder
    }

    /* A link at this path from the root to the destination as given, relative as the probe made them, or absolute. */
    static func link(_ path: String, in root: URL, to destination: String) throws {
        let link = root.appendingPathComponent(path)
        try FileManager.default.createDirectory(
            at: link.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try FileManager.default.createSymbolicLink(atPath: link.path, withDestinationPath: destination)
    }

    static func fixRecord(_ run: LintVerdictCacheTests.Run) -> [String: Any]? {
        run.records.first { $0["kind"] as? String == "fix" }
    }

    /* The fix record's `notWritten`, each entry as `<file from the root> -> <real path>`. */
    static func notWritten(_ run: LintVerdictCacheTests.Run) -> [String]? {
        (fixRecord(run)?["notWritten"] as? [[String: Any]])?.map { entry in
            let file = CheckFixture.relative(entry["file"] as? String ?? "", to: run.root)
            return "\(file) -> \(entry["realPath"] as? String ?? "")"
        }
    }

    static func changedFiles(_ run: LintVerdictCacheTests.Run) -> [String] {
        (fixRecord(run)?["changedFiles"] as? [[String: Any]] ?? []).map {
            CheckFixture.relative($0["file"] as? String ?? "", to: run.root)
        }
    }

    static func summary(_ run: LintVerdictCacheTests.Run) -> [String: Any]? {
        run.records.first { $0["kind"] as? String == "summary" }
    }

    static func realPath(_ url: URL) -> String { SourceWrite.realPath(of: url) ?? url.path }

    static func linkDestination(_ path: String, in root: URL) throws -> String {
        try FileManager.default.destinationOfSymbolicLink(atPath: root.appendingPathComponent(path).path)
    }

    /*
     A linked directory inside the root pointing outside it, holding a file that needs a fix and formatting: the file
     outside keeps its bytes, the gap names it with its real path, its findings still report (the fix the fixer would
     have made, and the formatting, as `--no-fix` reports it), nothing outside is counted as rewritten, and the run is
     incomplete with exit 1. The file inside beside it is fixed and written as always, the control that the run writes.
     */
    @Test func aLinkedDirectoryPointingOutsideIsCheckedAndNeverWritten() async throws {
        let outside = try Self.outside(["Outside.swift": Self.needsFixAndFormat])
        let package = try LintVerdictCacheTests.Package(files: [
            "Sources/Control/Inside.swift": "fileprivate  let inside = 3\n"
        ])
        try Self.link("Sources/Control/Linked", in: package.root, to: outside.path)
        let run = try await package.run(["--fix", "--lint"])

        #expect(
            try String(contentsOf: outside.appendingPathComponent("Outside.swift"), encoding: .utf8)
                == Self.needsFixAndFormat
        )
        #expect(
            Self.notWritten(run)
                == ["Sources/Control/Linked/Outside.swift -> \(Self.realPath(outside))/Outside.swift"]
        )
        #expect(Self.changedFiles(run) == ["Sources/Control/Inside.swift"], "the control: the file inside is written")
        #expect(Self.fixRecord(run)?["fixesApplied"] as? Int == 1, "only the fix that landed is counted")
        #expect(Self.fixRecord(run)?["filesReformatted"] as? Int == 1, "only the reformat that landed is counted")
        #expect(
            run.findings(of: PrivateOverFileprivate().name).map { $0.components(separatedBy: " ")[1] }
                == ["Sources/Control/Linked/Outside.swift:1:1"]
        )
        let formatFindings = run.records.filter {
            $0["kind"] as? String == "finding" && $0["source"] as? String == "format"
        }
        #expect(
            formatFindings.map { CheckFixture.relative($0["file"] as? String ?? "", to: run.root) }
                == ["Sources/Control/Linked/Outside.swift"]
        )
        #expect(Self.summary(run)?["complete"] as? Bool == false)
        #expect(Self.summary(run)?["exitCode"] as? Int == 1)
    }

    /*
     A linked file inside the root pointing at a file inside it: the target is fixed and formatted, and the link is
     still a link to the same target, not a plain file holding the fixed text.
     */
    @Test func aLinkedFileInsideTheRootIsWrittenThroughAndStaysALink() async throws {
        let package = try LintVerdictCacheTests.Package(files: ["Shared/Shared.swift": Self.needsFixAndFormat])
        let destination = "../../Shared/Shared.swift"
        try Self.link("Sources/Control/Linked.swift", in: package.root, to: destination)
        let run = try await package.run(["--fix"])

        #expect(try Self.linkDestination("Sources/Control/Linked.swift", in: package.root) == destination)
        #expect(
            try String(contentsOf: package.root.appendingPathComponent("Shared/Shared.swift"), encoding: .utf8)
                == Self.fixedAndFormatted
        )
        #expect(Self.notWritten(run) == [])
        #expect(Self.changedFiles(run) == ["Sources/Control/Linked.swift"])
        #expect(Self.summary(run)?["complete"] as? Bool == true)
    }

    /*
     A linked file inside the root pointing at a file outside it: the file outside keeps its bytes, the link is still a
     link to it, and the gap names it.
     */
    @Test func aLinkedFilePointingOutsideIsNeverWrittenAndStaysALink() async throws {
        let outside = try Self.outside(["OutsideFile.swift": Self.needsFixAndFormat])
        let package = try LintVerdictCacheTests.Package(files: [:])
        let destination = outside.appendingPathComponent("OutsideFile.swift").path
        try Self.link("Sources/Control/LinkedFile.swift", in: package.root, to: destination)
        let run = try await package.run(["--fix"])

        #expect(try Self.linkDestination("Sources/Control/LinkedFile.swift", in: package.root) == destination)
        #expect(try String(contentsOf: URL(fileURLWithPath: destination), encoding: .utf8) == Self.needsFixAndFormat)
        #expect(
            Self.notWritten(run)
                == ["Sources/Control/LinkedFile.swift -> \(Self.realPath(outside))/OutsideFile.swift"]
        )
        #expect(Self.changedFiles(run) == [])
        #expect(Self.summary(run)?["complete"] as? Bool == false)
    }

    /* A plain file still writes as it always has, and keeps its permissions, which are not the default's. */
    @Test func aPlainFileIsWrittenWithItsPermissionsKept() async throws {
        let path = "Sources/Control/Plain.swift"
        let package = try LintVerdictCacheTests.Package(files: [path: Self.needsFixAndFormat])
        let file = package.root.appendingPathComponent(path)
        try FileManager.default.setAttributes([.posixPermissions: 0o640], ofItemAtPath: file.path)
        let run = try await package.run(["--fix"])

        #expect(try String(contentsOf: file, encoding: .utf8) == Self.fixedAndFormatted)
        let attributes = try FileManager.default.attributesOfItem(atPath: file.path)
        #expect(attributes[.posixPermissions] as? Int == 0o640)
        #expect(attributes[.type] as? FileAttributeType == .typeRegular)
        #expect(Self.notWritten(run) == [])
        #expect(Self.changedFiles(run) == [path])
        let siblings = try FileManager.default.contentsOfDirectory(atPath: file.deletingLastPathComponent().path)
        #expect(siblings == ["Plain.swift"], "no temporary file is left beside it")
    }
}
