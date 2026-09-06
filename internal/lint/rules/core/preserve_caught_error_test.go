package core

import (
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// The case strings in this file were extracted mechanically from oxc's inline corpus at
// crates/oxc_linter/src/rules/eslint/preserve_caught_error.rs and verified byte against byte
// against that source. Nothing here was transcribed by hand, because a cooked escape in a
// fixture asserts the opposite of upstream while sitting green.

// TestPreserveCaughtErrorFires runs every failing case from upstream's corpus.
//
// The corpus is 30 inputs and 31 diagnostics: one input holds two uncaused throws and reports
// twice. That count was recovered from the snapshot by aligning each diagnostic on the source
// line it prints rather than by walking inputs in order, which is what the brief asks for and
// what separates a two-finding input from two inputs that happen to be adjacent.
func TestPreserveCaughtErrorFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source  string
		options any
		wantIds []string
	}{
		{
			source:  "try { doSomething(); } catch (err) { throw new Error/* ( */(); }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try { doSomething(); } catch (err) { throw new Error<() => void>(); }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            throw new Error(\"Something failed\");\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            const unrelated = new Error(\"other\");\n\t\t\t            throw new Error(\"Something failed\", { cause: unrelated });\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            const e = err;\n\t\t\t            throw new Error(\"Failed\", { cause: e });\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            throw new Error(\"Failed\", { cause: error.message });\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            if (shouldThrow) {\n\t\t\t                while (true) {\n\t\t\t                    if (Math.random() > 0.5) {\n\t\t\t                        throw new Error(\"Failed without cause\");\n\t\t\t                    }\n\t\t\t                }\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            switch (error.code) {\n\t\t\t                case \"A\":\n\t\t\t                    throw new Error(\"Type A\");\n\t\t\t                case \"B\":\n\t\t\t                    throw new Error(\"Type B\", { cause: error });\n\t\t\t                default:\n\t\t\t                    throw new Error(\"Other\", { cause: error });\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            throw new Error(`The certificate key \"${chalk.yellow(keyFile)}\" is invalid.\n\t\t\t${err.message}`);\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            const errorMessage = \"Operation failed\";\n\t\t\t            throw new Error(errorMessage);\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (error) {\n\t\t\t            const errorMessage = \"Operation failed\";\n\t\t\t            throw new Error(errorMessage, { existingOption: true, complexOption: { moreOptions: {} } });\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            if (err.code === \"A\") {\n\t\t\t                throw new Error(\"Type A\");\n\t\t\t            }\n\t\t\t            throw new TypeError(\"Fallback error\");\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError", "preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            throw Error(\"Something failed\");\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            my_label:\n\t\t\t            throw new Error(\"Failed without cause\");\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            {\n\t\t\t                throw new Error(\"Something went wrong\");\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            {\n\t\t\t                throw new Error();\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            {\n\t\t\t                throw new AggregateError([], \"Lorem ipsum\");\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            {\n\t\t\t                throw new AggregateError();\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t        } catch (err) {\n\t\t\t            {\n\t\t\t                throw new AggregateError([]);\n\t\t\t            }\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t} catch {\n\t\t\t\t\t\tthrow new Error(\"Something went wrong\");\n\t\t\t\t\t}",
			options: PreserveCaughtErrorOptions{RequireCatchParameter: true},
			wantIds: []string{"missingCatchParameter"},
		},
		{
			source:  "try {\n\t\t\t            doSomething();\n\t\t\t        } catch (err) {\n\t\t\t            throw new Error(\"Something failed\", { cause });\n\t\t\t        }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch ({ message }) {\n\t\t\t\t\t\t\tthrow new Error(message);\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\t\tdoSomethingElse();\n\t\t\t\t\t\t} catch ({ ...error }) {\n\t\t\t\t\t\t\tthrow new Error(error.message);\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\t\tif (whatever) {\n\t\t\t\t\t\t\t\tconst error = anotherError;\n\t\t\t\t\t\t\t\tthrow new Error(\"Something went wrong\", { cause: error });\n\t\t\t\t\t\t\t}\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\t\tthrow new Error(\n\t\t\t\t\t\t\t\t\"Something went wrong\" // some comments\n\t\t\t\t\t\t\t);\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (err) {\n\t\t\t\t\t\t\tthrow new Error(\"Something failed\", {});\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\tconst cause = \"desc\";\n\t\t\t\t\t\tthrow new Error(\"Something failed\", { [cause]: \"Some error\" });\n\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {\n\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\tthrow new Error(\"Something failed\", { cause() { /* do something */ }  });\n\t\t\t\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try {} catch (error) {\n\t\t\t\tthrow new Error(\"Something failed\", {\n\t\t\t\t\tget cause() { return error; },\n\t\t\t\t\tset cause(value) { error = value; },\n\t\t\t\t});\n\t\t\t}",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
		{
			source:  "try { doSomething(); } catch (error) { throw new AggregateError([error], \"aggregate\", { cause: unrelated }); }",
			options: nil,
			wantIds: []string{"preserveCaughtError"},
		},
	}

	for index, testCase := range cases {
		result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", testCase.source, testCase.options)
		if len(result.Diagnostics) != len(testCase.wantIds) {
			t.Errorf("case %d: got %d findings, want %d\n%s", index, len(result.Diagnostics), len(testCase.wantIds), testCase.source)
			continue
		}
		rule_testing.ExpectFindings(t, result, testCase.wantIds...)
	}
}

