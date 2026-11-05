import Foundation
import Testing

@testable import CohereSwift

/*
 Generating an xcodegen project without writing into the developer's tree, against the real xcodegen.

 The invariant is the one a check owes: every file under the project's root is byte-identical after a
 generation, while the project, its Info.plist and its entitlements come out in the scratch. The tests need
 xcodegen and fail, never skip, without it, since a skipped invariant would read as a kept one.
 */
@Suite
struct XcodeGenerationTests {
    /* A spec, its sources, and a scratch folder beside them, under one UUID-named folder. `{root}` in the spec reads as the root's path. */
    struct Fixture {
        let folder: URL
        let root: URL
        let scratch: URL

        var specFile: URL { root.appendingPathComponent("project.yml") }

        init(spec: String, sources: [String: String] = [:]) throws {
            folder = FileManager.default.temporaryDirectory
                .appendingPathComponent("cohere-swift-xcodegen-\(UUID().uuidString)", isDirectory: true)
                .resolvingSymlinksInPath()
            root = folder.appendingPathComponent("project", isDirectory: true)
            scratch = folder.appendingPathComponent("scratch", isDirectory: true)
            try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
            try spec.replacingOccurrences(of: "{root}", with: root.path)
                .write(to: root.appendingPathComponent("project.yml"), atomically: true, encoding: .utf8)
            for (path, text) in sources {
                let file = root.appendingPathComponent(path)
                try FileManager.default.createDirectory(
                    at: file.deletingLastPathComponent(),
                    withIntermediateDirectories: true,
                )
                try text.write(to: file, atomically: true, encoding: .utf8)
            }
        }

        func generate() throws -> URL {
            try XcodeGeneration.generate(specFile: specFile, root: root, into: scratch, runner: ProcessRunner())
        }

        func remove() throws {
            try FileManager.default.removeItem(at: folder)
        }
    }

    static let applicationSpec = """
        name: Fixture
        options:
          bundleIdPrefix: com.example
        targets:
          App:
            type: application
            platform: iOS
            deploymentTarget: "17.0"
            sources: [Sources]
            info:
              path: Info.plist
              properties:
                UILaunchScreen: {}
                FixtureMarker: info-property
            entitlements:
              path: App.entitlements
              properties:
                com.apple.security.application-groups: [group.com.example.fixture]

        """

    static let applicationSources = [
        "Sources/First.swift": "let first = 1\n",
        "Sources/Second.swift": "let second = 2\n",
    ]

    /* Fails with a sentence, rather than skipping, when xcodegen cannot be run. */
    static func requireXcodegen() throws {
        let version = try ProcessRunner().run("xcodegen", ["--version"], in: FileManager.default.temporaryDirectory)
        try #require(
            version.succeeded,
            "`/usr/bin/env xcodegen --version` failed, and these tests need xcodegen (brew install xcodegen): \(version.standardError)",
        )
    }

    /* Every file and folder under `folder`, by its path inside it, with each file's bytes. A folder's bytes are empty. */
    static func snapshot(of folder: URL) throws -> [String: Data] {
        let base = folder.resolvingSymlinksInPath().path
        var entries: [String: Data] = [:]
        let enumerator = try #require(
            FileManager.default.enumerator(at: folder, includingPropertiesForKeys: [.isDirectoryKey])
        )
        for case let url as URL in enumerator {
            let path = String(url.resolvingSymlinksInPath().path.dropFirst(base.count))
            if try url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory == true {
                entries[path + "/"] = Data()
            }
            else {
                entries[path] = try Data(contentsOf: url)
            }
        }
        return entries
    }

    /* Every `path = …;` in a pbxproj, resolved against the folder the project sits in. */
    static func referencedPaths(in project: URL) throws -> Set<String> {
        let text = try String(contentsOf: project.appendingPathComponent("project.pbxproj"), encoding: .utf8)
        let sourceRoot = project.deletingLastPathComponent()
        var paths: Set<String> = []
        for line in text.split(separator: "\n") {
            guard let range = line.range(of: #"\bpath = ("[^"]*"|[^;]*);"#, options: .regularExpression) else {
                continue
            }
            let value = line[range].dropFirst("path = ".count).dropLast().trimmingCharacters(
                in: CharacterSet(charactersIn: "\"")
            )
            paths.insert(sourceRoot.appendingPathComponent(value).standardizedFileURL.resolvingSymlinksInPath().path)
        }
        return paths
    }

    @Test func generatingWritesNothingUnderTheRoot() throws {
        try Self.requireXcodegen()
        let fixture = try Fixture(spec: Self.applicationSpec, sources: Self.applicationSources)
        defer {
            do { try fixture.remove() }
            catch { Issue.record("could not remove the fixture: \(error)") }
        }
        let before = try Self.snapshot(of: fixture.root)

        let project = try fixture.generate()

        #expect(try Self.snapshot(of: fixture.root) == before, "the generation changed the project's own tree")
        #expect(
            project.deletingLastPathComponent().resolvingSymlinksInPath() == fixture.scratch.resolvingSymlinksInPath()
        )
        let info = try String(contentsOf: fixture.scratch.appendingPathComponent("App.Info.plist"), encoding: .utf8)
        #expect(info.contains("info-property"), "the target's Info.plist was not written into the scratch")
        let entitlements = try String(
            contentsOf: fixture.scratch.appendingPathComponent("App.entitlements"),
            encoding: .utf8,
        )
        #expect(
            entitlements.contains("group.com.example.fixture"),
            "the entitlements were not written into the scratch",
        )
        let realSources = fixture.root.appendingPathComponent("Sources").resolvingSymlinksInPath().path
        #expect(
            try Self.referencedPaths(in: project).contains(realSources),
            "the generated project does not name the real Sources folder",
        )
    }

    @Test func aMissingSourcePathIsRefusedInXcodegensWords() throws {
        try Self.requireXcodegen()
        let fixture = try Fixture(
            spec: """
                name: Fixture
                targets:
                  App:
                    type: application
                    platform: iOS
                    sources: [Sources, Models/Absent.task]

                """,
            sources: Self.applicationSources,
        )
        defer {
            do { try fixture.remove() }
            catch { Issue.record("could not remove the fixture: \(error)") }
        }

        let refusal = try #require(throws: XcodeGeneration.Refusal.self) { try fixture.generate() }

        #expect(refusal.description.contains("has a missing source directory"), "\(refusal.description)")
        #expect(refusal.description.contains("Models/Absent.task"), "\(refusal.description)")
        #expect(refusal.description.contains(fixture.specFile.path), "\(refusal.description)")
    }

    @Test func aPostGenerationCommandIsRefusedAndNeverRuns() throws {
        try Self.requireXcodegen()
        let fixture = try Fixture(
            spec: """
                name: Fixture
                options:
                  postGenCommand: touch "{root}/Marker"
                targets:
                  App:
                    type: application
                    platform: iOS
                    sources: [Sources]

                """,
            sources: Self.applicationSources,
        )
        defer {
            do { try fixture.remove() }
            catch { Issue.record("could not remove the fixture: \(error)") }
        }

        /* An expectation rather than a requirement, so a run that generated anyway still reaches the marker. */
        let refusal = #expect(throws: XcodeGeneration.Refusal.self) { try fixture.generate() }

        #expect(
            !FileManager.default.fileExists(atPath: fixture.root.appendingPathComponent("Marker").path),
            "the post-generation command ran",
        )
        let description = refusal?.description ?? ""
        #expect(description.contains("options.postGenCommand"), "\(description)")
        #expect(description.contains(fixture.specFile.path), "\(description)")
    }
}
