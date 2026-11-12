// swift-tools-version:6.2

/*
 cohere-swift: the Swift engine behind the `cohere` front door. It fixes, formats, type-checks and lints
 a Swift package in one process and reports what it did as the records `Contract.md` defines. The Go
 front door renders those records exactly as it renders a TypeScript run.

 swift-syntax and swift-format are pinned to one exact release, the one matching the toolchain (604.x
 for Swift 6.4). They come from packages rather than from the toolchain's own `usr/lib/swift/host`
 dylibs because swift-format's library depends on the swift-syntax package. Two copies of `SwiftSyntax`
 cannot share one binary, and the format phase needs swift-format in process. Measured before
 choosing: built this way, swift-format formats byte-identical to `xcrun swift-format` on 952 of our
 files.

 The floor, measured on 2026-10-03 (#1c80jyh) by stepping each down until the build failed: tools 6.2, which
 `.strictMemorySafety()` needs, and macOS 15, which Synchronization's `Mutex` needs. Below the tools floor
 the manifest is rejected ("'strictMemorySafety' is unavailable"), and below macOS 15 every `Mutex` is.
 Without `Mutex` the floor would be macOS 13, where swift-format's product stops it; 15 was kept for 1.0.
 So the stable macos-26 runners (Xcode 26, Swift 6.3) build this, and the binary runs on macOS 15 and up.
 */

import PackageDescription

/*
 Whether this engine's own inputs differed from their commit when it was built: `Package.swift`,
 `Package.resolved` and `Sources/`, the files the launcher's cache key reads. Gitignored files never count.

 It is the engine's side of the signal Go's build info gives the TypeScript engine, so a binary carrying
 code no commit holds says so on every run instead of only under `--version`.

 The development launcher states the answer, as COHERE_SWIFT_SOURCE_TREE_MODIFIED ("true" or "false"),
 read once from git under those paths, because it names the engine it caches by that same answer: an
 engine whose answer was read again here could disagree with its own name (#50wtb2s).

 The fallback below reads the whole repository, not only those paths. It is reached only by a committed or
 release build, whose tree is a clean worktree at the commit or a checkout the release requires to be clean,
 so the two meanings agree there. A package with no git context reads as modified. A git that fails, as on a
 worktree whose submodule is a symlink, reads as clean (measured 2026-11-12).

 SwiftPM caches a manifest's evaluation by the manifest's own text and the environment, not git's state,
 so with the default cache the git answer is whatever it was the last time this manifest was evaluated.
 Measured 2026-10-03: a modified tracked file read as clean. A build whose stamp matters passes
 `--manifest-cache none`, which evaluates it fresh. A stated answer is keyed (measured 2026-11-12).
 */
let sourceTreeModified =
    switch Context.environment["COHERE_SWIFT_SOURCE_TREE_MODIFIED"] {
    case "true": true
    case "false": false
    // Committed and release builds only, whose trees are clean: see above.
    default: Context.gitInformation?.hasUncommittedChanges ?? true
    }

/*
 The compiler checks cohere-swift's own `toolchain-require-upcoming-features` asks of every Swift target, held to here too.

 Strict memory safety's diagnostics are errors, not warnings. Toolchains disagree about what is unsafe (Swift 6.3
 flagged two closure calls in SwiftDemangler that 6.4 accepts), and a warning on the release runner's toolchain
 only scrolls past (v0.1.0's release run, 41766910807), so a build that falls short of the engine's own rule fails.
 */
let houseSettings: [SwiftSetting] = [
    .enableUpcomingFeature("ExistentialAny"),
    .enableUpcomingFeature("MemberImportVisibility"),
    .strictMemorySafety(),
    .treatWarning("StrictMemorySafety", as: .error),
]

let package = Package(
    name: "CohereSwift",
    platforms: [
        .macOS(.v15),
    ],
    products: [
        .executable(name: "cohere-swift", targets: ["CohereSwiftCommand"]),
        .executable(name: "cohere-swift-parity", targets: ["CohereSwiftParity"]),
    ],
    dependencies: [
        .package(url: "https://github.com/swiftlang/swift-syntax.git", exact: "604.0.0"),
        .package(url: "https://github.com/swiftlang/swift-format.git", exact: "604.0.0"),
    ],
    targets: [
        /*
         Type declarations for the part of libclang that reads the compiler's serialized diagnostics. Only
         declarations: the library itself is loaded at run time from the toolchain `xcrun` names, so the
         binary never carries one Xcode's path. See the header.
         */
        .target(name: "ClangDiagnosticsShim"),
        /* The same, for the part of sourcekitd's C API the engine calls: the compiler as a library, loaded the same way. */
        .target(name: "SourcekitdShim"),
        /* And for the index store the build writes: which declaration every name in every file resolves to. */
        .target(name: "IndexStoreShim"),
        /*
         Everything the engine knows, as a library so the tests reach the same code the command runs.
         The command target is only the process boundary: arguments in, records out, an exit code.
         */
        .target(
            name: "CohereSwift",
            dependencies: [
                "ClangDiagnosticsShim",
                "SourcekitdShim",
                "IndexStoreShim",
                .product(name: "SwiftSyntax", package: "swift-syntax"),
                .product(name: "SwiftParser", package: "swift-syntax"),
                .product(name: "SwiftParserDiagnostics", package: "swift-syntax"),
                .product(name: "SwiftOperators", package: "swift-syntax"),
                .product(name: "SwiftDiagnostics", package: "swift-syntax"),
                .product(name: "SwiftFormat", package: "swift-format"),
            ],
            swiftSettings: houseSettings + (sourceTreeModified ? [.define("COHERE_SOURCE_TREE_MODIFIED")] : [])
        ),
        .executableTarget(
            name: "CohereSwiftCommand",
            dependencies: ["CohereSwift"],
            swiftSettings: houseSettings
        ),
        /*
         The parity harness: the engine beside SwiftLint and swift-format's linter, rule by rule. A separate
         product, so the front door's `--product cohere-swift` build never compiles it.
         */
        .executableTarget(
            name: "CohereSwiftParity",
            dependencies: ["CohereSwift"],
            swiftSettings: houseSettings
        ),
        .testTarget(
            name: "CohereSwiftTests",
            dependencies: ["CohereSwift"],
            swiftSettings: houseSettings,
            /*
             The contract fixtures live beside Contract.md, outside this target, so the Go renderer's tests
             read the same files. The tests find them by walking up from their own source path rather than
             copying them in as resources, because a copy is a second agreement that can drift.
             */
        ),
    ]
)