// TestPreserveCaughtErrorStaysSilent runs every passing case from upstream's corpus.
//
// These are the cases that catch a port. Each was added upstream when somebody hit that bug, so
// a rule that reports on one of them is wrong in a way no invented fixture would have found.
func TestPreserveCaughtErrorStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source  string
		options any
	}{
		{source: "try {\n\t\t\t        throw new Error(\"Original error\");\n\t\t\t    } catch (error) {\n\t\t\t        throw new Error(\"Failed to perform error prone operations\", { cause: error });\n\t\t\t    }", options: nil},
		{source: "try {\n\t\t\t        doSomething();\n\t\t\t    } catch (e) {\n\t\t\t        console.error(e);\n\t\t\t    }", options: nil},
		{source: "try {\n\t\t\t        doSomething();\n\t\t\t    } catch (err) {\n\t\t\t        throw new Error(\"Failed\", { cause: err, extra: 42 });\n\t\t\t    }", options: nil},
		{source: "try {\n\t\t\t        doSomething();\n\t\t\t    } catch (error) {\n\t\t\t        switch (error.code) {\n\t\t\t            case \"A\":\n\t\t\t                throw new Error(\"Type A\", { cause: error });\n\t\t\t            case \"B\":\n\t\t\t                throw new Error(\"Type B\", { cause: error });\n\t\t\t            default:\n\t\t\t                throw new Error(\"Other\", { cause: error });\n\t\t\t        }\n\t\t\t    }", options: nil},
		{source: "try {\n\t\t\t\t\t// ...\n\t\t\t\t} catch (err) {\n\t\t\t\t\tconst opts = { cause: err }\n\t\t\t\t\tthrow new Error(\"msg\", { ...opts });\n\t\t\t\t}\n\t\t\t\t", options: nil},
		{source: "try {\n\t\t\t\t} catch (error) {\n\t\t\t\t\tfoo = {\n\t\t\t\t\t\tbar() {\n\t\t\t\t\t\t\tthrow new Error();\n\t\t\t\t\t\t}\n\t\t\t\t\t};\n\t\t\t\t}", options: nil},
		{source: "try {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\t\tconst args = [];\n\t\t\t\t\t\t\tthrow new Error(...args);\n\t\t\t\t\t}", options: nil},
		{source: "import { Error } from \"./my-custom-error.js\";\n\t\t\t\t\t\ttry {\n\t\t\t\t\t\t\tdoSomething();\n\t\t\t\t\t\t} catch (error) {\n\t\t\t\t\t\t\tthrow Error(\"Failed to perform error prone operations\");\n\t\t\t\t\t\t}", options: nil},
		{source: "try {\n\t\t\t\t\tdoSomething();\n\t\t\t\t} catch {\n\t\t\t\t\tthrow new Error(\"Something went wrong\");\n\t\t\t\t}", options: PreserveCaughtErrorOptions{RequireCatchParameter: false}},
		{source: "try { doSomething(); } catch (errorA) { try { doSomethingElse(); } catch (errorB) { throw new Error( `The certificate key \"${chalk.yellow(keyFile)}\" is invalid.\\n${errorA.message}`, { cause: errorB }); } }", options: nil},
		{source: "try { doSomething(); } catch (error) { throw new AggregateError([error], \"aggregate\", { cause: error }); }", options: nil},
	}

	for index, testCase := range cases {
		result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", testCase.source, testCase.options)
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			rule_testing.ExpectClean(t, result)
		})
	}
}

