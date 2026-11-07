import Foundation
import SwiftParser
import SwiftSyntax
import Synchronization

/*
 The compiler's own errors and warnings for every file we own, from a check that never builds (#11fs5xp, ruled on
 #nkk91tv 2026-11-06): no object, no IRGen, no link.

 A package whose platforms leave out macOS is not checked against the macOS host's SDK, which it never targets:
 `PlatformPackageTypesPhase` checks it for a platform it names.

 Each check has three steps, all into the engine's cache (`--scratch-path`), never the project's `.build`:
 - `swift package experimental-build-server` (`BuildServerClient`) gives every target's compiler arguments and
   prepares the dependencies' modules, bodies skipped and no object written. Its one allowed build is a
   build-tool plugin's host tool, which the types record names with how long it took;
 - `FrontendCheck` runs each owned module's frontend jobs as `-emit-sil` to /dev/null, with a serialized
   diagnostics record and an index unit per file, so the passes that report missing returns, overflow and data
   races run as in a build;
 - `CSyntaxCheck` runs `clang -fsyntax-only` over each owned C, C++ and Objective-C file, whose errors a build
   would stop at.

 Test targets are checked too, because their files are ours and a test that does not compile is a finding like
 any other. Then every serialized diagnostics file of each owned target is read (`CompilerRecords`), each
 diagnostic placed by its own file path.

 A file is covered only when its own `.dia` exists and is newer than the source. A missing or older record means
 the compiler did not vouch for the file as it stands. That file is listed by name and the run is incomplete,
 never quietly clean. A build server that fails is a named gap in the run, never a build in its place.
 */
struct TypesPhase {
    /* What the phase found and what it could not vouch for. */
    struct Result {
        var findings: [FindingRecord]
        var record: TypesRecord
        /* True when any compiler error was found: lint is then not reached. Warnings do not bail. */
        var hasErrors: Bool
    }

    let package: PackageModel
    let files: [FileSet.OwnedFile]
    let scratchPath: URL
    let resolutionAllowed: Bool
    /* The toolchain `swift --version` named; an unknown one never reuses a build, so a toolchain change can never be missed. */
    let toolchain: String
    let runner: ProcessRunner
    /* Trees the pipeline already parsed, by path, so fingerprinting a file does not parse it twice. Any file missing is parsed here. */
    var parsed: [String: SourceFileSyntax] = [:]
    /* The platform to check for when it is not the macOS host (`PlatformPackageTypesPhase`), nil for the host. */
    var platform: Platform?

    /* A platform the build server is started for: `--triple` and `--sdk`. */
    struct Platform: Equatable {
        var triple: String
        var sdk: String
    }

    /*
     The packages to check: the root, plus each local package that holds test files of ours. The root's build
     server lists a local package's libraries but never its tests, because a dependency's test targets are not
     part of the root's graph. Measured on Presence with builds: without these, RigSolve's, CaptureWire's and
     VRMKit's 26 test files had no compiler record at all.
     */
    private var checks: [(root: URL, scratchPath: URL)] {
        let testTargetNames = Set(files.filter { $0.targetKind == "test" }.map(\.targetName))
        let locals = package.localPackages.filter { local in
            local.targets.contains { $0.kind == "test" && testTargetNames.contains($0.name) }
        }
        return [(package.root, scratchPath)]
            + locals.map {
                ($0.root, scratchPath.appendingPathComponent("local/\($0.root.lastPathComponent)", isDirectory: true))
            }
    }

    /* What one check is started with, the words the build-input snapshot keys on. */
    private func arguments(for check: (root: URL, scratchPath: URL)) -> [String] {
        ["check", check.root.path, "--scratch-path", check.scratchPath.path]
            + (platform.map { ["--triple", $0.triple, "--sdk", $0.sdk] } ?? [])
            + (resolutionAllowed ? [] : ["--disable-automatic-resolution"])
    }

    /* The package roots and every path dependency, which together hold every file a check of them reads. */
    static func inputRoots(of package: PackageModel) -> [URL] {
        let roots = package.allPackages.flatMap { [$0.root] + $0.pathDependencyRoots }
        var seen = Set<String>()
        return roots.filter { root in
            !roots.contains { other in PackageModel.isInside(root, other) }
                && seen.insert(root.resolvingSymlinksInPath().path).inserted
        }
    }

