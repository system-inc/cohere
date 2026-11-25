import Foundation

/*
 The root a check or a build wrote into, and which wrote it, so the readers of what it recorded (`CompileCommands`,
 `IndexStore`) can find their files under it. A package is checked, never built (#11fs5xp): the build server
 prepares its dependencies into the scratch, and the frontend check writes its index store into
 `<scratch>/index-store`, the store the server names, and keeps the server's compile arguments in the scratch
 (`CompileCommands.store`). An Xcode root is built, the named exception: xcodebuild with `-derivedDataPath` keeps
 its build description under `<derivedData>/Build/` and its index store under `<derivedData>/Index.noindex/DataStore`,
 measured 2026-11-05 on copies of ahraos-presence.
 */
enum BuildLayout: Equatable {
    /* A check of a package into this scratch (`TypesPhase`). */
    case check(scratchPath: URL)
    /* An xcodebuild into this derived data (`XcodeTypesPhase.derivedData`). */
    case xcode(derivedData: URL)
}