// TestPreserveCaughtErrorAppliesUpstreamRepairs asserts what every repair WRITES, not which rule
// fired.
//
// Every one of upstream's 27 fix vectors is here and every one is asserted. Sampling would defeat
// the point: a fixture asserting a message id cannot see a correct finding carrying a fix that
// writes the right characters over the wrong range, and that is the defect this whole test
// exists to catch.
func TestPreserveCaughtErrorAppliesUpstreamRepairs(t *testing.T) {
	t.Parallel()

	vectors := []struct {
		before string
		after  string
	}{
		{
			before: "try { doSomething(); } catch (err) { throw new Error/* ( */(); }",
			after:  "try { doSomething(); } catch (err) { throw new Error/* ( */(\"\", { cause: err }); }",
		},
		{
			before: "try { doSomething(); } catch (err) { throw new Error<() => void>(); }",
			after:  "try { doSomething(); } catch (err) { throw new Error<() => void>(\"\", { cause: err }); }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\");\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\");\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\", { cause: error });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\", {});\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\", { cause: error });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\", { existingOption: true, complexOption: { option: {} } });\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (error) {\n                        throw new Error(\"Failed\", { existingOption: true, complexOption: { option: {} }, cause: error });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", {});\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        const unrelated = new Error(\"other\");\n                        throw new Error(\"Something failed\", { cause: unrelated });\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        const unrelated = new Error(\"other\");\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        const e = err;\n                        throw new Error(\"Something failed\", { cause: e });\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        const e = err;\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause: err.message });\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                if (shouldThrow) {\n                    while (true) {\n                        if (Math.random() > 0.5) {\n                            throw new Error(\"Failed without cause\");\n                        }\n                    }\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                if (shouldThrow) {\n                    while (true) {\n                        if (Math.random() > 0.5) {\n                            throw new Error(\"Failed without cause\", { cause: error });\n                        }\n                    }\n                }\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                switch (error.code) {\n                    case \"A\":\n                        throw new Error(\"Type A\");\n                    case \"B\":\n                        throw new Error(\"Type B\", { cause: error });\n                    default:\n                        throw new Error(\"Other\", { cause: error });\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                switch (error.code) {\n                    case \"A\":\n                        throw new Error(\"Type A\", { cause: error });\n                    case \"B\":\n                        throw new Error(\"Type B\", { cause: error });\n                    default:\n                        throw new Error(\"Other\", { cause: error });\n                }\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(`The certificate key \"${chalk.yellow(keyFile)}\" is invalid.\\n${err.message}`);\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(`The certificate key \"${chalk.yellow(keyFile)}\" is invalid.\\n${err.message}`, { cause: error });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                const errorMessage = \"Operation failed\";\n                throw new Error(errorMessage);\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                const errorMessage = \"Operation failed\";\n                throw new Error(errorMessage, { cause: error });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                if (err.code === \"A\") {\n                    throw new Error(\"Type A\");\n                }\n                throw new TypeError(\"Fallback error\");\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                if (err.code === \"A\") {\n                    throw new Error(\"Type A\", { cause: err });\n                }\n                throw new TypeError(\"Fallback error\", { cause: err });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                throw Error(\"Something failed\");\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                throw Error(\"Something failed\", { cause: err });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                my_label:\n                throw new Error(\"Failed without cause\");\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                my_label:\n                throw new Error(\"Failed without cause\", { cause: err });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new Error();\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new Error(\"\", { cause: err });\n                }\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError([], \"Lorem ipsum\");\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError([], \"Lorem ipsum\", { cause: err });\n                }\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError();\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError([], \"\", { cause: err });\n                }\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError([]);\n                }\n            }",
			after:  "try {\n                doSomething();\n            } catch (err) {\n                {\n                    throw new AggregateError([], \"\", { cause: err });\n                }\n            }",
		},
		{
			before: "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause });\n                    }",
			after:  "try {\n                        doSomething();\n                    } catch (err) {\n                        throw new Error(\"Something failed\", { cause: err });\n                    }",
		},
		{
			before: "try {\n\t\t\t\tdoSomething();\n\t\t\t} catch (error) {\n\t\t\t\tthrow new Error(\n\t\t\t\t\t\"Something went wrong\" // some comments\n\t\t\t\t);\n\t\t\t}",
			after:  "try {\n\t\t\t\tdoSomething();\n\t\t\t} catch (error) {\n\t\t\t\tthrow new Error(\n\t\t\t\t\t\"Something went wrong\", { cause: error } // some comments\n\t\t\t\t);\n\t\t\t}",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                const cause = \"desc\";\n                throw new Error(\"Something failed\", { [cause]: \"Some error\" });\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                const cause = \"desc\";\n                throw new Error(\"Something failed\", { [cause]: \"Some error\", cause: error });\n            }",
		},
		{
			before: "try {\n                doSomething();\n\t\t\t} catch (error) {\n                throw new Error(\"Something failed\", { cause() { /* do something */ }  });\n\t\t\t}",
			after:  "try {\n                doSomething();\n\t\t\t} catch (error) {\n                throw new Error(\"Something failed\", { cause: error  });\n\t\t\t}",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(\"Something failed\", { get cause() { } });\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(\"Something failed\", { cause: error });\n            }",
		},
		{
			before: "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(\"Something failed\", { set cause(value) { } });\n            }",
			after:  "try {\n                doSomething();\n            } catch (error) {\n                throw new Error(\"Something failed\", { cause: error });\n            }",
		},
	}

	for index, vector := range vectors {
		result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", vector.before)

		// The typed harness writes `strings.TrimSpace(source) + "\n"` to disk, so the text the
		// rule and the fix engine actually see is not byte-identical to the corpus string. The
		// expectation is put through the same transformation rather than the corpus string being
		// edited, so the fixture on disk stays byte-verifiable against upstream.
		want := strings.TrimSpace(vector.after) + "\n"

		t.Run("", func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("vector %d panicked: %v", index, recovered)
				}
			}()
			rule_testing.ExpectFixedSource(t, result, want)
		})
	}
}

