import Foundation

/*
 The root a build wrote into, and which build system wrote it, so the readers of what it recorded (`CompileCommands`,
 `IndexStore`) can find their files under it. The two build systems write the same formats under different roots,
 measured 2026-11-05 on copies of ahraos-presence: SwiftPM 6.4's swiftbuild keeps its build description and its
 index store's `v5` under `<scratch>/out/`, and xcodebuild with `-derivedDataPath` keeps the build description under
 `<derivedData>/Build/` and the index store under `<derivedData>/Index.noindex/DataStore`.
 */
enum BuildLayout: Equatable {
    /* A `swift build --scratch-path` into this scratch. */
    case swiftPackage(scratchPath: URL)
    /* An xcodebuild into this derived data (`XcodeTypesPhase.derivedData`). */
    case xcode(derivedData: URL)
}
