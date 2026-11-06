import Foundation
import Testing

@testable import CohereSwift

/*
 The two promises of a check that never builds (#11fs5xp): it writes no object, archive or binary, and it reports
 what a build reports, file by file. Serialized, because each runs a build server, frontends and builds.
 */
@Suite(.serialized)
struct CheckProofTests {
    /*
     The control for the proof below: the walk finds what a compiler writes by its bytes, whatever its name. A real
     object named like a record, an object whose `__text` is empty, and an archive named like text are compiled; a
     file named like an object is not; and a real Clang module container, an AST in a Mach-O object, is told apart.
     */
    @Test func theWalkFindsCompiledFilesByTheirBytes() throws {
        let folder = CheckFixture.scratch()
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        func clang(_ arguments: [String]) throws {
            let result = try ProcessRunner().run("xcrun", ["clang"] + arguments, in: folder)
            try #require(result.succeeded, "clang could not write the control: \(result.standardError)")
        }
        let source = folder.appendingPathComponent("tiny.c")
        try "int tiny(void) { return 1; }\n".write(to: source, atomically: true, encoding: .utf8)
        try clang(["-c", source.path, "-o", folder.appendingPathComponent("tiny.dia").path])
        let empty = folder.appendingPathComponent("empty.c")
        try "".write(to: empty, atomically: true, encoding: .utf8)
        try clang(["-c", empty.path, "-o", folder.appendingPathComponent("empty.swiftmodule").path])
        try "int tiny(void);\n".write(to: folder.appendingPathComponent("tiny.h"), atomically: true, encoding: .utf8)
        let map = folder.appendingPathComponent("module.modulemap")
        try "module Tiny {\n  header \"tiny.h\"\n}\n".write(to: map, atomically: true, encoding: .utf8)
        try clang([
            "-cc1", "-triple", "arm64-apple-macosx15.0", "-emit-module", "-fmodules", "-fmodule-name=Tiny",
            "-fmodule-format=obj", "-x", "c", map.path, "-o", folder.appendingPathComponent("Tiny.o").path,
        ])
        try Data("!<arch>\nmember".utf8).write(to: folder.appendingPathComponent("library.txt"))
        try Data("not compiled".utf8).write(to: folder.appendingPathComponent("plain.o"))

        let walk = CheckFixture.walk([folder])
        #expect(
            walk.compiled.map { URL(fileURLWithPath: $0).lastPathComponent } == [
                "empty.swiftmodule", "library.txt", "tiny.dia",
            ]
        )
        #expect(walk.moduleContainers.map { URL(fileURLWithPath: $0).lastPathComponent } == ["Tiny.o"])
    }

    /*
     The no-build proof: after prepare and the whole check of the fixture, nothing under the scratch or the package is
     an object, an archive, bitcode or a Mach-O binary. The only Mach-O files are the SDK's Clang module containers,
     named here so they are seen, and none is the fixture's. The check did run: every owned file has a record, and the
     index holds a unit for a file in the dependent target.
     */
    @Test func aCheckBuildsNothing() throws {
        let root = try CheckFixture.package()
        let scratch = CheckFixture.scratch()
        let checked = try CheckFixture.check(root, scratch: scratch)

        let walk = CheckFixture.walk([scratch, root])
        #expect(walk.compiled == [])
        #expect(!walk.moduleContainers.contains { $0.contains("CCore") || $0.hasPrefix(root.path) })

        let diagnostics = try CheckFixture.diagnostics(root, directories: checked.targetDirectories)
        #expect(diagnostics.withoutRecord == [])
        let reported = diagnostics.byFile.filter { !$0.value.isEmpty }
        #expect(reported.isEmpty, "the clean fixture reported \(reported)")
        #expect(
            try !CheckFixture.occurrences(
                of: root.appendingPathComponent("Sources/Feature/Welcome.swift"),
                storePath: checked.description.indexStorePath,
            )
            .isEmpty
        )
    }

    /*
     Every kind of diagnostic a build reports, the check reports the same, file by file, read through `CompilerRecords`
     from both. The check runs once with every kind planted. A build stops at its first failed task (measured: a C
     error left no Swift file compiled, and two failing Swift files left four unreached), so the build's record of
     each kind comes from its own incremental build with only that kind planted. Every other file is compared with a
     clean build's records, so the check also adds nothing a build does not say.
     */
    @Test func everyDiagnosticKindMatchesTheBuild() throws {
        let plants = CheckFixture.swiftPlants + [CheckFixture.cPlant]
        let checkedRoot = try CheckFixture.package(plants: plants)
        let checked = try CheckFixture.check(checkedRoot, scratch: CheckFixture.scratch())
        #expect(checked.swift.crashes.isEmpty)
        let fromCheck = try CheckFixture.diagnostics(checkedRoot, directories: checked.targetDirectories).byFile

        let builtRoot = try CheckFixture.package()
        let buildScratch = CheckFixture.scratch()
        let clean = try CheckFixture.build(builtRoot, scratch: buildScratch)
        try #require(clean.succeeded, "the clean fixture does not build: \(clean.standardError)")
        var fromBuild = try CheckFixture.diagnostics(
            builtRoot,
            directories: CheckFixture.buildDirectories(scratch: buildScratch),
        )
        .byFile
        for plant in plants {
            try CheckFixture.write(plant.text, to: plant.path, in: builtRoot)
            _ = try CheckFixture.build(builtRoot, scratch: buildScratch)
            let planted = try CheckFixture.diagnostics(
                builtRoot,
                directories: CheckFixture.buildDirectories(scratch: buildScratch),
            )
            fromBuild[plant.path] = planted.byFile[plant.path] ?? []
            try FileManager.default.removeItem(at: builtRoot.appendingPathComponent(plant.path))
        }

        for plant in plants {
            #expect(
                fromBuild[plant.path, default: []].contains { $0.contains(plant.message) },
                "the build's record of \(plant.path) lacks \"\(plant.message)\": \(fromBuild[plant.path, default: []])",
            )
        }
        #expect(fromCheck.keys.sorted() == fromBuild.keys.sorted())
        for file in fromCheck.keys.sorted() {
            #expect(
                fromCheck[file] == fromBuild[file],
                "\(file): the check said \(fromCheck[file] ?? []), the build \(fromBuild[file] ?? [])",
            )
        }
    }
}
