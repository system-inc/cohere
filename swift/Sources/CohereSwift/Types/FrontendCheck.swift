import Foundation
import Synchronization

/*
 The compiler's diagnostics and index for a module's files, from the frontend's diagnostic pipeline run without
 code generation: no object, no IRGen, no link (#11fs5xp, ruled on #nkk91tv 2026-11-06).

 The driver plans each module's jobs as `-typecheck` with `-driver-print-jobs`, so it runs nothing, because it
 crashes when asked to plan `SIL` output (the probe's finding). Each planned frontend job is then rewritten:
 - `-typecheck` becomes `-emit-sil`, each primary's `SIL` going to /dev/null. Plain `-typecheck` stops before the
   passes that report missing returns, constant overflow, infinite recursion, use before initialization,
   exclusivity violations and Swift 6's sending data races, all of which a build reports;
 - each primary gets its own `-serialize-diagnostics-path`, laid out as a build lays its records out
   (`<root>/<Module>.build/Objects-normal/check/<File>.dia`), so `CompilerRecords` reads them unchanged;
 - each primary gets its own `-index-unit-output-path`, and the job `-index-store-path` and `-index-system-modules`,
   which gives the full index, references inside bodies included (the probe: 603 of 603 occurrences on one file).

 Every job of every module runs in one bounded pool. A job that fails takes its batch-mates down with it: measured
 2026-11-06 on a fixture, one type error in a batch of nine left the other eight `.dia` files unreadable, and their
 six `SIL` diagnostics unreported. So every primary of a failed batch is rerun alone, which reported all seven. A
 frontend a signal ends is rerun the same way, and one that still crashes alone is a `Crash` naming its file and
 module: a dependency emitted with errors can crash its dependents (the probe saw signal 11), which is a finding,
 not a reason to stop.
 */
struct FrontendCheck {
    /* One module to check: its name and the compiler arguments the build server gave for it. */
    struct Module: Equatable, Sendable {
        var name: String
        var arguments: [String]

        init(name: String, arguments: [String]) {
            self.name = name
            self.arguments = arguments
        }

        /* A module named by its own `-module-name`, or nil when the arguments name none. */
        init?(arguments: [String]) {
            guard let flag = arguments.firstIndex(of: "-module-name"), flag + 1 < arguments.count else { return nil }
            self.init(name: arguments[flag + 1], arguments: arguments)
        }

        /* The Swift files the arguments compile, which the server lists as absolute paths. */
        var sources: [String] {
            arguments.filter { $0.hasSuffix(".swift") && $0.hasPrefix("/") }
        }
    }

    /* One frontend invocation as the driver planned it, `-typecheck` and all, before the check rewrites it. */
    struct Job: Equatable, Sendable {
        var module: String
        var executable: String
        /* The driver's arguments after the executable, `-frontend` first. */
        var plannedArguments: [String]
        /* The primary files, in the order the driver named them, which is the order per-primary outputs pair with. */
        var primaries: [String]

        /* The job with one primary: every other primary stays an input, but only `primary` is checked. */
        func narrowed(to primary: String) -> Job {
            var arguments: [String] = []
            var remaining = plannedArguments[...]
            while let argument = remaining.popFirst() {
                if argument == "-primary-file", let file = remaining.first, file != primary {
                    arguments.append(file)
                    remaining = remaining.dropFirst()
                    continue
                }
                arguments.append(argument)
            }
            return Job(module: module, executable: executable, plannedArguments: arguments, primaries: [primary])
        }
    }

    /* Where one primary's diagnostics went, and the unit its index went under. */
    struct Record: Equatable, Sendable {
        var module: String
        var file: String
        var diagnostics: URL
        var unitOutputPath: String
    }

    /*
     A compiler a signal ended while checking one file, alone, so the file is the one that crashes it: a Swift frontend
     here, or clang in `CSyntaxCheck`, whose module is the C target.
     */
    struct Crash: Error, CustomStringConvertible, Equatable, Sendable {
        var module: String
        var file: String
        var signal: Int32
        var standardError: String

