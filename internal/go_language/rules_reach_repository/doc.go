// Package rules_reach_repository exists only for its tests, which read cohere's own source to guard what a rule's run
// reaches: which packages take the run's fact resolver, which analyzers learn facts, and which packages write package
// state from a function. The last loads the rules' reach with types from the module root, and every load rooted there
// walks each directory of the repository, so Go's test cache reruns these tests on every change by design; they live
// in a package of their own so that check's tests do not have to (#zychpb8).
package rules_reach_repository
