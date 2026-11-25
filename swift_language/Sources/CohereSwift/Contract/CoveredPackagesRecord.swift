/*
 The packages inside the root that a run there accounts for, written under `--covered-packages` and nothing else,
 so the front door can skip running a nested package as its own project when the enclosing run already checks it
 (#sxs26s3: ahraos-presence's VRMKit, RigSolve and CaptureWire were counted twice). Decided by the code the run
 itself uses (PackageModel's traversal and isVendored), never by nesting on disk: a nested package nothing
 depends on, like Presence's RigSimulator, is not covered and runs as its own project.

 `checked` holds the root and each local package inside it whose files the run checks. `vendored` holds each
 local package under `Vendor/`, whose files the run lists as not checked and never runs as ours. Paths are
 absolute and standardized as Foundation spells them, which on macOS writes `/private/tmp` as `/tmp`, so a reader
 compares them with its own paths after resolving symlinks on both sides.
 */
public struct CoveredPackagesRecord: Codable, Equatable, Sendable {
    public var kind = "coveredPackages"
    public var root: String
    public var checked: [String]
    public var vendored: [String]

    public init(root: String, checked: [String], vendored: [String]) {
        self.root = root
        self.checked = checked
        self.vendored = vendored
    }
}
