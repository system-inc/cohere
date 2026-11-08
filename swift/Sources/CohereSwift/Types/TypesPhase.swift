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
 - `FrontendCheck` runs each owned module's frontend jobs as `-emit-sib` to /dev/null, with a serialized
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
        /*
         Why the check never started, when it did not (`notChecked`): its finding is the run's gap, no type error in
         any file, so lint still runs behind it and the typed rules say this is why they could not judge a file.
         */
        var notStarted: String? = nil
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
        let checked: CheckRun
        do {
            checked = try check(scope: scopeChanges.flatMap(scope(of:)))
        }
        catch let notStarted as CheckNotStarted {
            BuildInputSnapshot.forget(scratchPath: scratchPath)
            BuildInputSnapshot.forget(scratchPath: scratchPath, kind: .lastRecorded)
            return notChecked(because: notStarted, start: start)
        }
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
        /* The local packages whose session this run skipped, by folder name, since its scope held none of their modules. */
        var sessionsSkipped: [String] = []
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
        /* Whether prepare covered only the edited modules' own dependencies, and the interfaces the check emitted itself, by target. */
        var preparedEditedOnly = false
        var interfacesEmitted: [String] = []
        var emitSeconds = 0.0
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
                "checked without building: the build server prepared the dependencies in \(Self.seconds(prepareSeconds))"
            if preparedEditedOnly {
                words += ", only the edited modules' own"
                if !interfacesEmitted.isEmpty {
                    words +=
                        ", and the check emitted the \(interfacesEmitted.count == 1 ? "interface" : "interfaces") of \(interfacesEmitted.joined(separator: ", ")) itself in \(Self.seconds(emitSeconds))"
                }
            }
            words +=
                ", and \(jobs) frontend jobs checked \(files) files of \(modules) modules with -emit-sib in \(Self.seconds(checkSeconds))"
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
            if !sessionsSkipped.isEmpty {
                words +=
                    ", and no build server started for \(sessionsSkipped.count == 1 ? "the local package" : "the local packages") \(sessionsSkipped.joined(separator: ", ")), since no edit could change \(sessionsSkipped.count == 1 ? "it" : "them")"
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
     earlier session, then its owned C-family files. A server that fails is thrown as the named gap it is
     (`CheckNotStarted`), which `run` reports. With a scope, only its targets are checked again and every other one's
     records from the last check are read.
     */
    private func check(scope given: Scope?) throws -> CheckRun {
        var run = CheckRun()
        var scope = given
        let targetByFile = Dictionary(
            files.map { ($0.url.resolvingSymlinksInPath().path, $0.targetName) },
            uniquingKeysWith: { first, _ in first },
        )
        let ownedRoots = package.allPackages.map(\.root)
        var checkedTargets = Set<String>()
        let swiftc = try FrontendCheck.toolchainExecutable(named: "swiftc", runner: runner)
        let clang = try FrontendCheck.toolchainExecutable(named: "clang", runner: runner)
        for (sessionIndex, session) in checks.enumerated() {
            /* A target outside the scope keeps its records only when the last check left them. */
            let recordedDirectories = try recordDirectories(under: session.scratchPath)
            let recorded = Set(recordedDirectories.map(\.0))
            /* The root's session always starts: its server names the targets an edited C-family file widens the scope by. */
            if sessionIndex > 0, let scope,
                let kept = Self.recordsWithoutSession(
                    last: SessionRecord.stored(scratchPath: session.scratchPath),
                    scope: scope,
                    recorded: recordedDirectories,
                    alreadyChecked: checkedTargets,
                )
            {
                checkedTargets.formUnion(kept.map(\.0))
                run.targetDirectories += kept
                run.modulesKept += kept.count
                run.sessionsSkipped.append(session.root.lastPathComponent)
                ScratchPrune.markUsed(session.scratchPath)
                continue
            }
            let prepareStart = Date()
            let diagnosticsRoot = Self.checkDirectory(scratchPath: session.scratchPath)
            let frontend = FrontendCheck(
                swiftc: swiftc,
                diagnosticsRoot: diagnosticsRoot,
                indexStorePath: IndexStore.storePath(layout: .check(scratchPath: session.scratchPath)),
                workingDirectory: session.root,
                runner: runner,
            )
            /*
             The root's session prepares only the edited modules' own dependencies when the scope holds Swift edits
             alone, and the check emits the interfaces the scoped modules import (`InterfaceEmit`). A local package's
             session, or a scope with a C-family edit, prepares everything as before: a header reaches modules only the
             build server can name. Without a ledger to say which `Products` copies are older than their sources, or
             when a module this check reads is one the ledger holds no current copy of, prepare emits everything too,
             and nothing is checked before it is done. The ledger is read and kept under its lock.
             */
            let interfacesLock = sessionIndex == 0 ? InterfaceEmit.Ledger.Lock(scratchPath: session.scratchPath) : nil
            defer { withExtendedLifetime(interfacesLock) {} }
            let stored = interfacesLock == nil ? nil : InterfaceEmit.Ledger.stored(scratchPath: session.scratchPath)
            let ledger = stored.flatMap { stored in
                Self.readsStale(stored, scope: scope, checking: checkedCandidates(recorded: recorded)) ? nil : stored
            }
            let emittedBefore = (ledger?.targets ?? []).sorted().map {
                InterfaceEmit.Ledger.moduleDirectory(scratchPath: session.scratchPath, target: $0)
            }
            var narrowed = ledger != nil && scope?.cFamilyEdits.isEmpty == true
            let independent =
                sessionIndex == 0 && ledger == nil ? [] : (scope?.independent ?? []).intersection(recorded)
            let alreadyChecked = checkedTargets
            let held = Set(ledger?.targets ?? [])
            let interfaces = InterfaceEmit(swiftc: swiftc, workingDirectory: session.root, runner: runner)
            /* The emits a narrowed prepare leaves the check, planned while prepare runs. */
            let emits = PlannedEmits()
            let planGroup = DispatchGroup()
            defer { planGroup.wait() }
            let early = EarlyCheck()
            /* The check alongside prepare runs on, past the session's end, beside the emits and the other checks. */
            let earlyGroup = DispatchGroup()
            defer { earlyGroup.wait() }
            var alongside: (@Sendable (BuildServerClient.Description) -> Void)?
            if !independent.isEmpty {
                alongside = { @Sendable (known: BuildServerClient.Description) -> Void in
                    let first = Self.ownedModules(of: known, targetByFile: targetByFile, excluding: alreadyChecked)
                        .filter { independent.contains($0.name) }
                        .map { Self.searchingFirst(emittedBefore, in: $0) }
                    /* On a queue of its own, which always gets a thread, as the build server's alongside work does. */
                    DispatchQueue(label: "cohere-swift.types.early-check").async(group: earlyGroup) {
                        let start = Date()
                        let outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>
                        do throws(FrontendCheck.Failure) {
                            outcome = .success(try frontend.check(first))
                        }
                        catch {
                            outcome = .failure(error)
                        }
                        early.keep(outcome: outcome, seconds: Date().timeIntervalSince(start))
                    }
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
                    prepare: { known in
                        guard narrowed, let scope else { return nil }
                        /* An explicit module build reads no search path, so the check's own interfaces cannot reach it. */
                        let owned = Self.ownedModules(of: known, targetByFile: targetByFile, excluding: alreadyChecked)
                        guard !owned.contains(where: { InterfaceEmit.buildsModulesExplicitly($0.arguments) }) else {
                            narrowed = false
                            return nil
                        }
                        let checkingNow = Set(
                            owned.filter { scope.targets.contains($0.name) || !recorded.contains($0.name) }.map(\.name)
                        )
                        /* A scoped module a checked one imports that this session cannot emit is prepared instead. */
                        let ownedNames = Set(owned.map(\.name))
                        let importedScoped = scope.targets.filter { target in
                            checkingNow.contains {
                                scope.imports[$0, default: []].contains(Self.moduleName(of: target))
                            }
                        }
                        guard importedScoped.isSubset(of: ownedNames) else {
                            narrowed = false
                            return nil
                        }
                        let order = Self.interfacesToEmit(
                            owned: Set(owned.map(\.name)),
                            checking: checkingNow,
                            scope: scope,
                            held: held,
                        )
                        let directories = held.union(order).sorted().map {
                            InterfaceEmit.Ledger.moduleDirectory(scratchPath: session.scratchPath, target: $0)
                        }
                        let options = known.options
                        DispatchQueue(label: "cohere-swift.types.interface-plan").async(group: planGroup) {
                            let planned = Swift.Result { () throws(FrontendCheck.Failure) -> [InterfaceEmit.Planned] in
                                var planned: [InterfaceEmit.Planned] = []
                                for target in order {
                                    guard let module = owned.first(where: { $0.name == target }) else { continue }
                                    let arguments = InterfaceEmit.testableArguments(of: module, options: options)
                                    planned.append(
                                        try interfaces.planned(
                                            moduleName: FrontendCheck.Module(arguments: arguments)?.name
                                                ?? Self.moduleName(of: target),
                                            arguments: arguments,
                                            into: InterfaceEmit.Ledger.moduleDirectory(
                                                scratchPath: session.scratchPath,
                                                target: target,
                                            ),
                                            searchPaths: directories,
                                        )
                                    )
                                }
                                return planned
                            }
                            emits.keep(targets: order, directories: directories, planned: planned)
                        }
                        return Self.preparing(known.targets, owning: scope.independent, targetByFile: targetByFile)
                    },
                    alongside: alongside,
                )
            }
            catch {
                throw CheckNotStarted(
                    description:
                        "the check of \(session.root.path) failed before compiling anything, so the compiler never ran: \(error)\(unresolvableWords(error.description))"
                )
            }
            /* The root's server lists the whole graph, local packages' targets among them, so it places edited C files. */
            if sessionIndex == 0 {
                scope = scope?.widened(by: description)
            }
            /* Prepare emitted every module again, so no copy in `Products` is older than its sources. */
            if sessionIndex == 0, !narrowed {
                do {
                    try InterfaceEmit.Ledger(targets: []).store(scratchPath: session.scratchPath)
                }
                catch {
                    InterfaceEmit.Ledger.forget(scratchPath: session.scratchPath)
                }
            }
            run.prepareSeconds += Date().timeIntervalSince(prepareStart)
            run.preparedEditedOnly = run.preparedEditedOnly || narrowed
            run.toolsBuilt += description.prepared?.toolsBuilt ?? []
            run.pluginsCompiled += Self.pluginsCompiled(scratchPath: session.scratchPath, since: prepareStart)
            if session.scratchPath != scratchPath {
                ScratchPrune.markUsed(session.scratchPath)
            }

            let described = Self.ownedModules(
                of: description,
                targetByFile: targetByFile,
                excluding: alreadyChecked,
            )
            checkedTargets.formUnion(described.map(\.name))
            let checking = Set(
                described.filter { (scope?.targets.contains($0.name) ?? true) || !recorded.contains($0.name) }.map(
                    \.name
                )
            )
            let checkStart = Date()
            /* The folders of every interface the check emitted that `Products` holds an older copy of, searched first. */
            var emittedAll: [URL] = []

            /*
             The interfaces a narrowed prepare leaves the check (`interfacesToEmit`), planned while prepare ran and run now
             that it is done. Every module checked after reads them, and the ledger's earlier ones, before anything in
             `Products`.
             */
            if narrowed {
                let emitStart = Date()
                planGroup.wait()
                do throws(FrontendCheck.Failure) {
                    for planned in try emits.planned?.get() ?? [] {
                        try interfaces.run(planned)
                    }
                }
                catch {
                    /* What the ledger said can no longer be trusted, so the next check prepares everything. */
                    InterfaceEmit.Ledger.forget(scratchPath: session.scratchPath)
                    throw TypesFailure(description: "\(error)")
                }
                do {
                    let current = held.union(emits.targets)
                    let left = Set(ledger?.stale ?? []).union(
                        scope.map { $0.targets.intersection(described.map(\.name)) } ?? []
                    )
                    try InterfaceEmit.Ledger(targets: current.sorted(), stale: left.subtracting(current).sorted())
                        .store(
                            scratchPath: session.scratchPath
                        )
                }
                catch {
                    InterfaceEmit.Ledger.forget(scratchPath: session.scratchPath)
                    throw TypesFailure(
                        description: "the check could not keep the list of the interfaces it emitted: \(error)"
                    )
                }
                run.interfacesEmitted += emits.targets
                run.emitSeconds += Date().timeIntervalSince(emitStart)
                emittedAll = emits.directories
            }
            let ownedModules = described.map { Self.searchingFirst(emittedAll, in: $0) }
            let modules = ownedModules.filter { checking.contains($0.name) }
            run.targetDirectories += ownedModules.filter { !checking.contains($0.name) }.map {
                ($0.name, FrontendCheck.moduleDirectory(in: diagnosticsRoot, module: $0.name))
            }
            run.modulesKept += ownedModules.count - modules.count
            /* The modules the check alongside prepare took, which it may still be checking. */
            let checkedEarly = alongside == nil ? [] : checking.intersection(independent)
            var outcomes: [FrontendCheck.Outcome] = []
            do {
                let late = try frontend.check(modules.filter { !checkedEarly.contains($0.name) })
                earlyGroup.wait()
                if let earlier = early.outcome {
                    outcomes.append(try earlier.get())
                }
                outcomes.append(late)
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
            /* The owned C-family files this session took, kept or checked, which an earlier session had not. */
            var ownedCTaken = cSources.filter { ownedCPaths.contains(resolved($0.file.path)) }.count
            for source in keptC where checkedTargets.insert("C/\(resolved(source.file.path))").inserted {
                ownedCTaken += 1
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
            if sessionIndex > 0 {
                SessionRecord(targets: ownedModules.map(\.name).sorted(), ownsCFamily: ownedCTaken > 0)
                    .store(scratchPath: session.scratchPath)
            }
        }
        return run
    }

    /*
     What a local package's session took the last time it started, kept in its scratch beside its records: the owned
     modules it checked or kept, by target, and whether it took any owned C-family file.
     */
    struct SessionRecord: Codable, Equatable {
        var targets: [String]
        var ownsCFamily: Bool

        static func file(scratchPath: URL) -> URL {
            scratchPath.appendingPathComponent("check-session.json")
        }

        /* The record the last session into this scratch kept, nil when there is none it can read. */
        static func stored(scratchPath: URL) -> SessionRecord? {
            do {
                return try JSONDecoder().decode(
                    SessionRecord.self,
                    from: Data(contentsOf: file(scratchPath: scratchPath)),
                )
            }
            catch {
                /* Nothing to go by, so the session starts. */
                return nil
            }
        }

        func store(scratchPath: URL) {
            do {
                try JSONEncoder().encode(self).write(to: Self.file(scratchPath: scratchPath), options: .atomic)
            }
            catch {
                /* Not kept, so the next run starts this session: slower, and still right. */
            }
        }
    }

    /*
     The records a local package's session would only read again, so a run can leave the session out, or nil when it
     must start. It can when its last session took no owned C-family file, the scope reaches no C-family file, and
     every module the last session took is outside the scope, not taken by an earlier session of this run, and has
     the record directory that check left. A scope exists only when every edit since the last check that recorded
     every file was to an owned file, so a module outside it, and every module it imports, is as that check saw it,
     and its records stand. An edit to any module of the package, or to one it imports, puts it in the scope and
     starts the session. Measured 2026-11-06 on ahraos-presence: an interface edit to AhraOSPresence started four
     local packages' sessions with nothing in scope, one after another, 6.1s.
     */
    static func recordsWithoutSession(
        last: SessionRecord?,
        scope: Scope,
        recorded: [(String, URL)],
        alreadyChecked: Set<String>,
    ) -> [(String, URL)]? {
        guard let last, !last.ownsCFamily, !scope.cFamily, !last.targets.isEmpty else { return nil }
        let directories = Dictionary(recorded, uniquingKeysWith: { first, _ in first })
        var kept: [(String, URL)] = []
        for target in last.targets {
            guard !scope.targets.contains(target), !alreadyChecked.contains(target),
                let directory = directories[target]
            else { return nil }
            kept.append((target, directory))
        }
        return kept
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

    /* The module with these folders searched before any other, so it reads the interfaces the check emitted there. */
    static func searchingFirst(_ directories: [URL], in module: FrontendCheck.Module) -> FrontendCheck.Module {
        guard !directories.isEmpty else { return module }
        return FrontendCheck.Module(
            name: module.name,
            arguments: InterfaceEmit.searchingFirst(directories, in: module.arguments),
        )
    }

    /*
     The build server's targets a narrowed prepare asks for: each one compiling a file of these owned targets, whose
     prepare is their dependencies' and never their own module. An executable's two variants both compile its files.
     */
    static func preparing(
        _ targets: [BuildServerClient.Target],
        owning owners: Set<String>,
        targetByFile: [String: String],
    ) -> Set<String> {
        Set(
            targets.filter { target in
                target.sources.contains { source in
                    targetByFile[source.url.resolvingSymlinksInPath().path].map(owners.contains) ?? false
                }
            }
            .map(\.displayName)
        )
    }

    /*
     Whether a module this check could read is one a narrowed prepare left stale with no current copy: one the ledger
     lists as stale, outside the scope, that a module the check takes imports. Such a check prepares everything.
     */
    static func readsStale(_ ledger: InterfaceEmit.Ledger, scope: Scope?, checking: Set<String>) -> Bool {
        guard let scope else { return false }
        let stale = Set(ledger.stale).subtracting(scope.targets).map(moduleName(of:))
        return checking.union(scope.targets).contains { !scope.imports[$0, default: []].isDisjoint(with: stale) }
    }

    /* The owned targets the last check left no records for, which this check takes whatever the scope. */
    private func checkedCandidates(recorded: Set<String>) -> Set<String> {
        Set(files.map(\.targetName)).subtracting(recorded)
    }

    /*
     The interfaces to emit after a narrowed prepare, in the order they can be: each scoped module a checked module
     imports, and each scoped one the ledger holds, so its copy in the check's folder stays its current interface.
     */
    static func interfacesToEmit(
        owned: Set<String>,
        checking: Set<String>,
        scope: Scope,
        held: Set<String>,
    ) -> [String] {
        let imported = Set(checking.flatMap { scope.imports[$0, default: []] })
        return InterfaceEmit.emitOrder(
            owned.intersection(scope.targets).filter { imported.contains(moduleName(of: $0)) || held.contains($0) },
            imports: scope.imports,
        )
    }

    /* The emits planned while prepare ran, kept for the thread that runs them. */
    private final class PlannedEmits: Sendable {
        private struct State {
            var targets: [String] = []
            var directories: [URL] = []
            var planned: Swift.Result<[InterfaceEmit.Planned], FrontendCheck.Failure>?
        }

        private let state = Mutex(State())

        var targets: [String] { state.withLock { $0.targets } }
        var directories: [URL] { state.withLock { $0.directories } }
        var planned: Swift.Result<[InterfaceEmit.Planned], FrontendCheck.Failure>? { state.withLock { $0.planned } }

        func keep(
            targets: [String],
            directories: [URL],
            planned: Swift.Result<[InterfaceEmit.Planned], FrontendCheck.Failure>,
        ) {
            state.withLock { $0 = State(targets: targets, directories: directories, planned: planned) }
        }
    }

    /* The check that ran alongside prepare, kept for the thread that waits for it. */
    private final class EarlyCheck: Sendable {
        private struct State {
            var outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>?
            var seconds = 0.0
        }

        private let state = Mutex(State())

        var outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>? { state.withLock { $0.outcome } }
        var seconds: Double { state.withLock { $0.seconds } }

        func keep(outcome: Swift.Result<FrontendCheck.Outcome, FrontendCheck.Failure>, seconds: Double) {
            state.withLock { $0 = State(outcome: outcome, seconds: seconds) }
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
        /* The C-family files edited, by resolved path, which only the build server can place in a target (`widened(by:)`). */
        var cFamilyEdits: Set<String> = []

        /*
         The scope with the target owning each edited C-family file in it, and every target importing those, as the
         build server's target sources place them, headers among them: a Swift file that imports a C module breaks
         when one of its headers changes. Nil, a check of everything, when a file is in no target's sources.
         */
        func widened(by description: BuildServerClient.Description) -> Scope? {
            guard !cFamilyEdits.isEmpty else { return self }
            var owners = targets
            for path in cFamilyEdits {
                guard
                    let owner = description.targets.first(where: { target in
                        target.sources.contains { $0.url.resolvingSymlinksInPath().path == path }
                    })
                else { return nil }
                owners.insert(owner.displayName)
            }
            var widened = self
            widened.targets = TypesPhase.importers(of: owners, imports: imports)
            return widened
        }

        /*
         The scope's targets that import no other target of the scope. Their check needs no module prepare is about to
         emit again, since every module they import is one no edit changed, so it runs alongside prepare.
         */
        var independent: Set<String> {
            /* A header edit reaches modules only the build server can name, so nothing runs ahead of it. */
            guard cFamilyEdits.isEmpty else { return [] }
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
        var cFamilyEdits = Set<String>()
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
                cFamilyEdits.insert(resolved.path)
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
        return Scope(
            targets: Self.importers(of: changedTargets, imports: imports),
            cFamily: cFamily,
            imports: imports,
            cFamilyEdits: cFamilyEdits,
        )
    }

    /* These targets and every target that imports one of them, through any chain of imports. */
    static func importers(of targets: Set<String>, imports: [String: Set<String>]) -> Set<String> {
        var affected = targets
        var grew = true
        while grew {
            let names = Set(affected.map(moduleName(of:)))
            let reached = imports.filter { !affected.contains($0.key) && !$0.value.isDisjoint(with: names) }.map(\.key)
            affected.formUnion(reached)
            grew = !reached.isEmpty
        }
        return affected
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
        /* The compile arguments search the check's emitted interfaces, which a run checking alongside may be emitting. */
        let interfacesLock = InterfaceEmit.Ledger.Lock(scratchPath: scratchPath)
        defer { withExtendedLifetime(interfacesLock) {} }
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
        /*
         The compile arguments the last check kept search only current interfaces, but a module the ledger lists as
         stale has none anywhere: an edited module that imports one is checked, and that check prepares everything.
         */
        if let stale = InterfaceEmit.Ledger.stored(scratchPath: scratchPath)?.stale, !stale.isEmpty {
            let staleNames = Set(stale.map(Self.moduleName(of:)))
            let editedTargets = Set(edited.map(\.file.targetName))
            for file in files where editedTargets.contains(file.targetName) {
                guard let tree = tree(of: file), Self.importedModules(of: tree).isDisjoint(with: staleNames) else {
                    return nil
                }
            }
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

    /*
     A build server that failed before the compiler ran, a package `--no-fix` cannot resolve among them: the named gap
     the header promises, which `run` reports as the run's finding (`notChecked`) rather than ending the run.
     */
    struct CheckNotStarted: Error, CustomStringConvertible {
        var description: String
    }

    /*
     The result of a check that never started (#4txsasr): a finding at the package's Package.swift in the failure's
     own words, and every file of ours without a record, so the run is incomplete and names what went unchecked and
     why, and lint still runs over every file (`Result.notStarted`). Thrown out of the run, it ended the engine with
     nothing checked, and the front door could say only that the engine did not run: measured 2026-11-07 on a copy of
     ahraos-presence at 190bfa8c, where CapturePhone/PhoneCapture names a remote package and keeps no Package.resolved,
     so `--no-fix` cannot resolve it, and its 13 files went unnamed while the same package with its pins checks all
     13.
     */
    func notChecked(because failure: CheckNotStarted, start: Date) -> Result {
        let finding = FindingRecord(
            source: .compiler,
            file: package.root.appendingPathComponent("Package.swift").path,
            line: 1,
            column: 1,
            severity: .error,
            rule: "",
            messageId: "buildFailed",
            message: failure.description,
        )
        let record = TypesRecord(
            diagnostics: 1,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: files.map(\.url.path).sorted(),
            build: "not checked: \(failure.description)",
        )
        return Result(findings: [finding], record: record, hasErrors: true, notStarted: failure.description)
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