    func run() throws -> Result {
        /* A package whose platforms leave out macOS is never judged against the host's SDK, which it does not target. */
        if platform == nil, PlatformPackageTypesPhase.leavesOutHost(package) {
            return try PlatformPackageTypesPhase(
                package: package,
                files: files,
                scratchPath: scratchPath,
                resolutionAllowed: resolutionAllowed,
                toolchain: toolchain,
                runner: runner,
            )
            .run()
        }
        let start = Date()
        let snapshot =
            toolchain.hasPrefix("unknown")
            ? nil
            : BuildInputSnapshot.take(
                roots: Self.inputRoots(of: package),
                resolved: package.root.appendingPathComponent("Package.resolved"),
                toolchain: toolchain,
                arguments: checks.flatMap(arguments(for:)),
            )

        /*
         Nothing a check reads changed since the last check that succeeded, so its records are this check's
         records. Read them, and keep them only if every file of ours has one newer than its source: anything
         less (a scratch someone cleaned, a record gone) means checking after all. A file whose bytes are again
         the ones that check saw is unchanged too, an edit and its revert, judged by the time the check saw.
         */
        let stored = BuildInputSnapshot.stored(scratchPath: scratchPath)
        let changes = snapshot.flatMap { snapshot in stored.flatMap { snapshot.changes(since: $0) } }
        if let stored, let changes, !changes.edited.isEmpty,
            let checked = checkBodiesOnly(changes: changes, stored: stored, start: start)
        {
            return checked
        }
        if let changes, changes.edited.isEmpty {
            do {
                let directories = try checks.flatMap { try recordDirectories(under: $0.scratchPath) }
                if !directories.isEmpty {
                    let restoredWords =
                        changes.restored.isEmpty
                        ? ""
                        : " (\(changes.restored.count == 1 ? "1 file is" : "\(changes.restored.count) files are") \(Self.restoredWords))"
                    let reused = try read(
                        targetDirectories: directories,
                        start: start,
                        build:
                            "\(Self.reusedWords)\(restoredWords), so its compiler records were read without checking",
                        restored: changes.restored,
                    )
                    if reused.record.filesWithoutRecord.isEmpty {
                        return reused
                    }
                }
            }
            catch {
                /* The records could not be read as they stood: check, which rewrites them. */
            }
        }

        /*
         Each file's interface and bytes as this check sees them, so the next run can tell whether an edit stayed
         inside bodies, and whether an edit was undone.
         */
        let fingerprints = snapshot == nil ? Fingerprints() : fingerprints()
        let lastRecorded = BuildInputSnapshot.stored(scratchPath: scratchPath, kind: .lastRecorded)
        let scopeChanges = snapshot.flatMap { snapshot in
            lastRecorded.flatMap { snapshot.changes(since: $0, allowingFileSetChanges: true) }
        }
        let checked = try check(scope: scopeChanges.flatMap(scope(of:)))
        if var snapshot, checked.succeeded {
            snapshot.interfaces = fingerprints.interfaces
            snapshot.contents = fingerprints.contents
            snapshot.store(scratchPath: scratchPath)
        }
        else {
            BuildInputSnapshot.forget(scratchPath: scratchPath)
        }
        var build = checked.words(scratchPath: scratchPath)
        /* Only after every check succeeded, so no check of this run is still writing into the scratch. */
        if checked.succeeded {
            let pruned = pruneScratch()
            if !pruned.sentence.isEmpty {
                build += "; \(pruned.sentence)"
            }
        }
        let result = try read(
            targetDirectories: checked.targetDirectories,
            failures: checked.failures,
            extraFindings: checked.findings,
            start: start,
            build: build,
        )
        /*
         A check that left every file a record, its errors included, is the base the next check's scope is measured
         from. One a compiler crashed in, or that failed outside every file, is not: the next check looks at everything.
         */
        if var snapshot, result.record.filesWithoutRecord.isEmpty,
            !result.findings.contains(where: { ["compilerCrashed", "buildFailed"].contains($0.messageId) })
        {
            snapshot.interfaces = fingerprints.interfaces
            snapshot.contents = fingerprints.contents
            snapshot.store(scratchPath: scratchPath, kind: .lastRecorded)
        }
        else {
            BuildInputSnapshot.forget(scratchPath: scratchPath, kind: .lastRecorded)
        }
        return result
    }

    /* The words the types record begins with when no input changed, and those for a file whose edit was undone. */
    static let reusedWords = "no input changed since the last successful check"
    static let restoredWords = "again the bytes it checked"

    /* What this run's checks did, across the root and its local packages. */
    struct CheckRun {
        var targetDirectories: [(String, URL)] = []
        /* Findings no owned Swift file's record holds: a compiler crash, a C error. */
        var findings: [FindingRecord] = []
        /* Each frontend or clang job that failed without a signal, in its own words. */
        var failures: [String] = []
        var modules = 0
        /* The owned modules outside this run's scope, whose records the last check left were read again. */
        var modulesKept = 0
        /* The scoped modules checked while prepare ran, since they import no module it emitted again. */
        var modulesAlongsidePrepare = 0
        var files = 0
        var jobs = 0
        var rerunFiles = 0
        var cFiles = 0
        /* The dependencies indexed for their declarations this run, Swift modules and C-family files. */
        var dependencyModules = 0
        var dependencyCFiles = 0
        var indexSeconds = 0.0
        var prepareSeconds = 0.0
        var checkSeconds = 0.0
        var toolsBuilt: [BuildServerClient.ToolBuilt] = []
        /* The build-tool plugins SwiftPM compiled this run so it could run them, by name. */
        var pluginsCompiled: [String] = []

        /* No job failed and nothing crashed: what lets the build-input snapshot vouch for these records next time. */
        var succeeded: Bool { failures.isEmpty && findings.isEmpty }

        /*
         The types record's words. The one build a check allows is counted on every run, as the Go engine counts cgo's
         translation: which plugin host tools prepare built and how long each took, or that it built none.
         */
        func words(scratchPath: URL) -> String {
            var words =
                "checked without building: the build server prepared the dependencies in \(Self.seconds(prepareSeconds)), and \(jobs) frontend jobs checked \(files) files of \(modules) modules with -emit-sil in \(Self.seconds(checkSeconds))"
            if rerunFiles > 0 {
                words += ", \(rerunFiles) of them again alone after their batch failed"
            }
            if modulesAlongsidePrepare > 0 {
                words +=
                    ", \(modulesAlongsidePrepare == 1 ? "1 module" : "\(modulesAlongsidePrepare) modules") of them alongside prepare, since they import no edited module"
            }
            if modulesKept > 0 {
                words +=
                    ", \(modulesKept == 1 ? "1 module" : "\(modulesKept) modules") no edit could change read from the last check's records"
            }
            if cFiles > 0 {
                words += "; clang -fsyntax-only checked \(cFiles) C-family files"
            }
            if dependencyModules > 0 || dependencyCFiles > 0 {
                words +=
                    "; indexed the declarations of \(dependencyModules) dependency modules and \(dependencyCFiles) of their C-family files in \(Self.seconds(indexSeconds))"
            }
            words += ", scratch \(scratchPath.path); "
            if toolsBuilt.isEmpty {
                words += "prepare built no plugin host tool"
            }
            else {
                let tools = toolsBuilt.map { "\($0.name) in \(Self.seconds($0.seconds))" }.joined(separator: ", ")
                words +=
                    "prepare built \(toolsBuilt.count == 1 ? "1 plugin host tool" : "\(toolsBuilt.count) plugin host tools"), the build a check allows, a tool a build-tool plugin runs and not the project's code: \(tools)"
            }
            if !pluginsCompiled.isEmpty {
                words +=
                    "; SwiftPM compiled \(pluginsCompiled.count == 1 ? "1 build-tool plugin" : "\(pluginsCompiled.count) build-tool plugins") to run them: \(pluginsCompiled.joined(separator: ", "))"
            }
            return words
        }

