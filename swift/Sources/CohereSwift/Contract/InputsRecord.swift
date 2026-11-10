/*
 What a check run's verdict was read from, written once just before the summary (contract 3, additive), so the
 front door can replay the verdict with no engine started when nothing named here has moved (#nkk91tv). The Go
 engine writes the same record; the front door reads both alike.

 The standing rule is the Go engine's: anything a run reads to decide its verdict must be named here, since a read
 left out is a replay that goes stale without a word, and a superset only costs a miss. `RunInputs` says what this
 engine names and why.

 Every path is absolute, each list is sorted without duplicates, and an empty list is written `[]`.
 */
public struct InputsRecord: Codable, Equatable, Sendable {
    public var kind = "inputs"
    /* The files whose bytes the verdict can depend on. */
    public var files: [String]
    /* The directories whose listing decides which files exist. */
    public var directories: [String]
    /* The paths whose absence the verdict depends on: a file that, once there, would change the run. */
    public var absent: [String]
    /*
     Each git index among `files` the run read only for what it decides of its walk, which tracked paths the ignore
     rules exclude, so the front door keys it on `gitignore.TrackedIgnoredDigest` over `scope` rather than on bytes
     git rewrites on every `git status` (#jem2gsa). An index not named here is keyed on its bytes.
     */
    public var trackedIgnored: [TrackedIgnored]

    /*
     One index the run read for a walk: the index file and the root of its repository, both absolute, and the
     directory holding every path whose answer the run used, relative to that root and slash separated, `""` for the
     root itself.
     */
    public struct TrackedIgnored: Codable, Comparable, Sendable {
        public var index: String
        public var root: String
        public var scope: String

        public init(index: String, root: String, scope: String) {
            self.index = index
            self.root = root
            self.scope = scope
        }

        public static func < (left: TrackedIgnored, right: TrackedIgnored) -> Bool {
            (left.index, left.root, left.scope) < (right.index, right.root, right.scope)
        }
    }

    public init(files: [String], directories: [String], absent: [String], trackedIgnored: [TrackedIgnored] = []) {
        self.files = files
        self.directories = directories
        self.absent = absent
        self.trackedIgnored = trackedIgnored
    }
}
