import Testing

@testable import CohereSwift

/*
 Which target a build directory's records belong to. The names are the ones measured in real scratches:
 ahraos-macos for the testable variant, this engine's own package for an executable named for its product.
 A prefix match read `cohere-swift-parity-p.build` as `cohere-swift`'s and left the parity target's files
 without a record.
 */
struct TypesPhaseDirectoryTests {
    @Test func eachDirectoryNamesItsOwnTargetOrProduct() {
        #expect(TypesPhase.directoryStem("AhraOs-p.build") == "AhraOs")
        #expect(TypesPhase.directoryStem("CohereSwift-t.build") == "CohereSwift")
        #expect(TypesPhase.directoryStem("AhraOsServicesTests-p.build") == "AhraOsServicesTests")
        #expect(TypesPhase.directoryStem("AhraOsServices--36C56AD10F55DF70-testable-t.build") == "AhraOsServices")
        #expect(TypesPhase.directoryStem("cohere-swift-p.build") == "cohere-swift")
        #expect(TypesPhase.directoryStem("cohere-swift-parity-p.build") == "cohere-swift-parity")
    }
}
