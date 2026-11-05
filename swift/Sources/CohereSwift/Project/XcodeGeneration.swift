import Foundation

/*
 Generates the Xcode project an xcodegen spec describes, into the engine's own scratch, so the targets of a
 project that keeps only `project.yml` in git can be read without writing anything into the developer's tree.

 A check writes nothing it does not own, ignored by git or not. Plain `xcodegen generate --project <scratch>`
 still writes each target's Info.plist and entitlements beside the spec, and `--project-root` moves them no
 further, because xcodegen resolves a plist's path against the project root. So the spec is dumped as JSON,
 every path xcodegen would write is pointed into the scratch, and the project is generated from that JSON with
 the real root, which keeps every source path pointing at the real files (measured 2026-11-05: the tree stayed
 byte-identical).

 What `generate` writes, read from XcodeGen 2.45.4 (`GenerateCommand.swift`, `FileWriter.swift`, and
 ProjectSpec.md): the `.xcodeproj` bundle, by way of a process-unique folder under the system's temporary
 directory that it removes again; each target's `info` and `entitlements` plist, at its `path`; the spec cache
 under the home folder, only with `--use-cache` or `--cache-path`, which this never passes; and whatever the
 shell commands in `options.preGenCommand` and `options.postGenCommand` do, which is why those are refused.
 Schemes and breakpoints live inside the bundle. Templates can carry `info` and `entitlements` too, so their
 paths are redirected the same way, named for each target through xcodegen's own `${target_name}`.
 */
enum XcodeGeneration {
    /* xcodegen refused, is missing, or the spec asks for something a check may not do. `description` is a sentence for a person. */
    struct Refusal: Error, CustomStringConvertible {
        var description: String
    }

    /* The spec options that run a shell command while the project is generated. */
    static let commandOptions = ["preGenCommand", "postGenCommand"]

    /*
     Generates the project `specFile` describes into `directory` (created if missing, its previous contents
     replaced by the new project and spec), resolving the spec's paths against `root`. Returns the generated
     .xcodeproj. Writes nothing outside `directory`.
     */
    static func generate(specFile: URL, root: URL, into directory: URL, runner: ProcessRunner) throws -> URL {
        let dumped = try runXcodegen(
            ["dump", "--spec", specFile.path, "--project-root", root.path, "--type", "json"],
            in: root,
            specFile: specFile,
            runner: runner,
        )
        guard var spec = try JSONSerialization.jsonObject(with: dumped) as? [String: Any] else {
            throw Refusal(
                description:
                    "xcodegen's dump of \(specFile.path) is not a JSON object, so the Xcode project could not be generated and nothing in it was checked."
            )
        }
        guard let name = spec["name"] as? String, !name.isEmpty else {
            throw Refusal(
                description:
                    "\(specFile.path) names no project, so the Xcode project could not be generated and nothing in it was checked."
            )
        }
        if let options = spec["options"] as? [String: Any] {
            for key in commandOptions where options[key] != nil {
                throw Refusal(
                    description:
                        "\(specFile.path) sets options.\(key), a shell command xcodegen runs while it generates, and a check runs no project commands, so the project was not generated and nothing in it was checked."
                )
            }
        }

        /*
         The dump has already merged every included file, so the key goes: left in, the generation would read
         the includes a second time, doubling every list they add to.
         */
        spec.removeValue(forKey: "include")
        spec["targets"] = redirectedPlists(in: spec["targets"], directory: directory, namedFor: nil)
        spec["targetTemplates"] = redirectedPlists(
            in: spec["targetTemplates"],
            directory: directory,
            namedFor: "${target_name}",
        )

        try replaceContents(of: directory, root: root, specFile: specFile)
        let generatedSpec = directory.appendingPathComponent("project.json")
        try JSONSerialization.data(
            withJSONObject: spec,
            options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes],
        )
        .write(to: generatedSpec)

