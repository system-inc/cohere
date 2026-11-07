import Foundation

/*
 Runs every rule over the files in scope and accounts for what each rule did.

 The accounting is half the job. A rule that was offered no file, a rule that declined every file it was
 offered, and a rule that read every file and found nothing all produce the same empty finding list, and
 a reader deciding whether a clean run means anything needs to know which of the three it was looking at.
 */
struct Linter {
    /* The rules' findings and the record that says how they were produced. */
    struct Result {
        var findings: [FindingRecord]
        var record: LintRecord
        /* What each file's rules said before severity was applied, by path, which is what a verdict is kept as. */
        var verdicts: [String: Verdict] = [:]
    }

    /*
     One file's rules, as they returned: each enabled file rule that applies with what it found, and each enabled
     typed rule that applies with what it found, or nil when no symbols described the file, so the typed rules
     judged nothing. A rule absent from a map does not apply to the file.
     */
    struct Verdict: Sendable {
        var fileRules: [String: [FindingRecord]]
        var typedRules: [String]
        var typedFindings: [String: [FindingRecord]]?
    }

    let configuration: RuleConfiguration
    let fileRules: [any FileRule]
    var typedRules: [any TypedFileRule] = []
    /* What names resolve to in the files some enabled typed rule applies to, fetched before the walk (`SymbolProvider`). */
    var symbols = SymbolProvider.Result(symbols: [:], unavailable: [:], fromIndex: 0, fromSourcekitd: 0)