        /* Seconds to one decimal, `12.3s`. */
        static func seconds(_ seconds: Double) -> String {
            let tenths = Int((seconds * 10).rounded())
            return "\(tenths / 10).\(tenths % 10)s"
        }
    }

    /*
     The checks themselves: each package's build server session, then its owned modules not yet checked by an
     earlier session, then its owned C-family files. A server that fails is thrown as the named gap it is. With a
     scope, only its targets are checked again and every other one's records from the last check are read.
     */
    private func check(scope: Scope?) throws -> CheckRun {
        var run = CheckRun()
        let targetByFile = Dictionary(
            files.map { ($0.url.resolvingSymlinksInPath().path, $0.targetName) },
            uniquingKeysWith: { first, _ in first },
        )
        let ownedRoots = package.allPackages.map(\.root)
        var checkedTargets = Set<String>()
        let swiftc = try FrontendCheck.toolchainExecutable(named: "swiftc", runner: runner)
        let clang = try FrontendCheck.toolchainExecutable(named: "clang", runner: runner)
        for session in checks {
            let prepareStart = Date()
            let diagnosticsRoot = Self.checkDirectory(scratchPath: session.scratchPath)
            let frontend = FrontendCheck(
                swiftc: swiftc,
                diagnosticsRoot: diagnosticsRoot,
                indexStorePath: IndexStore.storePath(layout: .check(scratchPath: session.scratchPath)),
                workingDirectory: session.root,
                runner: runner,
            )
            /* A target outside the scope keeps its records only when the last check left them. */
            let recorded = Set(try recordDirectories(under: session.scratchPath).map(\.0))
            let independent = (scope?.independent ?? []).intersection(recorded)
            let alreadyChecked = checkedTargets
            let early = EarlyCheck()
            var alongside: (@Sendable (BuildServerClient.Description) -> Void)?
            if !independent.isEmpty {
                alongside = { @Sendable (known: BuildServerClient.Description) -> Void in
                    let first = Self.ownedModules(of: known, targetByFile: targetByFile, excluding: alreadyChecked)
                        .filter { independent.contains($0.name) }
                    let start = Date()
                    let outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>
                    do throws(FrontendCheck.Failure) {
                        outcome = .success(try frontend.check(first))
                    }
                    catch {
                        outcome = .failure(error)
                    }
                    early.keep(modules: first, outcome: outcome, seconds: Date().timeIntervalSince(start))
                }
            }
            let description: BuildServerClient.Description
            do {
                description = try BuildServerClient.describe(
                    configuration: .init(
                        packageRoot: session.root,
                        scratchPath: session.scratchPath,
                        triple: platform?.triple,
                        sdk: platform?.sdk,
                        resolutionAllowed: resolutionAllowed,
                    ),
                    alongside: alongside,
                )
            }
            catch {
                throw TypesFailure(
                    description:
                        "the check of \(session.root.path) failed before compiling anything, so the compiler never ran: \(error)\(unresolvableWords(error.description))"
                )
            }
            run.prepareSeconds += Date().timeIntervalSince(prepareStart)
            run.toolsBuilt += description.prepared?.toolsBuilt ?? []
            run.pluginsCompiled += Self.pluginsCompiled(scratchPath: session.scratchPath, since: prepareStart)
            if session.scratchPath != scratchPath {
                ScratchPrune.markUsed(session.scratchPath)
            }

            let ownedModules = Self.ownedModules(
                of: description,
                targetByFile: targetByFile,
                excluding: alreadyChecked,
            )
            checkedTargets.formUnion(ownedModules.map(\.name))
            let modules = ownedModules.filter {
                (scope?.targets.contains($0.name) ?? true) || !recorded.contains($0.name)
            }
            run.targetDirectories += ownedModules.filter { !modules.contains($0) }.map {
                ($0.name, FrontendCheck.moduleDirectory(in: diagnosticsRoot, module: $0.name))
            }
            run.modulesKept += ownedModules.count - modules.count
            let checkStart = Date()
            let checkedEarly = early.modules
            var outcomes: [FrontendCheck.Outcome] = []
            do {
                if let earlier = early.outcome {
                    outcomes.append(try earlier.get())
                }
                outcomes.append(try frontend.check(modules.filter { !checkedEarly.contains($0) }))
            }
            catch {
                throw TypesFailure(description: "\(error)")
            }
            CompileCommands(commands: ownedModules.compactMap(CompileCommands.command(checking:)))
                .store(scratchPath: session.scratchPath)
            run.modules += modules.count
            run.modulesAlongsidePrepare += checkedEarly.count
            run.files += modules.reduce(0) { $0 + $1.sources.count }
            for outcome in outcomes {
                run.jobs += outcome.jobs
                run.rerunFiles += outcome.rerunPrimaries.count
                run.targetDirectories += outcome.targetDirectories
                run.findings += outcome.crashes.map(Self.finding(for:))
                run.failures += outcome.failedJobs.map(\.standardError)
            }
            run.checkSeconds += Date().timeIntervalSince(checkStart) + early.seconds

            /*
             What a build left in its store beside the project's own files, the dependencies' declarations, which
             `--unused` and the typed rules resolve references through. Measured 2026-11-06 on ahraos-macos: without
             them, 8 files' imports went unchecked ("a reference in it resolves to no module the index declares"), and
             MarkdownUI, NetworkImage, SwiftTerm and cmark's modules were not in the store. A file a unit newer than it
             already describes is not indexed again, so a check after an edit indexes nothing of a dependency's.
             */
            let indexStart = Date()
            let storePath = IndexStore.storePath(layout: .check(scratchPath: session.scratchPath))
            let indexed = freshlyIndexed(storePath: storePath)
            func resolved(_ path: String) -> String { URL(fileURLWithPath: path).resolvingSymlinksInPath().path }
            let dependencies = FrontendCheck.modules(from: description.options).filter { module in
                !module.sources.contains { targetByFile[resolved($0)] != nil }
                    && module.sources.contains { !indexed.contains(resolved($0)) }
                    && checkedTargets.insert("dependency/\(module.name)").inserted
            }
            if !dependencies.isEmpty {
                var indexer = frontend
                indexer.mode = .indexDeclarations
                do {
                    _ = try indexer.check(dependencies)
                }
                catch {
                    throw TypesFailure(description: "\(error)")
                }
                run.dependencyModules += dependencies.count
            }

            let cFamily = CSyntaxCheck.sources(of: description.targets, options: description.options)
            let ownedC = cFamily.filter { source in
                ownedRoots.contains { PackageModel.isInside(source.file, $0) }
                    && !PackageModel.isInside(source.file, session.scratchPath)
            }
            let ownedCPaths = Set(ownedC.map { resolved($0.file.path) })
            /* An owned C file outside the scope keeps the record and unit the last check left for it. */
            let cRecorded = { (source: CSyntaxCheck.Source) -> Bool in
                FileManager.default.fileExists(
                    atPath: CSyntaxCheck.recordPath(in: diagnosticsRoot, of: source).path
                )
            }
            let keptC = ownedC.filter { source in
                !(scope?.cFamily ?? true) && cRecorded(source) && indexed.contains(resolved(source.file.path))
            }
            let keptCPaths = Set(keptC.map { resolved($0.file.path) })
            let cSources = cFamily.filter { source in
                let path = resolved(source.file.path)
                return
                    ((ownedCPaths.contains(path) && !keptCPaths.contains(path))
                    || (!ownedCPaths.contains(path) && !indexed.contains(path)))
                    && checkedTargets.insert("C/\(path)").inserted
            }
            for source in keptC where checkedTargets.insert("C/\(resolved(source.file.path))").inserted {
                if !run.targetDirectories.contains(where: { $0.0 == source.target }) {
                    run.targetDirectories.append(
                        (source.target, FrontendCheck.moduleDirectory(in: diagnosticsRoot, module: source.target))
                    )
                }
                run.findings += try cErrors(in: [
                    FrontendCheck.Record(
                        module: source.target,
                        file: source.file.path,
                        diagnostics: CSyntaxCheck.recordPath(in: diagnosticsRoot, of: source),
                        unitOutputPath: "",
                    )
                ])
            }
            if !cSources.isEmpty {
                let cOutcome: CSyntaxCheck.Outcome
                do {
                    var cCheck = CSyntaxCheck(
                        clang: clang,
                        diagnosticsRoot: diagnosticsRoot,
                        workingDirectory: session.root,
                        runner: runner,
                    )
                    cCheck.indexStorePath = storePath
                    cOutcome = try cCheck.check(cSources)
                }
                catch {
                    throw TypesFailure(description: "\(error)")
                }
                run.cFiles += cSources.filter { ownedCPaths.contains(resolved($0.file.path)) }.count
                run.dependencyCFiles += cSources.filter { !ownedCPaths.contains(resolved($0.file.path)) }.count
                let ownedRecords = cOutcome.records.filter { ownedCPaths.contains(resolved($0.file)) }
                let errors = try cErrors(in: ownedRecords)
                run.findings += cOutcome.crashes.filter { ownedCPaths.contains(resolved($0.file)) }.map(
                    Self.finding(for:)
                )
                run.findings += errors
                /* A file clang rejected without an error placed in it failed somewhere else, a header or its arguments. */
                run.failures +=
                    cOutcome.failedJobs.filter { failed in
                        failed.primaries.contains { ownedCPaths.contains(resolved($0)) }
                            && !errors.contains { finding in failed.primaries.contains { resolved($0) == finding.file }
                            }
                    }
                    .map(\.standardError)
            }
            run.indexSeconds += Date().timeIntervalSince(indexStart)
        }
        return run
    }

