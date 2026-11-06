package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// The corpus is ESLint's own, extracted mechanically rather than retyped.
//
// `/tmp/lint-sources-fresh/eslint/tests/lib/rules/no-restricted-imports.js` was loaded with its
// RuleTester stubbed, then every case was replayed against the installed ESLint 10.8.1 rule through
// the Linter API under `sourceType: module` with the typescript-eslint parser and a `.ts` filename,
// which is the only configuration cohere has. The expectations below are what the installed rule
// answered, not what the corpus file annotates. They agree with upstream's own annotations exactly:
// 111 cases upstream calls valid reported nothing, 152 it calls invalid reported, and zero disagreed
// in either direction.
//
// One row below then departs from that measurement, and it is marked at the line. Its source carries
// an `// eslint-disable-line` comment, so what the oracle recorded was ESLint's suppression layer
// removing a finding the rule had already produced. cohere suppresses outside the rule, so the rule's
// own verdict is what belongs in a rule fixture. Verified by re-running the same source without the
// comment, which reports. That makes 153 reporting rows here against the oracle's 152.
//
// The clone (10.10.0) and the installed build (10.8.1) hold byte-identical copies of this rule,
// checked with diff, so there is no version drift to reason about.
//
// Upstream's options are a positional array with four legal shapes; ours is one struct, and the
// decoder is where the four collapse. The rows below are written in the collapsed shape, so a row
// that came from `["error", "foo"]` reads as one path entry named `foo`.
func noRestrictedImportsCases() []noRestrictedImportsCase {
	return []noRestrictedImportsCase{
		{"import os from \"os\";", NoRestrictedImportsOptions{}, []string{}},
		{
			"import os from \"os\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "osx"}}},
			[]string{},
		},
		{
			"import fs from \"fs\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "crypto"}}},
			[]string{},
		},
		{"import path from \"path\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "crypto"},
			NoRestrictedImportsPath{Name: "stream"},
			NoRestrictedImportsPath{Name: "os"},
		}}, []string{}},
		{"import async from \"async\";", NoRestrictedImportsOptions{}, []string{}},
		{
			"import \"foo\"",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "crypto"}}},
			[]string{},
		},
		{
			"import \"foo/bar\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo"}}},
			[]string{},
		},
		{"import withPaths from \"foo/bar\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo"},
			NoRestrictedImportsPath{Name: "bar"},
		}}, []string{}},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo/c*"}}},
		}, []string{}},
		{
			"import foo from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../foo"}}},
			[]string{},
		},
		{
			"import foo from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../foo"}}},
			[]string{},
		},
		{"import foo from 'foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"../foo"}}},
		}, []string{}},
		{
			"import foo from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/foo"}}},
			[]string{},
		},
		{
			"import foo from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/foo"}}},
			[]string{},
		},
		{"import relative from '../foo';", NoRestrictedImportsOptions{}, []string{}},
		{
			"import relative from '../foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../notFoo"}}},
			[]string{},
		},
		{
			"import relativeWithPaths from '../foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../notFoo"}}},
			[]string{},
		},
		{"import relativeWithPatterns from '../foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"notFoo"}}},
		}, []string{}},
		{"import absolute from '/foo';", NoRestrictedImportsOptions{}, []string{}},
		{
			"import absolute from '/foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/notFoo"}}},
			[]string{},
		},
		{
			"import absoluteWithPaths from '/foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/notFoo"}}},
			[]string{},
		},
		{"import absoluteWithPatterns from '/foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"notFoo"}}},
		}, []string{}},
		{"import withPatternsAndPaths from \"foo/bar\";", NoRestrictedImportsOptions{
			Paths:    []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo"}},
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo/c*"}}},
		}, []string{}},
		{"import withGitignores from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo/*", "!foo/bar"}}},
		}, []string{}},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"foo/*", "!foo/bar"},
				Message: "foo is forbidden, use bar instead",
			},
		}}, []string{}},
		{
			"import withPatternsCaseSensitive from 'foo';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:         []string{"FOO"},
					Message:       "foo is forbidden, use bar instead",
					CaseSensitive: true,
				},
			}},
			[]string{},
		},
		{"import AllowedObject from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
		}}, []string{}},
		{"import DisallowedObject from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
		}}, []string{}},
		{
			"import * as DisallowedObject from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "bar",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{
			"import { AllowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{
			"import { 'AllowedObject' as bar } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{"import { ' ' as bar } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", ImportNames: []string{""}}},
		}, []string{}},
		{"import { '' as bar } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", ImportNames: []string{" "}}},
		}, []string{}},
		{
			"import { DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "bar",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{
			"import { AllowedObject as DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{
			"import { 'AllowedObject' as DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{},
		},
		{
			"import { AllowedObject, AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{
			"import { AllowedObject, AllowedObjectTwo  as DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{},
		},
		{
			"import AllowedObjectThree, { AllowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{},
		},
		{
			"import AllowedObject, { AllowedObjectTwo as DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{},
		},
		{
			"import AllowedObject, { AllowedObjectTwo as DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' and 'DisallowedObjectTwo' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject", "DisallowedObjectTwo"},
			}}},
			[]string{},
		},
		{
			"import AllowedObject, * as DisallowedObject from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "bar",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{},
		},
		{"import \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
			Name:        "foo",
			Message:     "Please import 'DisallowedObject' and 'DisallowedObjectTwo' from /bar/ instead.",
			ImportNames: []string{"DisallowedObject", "DisallowedObjectTwo"},
		}}}, []string{}},
		// The one row in this corpus whose expectation is not what the oracle answered, and the
		// reason is the oracle rather than the rule. Its source carries an `// eslint-disable-line`
		// comment, so ESLint's directive layer removed the finding after the rule produced it.
		// Measured both ways through the installed build: with the comment the Linter answers `[]`,
		// and with the same source minus the comment it answers `["importName"]`. cohere applies
		// suppression outside the rule, so the rule's own verdict is the one asserted here.
		{
			"import {\nAllowedObject,\nDisallowedObject, // eslint-disable-line\n} from \"foo\";",
			NoRestrictedImportsOptions{
				Paths: []NoRestrictedImportsPath{
					NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
				},
			},
			[]string{"importName"},
		},
		{
			"export * from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "bar"}}},
			[]string{},
		},
		{"export * from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "bar", ImportNames: []string{"DisallowedObject"}},
		}}, []string{}},
		{"export { 'AllowedObject' } from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
		}}, []string{}},
		{"export { 'AllowedObject' as DisallowedObject } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
			},
		}, []string{}},
		{
			"import { Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNames: []string{"Foo"}},
			}},
			[]string{},
		},
		{
			"import Foo from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNames: []string{"Foo"}},
			}},
			[]string{},
		},
		{"import Foo from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{}},
		{"import Foo from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNames: []string{"Foo"}, ImportNamePattern: "^Foo"},
		}}, []string{}},
		{
			"import Foo from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{},
		},
		{
			"import { Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{},
		},
		{
			"import { Bar as Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{},
		},
		{
			"import { Bar as Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo"},
					ImportNamePattern: "^Foo",
				},
			}},
			[]string{},
		},
		{
			"import Foo, { Baz as Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^(Foo|Bar)"},
			}},
			[]string{},
		},
		{
			"import Foo, { Baz as Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo"},
					ImportNamePattern: "^Bar",
				},
			}},
			[]string{},
		},
		{"export { Bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{}},
		{"export { Bar as Foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{}},
		{
			"import { AllowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "foo",
				Message:          "Please import anything except 'AllowedObject' from /bar/ instead.",
				AllowImportNames: []string{"AllowedObject"},
			}}},
			[]string{},
		},
		{"import { foo } from 'foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"foo"}}},
		}, []string{}},
		{"import { foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, AllowImportNames: []string{"foo"}},
		}}, []string{}},
		{"export { bar } from 'foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"bar"}}},
		}, []string{}},
		{"export { bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, AllowImportNames: []string{"bar"}},
		}}, []string{}},
		{"import { Foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, AllowImportNamePattern: "^Foo"},
		}}, []string{}},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Regex: "foo/(?!bar)", Message: "foo is forbidden, use bar instead"},
		}}, []string{}},
		{
			"import withPatternsCaseSensitive from 'foo';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Regex:         "FOO",
					Message:       "foo is forbidden, use bar instead",
					CaseSensitive: true,
				},
			}},
			[]string{},
		},
		{
			"import Foo from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Regex: "my/relative-module", ImportNamePattern: "^Foo"},
			}},
			[]string{},
		},
		{
			"import { Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Regex: "my/relative-module", ImportNamePattern: "^Foo"},
			}},
			[]string{},
		},
		{
			"import \"fs\"",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "fs"}}},
			[]string{"path"},
		},
		{"import os from \"os \";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs"},
			NoRestrictedImportsPath{Name: "crypto "},
			NoRestrictedImportsPath{Name: "stream"},
			NoRestrictedImportsPath{Name: "os"},
		}}, []string{"path"}},
		{
			"import \"foo/bar\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo/bar"}}},
			[]string{"path"},
		},
		{
			"import withPaths from \"foo/bar\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo/bar"}}},
			[]string{"path"},
		},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo"}}},
		}, []string{"patterns"}},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"bar"}}},
		}, []string{"patterns"}},
		{"import withPatterns from \"foo/baz\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"foo/*", "!foo/bar"},
				Message: "foo is forbidden, use foo/bar instead",
			},
		}}, []string{"patternWithCustomMessage"}},
		{"import withPatterns from \"foo/baz\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"foo/bar", "foo/baz"},
				Message: "some foo subimports are restricted",
			},
		}}, []string{"patternWithCustomMessage"}},
		{"import withPatterns from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo/bar"}}},
		}, []string{"patterns"}},
		{"import withPatternsCaseInsensitive from 'foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"FOO"}}},
		}, []string{"patterns"}},
		{"import withGitignores from \"foo/bar\";", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo/*", "!foo/baz"}}},
		}, []string{"patterns"}},
		{
			"export * from \"fs\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "fs"}}},
			[]string{"path"},
		},
		{
			"export * as ns from \"fs\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "fs"}}},
			[]string{"path"},
		},
		{
			"export {a} from \"fs\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "fs"}}},
			[]string{"path"},
		},
		{"export {foo as b} from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import 'foo'.", ImportNames: []string{"foo"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export {'foo' as b} from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import 'foo'.", ImportNames: []string{"foo"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export {'foo'} from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import 'foo'.", ImportNames: []string{"foo"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export {'👍'} from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import '👍'.", ImportNames: []string{"👍"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export {''} from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import ''.", ImportNames: []string{""}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export * as ns from \"fs\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "fs", Message: "Don't import 'foo'.", ImportNames: []string{"foo"}},
		}}, []string{"everythingWithCustomMessage"}},
		{"import withGitignores from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", Message: "Please import from 'bar' instead."},
		}}, []string{"pathWithCustomMessage"}},
		{"import withGitignores from \"bar\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo"},
			NoRestrictedImportsPath{Name: "bar", Message: "Please import from 'baz' instead."},
			NoRestrictedImportsPath{Name: "baz"},
		}}, []string{"pathWithCustomMessage"}},
		{"import withGitignores from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", Message: "Please import from 'bar' instead."},
		}}, []string{"pathWithCustomMessage"}},
		{
			"import DisallowedObject from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import the default import of 'foo' from /bar/ instead.",
				ImportNames: []string{"default"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import * as All from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{"everythingWithCustomMessage"},
		},
		{"export * from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
			Name:        "foo",
			Message:     "Please import 'DisallowedObject' from /bar/ instead.",
			ImportNames: []string{"DisallowedObject"},
		}}}, []string{"everythingWithCustomMessage"}},
		{"export * from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject1", "DisallowedObject2"}},
		}}, []string{"everything"}},
		{
			"import { DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import { DisallowedObject as AllowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import { 'DisallowedObject' as AllowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{"importNameWithCustomMessage"},
		},
		{"import { '👍' as bar } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"👍"}}},
		}, []string{"importName"}},
		{"import { '' as bar } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", ImportNames: []string{""}}},
		}, []string{"importName"}},
		{
			"import { AllowedObject, DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import { AllowedObject, DisallowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import { AllowedObject, DisallowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' and 'DisallowedObjectTwo' from /bar/ instead.",
				ImportNames: []string{"DisallowedObjectTwo", "DisallowedObject"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import { AllowedObject, DisallowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' and 'DisallowedObjectTwo' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject", "DisallowedObjectTwo"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import DisallowedObject, { AllowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import the default import of 'foo' from /bar/ instead.",
					ImportNames: []string{"default"},
				},
			}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import AllowedObject, { DisallowedObject as AllowedObjectTwo } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import 'DisallowedObject' from /bar/ instead.",
					ImportNames: []string{"DisallowedObject"},
				},
			}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import AllowedObject, * as AllowedObjectTwo from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject"},
			}}},
			[]string{"everythingWithCustomMessage"},
		},
		{
			"import AllowedObject, * as AllowedObjectTwo from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please import 'DisallowedObject' and 'DisallowedObjectTwo' from /bar/ instead.",
				ImportNames: []string{"DisallowedObject", "DisallowedObjectTwo"},
			}}},
			[]string{"everythingWithCustomMessage"},
		},
		{"import { DisallowedObjectOne, DisallowedObjectTwo, AllowedObject } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				ImportNames: []string{"DisallowedObjectOne", "DisallowedObjectTwo"},
			}},
		}, []string{"importName", "importName"}},
		{
			"import { DisallowedObjectOne, DisallowedObjectTwo, AllowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "foo",
					Message:     "Please import this module from /bar/ instead.",
					ImportNames: []string{"DisallowedObjectOne", "DisallowedObjectTwo"},
				},
			}},
			[]string{"importNameWithCustomMessage", "importNameWithCustomMessage"},
		},
		{"import { AllowedObject, DisallowedObject as Bar } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"DisallowedObject"}},
			},
		}, []string{"importName"}},
		{"import foo, { bar } from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"bar"}}},
		}, []string{"importName"}},
		{
			"import { Image, Text, ScrollView } from 'react-native'",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import Text from ui/_components instead",
					ImportNames: []string{"Text"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import TextInput from ui/_components instead",
					ImportNames: []string{"TextInput"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import View from ui/_components instead ",
					ImportNames: []string{"View"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import ScrollView from ui/_components instead",
					ImportNames: []string{"ScrollView"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import KeyboardAvoidingView from ui/_components instead",
					ImportNames: []string{"KeyboardAvoidingView"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import ImageBackground from ui/_components instead",
					ImportNames: []string{"ImageBackground"},
				},
				NoRestrictedImportsPath{
					Name:        "react-native",
					Message:     "import Image from ui/_components instead",
					ImportNames: []string{"Image"},
				},
			}},
			[]string{"importNameWithCustomMessage", "importNameWithCustomMessage", "importNameWithCustomMessage"},
		},
		{"import { foo, bar, baz } from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "mod", Message: "Import foo from qux instead.", ImportNames: []string{"foo"}},
			NoRestrictedImportsPath{Name: "mod", Message: "Import baz from qux instead.", ImportNames: []string{"baz"}},
		}}, []string{"importNameWithCustomMessage", "importNameWithCustomMessage"}},
		{"import { foo, bar, baz, qux } from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:        "mod",
				Message:     "Use `barbaz` instead of `bar`.",
				ImportNames: []string{"bar"},
			},
			NoRestrictedImportsPath{
				Name:        "mod",
				Message:     "Don't use 'foo' and `qux` from 'mod'.",
				ImportNames: []string{"foo", "qux"},
			},
		}}, []string{"importNameWithCustomMessage", "importNameWithCustomMessage", "importNameWithCustomMessage"}},
		{"import { foo, bar, baz, qux } from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:        "mod",
				Message:     "Don't use 'foo' or 'baz' from 'mod'.",
				ImportNames: []string{"foo", "baz"},
			},
			NoRestrictedImportsPath{
				Name:        "mod",
				Message:     "Don't use 'a' or 'c' from 'mod'.",
				ImportNames: []string{"a", "c"},
			},
			NoRestrictedImportsPath{
				Name:        "mod",
				Message:     "Use 'b' or `bar` from 'quux/mod' instead.",
				ImportNames: []string{"b", "bar"},
			},
		}}, []string{"importNameWithCustomMessage", "importNameWithCustomMessage", "importNameWithCustomMessage"}},
		{"import * as mod from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "mod", Message: "Import foo from qux instead.", ImportNames: []string{"foo"}},
			NoRestrictedImportsPath{Name: "mod", Message: "Import bar from qux instead.", ImportNames: []string{"bar"}},
		}}, []string{"everythingWithCustomMessage", "everythingWithCustomMessage"}},
		{"import { foo } from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "mod"},
			NoRestrictedImportsPath{Name: "mod", Message: "Import bar from qux instead.", ImportNames: []string{"bar"}},
		}}, []string{"path"}},
		{"import { bar } from 'mod'", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "mod"},
			NoRestrictedImportsPath{Name: "mod", Message: "Import bar from qux instead.", ImportNames: []string{"bar"}},
		}}, []string{"path", "importNameWithCustomMessage"}},
		{"import foo, { bar } from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"default"}}},
		}, []string{"importName"}},
		{"import foo, * as bar from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"default"}}},
		}, []string{"importName", "everything"}},
		{
			"import * as bar from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo"}}},
			[]string{"path"},
		},
		{"import { a, a as b } from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"a"}}},
		}, []string{"importName", "importName"}},
		{"export { x as y, x as z } from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"x"}}},
		}, []string{"importName", "importName"}},
		{"import foo, { default as bar } from 'mod';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", ImportNames: []string{"default"}}},
		}, []string{"importName", "importName"}},
		{
			"import relative from '../foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../foo"}}},
			[]string{"path"},
		},
		{
			"import relativeWithPaths from '../foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "../foo"}}},
			[]string{"path"},
		},
		{"import relativeWithPatterns from '../foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"../foo"}}},
		}, []string{"patterns"}},
		{
			"import absolute from '/foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/foo"}}},
			[]string{"path"},
		},
		{
			"import absoluteWithPaths from '/foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "/foo"}}},
			[]string{"path"},
		},
		{"import absoluteWithPatterns from '/foo';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"foo"}}},
		}, []string{"patterns"}},
		{"import absoluteWithPatterns from '#foo/bar';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"\\#foo"}}},
		}, []string{"patterns"}},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNames: []string{"Foo"}},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo, Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:       []string{"**/my/relative-module"},
				Message:     "Import from @/utils instead.",
				ImportNames: []string{"Foo", "Bar"},
			}}},
			[]string{"patternAndImportNameWithCustomMessage", "patternAndImportNameWithCustomMessage"},
		},
		{
			"import * as All from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNames: []string{"Foo"}},
			}},
			[]string{"patternAndEverything"},
		},
		{
			"import * as AllWithCustomMessage from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:       []string{"**/my/relative-module"},
					Message:     "Import from @/utils instead.",
					ImportNames: []string{"Foo"},
				},
			}},
			[]string{"patternAndEverythingWithCustomMessage"},
		},
		{"import * as All from 'foo-bar-baz';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"**"}, ImportNames: []string{"Foo", "Bar"}},
		}}, []string{"patternAndEverything"}},
		{
			"import * as AllWithCustomMessage from 'foo-bar-baz';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:       []string{"**/*-*-baz"},
					Message:     "Use only 'Baz'.",
					ImportNames: []string{"Foo", "Bar"},
				},
			}},
			[]string{"patternAndEverythingWithCustomMessage"},
		},
		{"import def, * as ns from 'mod';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"mod"}, ImportNames: []string{"default"}},
		}}, []string{"patternAndImportName", "patternAndEverything"}},
		{"import Foo from 'mod';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"mod"}, ImportNames: []string{"default"}},
		}}, []string{"patternAndImportName"}},
		{"import { Foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndImportName"}},
		{"import { Foo as Bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndImportName"}},
		{"import Foo, { Bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^(Foo|Bar)"},
		}}, []string{"patternAndImportName"}},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { FooBar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import Foo, { Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo|^Bar"},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo, Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^(Foo|Bar)"},
			}},
			[]string{"patternAndImportName", "patternAndImportName"},
		},
		{"import * as Foo from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndEverythingWithRegexImportName"}},
		{
			"import * as All from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"**/my/relative-module"}, ImportNamePattern: "^Foo"},
			}},
			[]string{"patternAndEverythingWithRegexImportName"},
		},
		{
			"import * as AllWithCustomMessage from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					Message:           "Import from @/utils instead.",
					ImportNamePattern: "^Foo",
				},
			}},
			[]string{"patternAndEverythingWithRegexImportNameAndCustomMessage"},
		},
		{
			"import * as AllWithCustomMessage from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:             []string{"**/my/relative-module"},
				Message:           "Import from @/utils instead.",
				ImportNames:       []string{"Foo"},
				ImportNamePattern: "^Foo",
			}}},
			[]string{"patternAndEverythingWithCustomMessage"},
		},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo"},
					ImportNamePattern: "^Foo",
				},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo", "Bar"},
					ImportNamePattern: "^Foo",
				},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Bar"},
					ImportNamePattern: "^Foo",
				},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo"},
					ImportNamePattern: "^Bar",
				},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import { Foo, Bar } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:             []string{"**/my/relative-module"},
					ImportNames:       []string{"Foo"},
					ImportNamePattern: "^Bar",
				},
			}},
			[]string{"patternAndImportName", "patternAndImportName"},
		},
		{"export { Foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndImportName"}},
		{"export { Foo as Bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndImportName"}},
		{"export { Foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNames: []string{"Bar"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndImportName"}},
		{"export * from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, ImportNamePattern: "^Foo"},
		}}, []string{"patternAndEverythingWithRegexImportName"}},
		{"export { Bar } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo"}, AllowImportNamePattern: "^Foo"},
		}}, []string{"allowedImportNamePattern"}},
		{
			"export { Bar } from 'foo';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:                  []string{"foo"},
				Message:                "Only imports that match the pattern '/^Foo/u' are allowed to be imported from 'foo'.",
				AllowImportNamePattern: "^Foo",
			}}},
			[]string{"allowedImportNamePatternWithCustomMessage"},
		},
		{"import { AllowedObject, DisallowedObject } from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"AllowedObject"}},
			},
		}, []string{"allowedImportName"}},
		{
			"import { AllowedObject, DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "foo",
				Message:          "Only 'AllowedObject' is allowed to be imported from 'foo'.",
				AllowImportNames: []string{"AllowedObject"},
			}}},
			[]string{"allowedImportNameWithCustomMessage"},
		},
		{"import { GoodThing, BadThing } from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"GoodThing", "AlsoGood"}},
		}}, []string{"allowedImportName"}},
		{
			"import { GoodThing, SomethingBad } from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{
					Name:             "foo",
					Message:          "Only good stuff allowed.",
					AllowImportNames: []string{"GoodThing", "AlsoGood"},
				},
			}},
			[]string{"allowedImportNameWithCustomMessage"},
		},
		{
			"import { AllowedObject, DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"foo"}, AllowImportNames: []string{"AllowedObject"}},
			}},
			[]string{"allowedImportName"},
		},
		{
			"import { AllowedObject, DisallowedObject } from \"foo\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"foo"},
				Message:          "Only 'AllowedObject' is allowed to be imported from 'foo'.",
				AllowImportNames: []string{"AllowedObject"},
			}}},
			[]string{"allowedImportNameWithCustomMessage"},
		},
		{
			"import { foo, bar, baz, qux } from \"foo-bar-baz\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:            []string{"foo-bar-baz"},
					AllowImportNames: []string{"foo", "bar", "baz"},
				},
			}},
			[]string{"allowedImportName"},
		},
		{
			"import { Allowed1, Disallowed, Allowed2 } from \"foo\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:            []string{"foo"},
					Message:          "Fix this please.",
					AllowImportNames: []string{"Allowed1", "Allowed2"},
				},
			}},
			[]string{"allowedImportNameWithCustomMessage"},
		},
		{"import * as AllowedObject from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"AllowedObject"}},
		}}, []string{"everythingWithAllowImportNames"}},
		{
			"import * as AllowedObject from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "foo",
				Message:          "Only 'AllowedObject' is allowed to be imported from 'foo'.",
				AllowImportNames: []string{"AllowedObject"},
			}}},
			[]string{"everythingWithAllowImportNamesAndCustomMessage"},
		},
		{"import * as AllowedObject from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"AllowedObject", "AnotherObject"}},
		}}, []string{"everythingWithAllowImportNames"}},
		{
			"import * as AllowedObject from \"foo\";",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "foo",
				Message:          "Nothing else.",
				AllowImportNames: []string{"AllowedObject", "AnotherObject"},
			}}},
			[]string{"everythingWithAllowImportNamesAndCustomMessage"},
		},
		{
			"import * as AllowedObject from \"foo/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"foo/*"}, AllowImportNames: []string{"AllowedObject"}},
			}},
			[]string{"everythingWithAllowImportNames"},
		},
		{
			"import * as AllowedObject from \"foo/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"foo/*"},
				Message:          "Only 'AllowedObject' is allowed to be imported from 'foo'.",
				AllowImportNames: []string{"AllowedObject"},
			}}},
			[]string{"everythingWithAllowImportNamesAndCustomMessage"},
		},
		{"import * as AllFooBar from \"foo/bar\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"foo/*"}, AllowImportNames: []string{"Foo", "Bar"}},
			NoRestrictedImportsPattern{
				Group:            []string{"*/bar"},
				Message:          "Good luck!",
				AllowImportNames: []string{"Foo", "Bar"},
			},
		}}, []string{"everythingWithAllowImportNames", "everythingWithAllowImportNamesAndCustomMessage"}},
		{
			"import * as AllowedObject from \"foo/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"foo/*"}, AllowImportNamePattern: "^Allow"},
			}},
			[]string{"everythingWithAllowedImportNamePattern"},
		},
		{
			"import * as AllowedObject from \"foo/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:                  []string{"foo/*"},
				Message:                "Only import names starting with 'Allow' are allowed to be imported from 'foo'.",
				AllowImportNamePattern: "^Allow",
			}}},
			[]string{"everythingWithAllowedImportNamePatternWithCustomMessage"},
		},
		{"import withPatterns from \"foo/baz\";", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Regex: "foo/(?!bar)", Message: "foo is forbidden, use bar instead"},
		}}, []string{"patternWithCustomMessage"}},
		{
			"import withPatternsCaseSensitive from 'FOO';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Regex:         "FOO",
					Message:       "foo is forbidden, use bar instead",
					CaseSensitive: true,
				},
			}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"import { Foo } from '../../my/relative-module';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Regex: "my/relative-module", ImportNamePattern: "^Foo"},
			}},
			[]string{"patternAndImportName"},
		},
		{
			"import withPatternsCaseSensitive from 'foo';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:         []string{"FOO"},
					Message:       "foo is forbidden, use bar instead",
					CaseSensitive: false,
				},
			}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"\n        // error\n        import { Foo_Enum } from '@app/api';\n        import { Bar_Enum } from '@app/api/bar';\n        import { Baz_Enum } from '@app/api/baz';\n        import { B_Enum } from '@app/api/enums/foo';\n\n        // no error\n        import { C_Enum } from '@app/api/enums';\n        ",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Regex: "@app/(?!(api/enums$)).*", ImportNamePattern: "_Enum$"},
			}},
			[]string{"patternAndImportName", "patternAndImportName", "patternAndImportName", "patternAndImportName"},
		},
		{"import foo from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"import foo = require('foo');", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"export { foo } from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"import foo from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"export { foo } from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"import 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{}},
		{"import foo from 'foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "import1"},
				NoRestrictedImportsPath{Name: "import2"},
			},
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"import1/private/*", "import2/*", "!import2/good"}},
			},
		}, []string{}},
		{"export { foo } from 'foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "import1"},
				NoRestrictedImportsPath{Name: "import2"},
			},
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"import1/private/*", "import2/*", "!import2/good"}},
			},
		}, []string{}},
		{"import foo from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", Message: "Please use import-bar instead."},
			NoRestrictedImportsPath{Name: "import-baz", Message: "Please use import-quux instead."},
		}}, []string{}},
		{"export { foo } from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", Message: "Please use import-bar instead."},
			NoRestrictedImportsPath{Name: "import-baz", Message: "Please use import-quux instead."},
		}}, []string{}},
		{"import foo from 'foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
			Name:        "import-foo",
			Message:     "Please use Bar from /import-bar/baz/ instead.",
			ImportNames: []string{"Bar"},
		}}}, []string{}},
		{
			"export { foo } from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "import-foo",
				Message:     "Please use Bar from /import-bar/baz/ instead.",
				ImportNames: []string{"Bar"},
			}}},
			[]string{},
		},
		{"import foo from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"import1/private/*"},
				Message: "usage of import1 private modules not allowed.",
			},
			NoRestrictedImportsPattern{
				Group:   []string{"import2/*", "!import2/good"},
				Message: "import2 is deprecated, except the modules in import2/good.",
			},
		}}, []string{}},
		{"export { foo } from 'foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"import1/private/*"},
				Message: "usage of import1 private modules not allowed.",
			},
			NoRestrictedImportsPattern{
				Group:   []string{"import2/*", "!import2/good"},
				Message: "import2 is deprecated, except the modules in import2/good.",
			},
		}}, []string{}},
		{
			"import foo = require('foo');",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "foo",
				Message:     "Please use Bar from /import-bar/baz/ instead.",
				ImportNames: []string{"foo"},
			}}},
			[]string{},
		},
		{"import type foo from 'import-foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use import-bar instead.",
				AllowTypeImports: true,
			},
		}}, []string{}},
		{"import type _ = require('import-foo');", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use import-bar instead.",
				AllowTypeImports: true,
			},
		}}, []string{}},
		{
			"import type { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"export type { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import type foo from 'import1/private/bar';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import1/private/*"},
				Message:          "usage of import1 private modules not allowed.",
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"export type { foo } from 'import1/private/bar';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import1/private/*"},
				Message:          "usage of import1 private modules not allowed.",
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{"import type { MyType } from './types';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:            []string{"fail"},
				Message:          "Please do not load from 'fail'.",
				AllowTypeImports: true,
			},
		}}, []string{}},
		{
			"\n  import type { foo } from 'import1/private/bar';\n  import type { foo } from 'import2/private/bar';\n\t\t",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:            []string{"import1/private/*"},
					Message:          "usage of import1 private modules not allowed.",
					AllowTypeImports: true,
				},
				NoRestrictedImportsPattern{
					Group:            []string{"import2/private/*"},
					Message:          "usage of import2 private modules not allowed.",
					AllowTypeImports: true,
				},
			}},
			[]string{},
		},
		{
			"import { Bar, type Baz } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				ImportNames:      []string{"Baz"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import type { Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import { Baz, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"export { Baz, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import type { Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				AllowImportNames: []string{"Foo"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import { Foo, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				AllowImportNames: []string{"Foo"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"export { Baz, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import/private/*"},
				Message:          "Please use 'Baz' from 'import/private/*' as a type only.",
				AllowImportNames: []string{"Baz"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import { Foo, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:                  []string{"import/private/*"},
				Message:                "Please use 'Baz' from 'import/private/*' as a type only.",
				AllowImportNamePattern: "^Foo",
				AllowTypeImports:       true,
			}}},
			[]string{},
		},
		{
			"export { Baz, type Bar } from \"import/private/bar\";",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:                  []string{"import/private/*"},
				Message:                "Please use 'Baz' from 'import/private/*' as a type only.",
				AllowImportNamePattern: "^Baz",
				AllowTypeImports:       true,
			}}},
			[]string{},
		},
		{"export { bar, type baz } from \"import-foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", AllowImportNames: []string{"bar"}, AllowTypeImports: true},
		}}, []string{}},
		{
			"\n  import type { foo } from 'import1/private/bar';\n  import type { foo } from 'import2/private/bar';\n\t\t",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Regex:            "import1/.*",
					Message:          "usage of import1 private modules not allowed.",
					AllowTypeImports: true,
				},
				NoRestrictedImportsPattern{
					Regex:            "import2/.*",
					Message:          "usage of import2 private modules not allowed.",
					AllowTypeImports: true,
				},
			}},
			[]string{},
		},
		{
			"import { foo } from 'import1/private';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Regex:            "import1/[A-Z]+",
				Message:          "usage of import1 private modules not allowed.",
				CaseSensitive:    true,
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"import { type Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{
			"export { type Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{},
		},
		{"import Bar = Foo.Bar;", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", ImportNames: []string{"Bar"}, AllowTypeImports: true},
		}}, []string{}},
		{"export type * from \"foo\";", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", AllowTypeImports: true}},
		}, []string{}},
		{"import type fs = require(\"fs\");", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{Group: []string{"f*"}, AllowTypeImports: true},
		}}, []string{}},
		{"export { type bar, baz } from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", ImportNames: []string{"bar"}, AllowTypeImports: true},
		}}, []string{}},
		{"import foo from 'import1';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{"path"}},
		{"import foo = require('import1');", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{"path"}},
		{"export { foo } from 'import1';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{"path"}},
		{"import foo from 'import1';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{"path"}},
		{"export { foo } from 'import1';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import1"},
			NoRestrictedImportsPath{Name: "import2"},
		}}, []string{"path"}},
		{"import foo from 'import1/private/foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "import1"},
				NoRestrictedImportsPath{Name: "import2"},
			},
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"import1/private/*", "import2/*", "!import2/good"}},
			},
		}, []string{"patterns"}},
		{"export { foo } from 'import1/private/foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{
				NoRestrictedImportsPath{Name: "import1"},
				NoRestrictedImportsPath{Name: "import2"},
			},
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"import1/private/*", "import2/*", "!import2/good"}},
			},
		}, []string{"patterns"}},
		{"import foo from 'import-foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", Message: "Please use import-bar instead."},
			NoRestrictedImportsPath{Name: "import-baz", Message: "Please use import-quux instead."},
		}}, []string{"pathWithCustomMessage"}},
		{"export { foo } from 'import-foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "import-foo", Message: "Please use import-bar instead."},
			NoRestrictedImportsPath{Name: "import-baz", Message: "Please use import-quux instead."},
		}}, []string{"pathWithCustomMessage"}},
		{
			"import { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "import-foo",
				Message:     "Please use Bar from /import-bar/baz/ instead.",
				ImportNames: []string{"Bar"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"export { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:        "import-foo",
				Message:     "Please use Bar from /import-bar/baz/ instead.",
				ImportNames: []string{"Bar"},
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{"import foo from 'import1/private/foo';", NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			NoRestrictedImportsPattern{
				Group:   []string{"import1/private/*"},
				Message: "usage of import1 private modules not allowed.",
			},
			NoRestrictedImportsPattern{
				Group:   []string{"import2/*", "!import2/good"},
				Message: "import2 is deprecated, except the modules in import2/good.",
			},
		}}, []string{"patternWithCustomMessage"}},
		{
			"export { foo } from 'import1/private/foo';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Group:   []string{"import1/private/*"},
					Message: "usage of import1 private modules not allowed.",
				},
				NoRestrictedImportsPattern{
					Group:   []string{"import2/*", "!import2/good"},
					Message: "import2 is deprecated, except the modules in import2/good.",
				},
			}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"import 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "import-foo"}}},
			[]string{"path"},
		},
		{"import 'import-foo';", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "import-foo", AllowTypeImports: true}},
		}, []string{"path"}},
		{"import foo from 'import-foo';", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use import-bar instead.",
				AllowTypeImports: true,
			},
		}}, []string{"pathWithCustomMessage"}},
		{"import foo = require('import-foo');", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use import-bar instead.",
				AllowTypeImports: true,
			},
		}}, []string{"pathWithCustomMessage"}},
		{
			"import { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"export { Bar } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar"},
				AllowTypeImports: true,
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"import foo from 'import1/private/bar';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import1/private/*"},
				Message:          "usage of import1 private modules not allowed.",
				AllowTypeImports: true,
			}}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"export { foo } from 'import1/private/bar';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Group:            []string{"import1/private/*"},
				Message:          "usage of import1 private modules not allowed.",
				AllowTypeImports: true,
			}}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"export { foo } from 'import1/private/bar';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{
					Regex:            "import1/.*",
					Message:          "usage of import1 private modules not allowed.",
					AllowTypeImports: true,
				},
			}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"import { foo } from 'import1/private-package';",
			NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{
				Regex:            "import1/private-[a-z]*",
				Message:          "usage of import1 private modules not allowed.",
				CaseSensitive:    true,
				AllowTypeImports: true,
			}}},
			[]string{"patternWithCustomMessage"},
		},
		{
			"export * from 'import1';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "import1"}}},
			[]string{"path"},
		},
		{"import type { InvalidTestCase } from '@typescript-eslint/utils/dist/ts-eslint';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"@typescript-eslint/utils/dist/*"}},
			},
		}, []string{"patterns"}},
		{"export type { InvalidTestCase } from '@typescript-eslint/utils/dist/ts-eslint';", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{
				NoRestrictedImportsPattern{Group: []string{"@typescript-eslint/utils/dist/*"}},
			},
		}, []string{"patterns"}},
		{
			"import { Bar, type Baz } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar and Baz from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar", "Baz"},
				AllowTypeImports: true,
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"export { Bar, type Baz } from 'import-foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "import-foo",
				Message:          "Please use Bar and Baz from /import-bar/baz/ instead.",
				ImportNames:      []string{"Bar", "Baz"},
				AllowTypeImports: true,
			}}},
			[]string{"importNameWithCustomMessage"},
		},
		{
			"\n\t\t\t  // Both regular and type imports should still be restricted\n\t\t\t  import { Foo } from 'restricted-path';\n\t\t\t  import type { Bar } from 'restricted-path';\n\t\t\t",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "restricted-path",
				Message:          "This import is restricted.",
				AllowTypeImports: false,
			}}},
			[]string{"pathWithCustomMessage", "pathWithCustomMessage"},
		},
		{
			"\n\t\t\t  // Both regular and type imports should still be restricted\n\t\t\t  export { Foo } from 'restricted-path';\n\t\t\t  export type { Bar } from 'restricted-path';\n\t\t\t",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{
				Name:             "restricted-path",
				Message:          "This export is restricted.",
				AllowTypeImports: false,
			}}},
			[]string{"pathWithCustomMessage", "pathWithCustomMessage"},
		},
		{"import type { bar } from \"mod\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "mod",
				Message:          "import 'foo' only as type",
				ImportNames:      []string{"foo"},
				AllowTypeImports: true,
			},
			NoRestrictedImportsPath{Name: "mod", Message: "don't import 'bar' at all", ImportNames: []string{"bar"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"export type { bar } from \"mod\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{
				Name:             "mod",
				Message:          "import 'foo' only as type",
				ImportNames:      []string{"foo"},
				AllowTypeImports: true,
			},
			NoRestrictedImportsPath{Name: "mod", Message: "don't import 'bar' at all", ImportNames: []string{"bar"}},
		}}, []string{"importNameWithCustomMessage"}},
		{"import fs = require(\"fs\");", NoRestrictedImportsOptions{
			Patterns: []NoRestrictedImportsPattern{NoRestrictedImportsPattern{Group: []string{"f*"}}},
		}, []string{"patterns"}},
		{"\n\t\t\texport type * from \"foo\";\n\t\t\texport * from \"foo\";\n\t\t\t", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "foo", AllowTypeImports: true}},
		}, []string{"path"}},
		{"\n\t\t\texport { } from \"mod\";\n\t\t\texport type { } from \"mod\";\n\t\t\t", NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{NoRestrictedImportsPath{Name: "mod", AllowTypeImports: true}},
		}, []string{"path"}},
		{"import { baz } from \"foo\";", NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
			NoRestrictedImportsPath{Name: "foo", AllowImportNames: []string{"bar"}, AllowTypeImports: true},
		}}, []string{"allowedImportName"}},
	}
}

// TestNoRestrictedImportsMatchesUpstream replays the whole corpus.
func TestNoRestrictedImportsMatchesUpstream(test *testing.T) {
	test.Parallel()

	cases := noRestrictedImportsCases()

	reporting := 0
	for _, testCase := range cases {
		if len(testCase.wantIds) > 0 {
			reporting++
		}
	}
	if len(cases) != 263 {
		test.Fatalf("expected 263 corpus cases, have %d", len(cases))
	}
	if reporting != 153 {
		test.Fatalf("expected 153 reporting cases, have %d", reporting)
	}

	for _, testCase := range cases {
		result := runNoRestrictedImports(test, testCase)
		rule_testing.ExpectFindings(test, result, testCase.wantIds...)
	}
}
