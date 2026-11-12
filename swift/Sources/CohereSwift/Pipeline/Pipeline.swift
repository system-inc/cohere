import Foundation

/*
 One run: describe the package, then fix and format, types, lint, unused, in that order, each phase's
 outcome stated whether or not it ran.

 The order is cohere's and the reasons are the same. Mutation runs first so everything downstream sees
 the repaired tree. A file that does not parse stops the run there, because rewriting or type-checking a
 tree the parser could not read produces nonsense. Compiler errors stop lint, because rule findings
 against code whose semantics are wrong are noise; compiler warnings do not, because warning-level code
 still means what it says.

 What counts as complete is narrower than "every phase ran". A phase the caller turned off (`--lint`
 alone, `--no-fix`, an opt-in report nobody asked for) withholds nothing they did not choose to withhold.
 A phase that was cut off, a phase that is not built yet, a file that could not be read, or a file the
 compiler left no record for all withhold findings nobody chose to give up, and any of them makes the
 run incomplete and its exit code 1.
 */
public struct Pipeline {
    private let options: CommandOptions
    private let writer: ContractWriter
    private let workingDirectory: URL
    private let runner: ProcessRunner
    /*
     Under `--no-cache`, a scratch of this run's own in the system's temporary directory, removed when the run
     ends. Every cache the engine keeps lives under its scratch (the package description, the build and its
     records, the build-input snapshot, the prune stamp), so a fresh one reads nothing a run before left and
     writes nothing to the project's `.cache`, as TypeScript's `--no-cache` promises.
     */
    let freshScratch: URL?