    /*
     The plugins SwiftPM compiled into `<scratch>/plugins/cache` since `start`: each plugin is an executable named for
     it there, beside its state, its record and its debug symbols. Measured 2026-11-06 on ahraos-presence: mlx-swift's
     `CudaBuild`, compiled once into a fresh scratch and reused after.
     */
    static func pluginsCompiled(scratchPath: URL, since start: Date) -> [String] {
        let cache = scratchPath.appendingPathComponent("plugins/cache", isDirectory: true)
        let entries =
            (try? FileManager.default.contentsOfDirectory(
                at: cache,
                includingPropertiesForKeys: [.contentModificationDateKey, .isRegularFileKey],
            )) ?? []
        return entries.filter { entry in
            guard entry.pathExtension.isEmpty, FileManager.default.isExecutableFile(atPath: entry.path),
                let values = try? entry.resourceValues(forKeys: [.contentModificationDateKey, .isRegularFileKey]),
                values.isRegularFile == true, let modified = values.contentModificationDate
            else { return false }
            return modified >= start
        }
        .map(\.lastPathComponent).sorted()
    }

    /* The files, by resolved path, that a unit of ours in this store describes and was written after. */
    private func freshlyIndexed(storePath: URL) -> Set<String> {
        guard FileManager.default.fileExists(atPath: storePath.path),
            let library = try? IndexStore.toolchainLibraryPath(runner: runner),
            let store = try? IndexStore(libraryPath: library, storePath: storePath)
        else { return [] }
        var fresh = Set<String>()
        for (path, described) in IndexStore.newestUnits(in: [store]) {
            let modified =
                (try? URL(fileURLWithPath: path).resourceValues(forKeys: [.contentModificationDateKey]))?
                .contentModificationDate ?? .distantFuture
            if described.unit.written >= modified {
                fresh.insert(path)
            }
        }
        return fresh
    }

