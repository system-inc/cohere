/*
 A package rule that also judges an Xcode project's targets, from the build settings Xcode resolves for them
 (`XcodeBuildSettings`), with the same verdict it gives a package's targets, since both read the target's language
 mode, features and memory safety from the model. What a target lacks is one function, read by the rule's finding
 and by the fix that writes project.yml (`XcodeSpecificationFix`), so the two never disagree about what is missing.
 */
protocol XcodeSettingRule: PackageRule {
    /* The build settings `target` lacks, as project.yml spells them; empty when it passes or is not this rule's to judge. */
    func xcodeSettingsMissing(for target: PackageModel.Target) -> [XcodeGenSpecification.Setting]
}