    /* `manifests` holds each owned package's parsed `Package.swift`, keyed by the package root's path; vendored packages are absent and never checked. */
    /*
     `reusable` holds, by path, what every enabled file rule that applies found in exactly the text being linted: the
     fix phase's last walk, or a verdict `LintVerdictCache` kept for these bytes. Each map is the whole answer for its
     file, a rule absent from it being one that does not apply, as `FileFixer` collects it, so those files are not
     walked by the file rules again. `typedReplayed` holds the same for the typed rules, from a verdict kept under the
     record that describes the file as it stands; `typedRules` holds, by path, which enabled typed rules apply, where
     a kept verdict already said, so they are not asked again.
     */
    func run(
        package: PackageModel,
        manifests: [String: ParsedFile],
        files: [ParsedFile],
        reusable: [String: [String: [FindingRecord]]] = [:],
        typedReplayed: [String: [String: [FindingRecord]]] = [:],
        typedRules knownTypedRules: [String: [String]] = [:],
    ) async -> Result {
        let ownedPackages = package.allPackages.filter { $0.root == package.root || !package.isVendored($0) }
        var findings: [FindingRecord] = []
        var listening: [String: Int] = [:]
        var reporting: [String: Int] = [:]
        var scopedOff: [String: Int] = [:]
        var rulesRun = 0

        for rule in RuleRegistry.packageRules {
            let severity = configuration.severity(of: rule.name)
            guard severity != .off else {
                scopedOff[rule.name] = 1
                continue
            }
            rulesRun += 1
            /*
             An Xcode project is judged only from the settings Xcode resolved for it (`XcodeBuildSettings`), and only on
             the targets it answered for (`toolchainTargets`). One whose settings this run did not read offers the rule
             nothing, so the rule is listed as silent there rather than judging a guess.
             */
            for member in ownedPackages
            where member.projectKind == .swiftPackage || !(member.xcodeBuildSettings?.targets.isEmpty ?? true) {
                listening[rule.name, default: 0] += 1
                let found = rule.findings(in: member, manifest: manifests[member.root.path]).map {
                    Self.applying(severity, to: $0)
                }
                reporting[rule.name, default: 0] += found.count
                findings.append(contentsOf: found)
            }
        }

        let enabled = fileRules.filter { configuration.severity(of: $0.name) != .off }
        for rule in fileRules where configuration.severity(of: rule.name) == .off {
            scopedOff[rule.name] = files.count
        }
        rulesRun += enabled.count
        let enabledTyped = typedRules.filter { configuration.severity(of: $0.name) != .off }
        for rule in typedRules where configuration.severity(of: rule.name) == .off {
            scopedOff[rule.name] = files.count
        }
        rulesRun += enabledTyped.count
        let symbols = symbols

        /*
         Every file is walked on its own, so files are walked side by side. Each answers with what every enabled
         rule found in it (nil where the rule does not apply), and the results are laid out rule by rule, file by
         file, the order a sequential walk printed, so the stream is the same however the work was scheduled.
         */
        let perFile = await withTaskGroup(of: (Int, Verdict).self) { group in
            for (index, file) in files.enumerated() {
                group.addTask {
                    let path = file.url.path
                    let byFileRule =
                        reusable[path]
                        ?? Dictionary(
                            enabled.compactMap { rule in
                                rule.applies(to: file) ? (rule.name, rule.findings(in: file)) : nil
                            },
                            uniquingKeysWith: { first, _ in first },
                        )
                    let applying =
                        knownTypedRules[path] ?? enabledTyped.filter { $0.applies(to: file) }.map(\.name)
                    /* A typed rule with no symbols for a file it applies to judged nothing there; the crash list below says so. */
                    var byTypedRule: [String: [FindingRecord]]?
                    if let replayed = typedReplayed[path] {
                        byTypedRule = replayed
                    }
                    else if applying.isEmpty {
                        byTypedRule = [:]
                    }
                    else if let found = symbols.symbols[path] {
                        byTypedRule = Dictionary(
                            enabledTyped.filter { applying.contains($0.name) }.map {
                                ($0.name, $0.findings(in: file, symbols: found))
                            },
                            uniquingKeysWith: { first, _ in first },
                        )
                    }
                    return (
                        index, Verdict(fileRules: byFileRule, typedRules: applying, typedFindings: byTypedRule),
                    )
                }
            }
            var collected = [Verdict?](repeating: nil, count: files.count)
            for await (index, verdict) in group {
                collected[index] = verdict
            }
            /* Every index was answered once: one task per file, each returning its own index. */
            return collected.compactMap(\.self)
        }
        for name in enabled.map(\.name) {
            let severity = configuration.severity(of: name)
            for verdict in perFile {
                guard let raw = verdict.fileRules[name] else { continue }
                listening[name, default: 0] += 1
                let found = raw.map { Self.applying(severity, to: $0) }
                reporting[name, default: 0] += found.count
                findings.append(contentsOf: found)
            }
        }
        for name in enabledTyped.map(\.name) {
            let severity = configuration.severity(of: name)
            for verdict in perFile {
                guard let raw = verdict.typedFindings?[name] else { continue }
                listening[name, default: 0] += 1
                let found = raw.map { Self.applying(severity, to: $0) }
                reporting[name, default: 0] += found.count
                findings.append(contentsOf: found)
            }
        }

        let ranNames = (RuleRegistry.packageRules.map(\.name) + fileRules.map(\.name) + typedRules.map(\.name)).filter {
            scopedOff[$0] == nil
        }
        /* A file a typed rule applies to and no source of symbols described: nothing that rule would say about it was said. */
        let unchecked = zip(files, perFile).filter { file, verdict in
            symbols.unavailable[file.url.path] != nil && !verdict.typedRules.isEmpty
        }
        /*
         Each crash names the rules it cost, so a reader knows what the file was not judged for and that every other
         rule judged it: a count with no rule leaves nobody able to say what a crashed file missed (@system_cohere,
         2026-11-05, from a one-file package that crashed under `--lint` before any build).
         */
        var crashes = unchecked.map { file, verdict in
            let missed = verdict.typedRules.sorted()
            return LintRecord.Crash(
                file: file.url.path,
                error:
                    "\(symbols.unavailable[file.url.path] ?? ""), so the rules that read what its names resolve to could not judge it (\(missed.joined(separator: ", "))); every other rule ran on it",
            )
        }
        /*
         An Xcode target whose settings Xcode could not give is unchecked by the toolchain rules, never judged on or
         off, and named at the project's definition with why, so the run is incomplete rather than clean.
         */
        let toolchainRules = RuleRegistry.packageRules.map(\.name).filter { scopedOff[$0] == nil }.sorted()
        if !toolchainRules.isEmpty {
            for member in ownedPackages {
                guard let settings = member.xcodeBuildSettings else { continue }
                for (target, reason) in settings.unchecked.sorted(by: { $0.key < $1.key }) {
                    crashes.append(
                        LintRecord.Crash(
                            file: settings.definition.path,
                            error:
                                "the build settings of \(target) could not be read: \(reason); so the rules that judge them did not (\(toolchainRules.joined(separator: ", ")))",
                        )
                    )
                }
            }
        }
        let silent = ranNames.filter { (listening[$0] ?? 0) == 0 }.sorted()
        let watchedAndQuiet = ranNames.filter { (listening[$0] ?? 0) > 0 && (reporting[$0] ?? 0) == 0 }.count

        let record = LintRecord(
            findings: findings.count,
            rulesRun: rulesRun,
            filesWalked: files.count,
            nodesVisited: files.reduce(0) { $0 + $1.nodeCount },
            elapsedMilliseconds: 0,
            reusedFrom: "",
            rulesSilent: silent,
            rulesWatchedAndQuiet: watchedAndQuiet,
            crashes: crashes,
            rulesScopedOff: scopedOff,
            rulesNotConfigured: [],
            configurationNote: configuration.note,
            filesReplayed: 0,
        )
        return Result(
            findings: findings,
            record: record,
            verdicts: Dictionary(zip(files.map(\.url.path), perFile), uniquingKeysWith: { first, _ in first }),
        )
    }

    /* A rule reports at error; the config can lower it to warning. Both count as findings. */
    private static func applying(_ severity: RuleConfiguration.Severity, to finding: FindingRecord) -> FindingRecord {
        var adjusted = finding
        adjusted.severity = severity == .warning ? .warning : .error
        return adjusted
    }
}
