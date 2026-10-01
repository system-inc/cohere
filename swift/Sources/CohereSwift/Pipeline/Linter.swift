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
    }

    let configuration: RuleConfiguration

    /* `manifests` holds each owned package's parsed `Package.swift`, keyed by the package root's path; vendored packages are absent and never checked. */
    func run(package: PackageModel, manifests: [String: ParsedFile], files: [ParsedFile]) -> Result {
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
            for member in ownedPackages {
                listening[rule.name, default: 0] += 1
                let found = rule.findings(in: member, manifest: manifests[member.root.path]).map { Self.applying(severity, to: $0) }
                reporting[rule.name, default: 0] += found.count
                findings.append(contentsOf: found)
            }
        }

        for rule in RuleRegistry.fileRules {
            let severity = configuration.severity(of: rule.name)
            guard severity != .off else {
                scopedOff[rule.name] = files.count
                continue
            }
            rulesRun += 1
            for file in files where rule.applies(to: file) {
                listening[rule.name, default: 0] += 1
                let found = rule.findings(in: file).map { Self.applying(severity, to: $0) }
                reporting[rule.name, default: 0] += found.count
                findings.append(contentsOf: found)
            }
        }

        let ranNames = (RuleRegistry.packageRules.map(\.name) + RuleRegistry.fileRules.map(\.name)).filter { scopedOff[$0] == nil }
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
            crashes: [],
            rulesScopedOff: scopedOff,
            rulesNotConfigured: [],
            configNote: configuration.note
        )
        return Result(findings: findings, record: record)
    }

    /* A rule reports at error; the config can lower it to warning. Both count as findings. */
    private static func applying(_ severity: RuleConfiguration.Severity, to finding: FindingRecord) -> FindingRecord {
        var adjusted = finding
        adjusted.severity = severity == .warning ? .warning : .error
        return adjusted
    }
}
