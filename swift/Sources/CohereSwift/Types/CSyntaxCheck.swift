import Foundation

/*
 The compiler's diagnostics for a package's C, C++ and Objective-C files, from `clang -fsyntax-only`: parsed and
 type-checked, nothing written but the diagnostics (#11fs5xp). Once a check never builds, a C target is no longer
 compiled, so without this a C error that a build reports would go unreported.

 Each file is checked with its own target's arguments from the build server, which asks them of one file per target
 and language (`BuildServerClient.sourceKitOptions`). Measured 2026-11-06 on Swift 6.4.2: a C file's arguments name
 the file itself, and carry `-fsyntax-only` already, beside an `-o` and the build's `-index-store-path` and
 `-index-unit-output-path`. So the file asked about is swapped for the file checked, and every output flag is
 dropped. clang still fills the module cache the arguments name, under the scratch, which holds precompiled
 headers and never an object. Diagnostics go where `FrontendCheck` puts Swift's, named by the C target, so
 `CompilerRecords` reads both alike. A planted error came back at the line and column a build's record holds.
 */
struct CSyntaxCheck {
    /* One file to check, with its target's arguments for its language, as the server gave them. */
    struct Source: Equatable, Sendable {
        var target: String
        var file: URL
        var options: BuildServerClient.TargetOptions
    }

    /* The server's arguments for a target could not be turned into a file's. */
    struct Failure: Error, CustomStringConvertible, Equatable {
        var target: String
        var detail: String

        var description: String { "the C check of target \(target) could not run: \(detail)" }
    }

    /* What a check did: a record per file, the crashes, the files clang rejected, and each target's record directory. */
    struct Outcome {
        var records: [FrontendCheck.Record]
        var crashes: [FrontendCheck.Crash]
        var failedJobs: [FrontendCheck.FailedJob]
        var targetDirectories: [(String, URL)]
    }

    /* The languages clang checks, as the server names them. */
    static let languages: Set<String> = ["c", "cpp", "objective-c", "objective-cpp"]

    /* `clang` from the toolchain the check uses. */
    let clang: String
    /* The root each target's `<Target>.build` record directory goes under: the same root as `FrontendCheck`'s. */
    let diagnosticsRoot: URL
    let workingDirectory: URL
    var runner = ProcessRunner()
    var parallelism = ProcessInfo.processInfo.activeProcessorCount

    /* Every C-family file of the targets that is not a header, paired with its target's options for its language. */
    static func sources(of targets: [BuildServerClient.Target], options: [BuildServerClient.TargetOptions]) -> [Source]
    {
        targets.flatMap { target in
            target.sources.compactMap { source -> Source? in
                guard !source.isHeader, languages.contains(source.language),
                    let option = options.first(where: {
                        $0.target.identifier == target.identifier && $0.language == source.language
                    })
                else { return nil }
                return Source(target: target.displayName, file: source.url, options: option)
            }
        }
    }

    /* Checks every file side by side, each with its own record. */
    func check(_ sources: [Source]) throws(Failure) -> Outcome {
        let ran = FrontendCheck.pooled(sources, width: parallelism) { source in
            Swift.Result { () throws(Failure) -> ProcessRunner.Result in
                let record = FrontendCheck.diagnosticsPath(
                    in: diagnosticsRoot,
                    module: source.target,
                    file: source.file.path,
                )
                let arguments = try Self.arguments(for: source, diagnostics: record)
                do {
                    try FrontendCheck.removeRecord(record)
                    return try runner.run(clang, arguments, in: workingDirectory)
                }
                catch {
                    throw Failure(
                        target: source.target,
                        detail: "clang could not be run on \(source.file.path): \(error)",
                    )
                }
            }
        }
        var outcome = Outcome(records: [], crashes: [], failedJobs: [], targetDirectories: [])
        for (source, result) in zip(sources, ran) {
            let result = try result.get()
            outcome.records.append(
                FrontendCheck.Record(
                    module: source.target,
                    file: source.file.path,
                    diagnostics: FrontendCheck.diagnosticsPath(
                        in: diagnosticsRoot,
                        module: source.target,
                        file: source.file.path,
                    ),
                    unitOutputPath: "",
                )
            )
            if !outcome.targetDirectories.contains(where: { $0.0 == source.target }) {
                outcome.targetDirectories.append(
                    (source.target, FrontendCheck.moduleDirectory(in: diagnosticsRoot, module: source.target))
                )
            }
            guard !result.succeeded else { continue }
            let standardError = FrontendCheck.tail(result.standardError)
            if let signal = result.terminatingSignal {
                outcome.crashes.append(
                    FrontendCheck.Crash(
                        module: source.target,
                        file: source.file.path,
                        signal: signal,
                        standardError: standardError,
                    )
                )
            }
            else {
                outcome.failedJobs.append(
                    FrontendCheck.FailedJob(
                        module: source.target,
                        primaries: [source.file.path],
                        exitCode: result.exitCode,
                        standardError: standardError,
                    )
                )
            }
        }
        return outcome
    }

    /* Outputs the build would write: objects, dependency files, its own diagnostics record and its index. */
    private static let outputFlagsWithValue: Set<String> = [
        "-o", "-index-store-path", "-index-unit-output-path", "--serialize-diagnostics", "-serialize-diagnostic-file",
        "-MF", "-MT", "-MQ",
    ]
    private static let outputFlags: Set<String> = ["-c", "-fsyntax-only", "-MD", "-MMD", "-M", "-MM"]

    /*
     The target's arguments as one file's syntax check: the file the server was asked about replaced by this one,
     every output dropped, then `-fsyntax-only` and this file's record. Arguments that do not name the file asked
     about cannot be told apart from the file's own, so they are a failure, never a guess.
     */
    static func arguments(for source: Source, diagnostics: URL) throws(Failure) -> [String] {
        let asked = source.options.askedFile.path
        guard source.options.arguments.contains(asked) else {
            throw Failure(
                target: source.target,
                detail:
                    "the build server's arguments for \(asked) do not name it, so they cannot be moved to \(source.file.path)",
            )
        }
        var arguments: [String] = []
        var remaining = source.options.arguments[...]
        while let argument = remaining.popFirst() {
            if outputFlagsWithValue.contains(argument) {
                remaining = remaining.dropFirst()
                continue
            }
            if outputFlags.contains(argument) {
                continue
            }
            arguments.append(argument == asked ? source.file.path : argument)
        }
        return arguments + ["-fsyntax-only", "--serialize-diagnostics", diagnostics.path]
    }
}
