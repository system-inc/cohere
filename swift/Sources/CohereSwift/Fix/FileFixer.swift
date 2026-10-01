import SwiftParser

/*
 Runs every enabled rule that proposes fixes over one file, applies the fixes, parses the result, and goes
 again until nothing more is proposed or the pass limit is reached.

 Passes repeat because one fix can expose another: folding a comment run can bring the next run into
 the same column. The limit exists because two fixers that undo each other would otherwise loop forever.
 A file that still has fixes at the limit is not hidden: its remaining findings surface in lint like any
 other finding.

 Rules see only the tree, so a fixed text is parsed fresh before the next pass. A pass that would produce
 text the parser rejects is discarded, and the file keeps the last text that parsed. A fixer that breaks a
 file must never be the reason the file stops compiling.
 */
struct FileFixer {
    /* What fixing one file did. */
    struct Result {
        var file: ParsedFile
        var applied: Int
        var refusalsByReason: [String: Int]
    }

    let configuration: RuleConfiguration
    let maximumPasses: Int

    func fix(_ file: ParsedFile) -> Result {
        var current = file
        var applied = 0
        var refusals: [String: Int] = [:]
        for _ in 0..<maximumPasses {
            let edits = RuleRegistry.fileRules
                .filter { configuration.severity(of: $0.name) != .off && $0.applies(to: current) }
                .flatMap { $0.findings(in: current) }
                .flatMap(\.fixes)
            guard !edits.isEmpty else { break }
            let result = FixApplier.apply(edits, to: current.source)
            refusals["overlaps another fix", default: 0] += result.refusedOverlapping
            refusals["invalid range", default: 0] += result.refusedInvalidRange
            guard result.applied > 0, result.text != current.source else { break }
            let tree = Parser.parse(source: result.text)
            if tree.hasError {
                refusals["the fixed text would not parse", default: 0] += result.applied
                break
            }
            let counter = NodeCounter(viewMode: .sourceAccurate)
            counter.walk(tree)
            current = ParsedFile(
                url: current.url,
                targetName: current.targetName,
                targetKind: current.targetKind,
                source: result.text,
                tree: tree,
                nodeCount: counter.count
            )
            applied += result.applied
        }
        return Result(file: current, applied: applied, refusalsByReason: refusals.filter { $0.value > 0 })
    }
}