    /* The owned Swift modules the server's options compile, one per target, named by the target, those checked by an earlier session left out. */
    static func ownedModules(
        of description: BuildServerClient.Description,
        targetByFile: [String: String],
        excluding checked: Set<String>,
    ) -> [FrontendCheck.Module] {
        var seen = checked
        return FrontendCheck.modules(from: description.options).compactMap { module -> FrontendCheck.Module? in
            guard
                let target = module.sources.lazy.compactMap({
                    targetByFile[URL(fileURLWithPath: $0).resolvingSymlinksInPath().path]
                }).first,
                seen.insert(target).inserted
            else { return nil }
            return FrontendCheck.Module(name: target, arguments: module.arguments)
        }
    }

    /* The check that ran alongside prepare, kept for the thread that waits for it. */
    private final class EarlyCheck: Sendable {
        private struct State {
            var modules: [FrontendCheck.Module] = []
            var outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>?
            var seconds = 0.0
        }

        private let state = Mutex(State())

        var modules: [FrontendCheck.Module] { state.withLock { $0.modules } }
        var outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>? { state.withLock { $0.outcome } }
        var seconds: Double { state.withLock { $0.seconds } }

        func keep(
            modules: [FrontendCheck.Module],
            outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>,
            seconds: Double,
        ) {
            state.withLock { $0 = State(modules: modules, outcome: outcome, seconds: seconds) }
        }
    }

    /* A compiler crash, a finding at the file it crashed on: a dependency emitted with errors can crash its dependents. */
    static func finding(for crash: FrontendCheck.Crash) -> FindingRecord {
        FindingRecord(
            source: .compiler,
            file: URL(fileURLWithPath: crash.file).resolvingSymlinksInPath().path,
            line: 1,
            column: 1,
            severity: .error,
            rule: "",
            messageId: "compilerCrashed",
            message: crash.description,
        )
    }