// The cases below are NOT from upstream's corpus. Each exists because reading our own code raised a
// question the corpus does not answer, and each verdict was measured on the oxlint release binary
// before the fixture was written rather than reasoned about afterwards. The command was
// `oxlint --config <rc> .` over a directory of one-line files, with `--fix` re-run separately to
// read what each repair writes.

// TestPreserveCaughtErrorResolvesTheCauseBindingBySymbol pins the discrimination the checker is
// declared for, in BOTH directions.
//
// Upstream resolves the cause value to a `symbol_id` and compares it against the catch binding's
// own symbol. Name matching agrees with that on the first case and disagrees on the second, and the
// second is in upstream's corpus precisely because somebody hit it. Both are asserted here as well,
// in one-line form, so that a mutation replacing the symbol comparison with a text comparison has
// something small and unambiguous to fail against.
func TestPreserveCaughtErrorResolvesTheCauseBindingBySymbol(t *testing.T) {
	t.Parallel()

	rightBinding := `try { a(); } catch (err) { throw new Error("m", { cause: err }); }`
	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", rightBinding); len(result.Diagnostics) != 0 {
		t.Errorf("the caught binding itself must be accepted, got %d findings", len(result.Diagnostics))
	}

	shadowed := `try { a(); } catch (err) { if (w) { const err = other; throw new Error("m", { cause: err }); } }`
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", shadowed)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
}

// TestPreserveCaughtErrorDoesNotDescendIntoANestedFunction pins one of the two non-descents.
//
// It is separate from the nested-catch test below on purpose. They are two independent guards in
// upstream and two independent early returns here, and a sweep that mutated one while the other's
// fixture covered the input would score a survivor for the wrong reason.
func TestPreserveCaughtErrorDoesNotDescendIntoANestedFunction(t *testing.T) {
	t.Parallel()

	// Measured silent on the release binary in all four shapes.
	silent := []string{
		`try { a(); } catch (err) { const f = function () { throw new Error("m"); }; f(); }`,
		`try { a(); } catch (err) { function g() { throw new Error("m"); } g(); }`,
		`try { a(); } catch (err) { o = { bar() { throw new Error("m"); } }; }`,
		`try { a(); } catch (err) { class K { m() { throw new Error("m"); } } }`,
	}
	for index, source := range silent {
		if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source); len(result.Diagnostics) != 0 {
			t.Errorf("case %d: a throw inside a nested function must be silent, got %d findings:\n%s",
				index, len(result.Diagnostics), source)
		}
	}
}

// TestPreserveCaughtErrorDoesDescendIntoAnArrowFunction pins the asymmetry the dispatch for this
// port described the other way round, and which reading the Rust alone would also get wrong.
//
// oxc stubs `visit_function`, which covers its `Function` node. An arrow function is a separate AST
// node with its own visitor that upstream never stubbed, so the walk goes straight through it. All
// three of these REPORT on the release binary, at columns 46, 52 and 64 of their respective files,
// while the four shapes in the test above are silent. Almost certainly an upstream bug; reproduced
// because the differential harness compares against oxlint.
func TestPreserveCaughtErrorDoesDescendIntoAnArrowFunction(t *testing.T) {
	t.Parallel()

	reporting := []string{
		`try { a(); } catch (err) { const f = () => { throw new Error("m"); }; f(); }`,
		`try { a(); } catch (err) { const f = async () => { throw new Error("m"); }; f(); }`,
		`try { a(); } catch (err) { const f = () => { const g = () => { throw new Error("m"); }; g(); }; f(); }`,
	}
	for index, source := range reporting {
		result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
		if len(result.Diagnostics) != 1 {
			t.Errorf("case %d: a throw inside an arrow function reports upstream, got %d findings:\n%s",
				index, len(result.Diagnostics), source)
			continue
		}
		rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	}

	// And the arrow's cause still resolves against the OUTER catch binding, which is what makes
	// descending into it coherent rather than merely permissive.
	clean := `try { a(); } catch (err) { const f = () => { throw new Error("m", { cause: err }); }; f(); }`
	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", clean); len(result.Diagnostics) != 0 {
		t.Errorf("an arrow attaching the outer caught error must be clean, got %d findings", len(result.Diagnostics))
	}
}

