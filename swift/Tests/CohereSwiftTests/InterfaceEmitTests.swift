import Foundation
import Testing

@testable import CohereSwift

/* The pieces of the check's own emit of an interface that need no compiler: its arguments, its order, its targets. */
struct InterfaceEmitTests {
    /*
     The driver is asked for prepare's emit: outputs and planning flags gone, prepare's own flags kept, bodies skipped
     once, the emitted interfaces searched before anything else, and the jobs printed rather than run.
     */
    @Test func theEmitKeepsPreparesFlagsAndSearchesTheEmittedInterfacesFirst() {
        let server = [
            "-module-name", "Core", "-I", "/out/Products/Debug", "-Xfrontend",
            "-experimental-allow-module-with-compiler-errors", "-index-unit-output-path", "/Core/Greeting.o",
            "-enable-testing", "/root/Sources/Core/Greeting.swift",
        ]
        let arguments = InterfaceEmit.driverArguments(
            server,
            interface: URL(fileURLWithPath: "/scratch/check/interfaces/Core/Core.swiftmodule"),
            searchPaths: [URL(fileURLWithPath: "/scratch/check/interfaces/CCoreSwift")],
        )
        #expect(
            arguments == [
                "-I", "/scratch/check/interfaces/CCoreSwift", "-module-name", "Core", "-I", "/out/Products/Debug",
                "-Xfrontend", "-experimental-allow-module-with-compiler-errors", "-enable-testing",
                "/root/Sources/Core/Greeting.swift", "-emit-module", "-emit-module-path",
                "/scratch/check/interfaces/Core/Core.swiftmodule", "-Xfrontend",
                "-experimental-skip-all-function-bodies",
                "-driver-print-jobs", "-driver-filelist-threshold=\(Int32.max)",
            ]
        )
        let skipping = InterfaceEmit.driverArguments(
            server + ["-Xfrontend", "-experimental-skip-all-function-bodies"],
            interface: URL(fileURLWithPath: "/i/Core.swiftmodule"),
            searchPaths: [],
        )
        #expect(skipping.filter { $0 == "-experimental-skip-all-function-bodies" }.count == 1)
    }

    /* Each interface is emitted after every one it imports, whatever their names. */
    @Test func anInterfaceIsEmittedAfterTheOnesItImports() {
        let imports: [String: Set<String>] = [
            "Alpha": ["Zeta", "Foundation"],
            "Zeta": ["Middle"],
            "Middle": [],
            "Feature-Kit": ["Alpha"],
        ]
        #expect(
            InterfaceEmit.emitOrder(["Alpha", "Zeta", "Middle", "Feature-Kit"], imports: imports) == [
                "Middle", "Zeta", "Alpha", "Feature-Kit",
            ]
        )
    }

    /* An executable's tests import its testable variant, offered beside the normal one; a library has one set. */
    @Test func theTestableVariantIsTheOneEmitted() {
        func options(_ arguments: [String]) -> BuildServerClient.TargetOptions {
            BuildServerClient.TargetOptions(
                target: .init(identifier: "t", displayName: "App", tags: [], dependencies: []),
                language: "swift",
                askedFile: URL(fileURLWithPath: "/root/Sources/App/main.swift"),
                arguments: arguments,
                workingDirectory: nil,
            )
        }
        let normal = ["-module-name", "App", "/root/Sources/App/main.swift"]
        let testable = normal + ["-Xfrontend", "-entry-point-function-name", "-Xfrontend", "App_main"]
        let module = FrontendCheck.Module(name: "App", arguments: normal)
        #expect(
            InterfaceEmit.testableArguments(of: module, options: [options(normal), options(testable)]) == testable
        )
        #expect(InterfaceEmit.testableArguments(of: module, options: [options(normal)]) == normal)
    }

    /* A narrowed prepare asks for every server target compiling a file of the edited targets, both of an executable's variants. */
    @Test func aNarrowedPrepareAsksForTheTargetsCompilingTheEditedFiles() {
        func target(_ name: String, _ files: [String]) -> BuildServerClient.Target {
            BuildServerClient.Target(
                identifier: name,
                displayName: name,
                tags: [],
                dependencies: [],
                sources: files.map { .init(url: URL(fileURLWithPath: $0), language: "swift", isHeader: false) },
            )
        }
        let targets = [
            target("App", ["/root/Sources/App/main.swift"]),
            target("App-product", ["/root/Sources/App/main.swift"]),
            target("AppTests-product", ["/root/Tests/AppTests/MainTests.swift"]),
        ]
        let targetByFile = ["/root/Sources/App/main.swift": "App", "/root/Tests/AppTests/MainTests.swift": "AppTests"]
        #expect(TypesPhase.preparing(targets, owning: ["App"], targetByFile: targetByFile) == ["App", "App-product"])
    }

    @Test func anExplicitModuleBuildIsToldApart() {
        #expect(InterfaceEmit.buildsModulesExplicitly(["-explicit-module-build"]))
        #expect(!InterfaceEmit.buildsModulesExplicitly(["-I", "/out"]))
    }
}