    public init(
        options: CommandOptions,
        writer: ContractWriter,
        workingDirectory: URL,
        runner: ProcessRunner = ProcessRunner(),
    ) {
        self.options = options
        self.writer = writer
        self.workingDirectory = workingDirectory
        self.runner = runner
        freshScratch =
            options.noCache
            ? FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-no-cache-\(UUID().uuidString)",
                isDirectory: true,
            )
            : nil
    }

    /*
     Whether the run computes side by side what does not depend on each other: the types phase's in-process check
     beside the fix phase and lint's reads, and the scripts' check beside the package's. Records are written in one
     order either way. Off only where a test compares a run's records with the serial run's.
     */
    var overlapsPhases = true

    /*
     The types phase begun (`beginTypes` in `run`): the package's phase with its start, nil at a scripts-only or an
     Xcode root, and the scripts' check running beside it, nil when there are none or the run is serial.
     */
    struct TypesBegun {
        var package: (phase: TypesPhase, start: TypesPhase.Start)?
        var scripts: BackgroundWork<ScriptTypesPhase.Result>?

        /* Whether the package's in-process check answered, so nothing a whole check writes was written after it began. */
        var answeredInProcess: Bool {
            (try? package?.start.bodiesOnly?.value()) != nil
        }
    }

    /* What lint's typed rules read names through (`typedLintContext` in `run`). */
    struct TypedLintContext {
        /* The enabled typed rules that apply to each file, by path. */
        var typedRules: [String: [String]]
        var typedCandidates: [ParsedFile]
        var symbolProvider: SymbolProvider
    }

    /* A run that could not happen at all. Exit 2, no summary: the front door says nothing was checked. */
    public struct RunFailure: Error, CustomStringConvertible {
        public var description: String
    }

    public func run() async throws -> Int32 {
        defer {
            if let freshScratch {
                do {
                    try FileManager.default.removeItem(at: freshScratch)
                }
                catch {
                    /* Left in the temporary directory, which the system clears; the run's answer stands either way. */
                }
            }
        }
        let toolchain = EngineVersion.toolchain(runner: runner)
        try writer.write(EngineVersion.provenance(toolchain: toolchain))
        if options.showVersion {
            return 0
        }

        let configurationRoot = options.root ?? workingDirectory
        /*
         The file the config is looked for beside, which its note names: the Package.swift that wins below, else the
         Xcode project's definition, else, at a scripts-only root, its scripts. A root holding two projects is refused
         below before any note prints.
         */
        let definition: String
        switch try? XcodeProject.rootProject(at: configurationRoot) {
            case let .xcode(source):
                definition = source.fileName
            case .swiftPackage:
                definition = "Package.swift"
            case nil:
                definition = "the scripts"
        }
        let configuration = try RuleConfiguration.load(
            packageRoot: configurationRoot,
            explicitPath: options.lintConfiguration,
            definition: definition,
        )
        if options.listRules || options.listRulesEnabled {
            for name in RuleRegistry.allNames {
                let severity = configuration.severity(of: name)
                if options.listRulesEnabled && severity == .off {
                    continue
                }
                try writer.write(RuleRecord(name: name, severity: severity.rawValue))
            }
            return 0
        }

        guard let root = options.root else {
            throw RunFailure(description: "no --root was given, so there is no package to check")
        }
        let xcodeSource: XcodeProject.Source?
        /* A root holding no project is the scripts-only root discovery gives the Swift files no project claims (#wpd36zj). */
        var scriptsOnly = false
        do {
            switch try XcodeProject.rootProject(at: root) {
                case .swiftPackage:
                    xcodeSource = nil
                case let .xcode(source):
                    xcodeSource = source
                case nil:
                    xcodeSource = nil
                    scriptsOnly = true
            }
        }
        catch let ambiguous as XcodeProject.AmbiguousRoot {
            throw RunFailure(description: ambiguous.description)
        }

        /* The packages a run here accounts for, from the same traversal the run uses: manifests only, no build. */
        if options.listCoveredPackages {
            let package = try loadProject(
                root: root,
                xcodeSource: xcodeSource,
                scriptsOnly: scriptsOnly,
                toolchain: toolchain,
            )
            .package
            let inside = package.localPackages.filter { PackageModel.isInside($0.root, root) }
            let path = { (model: PackageModel) in model.root.standardizedFileURL.path }
            try writer.write(
                CoveredPackagesRecord(
                    root: path(package),
                    checked: ([package] + inside.filter { !package.isVendored($0) }).map(path),
                    vendored: inside.filter { package.isVendored($0) }.map(path),
                )
            )
            return 0
        }

        /*
         The abbreviation vocabulary, loaded before anything is checked. A compiled-in copy that would not load
         refuses the run with exit 2: a naming rule with no words to judge would report nothing and read as a
         clean tree, the silent green this tool exists to stop. The identifier rule and the file-name rule read it,
         and a run with both turned off loads nothing, and its rule list holds an empty vocabulary that is never
         consulted.
         */
        var vocabulary = AbbreviationVocabulary()
        if (configuration.severity(of: ConsistencyNoAbbreviatedIdentifier.ruleName) != .off
            || configuration.severity(of: ConsistencyNoAbbreviatedFileName.ruleName) != .off)
            && (options.proposeFixes || options.runLint)
        {
            do {
                vocabulary = try AbbreviationVocabulary.compiledIn()
            }
            catch {
                throw RunFailure(
                    description:
                        "the naming rules' compiled-in words (policy/Abbreviations.json) could not be loaded, so nothing was checked: \(error)"
                )
            }
        }
        /* The shouting rule's words, by the same reasoning: with none, every acronym with a vowel would read as shouting. */
        var shoutingVocabulary = ShoutingVocabulary()
        if configuration.severity(of: ConsistencyNoShouting.ruleName) != .off
            && (options.proposeFixes || options.runLint)
        {
            do {
                shoutingVocabulary = try ShoutingVocabulary.compiledIn()
            }
            catch {
                throw RunFailure(
                    description:
                        "the shouting rule's compiled-in words (policy/Shouting.json) could not be loaded, so nothing was checked: \(error)"
                )
            }
        }
        /*
         The brands, by the same reasoning, for the two rules that read them: with none, the casing rule would report
         every type opening on a listed brand, and the folder rule would split a brand it should read as one word.
         */
        var brandVocabulary = BrandVocabulary()
        if (configuration.severity(of: SwiftFormatRule.lowerCamelCaseRuleName) != .off
            || configuration.severity(of: ConsistencyRequireSeparatedFolderWords.ruleName) != .off)
            && (options.proposeFixes || options.runLint)
        {
            do {
                brandVocabulary = try BrandVocabulary.compiledIn()
            }
            catch {
                throw RunFailure(
                    description:
                        "the casing rule's compiled-in brands (policy/Brands.json) could not be loaded, so nothing was checked: \(error)"
                )
            }
        }
        /* The folder rule's words, by the same reasoning: with none, no folder would split, and every one would read as clean. */
        var words = WordList()
        if configuration.severity(of: ConsistencyRequireSeparatedFolderWords.ruleName) != .off
            && (options.proposeFixes || options.runLint)
        {
            do {
                words = try WordList.compiledIn()
            }
            catch {
                throw RunFailure(
                    description:
                        "the folder rule's compiled-in words (policy/Words.txt) could not be loaded, so nothing was checked: \(error)"
                )
            }
        }
        let fileRules = RuleRegistry.fileRules(
            vocabulary: vocabulary,
            shoutingVocabulary: shoutingVocabulary,
            brandVocabulary: brandVocabulary,
            words: words,
            configuration: configuration,
        )

        let describeStart = Date()
        let (package, xcodeProject) = try loadProject(
            root: root,
            xcodeSource: xcodeSource,
            scriptsOnly: scriptsOnly,
            toolchain: toolchain,
        )
        let fileSet = try FileSet.build(package: package)
        let scope = try FileScope.resolve(options: options, fileSet: fileSet, workingDirectory: workingDirectory)
        /*
         A `.swift-format` left at or above any file of ours refuses the run, whatever phases were asked for, as a
         leftover Prettier config refuses a TypeScript run: Swift has one house format, and a file still there is
         one that `Format.sh` or an editor reads and cohere never will.
         */
        let leftovers = HouseSwiftFormat.leftoverConfigurationFiles(
            above: fileSet.owned.map(\.url),
            boundary: repositoryRoot(of: root),
        )
        guard leftovers.isEmpty else {
            throw RunFailure(description: leftovers.map(HouseSwiftFormat.refusal(of:)).joined(separator: "\n"))
        }
        if !fileSet.note.isEmpty {
            FileHandle.standardError.write(Data("note: \(fileSet.note)\n".utf8))
        }
        try writer.write(
            projectRecord(
                package: package,
                fileSet: fileSet,
                scope: scope,
                elapsed: Self.milliseconds(since: describeStart),
            )
        )

        /*
         Every ending of a run that described the project: what its verdict was read from, then the summary, so the
         front door can replay the verdict while nothing named has moved (`RunInputs`). Taken as the run ends, so a
         package a build first linked is named, and the front door refuses the record when anything named moved
         after the run began.
         */
        func inputsRecord() -> InputsRecord {
            var linkedPackageRoots: [URL] = []
            var pins: [URL] = []
            if let xcodeProject {
                let phase = XcodeTypesPhase(
                    package: package,
                    project: xcodeProject,
                    files: fileSet.targetFiles,
                    scratchPath: runScratch(for: root),
                    resolutionAllowed: !options.noFix,
                    toolchain: toolchain,
                    runner: runner,
                )
                linkedPackageRoots = phase.inputRoots
                pins = RunInputs.pinsRead(by: phase)
            }
            return RunInputs.record(
                package: package,
                fileSet: fileSet,
                linkedPackageRoots: linkedPackageRoots,
                pins: pins,
                boundary: repositoryRoot(of: root),
                settingsFile: options.lintConfiguration
                    ?? configurationRoot.appendingPathComponent(
                        "CohereSettings.json"
                    ),
                toolchainExecutable: (try? SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner))
                    .map(RunInputs.toolchainExecutable(libraryPath:)),
            )
        }
        /* The inputs record taken while the types phase's in-process check ran, kept only when that check answered. */
        var preparedInputs: InputsRecord?
        func finish(complete: Bool) throws -> Int32 {
            try writer.write(preparedInputs ?? inputsRecord())
            return try writer.finish(complete: complete)
        }

        var complete = true

        /* Parsing is the fix phase's first step: it is what the fixers and the formatter would rewrite. */
        let fixStart = Date()
        var parsed = await SourceParser().parse(scope.files)
        for error in parsed.parseErrors {
            try writer.write(error)
        }
        /* After `project` and before the first `phase`, as the contract places them, so the front door can name every file nothing checked. */
        for crash in parsed.unreadable {
            complete = false
            try writer.write(UnreadableRecord(file: crash.file, error: crash.error))
        }
        let filesThatDoNotParse = Set(parsed.parseErrors.map(\.file)).count
        /* What the fixer's last walk found in each file whose final text is the text it walked, by path, so lint need not walk it again. */
        var reusableFindings: [String: [String: [FindingRecord]]] = [:]
        var nothingRewritten = false

        /*
         The rules' verdicts the last runs kept (`LintVerdictCache`), read by the fixer, which need not walk a file whose
         kept verdict proposes no fix, and by lint. None under `--no-cache`, in a run that runs no rule, or when this
         engine cannot read its own executable to key them by.
         */
        let enabledFileRules = fileRules.map(\.name).filter { configuration.severity(of: $0) != .off }
        let enabledTypedRules = RuleRegistry.typedRules.map(\.name).filter { configuration.severity(of: $0) != .off }
        let lintCache: LintVerdictCache? =
            options.noCache || filesThatDoNotParse > 0 || !(options.proposeFixes || options.runLint)
            ? nil
            : LintVerdictCache.engineIdentity.map { engine in
                LintVerdictCache(
                    scratchPath: runScratch(for: root),
                    context: LintVerdictCache.context(
                        engine: engine,
                        ruleSet: LintVerdictCache.ruleSet(
                            fileRules: enabledFileRules,
                            typedRules: enabledTypedRules,
                            options: configuration.options,
                        ),
                    ),
                    enabledFileRules: enabledFileRules,
                )
            }
        /* Each file's key for those verdicts, by path, for the text it has now; a file the fix phase rewrites is keyed again. */
        var lintKeys = lintCache == nil ? [:] : LintVerdictCache.fileKeys(of: parsed.files)

        /*
         The types phase begun (`TypesPhase.start()`): the package's snapshot taken and its in-process check of a body-only
         edit started, and each standalone script of ours checked alone beside it, as `swift <file>` runs it
         (`ScriptTypesPhase`), unless the run is serial. The package is checked whole, whatever the scope: a change in one
         file changes what the rest of its module means.
         */
        func beginTypes() -> TypesBegun {
            var begun = TypesBegun()
            /* A scripts-only root has no target, and an Xcode root is checked by its own phase. */
            if package.projectKind != .scripts, xcodeProject == nil {
                var typesPhase = TypesPhase(
                    package: package,
                    files: fileSet.targetFiles,
                    scratchPath: runScratch(for: root),
                    resolutionAllowed: !options.noFix,
                    toolchain: toolchain,
                    runner: runner,
                )
                typesPhase.parsed = Dictionary(
                    parsed.files.map { ($0.url.path, $0.tree) },
                    uniquingKeysWith: { first, _ in first },
                )
                begun.package = (typesPhase, typesPhase.start())
            }
            if overlapsPhases, let scripts = scriptTypesPhase() {
                begun.scripts = BackgroundWork(label: "cohere-swift.types.scripts") { try scripts.run() }
            }
            return begun
        }
        /* The standalone scripts' check, nil when the root holds none. */
        func scriptTypesPhase() -> ScriptTypesPhase? {
            guard !fileSet.scripts.isEmpty else { return nil }
            return ScriptTypesPhase(
                scripts: fileSet.scripts,
                scratchPath: runScratch(for: root),
                workingDirectory: root,
                runner: runner,
            )
        }

        /*
         The types phase begun, run to its end: the package's check, then the scripts', their findings and files added to
         the package's, so one types record covers every file of ours. The scripts' check, when it ran beside, is waited
         for whatever the package's did, so nothing it does outlives the phase.
         */
        func build(_ begun: TypesBegun) throws -> TypesPhase.Result {
            defer { begun.scripts?.wait() }
            let targets: TypesPhase.Result
            if let checking = begun.package {
                targets = try checking.phase.run(after: checking.start)
            }
            else if let xcodeProject {
                targets = try XcodeTypesPhase(
                    package: package,
                    project: xcodeProject,
                    files: fileSet.targetFiles,
                    scratchPath: runScratch(for: root),
                    resolutionAllowed: !options.noFix,
                    toolchain: toolchain,
                    runner: runner,
                )
                .run()
            }
            else {
                /* No target, so no package or Xcode check: a scripts-only root's files are its scripts alone. */
                targets = ScriptTypesPhase.noTargets
            }
            let checked: ScriptTypesPhase.Result
            if let running = begun.scripts {
                checked = try running.value()
            }
            else if let scripts = scriptTypesPhase() {
                checked = try scripts.run()
            }
            else {
                return targets
            }
            return ScriptTypesPhase.adding(checked, count: fileSet.scripts.count, to: targets)
        }

        /*
         Begun now when the fix phase writes nothing, so the in-process check runs beside it; otherwise after the fix
         phase wrote, since the check reads the edited files' text as it will stand. Never in a run that does not reach it.
         */
        let typesBegunEarly =
            overlapsPhases && options.runTypes && !options.mutate && filesThatDoNotParse == 0 ? beginTypes() : nil

        if !options.runFix {
            try writer.write(PhaseRecord(name: .fix, outcome: .skipped, detail: "not requested"))
        }
        else if filesThatDoNotParse > 0 {
            /* Nothing is rewritten in a run where any file does not parse: the bail below says why, and the fix line says nothing moved. */
            try writer.write(
                fixRecord(
                    scope: scope,
                    considered: parsed.files.count,
                    rewritten: [],
                    fixesApplied: 0,
                    refusals: [:],
                    reformatted: 0,
                    notFormatted: ["a file in scope does not parse, so nothing was rewritten": parsed.files.count],
                    notWritten: [],
                )
            )
            try writer.write(
                PhaseRecord(name: .fix, outcome: .ran, elapsedMilliseconds: Self.milliseconds(since: fixStart))
            )
        }
        else {
            /*
             Fixers first, then the formatter over the fixed text, so the formatter has the last word on layout
             and a fixer's output is always formatted. Under `--no-fix` nothing is fixed: the fixable findings
             surface in lint, and the formatter is asked only what it would change. `--format-only` proposes no
             fix, so the formatter alone runs; `--no-format` leaves the formatter out, so the fixers alone do.
             */
            var fixesApplied = 0
            var refusals: [String: Int] = [:]
            var toFormat = parsed.files
            var fixerFindings: [String: [String: [FindingRecord]]] = [:]
            var fixedByFile: [String: [String: Int]] = [:]
            if options.proposeFixes {
                let fixer = FileFixer(configuration: configuration, rules: fileRules, maximumPasses: options.fixPasses)
                /* Each file is fixed on its own, so the files are fixed side by side; results are gathered back in input order. */
                let files = parsed.files
                let keys = lintKeys
                let results = await withTaskGroup(of: (Int, FileFixer.Result).self) { group in
                    for (index, file) in files.enumerated() {
                        group.addTask {
                            /*
                             A kept verdict for these bytes that proposes no fix is what the fixer's one pass would find:
                             it would apply nothing and stop. So the file is left as it is, and lint reads the verdict.
                             */
                            if let key = keys[file.url.path],
                                let kept = lintCache?.fileVerdict(path: file.url.path, key: key),
                                FixApplier.proposals(from: kept.fileRules.values.flatMap(\.self)).isEmpty
                            {
                                return (
                                    index,
                                    FileFixer.Result(
                                        file: file,
                                        applied: 0,
                                        appliedByRule: [:],
                                        refusalsByReason: [:],
                                        findingsOfFinalText: nil,
                                    ),
                                )
                            }
                            return (index, fixer.fix(file))
                        }
                    }
                    var collected = [FileFixer.Result?](repeating: nil, count: files.count)
                    for await (index, result) in group {
                        collected[index] = result
                    }
                    return collected
                }
                toFormat = zip(files, results).map { file, result in
                    guard let result else { return file }
                    fixesApplied += result.applied
                    if !result.appliedByRule.isEmpty {
                        fixedByFile[file.url.path] = result.appliedByRule
                    }
                    refusals.merge(result.refusalsByReason, uniquingKeysWith: +)
                    if let found = result.findingsOfFinalText {
                        fixerFindings[file.url.path] = found
                    }
                    return result.file
                }
            }
            let originals = Dictionary(
                parsed.files.map { ($0.url.path, $0.source) },
                uniquingKeysWith: { first, _ in first },
            )
            /*
             A run that writes is handed the formatted text. `--no-fix` asks only the verdict. Either way each verdict
             is kept in the package's scratch for the next run (`FormatVerdictCache`), and a file the store holds as
             formatted for its bytes is not formatted again, unless the fixer just moved them, since the formatter
             has the last word on every fix, or the run asked for `--format-all`. Under `--no-cache` nothing is read
             or kept.
             */
            let formatting: FormatPhase.Result
            if !options.runFormat {
                formatting = FormatPhase.Result(outcomes: toFormat.map { (file: $0, outcome: .unchanged) })
            }
            else if options.mutate {
                formatting = await FormatPhase().run(
                    toFormat,
                    fixed: Set(toFormat.filter { $0.source != originals[$0.url.path] }.map(\.url.path)),
                    everyFile: options.formatAll,
                    scratchPath: options.noCache ? nil : runScratch(for: root),
                )
            }
            else {
                formatting = await FormatPhase().check(
                    toFormat,
                    scratchPath: options.noCache ? nil : runScratch(for: root),
                )
            }
            var rewritten: [FileSet.OwnedFile] = []
            var changedFiles: [FixRecord.ChangedFile] = []
            var reformatted = 0
            var notFormatted: [String: Int] = [:]
            var notWritten: [FixRecord.NotWrittenFile] = []
            /*
             The root with every link resolved, which no file the phase writes may lie outside (#723rzxm). A root that
             does not resolve is taken as given, which can only refuse more: no real path lies below a path with a link
             in it.
             */
            let realRoot = SourceWrite.realPath(of: root) ?? root.standardizedFileURL.path
            /*
             A file the phase does not write, under `--no-fix` or outside the root, is reported as what formatting would
             change: one finding per file, at the first line that moves.
             */
            func notFormattedFinding(_ file: ParsedFile, line: Int) -> FindingRecord {
                FindingRecord(
                    source: .format,
                    file: file.url.path,
                    line: line,
                    column: 1,
                    severity: .error,
                    rule: "cohere-swift/consistency-require-formatting",
                    messageId: "notFormatted",
                    message: "not formatted the house way; a run without --no-fix rewrites it",
                )
            }
            for (file, outcome) in formatting.outcomes {
                var final = file.source
                var formattedHere = false
                switch outcome {
                    case .unchanged:
                        break
                    case let .changed(formatted):
                        final = formatted
                        formattedHere = true
                    case let .wouldChange(line):
                        try writer.write(notFormattedFinding(file, line: line))
                    case let .failed(reason):
                        notFormatted["the formatter failed: \(reason)", default: 0] += 1
                        complete = false
                }
                /* Where the one write of a file whose text moved would go; nil for a file the run leaves as it is. */
                let destination =
                    options.mutate && final != originals[file.url.path]
                    ? SourceWrite.destination(of: file.url, root: realRoot) : nil
                /*
                 A file reached through a link to outside the root is checked as `--no-fix` checks it and never
                 written: none of its fixes landed, lint walks the text on disk, so the findings they would have fixed
                 report, and the formatter's verdict on that text is a finding. It is named, and the run is incomplete.
                 */
                if case let .outside(realPath) = destination {
                    notWritten.append(FixRecord.NotWrittenFile(file: file.url.path, realPath: realPath))
                    complete = false
                    fixesApplied -= (fixedByFile[file.url.path] ?? [:]).values.reduce(0, +)
                    if options.runFormat, let original = originals[file.url.path],
                        case let .changed(formatted) = FormatPhase.format(
                            source: original,
                            url: file.url,
                            configuration: HouseSwiftFormat.configuration,
                        )
                    {
                        try writer.write(
                            notFormattedFinding(file, line: FormatPhase.firstDifferingLine(original, formatted))
                        )
                    }
                    continue
                }
                if formattedHere {
                    reformatted += 1
                }
                /* The fixer walked exactly this text when the formatter left it as the fixer did, so its findings are lint's. */
                if final == file.source, let found = fixerFindings[file.url.path] {
                    reusableFindings[file.url.path] = found
                }
                /* One write per file, of the fixed and formatted text, and only when it differs from what was read. */
                if case let .inside(realPath) = destination {
                    try SourceWrite.write(final, toRealPath: realPath)
                    changedFiles.append(
                        FixRecord.ChangedFile(
                            file: file.url.path,
                            fixedBy: fixedByFile[file.url.path] ?? [:],
                            formatted: formattedHere,
                        )
                    )
                    var owned = FileSet.OwnedFile(
                        url: file.url,
                        targetName: file.targetName,
                        targetKind: file.targetKind,
                        packageRoot: file.packageRoot,
                    )
                    owned.scriptPlatform = file.scriptPlatform
                    rewritten.append(owned)
                }
            }
            /* Rewritten files are parsed again, so lint reads the text that is on disk now rather than the text that was. */
            if !rewritten.isEmpty {
                let reparsed = await SourceParser().parse(rewritten)
                let replacements = Dictionary(
                    reparsed.files.map { ($0.url.path, $0) },
                    uniquingKeysWith: { first, _ in first },
                )
                parsed.files = parsed.files.map { replacements[$0.url.path] ?? $0 }
                if lintCache != nil {
                    lintKeys.merge(LintVerdictCache.fileKeys(of: reparsed.files)) { _, rekeyed in rekeyed }
                }
            }
            nothingRewritten = rewritten.isEmpty
            try writer.write(
                fixRecord(
                    scope: scope,
                    considered: parsed.files.count,
                    rewritten: changedFiles,
                    fixesApplied: fixesApplied,
                    refusals: refusals,
                    reformatted: reformatted,
                    notFormatted: notFormatted,
                    notWritten: notWritten.sorted { $0.file < $1.file },
                )
            )
            try writer.write(
                PhaseRecord(name: .fix, outcome: .ran, elapsedMilliseconds: Self.milliseconds(since: fixStart))
            )
        }

        if filesThatDoNotParse > 0 {
            let reason =
                "parsing bailed: \(filesThatDoNotParse) files do not parse, and checking a tree the parser could not read reports nonsense"
            try writer.write(
                PhaseRecord(
                    name: .types,
                    outcome: options.runTypes ? .notReached : .skipped,
                    detail: options.runTypes ? reason : "not requested",
                )
            )
            try writer.write(
                PhaseRecord(
                    name: .lint,
                    outcome: options.runLint ? .notReached : .skipped,
                    detail: options.runLint ? reason : "not requested",
                )
            )
            try writer.write(unusedPhase(notReached: reason))
            return try finish(complete: false)
        }

        /*
         The file rules' verdict kept for each file's final text, by path, for the files the fixer's walk does not
         already answer, and for every kept file which typed rules apply, which the same bytes decide.
         */
        var replayedFileRules: [String: [String: [FindingRecord]]] = [:]
        var knownTypedRules: [String: [String]] = [:]
        if let lintCache {
            for file in parsed.files {
                guard let key = lintKeys[file.url.path],
                    let kept = lintCache.fileVerdict(path: file.url.path, key: key)
                else { continue }
                knownTypedRules[file.url.path] = kept.typedRules
                if reusableFindings[file.url.path] == nil {
                    replayedFileRules[file.url.path] = kept.fileRules
                }
            }
        }
        let typedRulesByName = Dictionary(
            RuleRegistry.typedRules.map { ($0.name, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        /* The enabled typed rules that apply to a file: as kept for its bytes, or asked of each rule. */
        func typedRulesApplying(to file: ParsedFile) -> [String] {
            knownTypedRules[file.url.path]
                ?? enabledTypedRules.filter { typedRulesByName[$0]?.applies(to: file) ?? false }
        }

        /*
         What lint's typed rules read names through: which enabled typed rules apply to each file, the files any applies
         to, and where their names are looked up. The names the types phase's in-process check gave are added once it
         answered, so this can be settled while it runs.
         */
        func typedLintContext() -> TypedLintContext {
            var typedRules: [String: [String]] = [:]
            for file in parsed.files {
                typedRules[file.url.path] = typedRulesApplying(to: file)
            }
            var symbolProvider = SymbolProvider(
                layouts: Self.symbolLayouts(package: package, scratch: runScratch(for: root)),
                runner: runner,
            )
            symbolProvider.ownedModules = Self.ownedModules(of: package)
                .union(Self.compiledModules(of: package, fileSet: fileSet, layouts: symbolProvider.layouts))
                .union(fileSet.scripts.map(\.targetName))
            return TypedLintContext(
                typedRules: typedRules,
                typedCandidates: parsed.files.filter { !(typedRules[$0.url.path] ?? []).isEmpty },
                symbolProvider: symbolProvider,
            )
        }
        /* The typed key of a file under a record: its own key, the record, and the modules the package owns. */
        func typedKey(_ path: String, _ record: IndexStore.FreshRecord.Identity, ownedModules: Set<String>) -> String? {
            lintKeys[path].map {
                LintVerdictCache.typedKey(fileKey: $0, record: record, ownedModules: ownedModules)
            }
        }
        /* What the index says of the typed candidates, each file whose typed verdict is kept under the record a read would answer from left unread. */
        func indexed(_ context: TypedLintContext) -> SymbolProvider.Indexed? {
            context.symbolProvider.indexed(for: context.typedCandidates) { file, record in
                guard let lintCache,
                    let key = typedKey(file.url.path, record, ownedModules: context.symbolProvider.ownedModules)
                else { return false }
                return lintCache.typedVerdict(path: file.url.path, key: key) != nil
            }
        }

        var typeErrors = 0
        var lintBuildFailure: String?
        /* The names the types phase's in-process check recorded, handed to lint so it never checks those files again. */
        var answeredSymbols: [String: FileSymbols] = [:]
        /* Lint's typed context and its reads of the index, taken while the in-process check ran and kept only if it answered. */
        var prefetched: (context: TypedLintContext, indexed: SymbolProvider.Indexed?)?
        if !options.runTypes {
            /*
             A typed rule reads what names resolve to from the build's index and compile commands, so a `--lint` run
             whose typed rules apply to a file still builds, the way TypeScript's lint still loads the checker. What
             the compiler said is not this run's to report, and the build is skipped when no input changed since the
             last one. Without it, a tree no run had built marked every such file crashed (@ahra_os_presence_motion,
             2026-11-05: 1,251 files at ahraos-presence's root under `--lint --no-fix`).
             */
            let typedRulesApply = parsed.files.contains { !typedRulesApplying(to: $0).isEmpty }
            if options.runLint && typedRulesApply {
                do {
                    /* A build that failed after compiling something is said by a `buildFailed` finding rather than thrown. */
                    let types = try build(beginTypes())
                    lintBuildFailure = types.findings.first { $0.messageId == "buildFailed" }?.message
                    answeredSymbols = types.symbols
                }
                catch let failure as TypesPhase.TypesFailure {
                    /* Lint still runs: the rules that need no build judge every file, and each file the typed rules cannot judge says this build is why. */
                    lintBuildFailure = failure.description
                }
            }
            /* Exactly `not requested`, the one detail the contract reads as nobody asking: the build is lint's input, not a types phase. */
            try writer.write(PhaseRecord(name: .types, outcome: .skipped, detail: "not requested"))
        }
        else {
            let begun = typesBegunEarly ?? beginTypes()
            /*
             While the in-process check runs, the reads that need nothing it answers: what the inputs record names, and,
             once the scripts' check stopped writing into its index, lint's reads of the index. Each is kept only when
             that check answered: a whole check after it writes the stores these reads describe, and may resolve
             packages into the tree.
             */
            if overlapsPhases, begun.package?.start.bodiesOnly != nil {
                preparedInputs = inputsRecord()
                if options.runLint {
                    begun.scripts?.wait()
                    let context = typedLintContext()
                    prefetched = (context, indexed(context))
                }
            }
            let types = try build(begun)
            if !begun.answeredInProcess {
                preparedInputs = nil
                prefetched = nil
            }
            answeredSymbols = types.symbols
            for finding in types.findings {
                try writer.write(finding)
            }
            try writer.write(types.record)
            try writer.write(
                PhaseRecord(name: .types, outcome: .ran, elapsedMilliseconds: types.record.elapsedMilliseconds)
            )
            if !types.record.filesWithoutRecord.isEmpty {
                complete = false
            }
            typeErrors = types.findings.filter { $0.severity == .error }.count
            /*
             A check that never started found no type error in any file, so lint is not kept from judging them
             (#4txsasr): the rules that need no build judge every file, and each file the typed rules cannot judge
             says the failed check is why, as under `--lint`.
             */
            if let notStarted = types.notStarted {
                typeErrors -= 1
                lintBuildFailure = notStarted
            }
        }

        if typeErrors > 0 && options.runLint {
            try writer.write(
                PhaseRecord(
                    name: .lint,
                    outcome: .notReached,
                    detail: "types bailed: \(typeErrors) type errors — lint findings against wrong semantics are noise",
                )
            )
            try writer.write(
                unusedPhase(
                    notReached:
                        "types bailed: \(typeErrors) type errors, and the index of a package that does not compile is not the package"
                )
            )
            return try finish(complete: false)
        }

        if !options.runLint {
            try writer.write(PhaseRecord(name: .lint, outcome: .skipped, detail: "not requested"))
        }
        else {
            let lintStart = Date()
            var linter = Linter(configuration: configuration, fileRules: fileRules)
            linter.typedRules = RuleRegistry.typedRules
            let context = prefetched?.context ?? typedLintContext()
            var symbolProvider = context.symbolProvider
            symbolProvider.answered = answeredSymbols
            let keys = lintKeys
            linter.symbols = symbolProvider.symbols(
                for: context.typedCandidates,
                from: prefetched.map(\.indexed) ?? indexed(context),
            )
            var typedReplayed: [String: [String: [FindingRecord]]] = [:]
            for (path, record) in linter.symbols.replayed {
                if let key = typedKey(path, record, ownedModules: context.symbolProvider.ownedModules),
                    let kept = lintCache?.typedVerdict(path: path, key: key)
                {
                    typedReplayed[path] = kept
                }
            }
            if let lintBuildFailure {
                linter.symbols.unavailable = linter.symbols.unavailable.mapValues {
                    "\($0), since the check this run made for the typed rules failed: \(lintBuildFailure)"
                }
            }
            /*
             At an Xcode root, Xcode's own answer for each target's toolchain settings, which only the toolchain rules
             read, so it is asked for here, when one of them is enabled. Its time is in the lint phase's elapsed. Asked
             after the build, it finds the packages that build resolved, and a run whose types bailed never asks.
             */
            var judged = package
            if let loaded = xcodeProject,
                RuleRegistry.packageRules.contains(where: { configuration.severity(of: $0.name) != .off })
            {
                judged = toolchainSettingsRead(
                    into: package,
                    project: loaded,
                    fileSet: fileSet,
                    root: root,
                    toolchain: toolchain,
                )
            }
            let lint = await linter.run(
                package: judged,
                manifests: await manifests(of: package),
                files: parsed.files,
                reusable: reusableFindings.merging(replayedFileRules) { walked, _ in walked },
                typedReplayed: typedReplayed,
                typedRules: context.typedRules,
            )
            for finding in lint.findings {
                try writer.write(finding)
            }
            /* A file is replayed when the store answered for every rule that judged it: its file rules, and its typed rules if any apply. */
            let filesReplayed = parsed.files.filter { file in
                replayedFileRules[file.url.path] != nil
                    && ((context.typedRules[file.url.path] ?? []).isEmpty || typedReplayed[file.url.path] != nil)
            }
            if let lintCache {
                /* A typed verdict is kept under the record it was read from or replayed under; one sourcekitd gave has none. */
                var typedKeys: [String: String] = [:]
                for (path, record) in linter.symbols.records.merging(
                    linter.symbols.replayed,
                    uniquingKeysWith: { read, _ in read },
                ) {
                    typedKeys[path] = typedKey(path, record, ownedModules: context.symbolProvider.ownedModules)
                }
                lintCache.store(
                    lintCache.entries(
                        of: parsed.files,
                        verdicts: lint.verdicts,
                        keys: keys,
                        replayed: Set(filesReplayed.map(\.url.path)),
                        typedKeys: typedKeys,
                    )
                )
            }
            /*
             Reported as reused only when every file's findings came from the fix phase and nothing was rewritten,
             because that is what the front door's line says. A run that reused some files and walked others
             still saves the walk, and says it ran.
             */
            let reusedEverything =
                nothingRewritten && context.typedCandidates.isEmpty && !parsed.files.isEmpty
                && parsed.files.allSatisfy { reusableFindings[$0.url.path] != nil }
            var record = lint.record
            record.elapsedMilliseconds = Self.milliseconds(since: lintStart)
            record.reusedFrom = reusedEverything ? "fix" : ""
            record.filesReplayed = filesReplayed.count
            try writer.write(record)
            try writer.write(
                reusedEverything
                    ? PhaseRecord(
                        name: .lint,
                        outcome: .reused,
                        elapsedMilliseconds: record.elapsedMilliseconds,
                        detail: "the fix phase's walk (nothing was rewritten)",
                    )
                    : PhaseRecord(name: .lint, outcome: .ran, elapsedMilliseconds: record.elapsedMilliseconds)
            )
            if !record.crashes.isEmpty {
                complete = false
            }
        }

        if options.unused {
            try await runUnused(package: package, root: root, fileSet: fileSet, parsed: parsed.files)
        }
        else {
            try writer.write(PhaseRecord(name: .unused, outcome: .skipped, detail: Self.unusedNotRequested))
        }
        return try finish(complete: complete)
    }

    static let unusedNotRequested = "not requested — this is a report, ask for it with --unused"

    /*
     The run's project: SwiftPM's answer for a package, or an Xcode project read into the same model, with the
     bundle and project file its types phase builds from, or a scripts-only root's model, which nothing is read for.
     */
    private func loadProject(
        root: URL,
        xcodeSource: XcodeProject.Source?,
        scriptsOnly: Bool,
        toolchain: String,
    ) throws -> (package: PackageModel, xcode: XcodeProject.Loaded?) {
        if scriptsOnly {
            return (PackageModel.scriptsOnly(root: root), nil)
        }
        if let xcodeSource {
            let loaded = try XcodeProject.load(
                source: xcodeSource,
                root: root,
                scratchPath: runScratch(for: root),
                runner: runner,
            )
            return (loaded.model, loaded)
        }
        let package = try PackageModel.load(
            root: root,
            scratchPath: runScratch(for: root),
            runner: runner,
            toolchain: toolchain,
        )
        return (package, nil)
    }

    /*
     The model with Xcode's answer for each owned target's toolchain settings (`XcodeBuildSettings`), read through the
     invocation the types phase builds with, so it resolves and writes exactly where that build does, and findings
     about a target point at its line in the project's definition. At an xcodegen root the spec's text goes with it, for
     the edit a finding suggests; a committed project file is never offered one.
     */
    private func toolchainSettingsRead(
        into package: PackageModel,
        project: XcodeProject.Loaded,
        fileSet: FileSet,
        root: URL,
        toolchain: String,
    ) -> PackageModel {
        let phase = XcodeTypesPhase(
            package: package,
            project: project,
            files: fileSet.targetFiles,
            scratchPath: runScratch(for: root),
            resolutionAllowed: !options.noFix,
            toolchain: toolchain,
            runner: runner,
        )
        let definitionText = try? String(contentsOf: project.definition, encoding: .utf8)
        let isSpecification = package.projectKind == .xcodeGen
        let targetLines =
            isSpecification
            ? XcodeGenSpecification(text: definitionText ?? "").targetLines
            : XcodeBuildSettings.targetLines(inProjectFile: definitionText ?? "", targets: project.projectFile.targets)
        return XcodeBuildSettings.read(
            phase: phase,
            definition: project.definition,
            targetLines: targetLines,
            specification: isSpecification ? definitionText : nil,
        )
        .applied(to: package)
    }

    /* The unused phase of a run cut off before it: not reached when it was asked for, not requested otherwise. */
    private func unusedPhase(notReached reason: String) -> PhaseRecord {
        options.unused
            ? PhaseRecord(name: .unused, outcome: .notReached, detail: reason)
            : PhaseRecord(name: .unused, outcome: .skipped, detail: Self.unusedNotRequested)
    }

    /*
     The unused report, over the whole package whatever the scope, since an import or a private declaration is
     used or not by its file alone but the index that answers is the whole build's. It reads the index the types phase just refreshed,
     or the last build's, and a file that index does not describe as it stands is counted as not checked
     rather than guessed at. Its records are a report, not findings: the writer does not count them, so they
     never fail a run, the way the TypeScript unused report never does. An Xcode root's index is its xcodebuild's,
     in the derived data `symbolLayouts` names.
     */
    private func runUnused(package: PackageModel, root: URL, fileSet: FileSet, parsed: [ParsedFile]) async throws {
        let start = Date()
        let stores: [IndexStore]
        let demangler: SwiftDemangler
        do {
            let library = try IndexStore.toolchainLibraryPath(runner: runner)
            stores = Self.symbolLayouts(package: package, scratch: runScratch(for: root)).compactMap {
                try? IndexStore(libraryPath: library, storePath: IndexStore.storePath(layout: $0))
            }
            demangler = try SwiftDemangler.shared(runner: runner)
        }
        catch {
            try writer.write(
                PhaseRecord(
                    name: .unused,
                    outcome: .notReached,
                    detail: "the index store or the demangler could not be loaded: \(error)",
                )
            )
            return
        }
        guard !stores.isEmpty else {
            try writer.write(
                PhaseRecord(
                    name: .unused,
                    outcome: .notReached,
                    detail:
                        "no build has written an index store for this package yet; a run with the types phase writes one",
                )
            )
            return
        }
        /* Every file of ours, the out-of-scope ones parsed here: an `@_exported import` anywhere decides what a module re-exports. */
        let parsedPaths = Set(parsed.map(\.url.path))
        let missing = fileSet.owned.filter { !parsedPaths.contains($0.url.path) }
        let files = missing.isEmpty ? parsed : parsed + (await SourceParser().parse(missing)).files
        let imports = UnusedImports(stores: stores, demangler: demangler).run(files: files)
        try writeReport(
            rule: UnusedImports.ruleName,
            findings: imports.findings,
            filesChecked: imports.filesChecked,
            filesUnchecked: imports.filesUnchecked,
            checked: imports.importsChecked,
            skipped: imports.importsExported > 0
                ? ["re-exported with @_exported, which is API": imports.importsExported] : [:],
            since: start,
        )
        let declarationsStart = Date()
        var unusedDeclarations = UnusedDeclarations(stores: stores, package: package)
        unusedDeclarations.ownedModules.formUnion(
            Self.compiledModules(
                of: package,
                fileSet: fileSet,
                layouts: Self.symbolLayouts(package: package, scratch: runScratch(for: root)),
            )
        )
        unusedDeclarations.ownedModules.formUnion(fileSet.scripts.map(\.targetName))
        let declarations = unusedDeclarations.run(files: files)
        try writeReport(
            rule: UnusedDeclarations.ruleName,
            findings: declarations.findings,
            filesChecked: declarations.filesChecked,
            filesUnchecked: declarations.filesUnchecked,
            checked: declarations.declarationsChecked,
            skipped: declarations.skipped,
            since: declarationsStart,
        )
        try writer.write(
            PhaseRecord(name: .unused, outcome: .ran, elapsedMilliseconds: Self.milliseconds(since: start))
        )
    }

    /* One rule's part of the unused report: its records in file order, then the coverage record that counts them. */
    private func writeReport(
        rule: String,
        findings: [(finding: FindingRecord, subject: String)],
        filesChecked: Int,
        filesUnchecked: [String: String],
        checked: Int,
        skipped: [String: Int],
        since start: Date,
    ) throws {
        let ordered = findings.sorted {
            ($0.finding.file, $0.finding.line, $0.finding.column) < (
                $1.finding.file, $1.finding.line, $1.finding.column,
            )
        }
        for found in ordered {
            try writer.write(UnusedRecord(found.finding, subject: found.subject))
        }
        var notChecked: [String: Int] = [:]
        for reason in filesUnchecked.values {
            notChecked[reason, default: 0] += 1
        }
        try writer.write(
            UnusedCoverageRecord(
                rule: rule,
                filesChecked: filesChecked,
                filesNotChecked: notChecked,
                checked: checked,
                skipped: skipped,
                found: ordered.count,
                elapsedMilliseconds: Self.milliseconds(since: start),
            )
        )
    }

    private func projectRecord(
        package: PackageModel,
        fileSet: FileSet,
        scope: FileScope,
        elapsed: Int,
    ) -> ProjectRecord {
        let counts = Dictionary(grouping: fileSet.owned, by: \.targetName).mapValues(\.count)
        /* A script is a file of ours that no target compiles, so it counts among the package's files too, and its own target is listed beside the package's. */
        let scripts = fileSet.scripts
        return ProjectRecord(
            root: package.root.path,
            package: package.name,
            elapsedMilliseconds: elapsed,
            filesInPackage: fileSet.filesInPackage.count + scripts.count,
            filesOurs: fileSet.owned.count,
            filesInScope: scope.files.count,
            scopeDescription: scope.description,
            /* A local package's targets are named `<package>/<target>`, so a reader can tell Presence's own targets from VRMKit's. */
            targets: package.allPackages.flatMap { member in
                member.targets.map { target in
                    let name = member.root == package.root ? target.name : "\(member.name)/\(target.name)"
                    return ProjectRecord.Target(
                        name: name,
                        kind: target.kind,
                        files: counts[target.name] ?? 0,
                        languageMode: target.languageMode,
                    )
                }
            }
                + scripts.map {
                    ProjectRecord.Target(name: $0.targetName, kind: "script", files: 1, languageMode: "6")
                },
            excluded: fileSet.excluded,
            projectKind: package.projectKind.rawValue,
        )
    }

    /* The fix line's numbers. Rewrites, fixes and reformats are separate counts, so a run where only the formatter moved reads differently from one where a fixer did. */
    private func fixRecord(
        scope: FileScope,
        considered: Int,
        rewritten: [FixRecord.ChangedFile],
        fixesApplied: Int,
        refusals: [String: Int],
        reformatted: Int,
        notFormatted: [String: Int],
        notWritten: [FixRecord.NotWrittenFile],
    ) -> FixRecord {
        FixRecord(
            filesConsidered: considered,
            filesRewritten: rewritten.count,
            fixesApplied: fixesApplied,
            fixesRefused: refusals.values.reduce(0, +),
            refusalsByReason: refusals,
            filesReformatted: reformatted,
            filesNotFormatted: notFormatted.values.reduce(0, +),
            notFormattedReasons: notFormatted,
            formatScope: !options.runFormat
                ? "none: formatting was left out (--no-format)"
                : scope.everything ? "every file in the package" : scope.description,
            changedFiles: rewritten,
            notWritten: notWritten,
        )
    }

    /*
     The top of the git repository holding the package, or the package itself outside git: the highest
     directory a leftover `.swift-format` is looked for in. Found by the `.git` on disk, the same walk the file
     set uses.
     */
    private func repositoryRoot(of root: URL) -> URL {
        IgnoreRules.Repository.containing(root)?.root ?? root
    }

    /* The modules of every target this package owns, vendored packages left out, spelled as the compiler spells a module: non-identifier characters made `_`. */
    static func ownedModules(of package: PackageModel) -> Set<String> {
        let owned = package.allPackages.filter { $0.root == package.root || !package.isVendored($0) }
        return Set(
            owned.flatMap(\.targets).map { target in
                String(target.name.map { $0.isLetter || $0.isNumber || $0 == "_" ? $0 : "_" })
            }
        )
    }

    /*
     At an Xcode root, the modules the build compiled our files into, which `ownedModules(of:)` cannot name: a
     target's module is named for its product, not the target (CapturePhone's is `Capture`), and these are the
     names the index records declarations under. Empty for a package, whose modules are its targets' names.
     */
    static func compiledModules(of package: PackageModel, fileSet: FileSet, layouts: [BuildLayout]) -> Set<String> {
        guard package.projectKind != .swiftPackage else { return [] }
        let ownedFiles = fileSet.owned.map(\.url)
        var modules: Set<String> = []
        for layout in layouts {
            if let table = try? CompileCommands.read(layout: layout) {
                modules.formUnion(table.modules(compiling: ownedFiles))
            }
        }
        return modules
    }

    /*
     Every check or build of this package that writes an index store and compile commands: for a package, whatever
     platform it is checked for, the root's scratch and each local package's that the types phase checks for its
     tests; for an Xcode root, the one xcodebuild's derived data, which every target and linked package builds into.
     And for either, the check of the standalone scripts of ours (`ScriptTypesPhase`).
     */
    static func symbolLayouts(package: PackageModel, scratch: URL) -> [BuildLayout] {
        /* The scripts' check, last: its store and commands describe only scripts, so no other file is looked for there first. */
        let scripts = BuildLayout.check(scratchPath: ScriptTypesPhase.scratchPath(in: scratch))
        guard package.projectKind == .swiftPackage else {
            return [.xcode(derivedData: XcodeTypesPhase.derivedData(scratchPath: scratch)), scripts]
        }
        let scratchPaths =
            [scratch]
            + package.localPackages.map {
                scratch.appendingPathComponent("local/\($0.root.lastPathComponent)", isDirectory: true)
            }
        return scratchPaths.map { .check(scratchPath: $0) } + [scripts]
    }

    /* Each owned package's manifest, parsed, so package rules can point at the line that would fix them. A manifest that cannot be read is absent, and its findings point at line 1. */
    private func manifests(of package: PackageModel) async -> [String: ParsedFile] {
        /* An Xcode project has no manifest: its package rules read the settings Xcode resolved and point at its definition (`XcodeBuildSettings`). */
        guard package.projectKind == .swiftPackage else { return [:] }
        let owned = package.allPackages.filter { $0.root == package.root || !package.isVendored($0) }
        let files = owned.map {
            FileSet.OwnedFile(
                url: $0.root.appendingPathComponent("Package.swift"),
                targetName: $0.root.path,
                targetKind: "manifest",
                packageRoot: $0.root,
            )
        }
        let result = await SourceParser().parse(files)
        return Dictionary(result.files.map { ($0.targetName, $0) }, uniquingKeysWith: { first, _ in first })
    }

    /*
     Where SwiftPM writes when the engine runs it: the project's own `.cache/cohere/swift`, beside the dispatcher's
     table (#wb3xz1h), so deleting that one folder is always a correct answer and a worktree gets its own. It is
     never `.build`, so a run never takes the developer's lock. SwiftPM's walks skip hidden directories, and the
     file set's walk skips this scratch by its path (`FileSet.buildScratches`), whatever the project's
     `.gitignore` says, so nothing here is ever read as source. `--no-fix` writes no source file; this cache is cohere's.
     */
    /*
     This run's scratch: its own under `--no-cache`, the package's otherwise, whose parent is made first through
     `CacheDirectory.make`, so the run that creates the project's `.cache/cohere` leaves it ignoring itself.
     */
    private func runScratch(for root: URL) -> URL {
        if let freshScratch {
            return freshScratch
        }
        let scratch = Self.scratchPath(for: root)
        do {
            try CacheDirectory.make(scratch.deletingLastPathComponent())
        }
        catch {
            /* The scratch's own creation, by whatever writes there first, says why the directory could not be made. */
        }
        return scratch
    }

    public static func scratchPath(for root: URL) -> URL {
        CacheDirectory.of(root: root).appendingPathComponent("swift", isDirectory: true)
    }

    static func milliseconds(since start: Date) -> Int {
        Int((Date().timeIntervalSince(start) * 1000).rounded())
    }
}