// TestPreserveCaughtErrorDoesNotDescendIntoANestedCatch pins the other non-descent.
//
// The inner catch is analyzed on its own try statement, so its throws are compared against ITS
// parameter. Without this guard, the first case would report: `errorB` is not `errorA`.
func TestPreserveCaughtErrorDoesNotDescendIntoANestedCatch(t *testing.T) {
	t.Parallel()

	inner := `try { a(); } catch (errorA) { try { b(); } catch (errorB) { throw new Error("m", { cause: errorB }); } }`
	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", inner); len(result.Diagnostics) != 0 {
		t.Errorf("a nested catch attaching its own error must be clean, got %d findings", len(result.Diagnostics))
	}

	// The other direction: the inner throw attaching the OUTER error is a finding, because the
	// inner clause's own analysis compares against `errorB`. One finding, not two, which is what
	// says the outer walk stopped at the nested catch rather than also reporting there.
	outer := `try { a(); } catch (errorA) { try { b(); } catch (errorB) { throw new Error("m", { cause: errorA }); } }`
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", outer)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
}

// TestPreserveCaughtErrorRecognizesOnlyThreeConstructors records the divergence from ESLint.
//
// oxc's list is `Error`, `TypeError`, `AggregateError`. ESLint's is eight names plus a custom-class
// option. Measured: `throw new RangeError("m")` inside a catch is silent on the release binary and
// reports under ESLint's Linter API on the same input. oxc wins, and this is the fixture that would
// notice if somebody helpfully widened the list.
func TestPreserveCaughtErrorRecognizesOnlyThreeConstructors(t *testing.T) {
	t.Parallel()

	silent := []string{
		`try { a(); } catch (err) { throw new RangeError("m"); }`,
		`try { a(); } catch (err) { throw new SyntaxError("m"); }`,
		`try { a(); } catch (err) { throw new EvalError("m"); }`,
		`try { a(); } catch (err) { throw new ReferenceError("m"); }`,
		`try { a(); } catch (err) { throw new URIError("m"); }`,
	}
	for _, source := range silent {
		if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source); len(result.Diagnostics) != 0 {
			t.Errorf("only Error, TypeError and AggregateError are recognized, but this reported:\n%s", source)
		}
	}

	reporting := []string{
		`try { a(); } catch (err) { throw new Error("m"); }`,
		`try { a(); } catch (err) { throw new TypeError("m"); }`,
		`try { a(); } catch (err) { throw new AggregateError([], "m"); }`,
	}
	for _, source := range reporting {
		result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
		rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	}
}

// TestPreserveCaughtErrorRequiresTheGlobalBinding pins the checker's second job.
//
// The corpus covers the import form. This adds the two source-declared shapes, which are the ones
// that would slip past a name-only test, and it is the fixture a mutant removing the global test
// has to fail against.
func TestPreserveCaughtErrorRequiresTheGlobalBinding(t *testing.T) {
	t.Parallel()

	shadowed := []string{
		"class Error {}\ntry { a(); } catch (err) { throw new Error(\"m\"); }",
		"let Error = X;\ntry { a(); } catch (err) { throw new Error(\"m\"); }",
		"function TypeError() {}\ntry { a(); } catch (err) { throw new TypeError(\"m\"); }",
	}
	for _, source := range shadowed {
		if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source); len(result.Diagnostics) != 0 {
			t.Errorf("a locally-declared Error shadows the global and must be silent:\n%s", source)
		}
	}
}

// TestPreserveCaughtErrorSkipsNoParentheses records the two paren measurements, which fall opposite
// ways and would each be a silent divergence if guessed.
//
// Measured on the release binary: `new (Error)("m")` is SILENT because upstream destructures the
// callee identifier directly and a parenthesized expression is not one. `{ cause: (err) }` REPORTS
// for the same reason at the value position. Adding a paren skip at either site flips a real
// verdict, and the corpus writes neither form.
func TestPreserveCaughtErrorSkipsNoParentheses(t *testing.T) {
	t.Parallel()

	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts",
		`try { a(); } catch (err) { throw new (Error)("m"); }`); len(result.Diagnostics) != 0 {
		t.Errorf("a parenthesized callee is silent upstream, got %d findings", len(result.Diagnostics))
	}

	parenthesizedCause := `try { a(); } catch (err) { throw new Error("m", { cause: (err) }); }`
	rule_testing.ExpectFindings(t, rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", parenthesizedCause),
		"preserveCaughtError")

	// A non-null assertion falls the same way and for the same reason, and this one DOES carry a
	// repair upstream: `{ cause: err! }` becomes `{ cause: err }`.
	assertedCause := `try { a(); } catch (err) { throw new Error("m", { cause: err! }); }`
	assertedResult := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", assertedCause)
	rule_testing.ExpectFindings(t, assertedResult, "preserveCaughtError")
	rule_testing.ExpectFixedSource(t, assertedResult,
		"try { a(); } catch (err) { throw new Error(\"m\", { cause: err }); }\n")
}

