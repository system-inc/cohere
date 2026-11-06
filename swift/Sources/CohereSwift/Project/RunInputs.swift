import Foundation

/*
 What a check run's verdict was read from, as the inputs record names it (#nkk91tv), so the front door can replay
 an unchanged project's verdict without starting this engine. A path left out is a replay that goes stale without a
 word, which is the one failure cohere exists to end, and a path too many only costs a replay. So every doubt here
 is settled by naming the path.

 What is named, and why each is enough:

 - every file and every directory under each root a build reads (the project's root, each local and
   path package's, and at an Xcode root the packages its project links), walked as `BuildInputSnapshot` walks them
   to decide a build is unchanged: a directory whose name starts with `.` is not entered, so neither `.git`,
   `.build` nor cohere's own `.cache` is, and everything else is, git-ignored or not, since a target compiles a
   file in its directory whatever git says. Each directory's listing is what makes a new file, a deleted one or a
   renamed one a miss. Hidden files in a walked directory are named too, `.gitignore` and `.swift-format` among
   them; only `.DS_Store` is left out, which Finder rewrites as it browses and nothing a run does reads;
 - every source of every target, vendored packages' too, and the directory holding each, which catches a target
   whose directory is a link the walk does not follow, or that sits outside every root;
 - each package's `Package.swift` and `Package.resolved`, there or absent. A remote dependency's checkout lives in
   the engine's scratch and is pinned by the resolved file, so the file stands for it;
 - every directory the file set's walk listed (`FileSet.directoriesListed`), hidden ones it entered among them, and
   every ignore file and git config file it consulted, there or absent, since a line in any of them moves files in
   or out of the run. A `.git` in each directory from a package up to its repository is named absent, since one
   appearing moves the repository's boundary, and a `.git` file a worktree points through is named as a file;
 - a `.swift-format` in each directory from each file of ours up to the repository, absent, since one appearing
   refuses the run;
 - the settings file, there or absent;
 - the toolchain: the `swift` that `/usr/bin/env` runs (the first on `PATH`), the `swift` xcrun names with the
   compiler and package tools beside it, the toolchain's `ToolchainInfo.plist`, the Xcode around it by its
   `version.plist`, and each SDK's `SDKSettings.json`, so a new Xcode, toolchain or SDK is a miss. Which toolchain
   xcrun picks is the front door's to key (its environment and `xcode-select`), since a replay never asks xcrun;
 - `xcrun` and `xcodebuild` as `PATH` finds them, and `xcodegen` at a spec's root, which generates the project.

 The engine's own identity is not here: the front door names the binary it ran and keys how it chose it.
 */
struct RunInputs {
    private(set) var files: Set<String> = []
    private(set) var directories: Set<String> = []
    private(set) var absent: Set<String> = []

    /*
     The record for a run over `package`, whose file set is `fileSet`. The package's root, each local package's and each
     path dependency's are walked whole, and `linkedPackageRoots` too, the packages an Xcode root's project links;
     `pins` are the package pins an Xcode root's build read (`pinsRead(by:)`), named there or absent wherever they live;
     `boundary` is the top of the repository holding the root, or the root outside one; `settingsFile` is the settings
     the run read or looked for; `toolchainExecutable` is the `swift` xcrun names, or nil when it could not be asked.
     */
    static func record(
        package: PackageModel,
        fileSet: FileSet,
        linkedPackageRoots: [URL] = [],
        pins: [URL] = [],
        boundary: URL,
        settingsFile: URL,
        toolchainExecutable: String?,
        environment: [String: String] = ProcessInfo.processInfo.environment,
    ) -> InputsRecord {
        var inputs = RunInputs()
        for root in [package.root] + TypesPhase.inputRoots(of: package) + linkedPackageRoots {
            inputs.walk(Self.path(root))
        }
        for directory in fileSet.directoriesListed {
            inputs.directories.insert(Self.path(URL(fileURLWithPath: directory)))
        }
        for member in package.allPackages {
            inputs.fileOrAbsent(member.root.appendingPathComponent("Package.swift"))
            inputs.fileOrAbsent(member.root.appendingPathComponent("Package.resolved"))
            for source in member.targets.flatMap(\.sources) {
                inputs.fileOrAbsent(source)
                inputs.listing(source.deletingLastPathComponent())
            }
            inputs.repositoryMarkers(above: member.root)
        }
        for pinned in pins {
            inputs.fileOrAbsent(pinned)
        }
        for owned in fileSet.owned {
            inputs.fileOrAbsent(owned.url)
            inputs.listing(owned.url.deletingLastPathComponent())
        }
        for consulted in fileSet.ignoreFilesConsulted {
            inputs.fileOrAbsent(URL(fileURLWithPath: consulted))
        }
        inputs.formatConfigurations(above: fileSet.owned.map(\.url), boundary: boundary)
        inputs.fileOrAbsent(settingsFile)
        if let toolchainExecutable {
            inputs.toolchain(executable: URL(fileURLWithPath: toolchainExecutable))
        }
        var tools = ["swift", "xcrun", "xcodebuild"]
        if package.projectKind == .xcodeGen {
            tools.append("xcodegen")
        }
        for tool in tools {
            if let found = Self.onPath(tool, environment: environment) {
                inputs.fileOrAbsent(found)
            }
        }
        return InputsRecord(
            files: inputs.files.sorted(),
            directories: inputs.directories.sorted(),
            absent: inputs.absent.sorted(),
        )
    }