        var description: String {
            "the compiler crashed with signal \(signal) checking \(file) in module \(module): \(standardError)"
        }
    }

    /* A job that failed without a signal after any rerun: its files' errors are in their records, if it wrote them. */
    struct FailedJob: Equatable, Sendable {
        var module: String
        var primaries: [String]
        var exitCode: Int32
        var standardError: String
    }

    /* The check could not plan or start a module's jobs, so nothing it says about that module can be trusted. */
    struct Failure: Error, CustomStringConvertible, Equatable {
        var module: String
        var detail: String

        var description: String { "the check of module \(module) could not run: \(detail)" }
    }

    /* What a check did: a record per primary, the crashes, the batches it reran, and the jobs that still failed. */
    struct Outcome {
        var records: [Record]
        var crashes: [Crash]
        /* The primaries rerun one at a time because their batch failed or crashed. */
        var rerunPrimaries: [String]
        var failedJobs: [FailedJob]
        var jobs: Int
        /* Each module's record directory, by module name, in the shape `CompilerRecords.read(targetDirectories:)` takes. */
        var targetDirectories: [(String, URL)]
    }

    /* `swiftc` from the toolchain the check uses, which plans the jobs. */
    let swiftc: String
    /* The root each module's `<Module>.build` record directory goes under. */
    let diagnosticsRoot: URL
    let indexStorePath: URL
    /* Where the driver and the frontends run. Every path they are given is absolute; this only gives them a home. */
    let workingDirectory: URL
    var runner = ProcessRunner()
    /* How many frontends run at once. */
    var parallelism = ProcessInfo.processInfo.activeProcessorCount
    /* The most primaries the driver puts in one batch, the size the probe measured with. */
    var batchSize = 25

    /* Plans every module, side by side, then runs every job in one pool. */
    func check(_ modules: [Module]) throws(Failure) -> Outcome {
        let planned = Self.pooled(modules, width: parallelism) { module in
            Swift.Result { () throws(Failure) -> [Job] in try plan(module) }
        }
        var jobs: [Job] = []
        for result in planned {
            jobs += try result.get()
        }
        return try run(jobs)
    }

    /*
     The driver's `-typecheck` jobs for one module, planned and not run. Batches hold at most `batchSize` primaries,
     and file lists are kept inline, so each printed line is a whole command.
     */
    func plan(_ module: Module) throws(Failure) -> [Job] {
        let batches = max(1, (module.sources.count + batchSize - 1) / batchSize)
        let arguments =
            Self.cleaned(module.arguments) + [
                "-typecheck", "-enable-batch-mode", "-driver-batch-count", "\(batches)",
                "-driver-filelist-threshold=\(Int32.max)", "-driver-print-jobs",
            ]
        let result: ProcessRunner.Result
        do {
            result = try runner.run(swiftc, arguments, in: workingDirectory)
        }
        catch {
            throw Failure(module: module.name, detail: "the driver could not be started: \(error)")
        }
        guard result.succeeded else {
            throw Failure(
                module: module.name,
                detail: "the driver could not plan its jobs: \(Self.tail(result.standardError))",
            )
        }
        var jobs: [Job] = []
        for line in String(decoding: result.standardOutput, as: UTF8.self).split(whereSeparator: \.isNewline) {
            let words = Self.shellWords(String(line))
            guard words.count > 1, words[1] == "-frontend", let executable = words.first else { continue }
            guard words.contains("-typecheck") else {
                throw Failure(module: module.name, detail: "the driver planned a job that is not -typecheck: \(line)")
            }
            let primaries = zip(words, words.dropFirst()).filter { $0.0 == "-primary-file" }.map(\.1)
            guard !primaries.isEmpty else {
                throw Failure(module: module.name, detail: "the driver planned a job with no primary file: \(line)")
            }
            jobs.append(
                Job(
                    module: module.name,
                    executable: executable,
                    plannedArguments: Array(words.dropFirst()),
                    primaries: primaries,
                )
            )
        }
        guard !jobs.isEmpty else {
            throw Failure(
                module: module.name,
                detail: "the driver planned no frontend job for \(module.sources.count) files",
            )
        }
        return jobs
    }