    /*
     The errors clang reported in the C-family files themselves, each a finding at its place. Only errors: a build
     never reported a C file's warnings, since no C file is one the engine owns, but it stopped at a C error.
     */
    private func cErrors(in records: [FrontendCheck.Record]) throws -> [FindingRecord] {
        let reader = try SerializedDiagnosticsReader(
            libraryPath: SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner)
        )
        var findings: [FindingRecord] = []
        for record in records where FileManager.default.fileExists(atPath: record.diagnostics.path) {
            let file = URL(fileURLWithPath: record.file).resolvingSymlinksInPath().path
            for diagnostic in (try? reader.read(record.diagnostics)) ?? []
            where diagnostic.severity == .error
                && URL(fileURLWithPath: diagnostic.file).resolvingSymlinksInPath().path == file
            {
                findings.append(
                    FindingRecord(
                        source: .compiler,
                        file: file,
                        line: max(diagnostic.line, 1),
                        column: max(diagnostic.column, 1),
                        severity: .error,
                        /* clang's category, "Semantic Issue", names no warning group, so the rule is empty as for a Swift error in none. */
                        rule: "",
                        messageId: "",
                        message: diagnostic.message,
                    )
                )
            }
        }
        return findings
    }

    /*
     What a check after an edit must look at again: the targets holding an edited file and every owned target that
     imports one of them, directly or through another, and whether any C-family file or header changed.
     */
    struct Scope: Equatable, Sendable {
        var targets: Set<String>
        var cFamily: Bool
        /* Each owned target's imported modules, by target name. */
        var imports: [String: Set<String>] = [:]

        /*
         The scope's targets that import no other target of the scope. Their check needs no module prepare is about to
         emit again, since every module they import is one no edit changed, so it runs alongside prepare.
         */
        var independent: Set<String> {
            let names = Set(targets.map(TypesPhase.moduleName(of:)))
            return targets.filter { target in
                imports[target, default: []].subtracting([TypesPhase.moduleName(of: target)]).isDisjoint(with: names)
            }
        }
    }

    /* A target's module name, as SwiftPM makes it: every character that cannot be in an identifier made `_`. */
    static func moduleName(of target: String) -> String {
        String(target.map { $0.isLetter || $0.isNumber || $0 == "_" ? $0 : "_" })
    }

    static let cFamilyExtensions: Set<String> = [
        "c", "cc", "cpp", "cxx", "m", "mm", "h", "hh", "hpp", "hxx", "inl", "inc", "modulemap",
    ]

    /*
     The scope of a check after these changes since the last check that recorded every file, files added and removed
     among them, or nil for the whole package. Only the compiler's own knowledge of what
     depends on what is safe here, and the build server's target graph holds no edge across packages (measured
     2026-11-06 on ahraos-presence: AhraOSPresence lists none of the local packages it uses, Voice not MLX), so the
     graph is the imports each owned file makes: a module can change what the compiler says of another only through an
     import, and a chain of them, a re-export among them, is followed to its end. Any edit to something that is
     neither an owned Swift file nor a C-family file or header, a manifest or a resource, checks everything.
     */
    func scope(of changes: BuildInputSnapshot.Changes) -> Scope? {
        let owned = Dictionary(
            files.map { ($0.url.resolvingSymlinksInPath().path, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        var changedTargets = Set<String>()
        var cFamily = false
        /*
         A removed Swift file belonged to the target that still owns a file in its folder: its own path cannot be
         resolved once it is gone, but its folder's can. A folder with none left, or any other file removed, checks
         everything.
         */
        for path in changes.removed {
            let removed = URL(fileURLWithPath: path)
            let folder = removed.deletingLastPathComponent().resolvingSymlinksInPath().path
            guard removed.pathExtension == "swift",
                let neighbor = files.first(where: {
                    $0.url.deletingLastPathComponent().resolvingSymlinksInPath().path == folder
                })
            else { return nil }
            changedTargets.insert(neighbor.targetName)
        }
        for path in changes.edited {
            let resolved = URL(fileURLWithPath: path).resolvingSymlinksInPath()
            if let file = owned[resolved.path] {
                changedTargets.insert(file.targetName)
            }
            else if Self.cFamilyExtensions.contains(resolved.pathExtension.lowercased()) {
                cFamily = true
            }
            else {
                return nil
            }
        }
        var imports: [String: Set<String>] = [:]
        for file in files {
            guard let tree = tree(of: file) else { return nil }
            imports[file.targetName, default: []].formUnion(Self.importedModules(of: tree))
        }
        var affected = changedTargets
        var grew = true
        while grew {
            let names = Set(affected.map(Self.moduleName(of:)))
            let reached = imports.filter { !affected.contains($0.key) && !$0.value.isDisjoint(with: names) }.map(\.key)
            affected.formUnion(reached)
            grew = !reached.isEmpty
        }
        return Scope(targets: affected, cFamily: cFamily, imports: imports)
    }

    /*
     The modules a file imports at its top level, whatever the import's kind or attributes, in every branch of every
     `#if` however deep, so a configuration this check does not compile can only widen the scope, never narrow it.
     */
    static func importedModules(of tree: SourceFileSyntax) -> Set<String> {
        importedModules(in: tree.statements)
    }

    private static func importedModules(in statements: CodeBlockItemListSyntax) -> Set<String> {
        var modules = Set<String>()
        for statement in statements {
            if let declaration = statement.item.as(ImportDeclSyntax.self), let first = declaration.path.first {
                modules.insert(first.name.text)
            }
            if let condition = statement.item.as(IfConfigDeclSyntax.self) {
                for branch in condition.clauses {
                    if case .statements(let nested) = branch.elements {
                        modules.formUnion(importedModules(in: nested))
                    }
                }
            }
        }
        return modules
    }

    /* Where a check into this scratch keeps its records: `<scratch>/check/<Target>.build`. */
    static func checkDirectory(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("check", isDirectory: true)
    }

    /* The scratch prune over every scratch this run checked into (`ScratchPrune`). A store that cannot be opened is left as it is. */
    private func pruneScratch() -> ScratchPrune.Outcome {
        guard let library = try? IndexStore.toolchainLibraryPath(runner: runner) else { return ScratchPrune.Outcome() }
        let stores = checks.compactMap { check -> (store: IndexStore, storePath: URL)? in
            let storePath = IndexStore.storePath(layout: .check(scratchPath: check.scratchPath))
            return (try? IndexStore(libraryPath: library, storePath: storePath)).map { ($0, storePath) }
        }
        return ScratchPrune.run(
            stores: stores,
            scratchPath: scratchPath,
            listedLocalPackages: Set(package.localPackages.map(\.root.lastPathComponent)),
        )
    }

    /* The words the types record uses for a run that checked its edited files in process, so a reader can tell it from a whole check. */
    static let checkedInProcessWords = "changed inside function bodies only, so sourcekitd checked them in this process"

    /*
     The edited files checked in sourcekitd, and every other file read from the last check's records, when the
     edits cannot have changed what the compiler says about any other file. Nil whenever that is not certain,
     which means checking: anything but owned Swift files changed, a file's interface changed, the last check
     left no interface for it, the check kept no compile arguments for it, or sourcekitd could not answer.
     Measured on Presence before this path: a one-file edit cost a 19 to 33s incremental build.

     Each file is asked about only when it changed since the last run asked (`BodiesOnlyAnswers`), and a file
     whose bytes are again the build's is not asked at all (`BuildInputSnapshot.changes(since:)`).
     */
    private func checkBodiesOnly(
        changes: BuildInputSnapshot.Changes,
        stored: BuildInputSnapshot,
        start: Date,
    ) -> Result? {
        let owned = Dictionary(
            files.map { ($0.url.resolvingSymlinksInPath().path, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        var edited: [(path: String, file: FileSet.OwnedFile)] = []
        var interfaces: [String: String] = [:]
        for path in changes.edited {
            let resolved = URL(fileURLWithPath: path).resolvingSymlinksInPath().path
            guard let file = owned[resolved], let before = stored.interfaces[resolved], let tree = tree(of: file)
            else { return nil }
            let interface = InterfaceFingerprint.of(tree)
            guard interface == before else { return nil }
            edited.append((resolved, file))
            interfaces[resolved] = interface
        }
        do {
            var tables = [try CompileCommands.read(layout: .check(scratchPath: scratchPath))]
            for check in checks.dropFirst() where FileManager.default.fileExists(atPath: check.scratchPath.path) {
                tables.append(try CompileCommands.read(layout: .check(scratchPath: check.scratchPath)))
            }
            let earlier = BodiesOnlyAnswers.stored(scratchPath: scratchPath)
            var kept = BodiesOnlyAnswers()
            var replayed = 0
            var answered: [String: [FindingRecord]] = [:]
            for (path, file) in edited.sorted(by: { $0.path < $1.path }) {
                guard let command = tables.lazy.compactMap({ $0.command(for: file.url) }).first else { return nil }
                /* Read once: these bytes are both what the key holds and the text sourcekitd checks. */
                let contents = try Data(contentsOf: URL(fileURLWithPath: path))
                guard let text = String(data: contents, encoding: .utf8) else { return nil }
                let key = try BodiesOnlyAnswers.key(
                    contents: contents,
                    arguments: command.arguments,
                    build: stored.fingerprint,
                    siblingInterfaces: BodiesOnlyAnswers.siblingInterfaces(
                        of: path,
                        current: interfaces,
                        built: stored.interfaces,
                    ),
                )
                let answer = try kept.answer(path: path, key: key, replaying: earlier) {
                    try Self.findings(
                        in: Sourcekitd.shared(runner: runner).diagnostics(
                            file: path,
                            arguments: command.arguments,
                            text: text,
                        ),
                        of: path,
                    )
                }
                answered[path] = answer.findings
                replayed += answer.replayed ? 1 : 0
            }
            let directories = try checks.flatMap { try recordDirectories(under: $0.scratchPath) }
            guard !directories.isEmpty else { return nil }
            let replayedWords =
                replayed == 0 ? "" : ", \(replayed) of them answered by an earlier run's check of the same text"
            let result = try read(
                targetDirectories: directories,
                start: start,
                build:
                    "\(edited.count == 1 ? "1 file" : "\(edited.count) files") \(Self.checkedInProcessWords)\(replayedWords), and the rest were read from the last successful check's compiler records",
                answeredInProcess: answered,
                restored: changes.restored,
            )
            guard result.record.filesWithoutRecord.isEmpty else { return nil }
            kept.store(scratchPath: scratchPath)
            return result
        }
        catch {
            /* Anything this path could not do, a whole check does. */
            return nil
        }
    }

    /* sourcekitd's diagnostics placed in this file, as the findings a build's record of it would give. */
    static func findings(in diagnostics: [Sourcekitd.Diagnostic], of path: String) -> [FindingRecord] {
        diagnostics.compactMap { diagnostic in
            guard let severity = diagnostic.findingSeverity,
                URL(fileURLWithPath: diagnostic.file).resolvingSymlinksInPath().path == path
            else { return nil }
            return FindingRecord(
                source: .compiler,
                file: path,
                line: max(diagnostic.line, 1),
                column: max(diagnostic.column, 1),
                severity: severity,
                rule: TypesOracle.group(of: diagnostic),
                messageId: "",
                message: diagnostic.message,
            )
        }
    }

    /* Every owned file's interface and content hash, by resolved path. */
    struct Fingerprints {
        var interfaces: [String: String] = [:]
        var contents: [String: String] = [:]
    }

    /*
     Every owned file's interface and bytes, by resolved path, fingerprinted side by side. A file that cannot be
     read has neither, so an edit to it always checks again, and its revert is never taken for unchanged.
     */
    private func fingerprints() -> Fingerprints {
        let owned = files
        let byPath = Mutex(Fingerprints())
        DispatchQueue.concurrentPerform(iterations: owned.count) { index in
            guard let interface = tree(of: owned[index]).map(InterfaceFingerprint.of) else { return }
            let resolved = owned[index].url.resolvingSymlinksInPath()
            let contents = PackageDescriptionCache.fingerprint(of: resolved)
            byPath.withLock { fingerprints in
                fingerprints.interfaces[resolved.path] = interface
                if contents != "absent" {
                    fingerprints.contents[resolved.path] = contents
                }
            }
        }
        return byPath.withLock { $0 }
    }

    private func tree(of file: FileSet.OwnedFile) -> SourceFileSyntax? {
        if let tree = parsed[file.url.path] {
            return tree
        }
        guard let source = try? String(contentsOf: file.url, encoding: .utf8) else { return nil }
        return Parser.parse(source: source)
    }

    /* Every owned file's diagnostics from the records under these target directories, and which files have none, judged by `CompilerRecords`. */
    func read(
        targetDirectories: [(String, URL)],
        failures: [String] = [],
        extraFindings: [FindingRecord] = [],
        start: Date,
        build: String,
        answeredInProcess: [String: [FindingRecord]] = [:],
        restored: [String: Date] = [:],
    ) throws -> Result {
        let reading = try CompilerRecords(files: files, runner: runner).read(
            targetDirectories: targetDirectories,
            answeredInProcess: answeredInProcess,
            restored: restored,
        )
        var findings = reading.findings + extraFindings
        let withoutRecord = reading.filesWithoutRecord

        /* A failed check with no error in any file of ours failed somewhere no file carries: a dependency, a header. Said, never swallowed. */
        if let failed = failures.first, !findings.contains(where: { $0.severity == .error }) {
            findings.append(
                FindingRecord(
                    source: .compiler,
                    file: package.root.appendingPathComponent("Package.swift").path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: "",
                    messageId: "buildFailed",
                    message:
                        "the check failed outside any file this run checks, so the package does not type-check: \(failed)",
                )
            )
        }

        findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        let record = TypesRecord(
            diagnostics: findings.count,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: withoutRecord.sorted(),
            build: build,
        )
        return Result(findings: findings, record: record, hasErrors: findings.contains { $0.severity == .error })
    }

    /*
     Why `--no-fix` cannot resolve what a package without a Package.resolved needs, said after SwiftPM's words.
     The Xcode phase resolves into the scratch (#s8p20fe), but SwiftPM keeps a package's pins only beside its
     Package.swift and offers no flag to keep them elsewhere (6.4.2's `swift build --help` and `swift package
     --help`, 2026-11-05), so resolving there would write into the tree. Measured on a fresh scratch: with the
     file present the pinned revisions clone into the scratch and the build succeeds, so only its absence is a gap.
     */
    private func unresolvableWords(_ said: String) -> String {
        guard !resolutionAllowed,
            said.contains(
                "a resolved file is required when automatic dependency resolution is disabled"
            )
        else { return "" }
        return
            ". Under --no-fix nothing is written into the tree, and SwiftPM keeps a package's resolution only in the Package.resolved beside its Package.swift, so a check cannot resolve this package: commit its Package.resolved, or run without --no-fix once"
    }

    /* The check could not run, or its records could not be read at all. */
    struct TypesFailure: Error, CustomStringConvertible {
        var description: String
    }

    /* The record directory a check into this scratch left for each target we own files in, `<Target>.build`, with that target's name. */
    private func recordDirectories(under scratchPath: URL) throws -> [(String, URL)] {
        let root = Self.checkDirectory(scratchPath: scratchPath)
        guard FileManager.default.fileExists(atPath: root.path) else { return [] }
        let targetNames = Set(files.map(\.targetName))
        return try FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil).compactMap {
            directory in
            let name = directory.lastPathComponent
            guard name.hasSuffix(".build") else { return nil }
            let target = String(name.dropLast(".build".count))
            return targetNames.contains(target) ? (target, directory) : nil
        }
    }

    /*
     Every build directory belonging to a target we own files in, under an xcodebuild's intermediates and
     configuration folder (`Build/Intermediates.noindex` and `Debug-iphoneos`), with that target's name:
     `<Target>-p.build`, `<Target>-t.build`, and the testable variants (`XcodeTypesPhase`).
     */
    static func buildDirectories(
        in intermediatesDirectory: URL,
        configuration configurationName: String,
        package: PackageModel,
        files: [FileSet.OwnedFile],
    ) throws -> [(String, URL)] {
        let targetNames = Set(files.map(\.targetName))
        let productTargets = Self.productTargets(of: package)
        let manager = FileManager.default
        guard manager.fileExists(atPath: intermediatesDirectory.path) else { return [] }
        var directories: [(String, URL)] = []
        for packageDirectory in try manager.contentsOfDirectory(
            at: intermediatesDirectory,
            includingPropertiesForKeys: nil,
        ) {
            let configuration = packageDirectory.appendingPathComponent(configurationName, isDirectory: true)
            guard manager.fileExists(atPath: configuration.path) else { continue }
            for targetDirectory in try manager.contentsOfDirectory(at: configuration, includingPropertiesForKeys: nil) {
                let name = targetDirectory.lastPathComponent
                guard name.hasSuffix(".build"), !name.contains("-product-") else { continue }
                /*
                 Matched by the directory's exact stem, never by prefix: `cohere-swift-` is a prefix of
                 `cohere-swift-parity-p.build`, and `AhraOsServices-` of nothing it should not be only by luck.
                 */
                let stem = Self.directoryStem(name)
                if let target = targetNames.contains(stem) ? stem : productTargets[stem], targetNames.contains(target) {
                    directories.append((target, targetDirectory))
                }
            }
        }
        return directories
    }

    /* Every single-target product of every package an xcodebuild compiles, to its target. */
    private static func productTargets(of package: PackageModel) -> [String: String] {
        package.allPackages.reduce(into: [:]) { merged, member in
            merged.merge(member.productTargets) { first, _ in first }
        }
    }

    /*
     The target or product a build directory belongs to: `AhraOs-p.build` and `CohereSwift-t.build` drop their
     last dash-separated part, and a testable variant, `AhraOsServices--36C56AD10F55DF70-testable-t.build`,
     keeps what comes before its double dash.
     */
    static func directoryStem(_ name: String) -> String {
        let base = String(name.dropLast(".build".count))
        if let variant = base.range(of: "--") {
            return String(base[..<variant.lowerBound])
        }
        guard let lastDash = base.lastIndex(of: "-") else { return base }
        return String(base[..<lastDash])
    }

    /* The last lines of the build's own output, colours stripped, for a failure no file carries. */
    static func tail(of build: ProcessRunner.Result) -> String {
        let text = String(decoding: build.standardOutput, as: UTF8.self) + build.standardError
        let plain = text.replacingOccurrences(
            of: #"\u{1B}\[[0-9;]*m|\u{1B}\]8;;[^\u{1B}]*\u{1B}\\"#,
            with: "",
            options: .regularExpression,
        )
        let errors = plain.split(separator: "\n").filter { $0.contains("error:") }
        return (errors.isEmpty ? plain.split(separator: "\n").suffix(5) : errors.prefix(5)).joined(separator: " / ")
    }
}
