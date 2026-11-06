package docs_data

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CommandLineInterface is cli.json: the flags the binary's own flag set prints, and each verb's.
type CommandLineInterface struct {
	Flags []CommandLineInterfaceFlag `json:"flags"`
	Verbs []CommandLineInterfaceVerb `json:"verbs,omitempty"`
}

// CommandLineInterfaceFlag is one flag as the flag package describes it.
type CommandLineInterfaceFlag struct {
	Name string `json:"name"`
	// Argument is the name the help gives the flag's value: a backquoted word in its usage, or else its
	// type. Empty for a boolean flag, which takes none.
	Argument string `json:"argument,omitempty"`
	Usage    string `json:"usage"`
	// Default is the flag's default, present only when it differs from its type's zero value, which is
	// when the flag package prints it.
	Default string `json:"default,omitempty"`
}

// CommandLineInterfaceVerb is a subcommand with a flag set of its own.
type CommandLineInterfaceVerb struct {
	Name string `json:"name"`
	// Usage is the verb's synopsis lines, and Summary the prose its help prints before the flags.
	Usage   []string                   `json:"usage"`
	Summary string                     `json:"summary,omitempty"`
	Flags   []CommandLineInterfaceFlag `json:"flags"`
}

// flagHeading is the first line of one flag in flag.PrintDefaults: two spaces, the name, and the
// argument's name when it takes one.
var flagHeading = regexp.MustCompile(`^  -([^\s]+)(?: (\S+))?$`)

// flagDefault is the suffix PrintDefaults appends to a non-zero default: quoted for a string flag.
var flagDefault = regexp.MustCompile(` \(default ("(?:[^"\\]|\\.)*"|[^()"]*)\)$`)

// parseCommandLineInterface reads the top-level help and each verb's.
func parseCommandLineInterface(help []byte, verbHelp map[string][]byte) (CommandLineInterface, error) {
	_, flags, parseFlagHelpError := parseFlagHelp(help)
	if parseFlagHelpError != nil {
		return CommandLineInterface{}, fmt.Errorf("docs_data: cohere -help: %w", parseFlagHelpError)
	}
	cli := CommandLineInterface{Flags: flags}
	verbs := make([]string, 0, len(verbHelp))
	for verb := range verbHelp {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)
	for _, verb := range verbs {
		preamble, flags, parseFlagHelpError := parseFlagHelp(verbHelp[verb])
		if parseFlagHelpError != nil {
			return CommandLineInterface{}, fmt.Errorf("docs_data: cohere %s -help: %w", verb, parseFlagHelpError)
		}
		entry := CommandLineInterfaceVerb{Name: verb, Flags: flags}
		var summary []string
		for _, line := range preamble {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "":
			case strings.HasPrefix(trimmed, "usage: "):
				entry.Usage = append(entry.Usage, strings.TrimPrefix(trimmed, "usage: "))
			case len(entry.Usage) > 0 && len(summary) == 0 && strings.HasPrefix(line, "       "):
				entry.Usage = append(entry.Usage, trimmed)
			default:
				summary = append(summary, trimmed)
			}
		}
		if len(entry.Usage) == 0 {
			return CommandLineInterface{}, fmt.Errorf("docs_data: cohere %s -help printed no usage line", verb)
		}
		entry.Summary = strings.Join(summary, " ")
		cli.Verbs = append(cli.Verbs, entry)
	}
	return cli, nil
}

// parseFlagHelp splits help text into the lines before the first flag and the flags, as
// flag.PrintDefaults writes them: each flag a heading line, then its usage on lines indented four
// spaces and a tab. A flag whose usage is missing refuses the parse, so a change in the format fails
// here rather than publishing flags with no description.
func parseFlagHelp(help []byte) ([]string, []CommandLineInterfaceFlag, error) {
	var preamble []string
	var flags []CommandLineInterfaceFlag
	var usage []string
	finish := func() error {
		if len(flags) == 0 {
			return nil
		}
		current := &flags[len(flags)-1]
		if len(usage) == 0 {
			return fmt.Errorf("flag -%s has no usage", current.Name)
		}
		text := strings.Join(usage, "\n")
		if match := flagDefault.FindStringSubmatchIndex(text); match != nil {
			value := text[match[2]:match[3]]
			if strings.HasPrefix(value, `"`) {
				unquoted, unquoteError := strconv.Unquote(value)
				if unquoteError != nil {
					return fmt.Errorf("flag -%s: default %s: %w", current.Name, value, unquoteError)
				}
				value = unquoted
			}
			current.Default = value
			text = text[:match[0]]
		}
		current.Usage = text
		usage = nil
		return nil
	}
	for _, line := range strings.Split(strings.TrimRight(string(help), "\n"), "\n") {
		if match := flagHeading.FindStringSubmatch(line); match != nil {
			if finishError := finish(); finishError != nil {
				return nil, nil, finishError
			}
			flags = append(flags, CommandLineInterfaceFlag{Name: match[1], Argument: match[2]})
			continue
		}
		if len(flags) == 0 {
			preamble = append(preamble, line)
			continue
		}
		continued, isUsage := strings.CutPrefix(line, "    \t")
		if !isUsage {
			return nil, nil, fmt.Errorf("line %q is neither a flag nor its usage", line)
		}
		usage = append(usage, continued)
	}
	if finishError := finish(); finishError != nil {
		return nil, nil, finishError
	}
	if len(flags) == 0 {
		return nil, nil, fmt.Errorf("no flags")
	}
	return preamble, flags, nil
}
