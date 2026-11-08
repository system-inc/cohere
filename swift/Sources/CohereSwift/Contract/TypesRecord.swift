/*
 What the compiler said about the package, and which files it could not vouch for.

 `filesWithoutRecord` is the part that keeps this honest. The phase reads each file's serialized
 diagnostics rather than the build's output, because an incremental build does not reprint warnings in
 files it did not recompile; a file with no record is therefore a file nobody can say anything about,
 and the run is incomplete while any exist.
 */
public struct TypesRecord: Codable, Equatable, Sendable {
    public var kind = "types"
    public var diagnostics: Int
    public var files: Int
    public var elapsedMilliseconds: Int
    public var filesWithoutRecord: [String]
    public var build: String
    /*
     Additive within contract 3: what the check of the standalone scripts did with the SDK index seed (`SDKIndexSeed`),
     its key and whether it was there and used, absent when the check's store was not empty or no seed applied. A cold
     run that took the toolchain's SDK units is named, never hidden: the front door says it under `--verbose` and in
     `--json`'s summary as `swift.sdkIndexSeed`.
     */
    public var sdkIndexSeed: SDKIndexSeed?

    /* The seed's key, the name of its folder, and whether this run's store was seeded from it. */
    public struct SDKIndexSeed: Codable, Equatable, Sendable {
        public var key: String
        public var hit: Bool
    }

    public init(diagnostics: Int, files: Int, elapsedMilliseconds: Int, filesWithoutRecord: [String], build: String) {
        self.diagnostics = diagnostics
        self.files = files
        self.elapsedMilliseconds = elapsedMilliseconds
        self.filesWithoutRecord = filesWithoutRecord
        self.build = build
    }
}