        /*
         `--no-env` because the dump already expanded the environment's variables, and a second pass would expand
         whatever a value spelled `${...}` after the first, which xcodegen itself never does.
         */
        _ = try runXcodegen(
            [
                "generate", "--quiet", "--no-env", "--spec", generatedSpec.path, "--project-root", root.path,
                "--project",
                directory.path,
            ],
            in: directory,
            specFile: specFile,
            runner: runner,
        )
        let project = directory.appendingPathComponent("\(name).xcodeproj", isDirectory: true)
        guard FileManager.default.fileExists(atPath: project.appendingPathComponent("project.pbxproj").path) else {
            throw Refusal(
                description:
                    "xcodegen reported success for \(specFile.path) but wrote no project at \(project.path), so nothing in it was checked."
            )
        }
        return project
    }

    /*
     Each target's (or template's) `info` and `entitlements` path, pointed at its own file in `directory`. A
     target listing several platforms becomes one target per platform, so its file names carry xcodegen's
     `${platform}` and the targets it becomes never share one. A template's files are named for `templateName`,
     which is `${target_name}`, so every target built from it gets its own.
     */
    static func redirectedPlists(in targets: Any?, directory: URL, namedFor templateName: String?) -> Any? {
        guard var targets = targets as? [String: Any] else { return targets }
        for (targetName, value) in targets {
            guard var target = value as? [String: Any] else { continue }
            let baseName = (templateName ?? targetName).replacingOccurrences(of: "/", with: "-")
            let fileName = target["platform"] is [Any] ? "\(baseName)-${platform}" : baseName
            for (key, suffix) in [("info", ".Info.plist"), ("entitlements", ".entitlements")] {
                guard var plist = target[key] as? [String: Any], plist["path"] != nil else { continue }
                plist["path"] = directory.appendingPathComponent(fileName + suffix).path
                target[key] = plist
            }
            targets[targetName] = target
        }
        return targets
    }

    /*
     Empties `directory` by removing it whole, so a plist or project left by an earlier spec cannot be read as
     this one's. Refused when the directory is the root or holds it, since then emptying it would delete the
     project being checked.
     */
    static func replaceContents(of directory: URL, root: URL, specFile: URL) throws {
        let directoryPath = directory.resolvingSymlinksInPath().standardizedFileURL.path
        let rootPath = root.resolvingSymlinksInPath().standardizedFileURL.path
        guard directoryPath != rootPath,
            !rootPath.hasPrefix(directoryPath.hasSuffix("/") ? directoryPath : directoryPath + "/")
        else {
            throw Refusal(
                description:
                    "the folder \(directory.path) for the project \(specFile.path) describes holds the project itself, so it was not generated and nothing in it was checked."
            )
        }
        if FileManager.default.fileExists(atPath: directory.path) {
            try FileManager.default.removeItem(at: directory)
        }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    /* Runs one xcodegen command and hands back its standard output, refusing in a sentence when it could not run or failed. */
    private static func runXcodegen(
        _ arguments: [String],
        in workingDirectory: URL,
        specFile: URL,
        runner: ProcessRunner,
    ) throws -> Data {
        let notInstalled = Refusal(
            description:
                "xcodegen is not installed, so the Xcode project \(specFile.path) describes could not be generated and nothing in it was checked."
        )
        let result: ProcessRunner.Result
        do {
            result = try runner.run("xcodegen", arguments, in: workingDirectory)
        }
        catch is ProcessRunner.LaunchFailure {
            throw notInstalled
        }
        /* `/usr/bin/env` answers 127 when it finds no such command on the path. */
        if result.exitCode == 127 {
            throw notInstalled
        }
        guard result.succeeded else {
            let said = result.standardError.trimmingCharacters(in: .whitespacesAndNewlines)
            let words = said.isEmpty ? String(decoding: result.standardOutput, as: UTF8.self) : said
            throw Refusal(
                description:
                    "xcodegen refused \(specFile.path), so the Xcode project could not be generated and nothing in it was checked: \(words.trimmingCharacters(in: .whitespacesAndNewlines))"
            )
        }
        return result.standardOutput
    }
}