    /*
     The pins an Xcode root's build read, chosen as `XcodeTypesPhase.restoreResolved` chooses what the build starts
     from (#nkk91tv, cache's review). The pins the repository commits, when it does: they replace whatever the build
     holds. Otherwise a spec's: its bundle is generated afresh into the scratch every run, so the build starts from the
     copy the last build kept there (`keptResolved`), the one file that decides which versions it builds. Otherwise a
     committed bundle's: the bundle's own for a plain run, which the walk names too, and under `--no-fix` the scratch
     workspace's, which keeps what its last build resolved. A copy in the scratch moves only when a build resolves
     again, and then during that run, which is therefore never recorded, so naming it costs a miss only when the pins
     really moved.
     */
    static func pinsRead(by phase: XcodeTypesPhase) -> [URL] {
        if let committed = phase.committedResolved {
            return [committed]
        }
        switch phase.package.projectKind {
            case .xcodeGen:
                return [phase.keptResolved]
            case .xcodeProject:
                return [phase.resolvedInBuild]
            case .swiftPackage:
                return []
        }
    }

    /* A path as the record spells it: absolute and standardized, symlinks left as they are, since the front door stats them as named. */
    static func path(_ url: URL) -> String {
        url.standardizedFileURL.path
    }

    /* Named as a file when something is there, a link whose target is there included, and as absent otherwise: a link to nothing reads as nothing, as the front door's stat reads it. */
    mutating func fileOrAbsent(_ url: URL) {
        let path = Self.path(url)
        if FileManager.default.fileExists(atPath: path) {
            files.insert(path)
        }
        else {
            absent.insert(path)
        }
    }

    /* A directory whose listing the run read, when it is one. */
    mutating func listing(_ url: URL) {
        if Self.isDirectory(url) {
            directories.insert(Self.path(url))
        }
    }

    /* Whether a directory is at `url`, through any links, as the front door's stat follows them. */
    static func isDirectory(_ url: URL) -> Bool {
        IgnoreRules.isDirectory(url.resolvingSymlinksInPath())
    }

    /* Every directory below `directory` that is not hidden, and every file in them. See the header for what is left out and why. */
    mutating func walk(_ directory: String) {
        guard Self.isDirectory(URL(fileURLWithPath: directory)) else {
            absent.insert(directory)
            return
        }
        guard directories.insert(directory).inserted else { return }
        let entries: [URL]
        do {
            entries = try FileManager.default.contentsOfDirectory(
                at: URL(fileURLWithPath: directory, isDirectory: true),
                includingPropertiesForKeys: [.isDirectoryKey, .isSymbolicLinkKey],
            )
        }
        catch {
            /* A directory that cannot be listed is still named above, by its own signature; nothing below it can be walked. */
            return
        }
        for entry in entries {
            let name = entry.lastPathComponent
            let values = try? entry.resourceValues(forKeys: [.isDirectoryKey, .isSymbolicLinkKey])
            if values?.isDirectory == true && values?.isSymbolicLink != true {
                if !name.hasPrefix(".") {
                    walk(directory + "/" + name)
                }
                continue
            }
            if name != ".DS_Store" {
                fileOrAbsent(URL(fileURLWithPath: directory + "/" + name))
            }
        }
    }