    /*
     Runs the jobs, the most primaries first, and then every primary of a batch that failed or crashed, alone. A
     crash that survives the rerun is a `Crash`; a job that cannot be started at all fails the whole check.
     */
    func run(_ jobs: [Job]) throws(Failure) -> Outcome {
        let ordered = jobs.sorted { $0.primaries.count > $1.primaries.count }
        var reruns: [Job] = []
        var crashes: [Crash] = []
        var failedJobs: [FailedJob] = []
        for (job, ran) in zip(ordered, Self.pooled(ordered, width: parallelism, execute)) {
            let result = try ran.get()
            guard !result.succeeded else { continue }
            if job.primaries.count > 1 {
                reruns += job.primaries.map(job.narrowed(to:))
            }
            else {
                record(result, of: job, crashes: &crashes, failedJobs: &failedJobs)
            }
        }
        for (job, ran) in zip(reruns, Self.pooled(reruns, width: parallelism, execute)) {
            let result = try ran.get()
            if !result.succeeded {
                record(result, of: job, crashes: &crashes, failedJobs: &failedJobs)
            }
        }

        var records: [Record] = []
        var directories: [(String, URL)] = []
        for job in ordered {
            if !directories.contains(where: { $0.0 == job.module }) {
                directories.append((job.module, moduleDirectory(job.module)))
            }
            for primary in job.primaries {
                records.append(
                    Record(
                        module: job.module,
                        file: primary,
                        diagnostics: diagnosticsPath(module: job.module, file: primary),
                        unitOutputPath: Self.unitOutputPath(module: job.module, file: primary),
                    )
                )
            }
        }
        return Outcome(
            records: records,
            crashes: crashes,
            rerunPrimaries: reruns.flatMap(\.primaries),
            failedJobs: failedJobs,
            jobs: ordered.count + reruns.count,
            targetDirectories: directories,
        )
    }

    private func record(
        _ result: ProcessRunner.Result,
        of job: Job,
        crashes: inout [Crash],
        failedJobs: inout [FailedJob],
    ) {
        let standardError = Self.tail(result.standardError)
        if let signal = result.terminatingSignal, let file = job.primaries.first {
            crashes.append(Crash(module: job.module, file: file, signal: signal, standardError: standardError))
        }
        else {
            failedJobs.append(
                FailedJob(
                    module: job.module,
                    primaries: job.primaries,
                    exitCode: result.exitCode,
                    standardError: standardError,
                )
            )
        }
    }

    /*
     One job, rewritten and run. Its primaries' old records are removed first, so a frontend that dies before writing
     one leaves the file without a record rather than vouched for by the last run's.
     */
    private func execute(_ job: Job) -> Swift.Result<ProcessRunner.Result, Failure> {
        do {
            for primary in job.primaries {
                try Self.removeRecord(diagnosticsPath(module: job.module, file: primary))
            }
            return .success(try runner.run(job.executable, frontendArguments(of: job), in: workingDirectory))
        }
        catch {
            return .failure(Failure(module: job.module, detail: "a frontend job could not be run: \(error)"))
        }
    }

