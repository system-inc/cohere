import Foundation
import Testing

@testable import CohereSwift

/*
 `--covered-packages`: which packages inside the root a run there checks, from the run's own traversal, so the
 front door can skip running them again (#sxs26s3, Presence's VRMKit, RigSolve and CaptureWire counted twice).
 The layout holds each case: a local package inside the root (checked), one under `Vendor/` (vendored), and a
 nested package nothing depends on (neither, so it runs as its own project).
 */
struct CoveredPackagesTests {
    static func write(_ text: String, to path: String, under root: URL) throws {
        let file = root.appendingPathComponent(path)
        try FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try text.write(to: file, atomically: true, encoding: .utf8)
    }

    static func library(_ name: String) -> String {
        """
        // swift-tools-version:6.2
        import PackageDescription
        let package = Package(name: "\(name)", products: [.library(name: "\(name)", targets: ["\(name)"])], targets: [.target(name: "\(name)")])

        """
    }

    @Test func theRunNamesThePackagesItChecksAndTheOnesItVendors() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-covered-\(UUID().uuidString)",
            isDirectory: true,
        )
        try Self.write(
            """
            // swift-tools-version:6.2
            import PackageDescription
            let package = Package(
                name: "App",
                dependencies: [.package(path: "Libraries/Lib"), .package(path: "Vendor/Third")],
                targets: [.target(name: "App", dependencies: [.product(name: "Lib", package: "Lib"), .product(name: "Third", package: "Third")])]
            )

            """,
            to: "Package.swift",
            under: root,
        )
        try Self.write("struct App {}\n", to: "Sources/App/App.swift", under: root)
        for (directory, name) in [("Libraries/Lib", "Lib"), ("Vendor/Third", "Third"), ("Tools/Sim", "Sim")] {
            try Self.write(Self.library(name), to: "\(directory)/Package.swift", under: root)
            try Self.write("public struct \(name) {}\n", to: "\(directory)/Sources/\(name)/\(name).swift", under: root)
        }

        let options = try CommandOptions.parse(
            ["--contract", "\(EngineVersion.contract)", "--root", root.path, "--covered-packages"],
            workingDirectory: root,
        )
        var lines = Data()
        let exit = try await Pipeline(
            options: options,
            writer: ContractWriter { lines.append($0) },
            workingDirectory: root,
        )
        .run()
        let records = try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }
        #expect(exit == 0)
        #expect(records.map { $0["kind"] as? String } == ["provenance", "coveredPackages"], "nothing but the answer")
        let named = { (key: String) in
            ((records.last?[key] as? [String]) ?? []).map {
                $0.replacingOccurrences(of: root.standardizedFileURL.path, with: "")
            }
        }
        #expect(named("checked") == ["", "/Libraries/Lib"])
        #expect(named("vendored") == ["/Vendor/Third"])
        #expect(
            !FileManager.default.fileExists(atPath: root.appendingPathComponent(".cache/cohere/swift/out").path),
            "nothing was built",
        )
    }
}