// TestPreserveCaughtErrorReadsTheFirstCauseKey pins the ordering decision, where oxc, ESLint and
// JavaScript itself all give different answers.
//
// Upstream returns on the FIRST `cause` key. JavaScript's runtime takes the last. ESLint takes the
// last too. Measured on the release binary: the first input is silent, the second reports.
func TestPreserveCaughtErrorReadsTheFirstCauseKey(t *testing.T) {
	t.Parallel()

	firstIsCorrect := `try { a(); } catch (err) { throw new Error("m", { cause: err, cause: other }); }`
	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", firstIsCorrect); len(result.Diagnostics) != 0 {
		t.Errorf("upstream reads the first cause key and stops, so this is silent, got %d findings",
			len(result.Diagnostics))
	}

	lastIsCorrect := `try { a(); } catch (err) { throw new Error("m", { cause: other, cause: err }); }`
	rule_testing.ExpectFindings(t, rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", lastIsCorrect),
		"preserveCaughtError")
}

// TestPreserveCaughtErrorDoesNotReadAStringLiteralCauseKey pins the key-shape narrowness.
//
// Upstream matches `PropertyKey::StaticIdentifier` and nothing else, so a quoted key is not a cause
// at all. ESLint's `getStaticPropertyName` resolves it and stays clean on this input; oxlint
// reports it. This also pins the declined fix: upstream's `--fix` writes
// `{ "cause": err, cause: err }`, a duplicate key, which this port refuses to write.
func TestPreserveCaughtErrorDoesNotReadAStringLiteralCauseKey(t *testing.T) {
	t.Parallel()

	source := `try { a(); } catch (err) { throw new Error("m", { "cause": err }); }`
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")

	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Errorf("this shape reports without a repair, because upstream's repair writes a duplicate `cause` key")
		}
	}
}

// TestPreserveCaughtErrorWithholdsAFixThatWouldDuplicateTheKey covers the second declined shape.
//
// Upstream turns `{ cause: other, cause: err }` into `{ cause: err, cause: err }`. Measured by
// running `oxlint --fix` and reading the file. A fix is applied unattended, so this port reports
// and proposes nothing rather than writing a duplicate key into a real tree.
func TestPreserveCaughtErrorWithholdsAFixThatWouldDuplicateTheKey(t *testing.T) {
	t.Parallel()

	source := `try { a(); } catch (err) { throw new Error("m", { cause: other, cause: err }); }`
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Errorf("a repair here would leave two `cause` keys in one object literal")
		}
	}
}

// TestPreserveCaughtErrorReportsWithoutAFixWhereUpstreamDoes covers the four shapes upstream itself
// reports and repairs nothing on, so that a later reader does not mistake them for the two shapes
// declined above on purpose.
//
// All four were measured: they report under `oxlint`, and `oxlint --fix` leaves the file byte
// identical.
func TestPreserveCaughtErrorReportsWithoutAFixWhereUpstreamDoes(t *testing.T) {
	t.Parallel()

	sources := []string{
		`try { a(); } catch (err) { const o = {}; throw new Error("m", o); }`,
		`try { a(); } catch (err) { throw new Error("m", 5); }`,
		`try { a(); } catch (err) { throw new Error("m", {}, 3); }`,
		`try { a(); } catch (err) { throw new AggregateError([err], "m", "x"); }`,
	}
	for index, source := range sources {
		result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
		rule_testing.ExpectFindings(t, result, "preserveCaughtError")
		for _, diagnostic := range result.Diagnostics {
			if len(diagnostic.Fixes) != 0 {
				t.Errorf("case %d proposes a repair upstream does not:\n%s", index, source)
			}
		}
	}
}