    /*
     The planned job as the check runs it: `-emit-sil` in place of `-typecheck`, and per primary, in the driver's
     order, `SIL` to /dev/null, a diagnostics record and an index unit. Whatever outputs the driver planned are dropped
     first, so only these are written.
     */
    func frontendArguments(of job: Job) -> [String] {
        var arguments: [String] = []
        var remaining = job.plannedArguments[...]
        while let argument = remaining.popFirst() {
            if Self.plannedOutputsWithValue.contains(argument) {
                remaining = remaining.dropFirst()
                continue
            }
            if argument == "-index-system-modules" {
                continue
            }
            arguments.append(argument == "-typecheck" ? "-emit-sil" : argument)
        }
        for _ in job.primaries {
            arguments += ["-o", "/dev/null"]
        }
        for primary in job.primaries {
            arguments += ["-serialize-diagnostics-path", diagnosticsPath(module: job.module, file: primary).path]
        }
        for primary in job.primaries {
            arguments += ["-index-unit-output-path", Self.unitOutputPath(module: job.module, file: primary)]
        }
        return arguments + ["-index-store-path", indexStorePath.path, "-index-system-modules"]
    }

    private static let plannedOutputsWithValue: Set<String> = [
        "-o", "-serialize-diagnostics-path", "-index-store-path", "-index-unit-output-path",
        "-emit-dependencies-path", "-emit-reference-dependencies-path",
    ]

    func moduleDirectory(_ module: String) -> URL {
        Self.moduleDirectory(in: diagnosticsRoot, module: module)
    }

    func diagnosticsPath(module: String, file: String) -> URL {
        Self.diagnosticsPath(in: diagnosticsRoot, module: module, file: file)
    }

    static func moduleDirectory(in root: URL, module: String) -> URL {
        root.appendingPathComponent("\(module).build", isDirectory: true)
    }

    /*
     `<root>/<Module>.build/Objects-normal/check/<File>.dia`: the layout `CompilerRecords.diaFiles(in:)` walks, named by
     the file's stem as a build names its records, so a module's records are keyed as a build's are.
     */
    static func diagnosticsPath(in root: URL, module: String, file: String) -> URL {
        moduleDirectory(in: root, module: module).appendingPathComponent(
            "Objects-normal/check/\(URL(fileURLWithPath: file).deletingPathExtension().lastPathComponent).dia"
        )
    }

    /* Removes a record a previous check left for a file about to be checked again. */
    static func removeRecord(_ record: URL) throws {
        try FileManager.default.createDirectory(
            at: record.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        if FileManager.default.fileExists(atPath: record.path) {
            try FileManager.default.removeItem(at: record)
        }
    }

    /*
     The name a primary's index unit is filed under. Never written: a unit's output path only identifies it, and one
     per module and file keeps two modules' `Utilities.swift` apart.
     */
    static func unitOutputPath(module: String, file: String) -> String {
        "/\(module)/\(URL(fileURLWithPath: file).deletingPathExtension().lastPathComponent).o"
    }

    /*
     The flags prepare adds, which change what the compiler accepts (a module with errors, bodies skipped), and the
     driver flags that would make it write outputs or plan a build rather than a check.
     */
    private static let prepareFlags: Set<String> = [
        "-experimental-lazy-typecheck", "-experimental-skip-all-function-bodies",
        "-experimental-allow-module-with-compiler-errors", "-empty-abi-descriptor",
        "-experimental-skip-non-exportable-decls",
    ]
    private static let droppedFlags: Set<String> = [
        "-incremental", "-parseable-output", "-c", "-emit-module", "-whole-module-optimization", "-wmo",
        "-emit-dependencies", "-emit-objc-header", "-enable-batch-mode", "-serialize-diagnostics",
        "-index-system-modules", "-typecheck",
    ]
    private static let droppedFlagsWithValue: Set<String> = [
        "-output-file-map", "-emit-module-path", "-index-store-path", "-emit-objc-header-path",
        "-index-unit-output-path", "-driver-batch-count", "-o",
    ]

    /* The server's arguments with prepare's flags and every output and planning flag removed. */
    static func cleaned(_ arguments: [String]) -> [String] {
        var cleaned: [String] = []
        var remaining = arguments[...]
        while let argument = remaining.popFirst() {
            if argument == "-Xfrontend", let next = remaining.first, prepareFlags.contains(next) {
                remaining = remaining.dropFirst()
                continue
            }
            if droppedFlags.contains(argument)
                || (argument.hasPrefix("-j") && argument.dropFirst(2).allSatisfy(\.isNumber))
            {
                continue
            }
            if droppedFlagsWithValue.contains(argument) {
                remaining = remaining.dropFirst()
                continue
            }
            cleaned.append(argument)
        }
        return cleaned
    }

    /*
     The Swift modules among the server's options, one per module. An executable's module can be offered twice, once
     testable for its tests (`-entry-point-function-name`, as `CompileCommands` found in the task store), and its
     files are checked once, through the normal one.
     */
    static func modules(from options: [BuildServerClient.TargetOptions]) -> [Module] {
        let swift = options.filter { $0.language == "swift" }.sorted {
            !$0.arguments.contains("-entry-point-function-name") && $1.arguments.contains("-entry-point-function-name")
        }
        var modules: [Module] = []
        for option in swift {
            guard let module = Module(arguments: option.arguments), !modules.contains(where: { $0.name == module.name })
            else { continue }
            modules.append(module)
        }
        return modules
    }

    /* A tool beside the `swift` that `xcrun` resolves: `swiftc` for the check, `clang` for `CSyntaxCheck`. */
    static func toolchainExecutable(named name: String, runner: ProcessRunner = ProcessRunner()) throws -> String {
        URL(fileURLWithPath: try SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner))
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("bin/\(name)")
            .path
    }

