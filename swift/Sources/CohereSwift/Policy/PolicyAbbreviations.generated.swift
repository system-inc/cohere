/*
 Generated from cohere's policy/Abbreviations.json by `go run ./policy/tools/generate`. Do not edit it here:
 edit the file and run that again. While the two differ, a test in each engine fails.
 */
enum PolicyAbbreviations {
    /* policy/Abbreviations.json, byte for byte. */
    static let file = ##"""
        {
            "about": "The abbreviation vocabulary for nexus/consistency-no-abbreviated-identifier, read by the Go rule through go:embed and by the Swift engine directly. Each entry is one abbreviated word: `expansions` are the words it stands in for, the most common meaning first, and each form says where in a name the word is judged. A suffix or segment form matches the word capitalized, and suggests the expansion capitalized. A word with more than one common meaning lists every one, and its messages name them all and ask for whichever it means, never one guess; a word whose meanings are too many to list has no `expansions` and gives its `advice` instead. No engine fixes a name from this file, and a word with more than one expansion must never be fixed, since only its author knows which it means. Order is observable: within each form the rule reports the first entry that matches, so `ms` sits between `params` and `ref`, the early prefixes run `ctx` through `prev` in this order, and `vars` precedes `var`. Framework exemptions (`params` in route files, `ref` in React property positions, `config` in middleware, TanStack's `queryFn`) are policy rather than vocabulary and stay in the Go rule.",
            "abbreviations": [
                {
                    "abbreviation": "prop",
                    "expansions": ["property"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "props",
                    "expansions": ["properties"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "param",
                    "expansions": ["parameter"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "params",
                    "expansions": ["parameters"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "ms",
                    "expansions": ["milliseconds"],
                    "reason": "A millisecond unit read as a camelCase word: a lowercase letter before it, and the end of the name or the next word after, so `maximumAgeMsHalf` counts while `SMS`, `LLMs` and `msgText` do not. The tree spells the unit `InMilliseconds`, more than two to one over `Ms` for the identical value.",
                    "suffix": { "matcher": "millisecondWord", "replacement": "InMilliseconds" }
                },
                {
                    "abbreviation": "ref",
                    "expansions": ["reference"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "config",
                    "expansions": ["configuration"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "idx",
                    "expansions": ["index"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "arg",
                    "expansions": ["argument"],
                    "advice": "Use \"argument\", \"commandArgument\", or a more descriptive name.",
                    "reason": "The whole word and the prefix give advice rather than one rename, because the right name depends on what the argument is.",
                    "whole": { "style": "advice" },
                    "prefix": { "phase": "late", "style": "advice" },
                    "suffix": {}
                },
                {
                    "abbreviation": "args",
                    "expansions": ["arguments"],
                    "advice": "Use \"arguments\", \"commandArguments\", \"commandLineArguments\", or a more descriptive name.",
                    "reason": "`arguments` is a reserved binding, so the whole word and the prefix give advice rather than the bare expansion.",
                    "whole": { "style": "advice" },
                    "prefix": { "phase": "late", "style": "advice" },
                    "suffix": {}
                },
                {
                    "abbreviation": "acc",
                    "advice": "Pick a name that describes what is being accumulated, \"total\", \"sum\", \"groupedItems\", or whatever fits the reduce.",
                    "reason": "A reduce accumulator has no single expansion: the right name is whatever it accumulates.",
                    "whole": { "style": "advice" }
                },
                {
                    "abbreviation": "char",
                    "expansions": ["character"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "fn",
                    "advice": "Use \"callback\", \"handler\", \"factory\", or a more descriptive name.",
                    "reason": "A function wants a role rather than an expansion: `function` names the kind, not the job.",
                    "whole": { "style": "advice" },
                    "prefix": { "phase": "late", "style": "advice" },
                    "suffix": {
                        "advice": "Use a more descriptive name, drop the \"Fn\" suffix or rename to a role like \"Handler\", \"Callback\", \"Factory\"."
                    }
                },
                {
                    "abbreviation": "str",
                    "expansions": ["string"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "val",
                    "expansions": ["value"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "arr",
                    "expansions": ["array"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "obj",
                    "expansions": ["object"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "num",
                    "expansions": ["number"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "res",
                    "expansions": ["response"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "err",
                    "expansions": ["error"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "req",
                    "expansions": ["request"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "msg",
                    "expansions": ["message"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "min",
                    "expansions": ["minimum"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "max",
                    "expansions": ["maximum"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "suffix": {}
                },
                {
                    "abbreviation": "ctx",
                    "expansions": ["context"],
                    "reason": "An early prefix: judged before any suffix, so a name starting with it reports the prefix even when it also ends with an abbreviation.",
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" }
                },
                {
                    "abbreviation": "cwd",
                    "expansions": ["workingDirectory"],
                    "segment": {}
                },
                {
                    "abbreviation": "dir",
                    "expansions": ["directory"],
                    "reason": "A segment ends at the end of the name or the start of the next word, which is what keeps `Direction` from reading as `Dir`.",
                    "segment": {}
                },
                {
                    "abbreviation": "env",
                    "expansions": ["environment"],
                    "segment": {}
                },
                {
                    "abbreviation": "cli",
                    "expansions": ["commandLineInterface"],
                    "segment": {}
                },
                {
                    "abbreviation": "len",
                    "expansions": ["length"],
                    "segment": {}
                },
                {
                    "abbreviation": "seq",
                    "expansions": ["sequence"],
                    "segment": {}
                },
                {
                    "abbreviation": "db",
                    "expansions": ["database"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" },
                    "segment": {}
                },
                {
                    "abbreviation": "tx",
                    "expansions": ["transaction"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" },
                    "segment": {}
                },
                {
                    "abbreviation": "opts",
                    "expansions": ["options"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" }
                },
                {
                    "abbreviation": "cur",
                    "expansions": ["current"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" }
                },
                {
                    "abbreviation": "pct",
                    "expansions": ["percent"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" }
                },
                {
                    "abbreviation": "prev",
                    "expansions": ["previous"],
                    "whole": { "style": "plain" },
                    "prefix": { "phase": "early" }
                },
                {
                    "abbreviation": "vars",
                    "expansions": ["variables"],
                    "reason": "Before `var`, so the longer word wins.",
                    "segment": {}
                },
                {
                    "abbreviation": "var",
                    "expansions": ["variable"],
                    "segment": {}
                },
                {
                    "abbreviation": "buf",
                    "expansions": ["buffer"],
                    "reason": "The thirteen from `buf` to `sb` are Go's everyday abbreviations, added for every engine by @system_cohere's ruling on #ec8mvbg so the house keeps one vocabulary. Each is judged as the whole name, as a prefix followed by a capital, and as a segment, so only a whole camelCase word counts: `Buffer` and `Buffalo` never read as `Buf`.",
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "tmp",
                    "expansions": ["temporary"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "pkg",
                    "expansions": ["package"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "src",
                    "expansions": ["source"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "dst",
                    "expansions": ["destination"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "conn",
                    "expansions": ["connection"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "cmd",
                    "expansions": ["command"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "cfg",
                    "expansions": ["configuration"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "pos",
                    "expansions": ["position", "positive"],
                    "reason": "Every form needs a word boundary after it, so `Posix` and `Post` never read as `Pos`. It has two common meanings: a census found `resultPos` beside `resultNeg`, where it means positive, so every message names both.",
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "wg",
                    "expansions": ["waitGroup"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "mu",
                    "expansions": ["mutex"],
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "ch",
                    "expansions": ["channel"],
                    "reason": "Every form needs a word boundary after it, so `Check` and `Change` never read as `Ch`.",
                    "whole": { "style": "orDescriptive" },
                    "prefix": { "phase": "late" },
                    "segment": {}
                },
                {
                    "abbreviation": "sb",
                    "advice": "Spell out what it holds.",
                    "reason": "Go's habit reads `sb` as a strings.Builder, but a census found it naming a scroll region's bottom, and the letters fit more words than a list could hold, so every form asks for the name rather than guessing one.",
                    "whole": { "style": "advice" },
                    "prefix": { "phase": "late", "style": "advice" },
                    "segment": { "style": "advice" }
                }
            ],
            "allowedNames": [
                { "name": "URLSearchParams", "reason": "A Web API, and not ours to rename." }
            ],
            "allowedSegments": [
                {
                    "segment": "InnoDb",
                    "reason": "MySQL's storage engine, a proper noun. The `Db` in it stands in for nothing, and `InnoDatabase` names a thing that does not exist. Matched as a segment because it appears inside longer names like `InnoDbMaximumIndexKeyBytes`."
                }
            ]
        }

        """##
}