// TestPreserveCaughtErrorOptionDefaultsToOff pins the default, which is the line most likely to
// have no upstream counterpart and the one a fourth option that bound and did nothing was shipped
// over this session.
//
// Routed through the rule's own registered decoder rather than by handing the struct in directly,
// because the decoder is what turns a config into options at runtime and a struct built here would
// leave both the JSON key and the default untested.
func TestPreserveCaughtErrorOptionDefaultsToOff(t *testing.T) {
	t.Parallel()

	bareCatch := `try { a(); } catch { throw new Error("m"); }`

	decode := rule.DecodeOptionsInto[PreserveCaughtErrorOptions]()

	// A rule configured as bare "error" is handed nil, which is what our own CohereSettings.json
	// does for this rule. That path has to reach the same verdict as an explicit false.
	if result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", bareCatch, nil); len(result.Diagnostics) != 0 {
		t.Errorf("nil options must behave as the documented default, got %d findings", len(result.Diagnostics))
	}

	explicitFalse, err := decode([]byte(`{"requireCatchParameter": false}`))
	if err != nil {
		t.Fatalf("decoding an explicit false failed: %v", err)
	}
	if decoded, ok := explicitFalse.(PreserveCaughtErrorOptions); !ok || decoded.RequireCatchParameter {
		t.Errorf("an explicit false must decode to false, got %#v", explicitFalse)
	}
	if result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", bareCatch, explicitFalse); len(result.Diagnostics) != 0 {
		t.Errorf("an explicit false must be silent, got %d findings", len(result.Diagnostics))
	}

	explicitTrue, err := decode([]byte(`{"requireCatchParameter": true}`))
	if err != nil {
		t.Fatalf("decoding an explicit true failed: %v", err)
	}
	if decoded, ok := explicitTrue.(PreserveCaughtErrorOptions); !ok || !decoded.RequireCatchParameter {
		t.Fatalf("an explicit true must decode to true, got %#v", explicitTrue)
	}
	result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", bareCatch, explicitTrue)
	rule_testing.ExpectFindings(t, result, "missingCatchParameter")
}

// TestPreserveCaughtErrorOptionSuppressesTheThrowFinding pins the shape of upstream's `else if`,
// which is the part of this option most likely to be ported as an additional finding.
//
// A parameterless catch under the option reports ONE finding, on the clause, and its body is never
// walked. So the throw inside it, which would otherwise be examined, contributes nothing. The count
// is the assertion: two findings here would mean the body was walked as well.
func TestPreserveCaughtErrorOptionSuppressesTheThrowFinding(t *testing.T) {
	t.Parallel()

	source := `try { a(); } catch { throw new Error("m"); throw new TypeError("n"); }`
	options := PreserveCaughtErrorOptions{RequireCatchParameter: true}
	result := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", source, options)
	rule_testing.ExpectFindings(t, result, "missingCatchParameter")
}

// TestPreserveCaughtErrorPointsAtTheWholeThrowStatement asserts WHERE the finding lands, which no
// message-id fixture can see and which the snapshot settles unambiguously.
//
// The span in oxc's snapshot covers the throw statement including its semicolon. The other message
// points at the catch clause instead, and asserting both is what keeps the two distinguishable.
func TestPreserveCaughtErrorPointsAtTheWholeThrowStatement(t *testing.T) {
	t.Parallel()

	source := `try { a(); } catch (err) { throw new Error("m"); }`
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	reported := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
	if reported != `throw new Error("m");` {
		t.Errorf("the finding points at %q, want the whole throw statement", reported)
	}

	clause := `try { a(); } catch { throw new Error("m"); }`
	clauseResult := rule_testing.RunTypedWithOptions(t, PreserveCaughtError, "input.ts", clause,
		PreserveCaughtErrorOptions{RequireCatchParameter: true})
	if len(clauseResult.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(clauseResult.Diagnostics))
	}
	reportedClause := clause[clauseResult.Diagnostics[0].Range.Pos():clauseResult.Diagnostics[0].Range.End()]
	if reportedClause != `catch { throw new Error("m"); }` {
		t.Errorf("the missing-parameter finding points at %q, want the catch clause", reportedClause)
	}
}

// TestPreserveCaughtErrorMessagesSayDifferentThings guards the two messages against each other.
//
// Asserted as literals typed here rather than against the rule's own constants, because a mutation
// rewriting a message moves both sides of that comparison together and the assertion stays green.
func TestPreserveCaughtErrorMessagesSayDifferentThings(t *testing.T) {
	t.Parallel()

	if messagePreserveCaughtError.Id != "preserveCaughtError" {
		t.Errorf("the throw message id is %q", messagePreserveCaughtError.Id)
	}
	if messageMissingCatchParameter.Id != "missingCatchParameter" {
		t.Errorf("the clause message id is %q", messageMissingCatchParameter.Id)
	}
	if messagePreserveCaughtError.Description == messageMissingCatchParameter.Description {
		t.Errorf("the two messages must not share a description")
	}
	if !strings.Contains(messagePreserveCaughtError.Description, "cause") {
		t.Errorf("the throw message must name the `cause` option")
	}
	if !strings.Contains(messageMissingCatchParameter.Description, "parameter") {
		t.Errorf("the clause message must name the missing parameter")
	}
}