    /*
     A `.git` in each directory from `root` up to the repository holding it, absent, and that repository's `.git` when it
     is a file, a worktree's or a submodule's pointer, with the `commondir` its git directory names the shared one by.
     A `.git` directory is not named: its config and `info/exclude` are, among the files the ignore rules consulted, and
     the directory itself moves with every lock file git writes there. Outside any repository, every ancestor's `.git`
     is absent, since one appearing would make the file set read ignore rules it did not.
     */
    mutating func repositoryMarkers(above root: URL) {
        var candidate = root.resolvingSymlinksInPath().path
        while true {
            let marker = (candidate as NSString).appendingPathComponent(".git")
            if FileManager.default.fileExists(atPath: marker) {
                if !Self.isDirectory(URL(fileURLWithPath: marker)) {
                    files.insert(marker)
                    if let gitDirectory = IgnoreRules.Repository.linkedGitDirectory(URL(fileURLWithPath: marker)) {
                        fileOrAbsent(gitDirectory.appendingPathComponent("commondir"))
                    }
                }
                return
            }
            absent.insert(marker)
            let parent = (candidate as NSString).deletingLastPathComponent
            if parent == candidate || parent.isEmpty {
                return
            }
            candidate = parent
        }
    }

    /* A `.swift-format` in each directory from each file up to `boundary`, as `HouseSwiftFormat.leftoverConfigurationFiles` looks for one. */
    mutating func formatConfigurations(above files: [URL], boundary: URL) {
        let boundaryPath = boundary.resolvingSymlinksInPath().path
        var visited: Set<String> = []
        for file in files {
            var directory = file.resolvingSymlinksInPath().deletingLastPathComponent()
            while directory.path == boundaryPath || directory.path.hasPrefix(boundaryPath + "/"),
                visited.insert(directory.path).inserted
            {
                fileOrAbsent(directory.appendingPathComponent(".swift-format"))
                if directory.path == boundaryPath || directory.path == "/" {
                    break
                }
                directory.deleteLastPathComponent()
            }
        }
    }

    /*
     The toolchain around `executable`, the `swift` xcrun names (`<toolchain>/usr/bin/swift`): it and the tools beside it
     that a run starts, the toolchain's `ToolchainInfo.plist`, the `version.plist` of the Xcode holding it, and every
     SDK's `SDKSettings.json`, an Xcode's under `Platforms` and a Command Line Tools' under `SDKs`. Each is named only
     when it is there: a layout without one has nothing of it to change.
     */
    mutating func toolchain(executable: URL) {
        let resolved = executable.resolvingSymlinksInPath()
        let binaries = resolved.deletingLastPathComponent()
        for tool in [resolved.lastPathComponent, "swift-frontend", "swift-build", "swift-package"] {
            namedWhenThere(binaries.appendingPathComponent(tool))
        }
        let toolchainRoot = binaries.deletingLastPathComponent().deletingLastPathComponent()
        namedWhenThere(toolchainRoot.appendingPathComponent("ToolchainInfo.plist"))
        var sdkDirectories = [toolchainRoot.appendingPathComponent("SDKs")]
        let components = resolved.pathComponents
        if let developerIndex = components.lastIndex(of: "Developer") {
            let developer = URL(fileURLWithPath: NSString.path(withComponents: Array(components[...developerIndex])))
            namedWhenThere(developer.deletingLastPathComponent().appendingPathComponent("version.plist"))
            let platforms = developer.appendingPathComponent("Platforms")
            for platform in Self.entries(of: platforms) {
                sdkDirectories.append(platform.appendingPathComponent("Developer/SDKs"))
            }
        }
        for sdkDirectory in sdkDirectories {
            for sdk in Self.entries(of: sdkDirectory) {
                namedWhenThere(sdk.appendingPathComponent("SDKSettings.json").resolvingSymlinksInPath())
            }
        }
    }

    private mutating func namedWhenThere(_ url: URL) {
        if FileManager.default.fileExists(atPath: url.path) {
            files.insert(Self.path(url))
        }
    }

    private static func entries(of directory: URL) -> [URL] {
        (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
    }

    /* The first executable named `tool` on `PATH`, as `/usr/bin/env` finds it, or nil. */
    static func onPath(_ tool: String, environment: [String: String]) -> URL? {
        for directory in (environment["PATH"] ?? "").split(separator: ":") where directory.hasPrefix("/") {
            let candidate = URL(fileURLWithPath: String(directory)).appendingPathComponent(tool)
            if FileManager.default.isExecutableFile(atPath: candidate.path) {
                return candidate
            }
        }
        return nil
    }

    /*
     The `swift` xcrun names, recovered from the libclang path the diagnostics reader resolved from it
     (`<toolchain>/usr/lib/libclang.dylib`), so a run that already asked xcrun does not ask again.
     */
    static func toolchainExecutable(libraryPath: String) -> String {
        URL(fileURLWithPath: libraryPath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("bin/swift")
            .path
    }
}