    /*
     A printed command split back into its words, as a POSIX shell splits them. The driver quotes a word holding a
     space or a shell character in single quotes, an embedded quote as `'\''`, so quotes and backslashes are honoured.
     */
    static func shellWords(_ line: String) -> [String] {
        var words: [String] = []
        var current = ""
        var inWord = false
        var quote: Character?
        var escaped = false
        for character in line {
            if escaped {
                current.append(character)
                escaped = false
                continue
            }
            if let open = quote {
                if character == open {
                    quote = nil
                }
                else if character == "\\", open == "\"" {
                    escaped = true
                }
                else {
                    current.append(character)
                }
                continue
            }
            switch character {
                case "'", "\"":
                    quote = character
                    inWord = true
                case "\\":
                    escaped = true
                    inWord = true
                case " ", "\t":
                    if inWord {
                        words.append(current)
                        current = ""
                        inWord = false
                    }
                default:
                    current.append(character)
                    inWord = true
            }
        }
        if inWord {
            words.append(current)
        }
        return words
    }

    /*
     Runs `work` on every element with at most `width` at once, and hands back the answers in the elements' order.
     The workers take the next element as they finish, so one long job does not hold up a batch of short ones.
     */
    static func pooled<Element: Sendable, Answer: Sendable>(
        _ elements: [Element],
        width: Int,
        _ work: @Sendable (Element) -> Answer,
    ) -> [Answer] {
        let next = Mutex(0)
        let answers = Mutex<[Answer?]>(Array(repeating: nil, count: elements.count))
        DispatchQueue.concurrentPerform(iterations: max(1, min(width, elements.count))) { _ in
            while true {
                let taken = next.withLock { index -> Int? in
                    guard index < elements.count else { return nil }
                    index += 1
                    return index - 1
                }
                guard let taken else { return }
                let answer = work(elements[taken])
                answers.withLock { $0[taken] = answer }
            }
        }
        /* Every slot was filled: each index below `elements.count` was taken exactly once and answered. */
        return answers.withLock { $0 }.compactMap(\.self)
    }

    /* The last lines of a tool's standard error, for a failure. */
    static func tail(_ text: String) -> String {
        text.split(whereSeparator: \.isNewline).suffix(5).joined(separator: " / ")
    }
}