// TestPreserveCaughtErrorRequiresTheTypedHarness fails loudly if somebody reverts the checker
// declaration, which would otherwise buy a vacuous green rather than an obvious crash.
//
// Measured on this substrate rather than assumed: `GetSymbolAtLocation` on a nil checker returns
// nil rather than panicking, so an untyped run of this rule goes completely SILENT. Every clean
// fixture would then pass for the wrong reason.
func TestPreserveCaughtErrorRequiresTheTypedHarness(t *testing.T) {
	t.Parallel()

	if !PreserveCaughtError.NeedsTypeChecker {
		t.Fatalf("this rule resolves symbols and must declare NeedsTypeChecker")
	}
	source := `try { a(); } catch (err) { throw new Error("m"); }`
	if result := rule_testing.Run(t, PreserveCaughtError, "input.ts", source); len(result.Diagnostics) != 0 {
		t.Errorf("the untyped harness hands the rule a nil checker, and it must decline rather than guess")
	}

	// The nil-checker guard covers BOTH messages, and this is the input that says so. The
	// missing-parameter finding needs no checker to reach, so a guard placed after the parameter
	// test would still emit it while every throw finding in the same file went silent. A mutation
	// sweep found this: neutering the guard changed nothing on any other input, because every other
	// path already declines once `GetSymbolAtLocation` starts answering nil, and the bare catch
	// under the option is the one place the rule can still speak without types.
	//
	// Uniform silence is the right answer. A file where the program failed to build would otherwise
	// report only its parameterless catch clauses and none of its uncaused throws, which reads as a
	// clean file with one odd complaint rather than as a rule that could not run.
	bareCatch := `try { a(); } catch { throw new Error("m"); }`
	options := PreserveCaughtErrorOptions{RequireCatchParameter: true}
	if result := rule_testing.RunWithOptions(t, PreserveCaughtError, "input.ts", bareCatch, options); len(result.Diagnostics) != 0 {
		t.Errorf("the missing-parameter finding must also be withheld on a nil checker, got %d findings",
			len(result.Diagnostics))
	}
	if result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source); len(result.Diagnostics) != 1 {
		t.Errorf("the typed harness must reach one finding, got %d", len(result.Diagnostics))
	}
}

// TestPreserveCaughtErrorHandlesADestructuredParameter pins the shape ESLint gives its own message
// to and oxc does not.
//
// ESLint reports `partiallyLostError` on the CLAUSE. oxc has no such message: it walks the body,
// every throw is checked, `is_catch_parameter` answers false for everything because the binding is
// not an identifier, and the ordinary message lands on the throw. Two of upstream's failing cases
// are this shape. What is added here is the fix half, which the corpus does not assert: there is no
// name to write, so the finding carries no repair.
func TestPreserveCaughtErrorHandlesADestructuredParameter(t *testing.T) {
	t.Parallel()

	source := "try { a(); } catch ({ message }) { throw new Error(message); }"
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Errorf("there is no single name to write, so no repair can be proposed")
		}
	}
}

// TestPreserveCaughtErrorHandlesACalleeWithNoArgumentList covers a shape upstream's corpus never
// writes and which our own code has an explicit early return for.
//
// `throw new Error` with no parentheses at all is valid JavaScript. Upstream's fixer looks for an
// opening parenthesis and returns `noop` when it finds none, so the finding lands with no repair.
func TestPreserveCaughtErrorHandlesACalleeWithNoArgumentList(t *testing.T) {
	t.Parallel()

	source := "try { a(); } catch (err) { throw new Error; }"
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", source)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			t.Errorf("there is no argument list to insert into, so no repair can be proposed")
		}
	}
}

// TestPreserveCaughtErrorScanPastsACommentedParenthesis covers both comment shapes the argument-list
// scan has to step over.
//
// Upstream's corpus carries the BLOCK form (`new Error/* ( */()`) and not the line form. The line
// form was found by a mutation sweep: disabling the line-comment arm survived every fixture, and
// the input that separates them was then measured on the release binary before this was written.
// Both report, and both repair to the same shape, with the insertion landing inside the real
// parentheses rather than inside the comment.
func TestPreserveCaughtErrorScanPastsACommentedParenthesis(t *testing.T) {
	t.Parallel()

	lineComment := "try { a(); } catch (err) {\n  throw new Error // (\n  ();\n}"
	result := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", lineComment)
	rule_testing.ExpectFindings(t, result, "preserveCaughtError")
	rule_testing.ExpectFixedSource(t, result,
		"try { a(); } catch (err) {\n  throw new Error // (\n  (\"\", { cause: err });\n}\n")

	// A line comment with no parenthesis in it exercises the same arm and is the case that says the
	// skip is about reaching the real parenthesis rather than about the character inside.
	emptyLineComment := "try { a(); } catch (err) {\n  throw new Error //\n  ();\n}"
	emptyResult := rule_testing.RunTyped(t, PreserveCaughtError, "input.ts", emptyLineComment)
	rule_testing.ExpectFindings(t, emptyResult, "preserveCaughtError")
	rule_testing.ExpectFixedSource(t, emptyResult,
		"try { a(); } catch (err) {\n  throw new Error //\n  (\"\", { cause: err });\n}\n")
}
