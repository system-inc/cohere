package strace_events_test

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/strace_events"
)

// The differential holds this package against both consumers' parsers (loom_copy_test.go and
// adamic_copy_test.go), line by line. Each line is read three ways into a reading: the call it
// finishes, if any, field by field, and what a consumer keys on in its arguments, walked argument by
// argument (a descriptor and its decoded path, then a quoted path, then the next argument), each path
// as that consumer keys it. Where a consumer's reading differs from this package's, the difference must
// be one of the known classes below, and on the in-repo corpus it must be on that consumer's ledger
// (ledger_test.go) with its class and what it changes for the consumer.

// A differenceClass is why a consumer reads a line otherwise than strace wrote it.
type differenceClass int

const (
	unclassified differenceClass = iota
	// unknownResult is a call strace printed with `= ?`, a result it never saw: a consumer that reads
	// only numbers drops the call.
	unknownResult
	// quotedArguments is a call whose arguments a consumer ends inside a quoted string, at a `) = 1` in
	// a shell's command line, or reads past a quote that never closes.
	quotedArguments
	// escapedPath is a path strace escaped that a consumer keys as spelled, or decodes otherwise than
	// strace escaped it: \76 is `>`, and Go's strconv.Unquote refuses it.
	escapedPath
	// resumedSpace is whitespace after a `resumed>` marker that a consumer keeps in the call's arguments.
	resumedSpace
	// lineForm is a line a consumer's grammar refuses though strace prints it so: a `[pid N]` or no pid,
	// whitespace around the line, or a call whose name holds a capital, and the resumption of a call
	// whose unfinished half was such a line.
	lineForm
	// negativeDescriptor is a descriptor printed as a negative number, -100 for AT_FDCWD under
	// strace's raw printing, which a consumer reads as no descriptor.
	negativeDescriptor
	// malformedText is a path holding what strace never writes, which this package marks Malformed:
	// a consumer that refuses the trace there reads it otherwise, and so does one that keys it.
	malformedText
)

var differenceClassNames = map[differenceClass]string{
	unclassified:       "unclassified",
	unknownResult:      "unknownResult",
	quotedArguments:    "quotedArguments",
	escapedPath:        "escapedPath",
	resumedSpace:       "resumedSpace",
	lineForm:           "lineForm",
	negativeDescriptor: "negativeDescriptor",
	malformedText:      "malformedText",
}

func (class differenceClass) String() string {
	return differenceClassNames[class]
}

// A direction is what a difference changes for the consumer that reads the line otherwise.
type direction int

const (
	// missedRead is a read the consumer never sees, so a product keyed without it is served stale.
	missedRead direction = iota + 1
	// misKeyedPath is a path the consumer keys as a name no file on disk has, or as another file.
	misKeyedPath
	// harmless changes no key: the consumer refuses the trace, or reads the same paths either way.
	harmless
)

// A consumer is a consumer's parser, run one line at a time beside this package's.
type consumer struct {
	name string
	// read is the consumer's reading of a line, the arguments of the call it finished, and whether it
	// finished one.
	read func(text string) (string, string, bool)
	// module is this package's reading of a line as the consumer would be handed it.
	module func(line strace_events.Line, call strace_events.Call, isCall bool) string
	// spelled is this package's reading with every escaped path decoded as the consumer decodes it: a
	// difference that vanishes there is the consumer's escapes alone.
	spelled func(line strace_events.Line, call strace_events.Call, isCall bool) string
}

// A difference is one line a consumer reads otherwise than this package.
type difference struct {
	line, oracle, module string
	class                differenceClass
}

// newLoomOracle is Loom's parser, keying a decoded descriptor path through ebpftrace's
// UnquoteDescriptor and a quoted one through its Unquote, as planner/trace.go does.
func newLoomOracle() consumer {
	parser := &loomParser{}
	return consumer{
		name: "loom",
		read: func(text string) (string, string, bool) {
			event, isEvent := parser.line(text)
			if !isEvent {
				return "no call", "", false
			}
			opened := ""
			if match := loomOpenedPath.FindStringSubmatch(event.line); match != nil {
				opened = loomDecoded(match[1])
			}
			return fmt.Sprintf(
				"%s pid %q result %d opened %q arguments %q %s",
				event.call, event.pid, event.result, opened, event.arguments, loomWalk(event.arguments),
			), event.arguments, true
		},
		module: func(line strace_events.Line, call strace_events.Call, isCall bool) string {
			return moduleReadingForLoom(call, isCall, unescaped, unescaped)
		},
		spelled: func(line strace_events.Line, call strace_events.Call, isCall bool) string {
			return moduleReadingForLoom(call, isCall, loomDecodedText, loomUnquotedText)
		},
	}
}

// unescaped is a path as this package hands it to be keyed, or "malformed" where it marks the text
// as holding what strace never writes, which the consumer then keys or refuses.
func unescaped(text strace_events.Text) string {
	if text.Malformed {
		return "malformed"
	}
	return text.Unescaped
}

// loomDecoded is a decoded descriptor path as Loom keys it, through ebpftrace.UnquoteDescriptor, or
// "undecodable" where that refuses it and Loom refuses the trace.
func loomDecoded(raw string) string {
	decoded, unquoteError := loomUnquoteDescriptor(raw)
	if unquoteError != nil {
		return "undecodable"
	}
	return decoded
}

// loomDecodedText is loomDecoded over a Text strace spelled.
func loomDecodedText(text strace_events.Text) string {
	return loomDecoded(text.Raw)
}

// loomUnquotedText is a quoted path as Loom's quoted reads it, through ebpftrace.Unquote.
func loomUnquotedText(text strace_events.Text) string {
	unquoted, unquoteError := loomUnquote(`"` + text.Raw + `"`)
	if unquoteError != nil {
		return "undecodable"
	}
	return unquoted
}

// moduleReadingForLoom is this package's reading of a call in Loom's terms, each decoded descriptor
// path through decoded and each quoted path through quoted. Loom reads an opened path only after a
// result of zero or more, and a result only as a number.
func moduleReadingForLoom(
	call strace_events.Call,
	isCall bool,
	decoded func(strace_events.Text) string,
	quoted func(strace_events.Text) string,
) string {
	if !isCall {
		return "no call"
	}
	if call.Result.Kind != strace_events.NumberResult {
		return fmt.Sprintf(
			"%s pid %q result %s arguments %q",
			call.Name, call.Pid, call.Result.Raw, call.Arguments,
		)
	}
	opened := ""
	if call.Result.Number >= 0 {
		opened = decoded(call.Result.Opened)
	}
	return fmt.Sprintf(
		"%s pid %q result %d opened %q arguments %q %s",
		call.Name, call.Pid, call.Result.Number, opened, call.Arguments,
		moduleWalk(call.Arguments, decoded, quoted, false),
	)
}

// loomWalk is a call's arguments as Loom's helpers read them: descriptor (with Loom's `+ ", "`, so a
// descriptor that ends the arguments reads, as its fchdir and listings read it), then quoted, then
// nextArgument.
func loomWalk(arguments string) string {
	pieces := []string{}
	rest := arguments
	for step := 0; step < 4 && rest != ""; step++ {
		after, isDescriptor := rest, false
		if match := loomDescriptor.FindStringSubmatch(rest + ", "); match != nil {
			name := "AT_FDCWD"
			if !strings.HasPrefix(match[0], name) {
				name = match[0][:strings.IndexFunc(match[0], isNotDigit)]
			}
			pieces = append(pieces, fmt.Sprintf("descriptor %s %q", name, loomDecoded(match[1])))
			after, isDescriptor = "", true
			if len(match[0]) <= len(rest) {
				after = rest[len(match[0]):]
			}
		}
		name, quotedRest, quotedError := loomQuoted(after)
		if quotedError == nil {
			pieces = append(pieces, fmt.Sprintf("path %q", name))
			rest = loomNextArgument(quotedRest)
			continue
		}
		if strings.HasPrefix(quotedError.Error(), "path ") {
			pieces = append(pieces, fmt.Sprintf("path %q", "undecodable"))
			break
		}
		if !isDescriptor {
			break
		}
		rest = after
	}
	return "[" + strings.Join(pieces, "; ") + "]"
}

func isNotDigit(character rune) bool {
	return character < '0' || character > '9'
}

// moduleWalk is a call's arguments as this package's helpers read them, each decoded path through
// decoded and each quoted one through quoted. endNeedsPath is whether a descriptor that ends the
// arguments reads only when it names a path, as Adamic's descriptorPath reads one.
func moduleWalk(
	arguments string,
	decoded func(strace_events.Text) string,
	quoted func(strace_events.Text) string,
	endNeedsPath bool,
) string {
	pieces := []string{}
	rest := arguments
	for step := 0; step < 4 && rest != ""; step++ {
		after, isDescriptor := rest, false
		if descriptor, descriptorRest, found := strace_events.DescriptorArgument(rest); found &&
			!(endNeedsPath && rest == descriptor.Name) {
			pieces = append(
				pieces,
				fmt.Sprintf("descriptor %s %q", descriptor.Name, decoded(descriptor.Path)),
			)
			after, isDescriptor = descriptorRest, true
		}
		text, quotedRest, isQuoted := strace_events.QuotedArgument(after)
		if isQuoted {
			unquoted := quoted(text)
			pieces = append(pieces, fmt.Sprintf("path %q", unquoted))
			if unquoted == "undecodable" {
				break
			}
			rest = strace_events.NextArgument(quotedRest)
			continue
		}
		if !isDescriptor {
			break
		}
		rest = after
	}
	return "[" + strings.Join(pieces, "; ") + "]"
}

// newAdamicOracle is Adamic's grammar, keying every path through its unescape, as settle.go does.
func newAdamicOracle() consumer {
	parser := &adamicParser{}
	return consumer{
		name: "adamic",
		read: func(text string) (string, string, bool) {
			parts, isCall, status, isExit := parser.line(text)
			if isExit {
				return fmt.Sprintf("exit %q", status), "", false
			}
			if !isCall {
				return "no call", "", false
			}
			return fmt.Sprintf(
				"%s pid %q result %s opened %q errno %q arguments %q %s",
				parts.call, parts.pid, parts.value, adamicUnescape(parts.opened), parts.errno,
				parts.arguments, adamicWalk(parts.arguments),
			), parts.arguments, true
		},
		module: func(line strace_events.Line, call strace_events.Call, isCall bool) string {
			return moduleReadingForAdamic(line, call, isCall, unescaped)
		},
		spelled: func(line strace_events.Line, call strace_events.Call, isCall bool) string {
			return moduleReadingForAdamic(line, call, isCall, adamicSpelling)
		},
	}
}

// adamicSpelling is a path as Adamic keys it: through strconv.Unquote, or as spelled where that
// refuses it.
func adamicSpelling(text strace_events.Text) string {
	return adamicUnescape(text.Raw)
}

// moduleReadingForAdamic is this package's reading of a line in Adamic's terms, every path through
// decoded.
func moduleReadingForAdamic(
	line strace_events.Line,
	call strace_events.Call,
	isCall bool,
	decoded func(strace_events.Text) string,
) string {
	if line.Kind == strace_events.ExitLine {
		return fmt.Sprintf("exit %q", line.Exit.Status)
	}
	if !isCall {
		return "no call"
	}
	return fmt.Sprintf(
		"%s pid %q result %s opened %q errno %q arguments %q %s",
		call.Name, call.Pid, call.Result.Raw, decoded(call.Result.Opened), call.Result.Errno,
		call.Arguments, moduleWalk(call.Arguments, decoded, decoded, true),
	)
}

// adamicWalk is a call's arguments as Adamic's helpers read them: directoryArgument, or
// descriptorPath for a descriptor that ends them, then quotedArgument, then secondTarget's trim.
func adamicWalk(arguments string) string {
	pieces := []string{}
	rest := arguments
	for step := 0; step < 4 && rest != ""; step++ {
		after, isDescriptor := rest, false
		if match := adamicDirectory.FindStringSubmatch(rest); match != nil {
			pieces = append(pieces, fmt.Sprintf("descriptor %s %q", match[1], adamicUnescape(match[2])))
			after, isDescriptor = rest[len(match[0]):], true
		} else if match := adamicDescriptor.FindStringSubmatch(rest); match != nil && len(match[0]) == len(rest) {
			name := rest[:strings.IndexByte(rest, '<')]
			pieces = append(pieces, fmt.Sprintf("descriptor %s %q", name, adamicUnescape(match[1])))
			after, isDescriptor = "", true
		}
		if match := adamicQuoted.FindStringSubmatch(after); match != nil {
			pieces = append(pieces, fmt.Sprintf("path %q", adamicUnescape(match[1])))
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(after[len(match[0]):]), ","))
			continue
		}
		if !isDescriptor {
			break
		}
		rest = after
	}
	return "[" + strings.Join(pieces, "; ") + "]"
}

// compareTrace reads a trace's lines through this package and each consumer's parser in step, and
// reports every line a consumer reads otherwise, classified, to found.
func compareTrace(
	trace io.Reader,
	consumers []consumer,
	found func(consumer consumer, difference difference),
) (int, error) {
	scanner := bufio.NewScanner(trace)
	// strace's lines run as long as an execve's argv, so the buffer is the larger of the two consumers'.
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	joiner := &strace_events.Joiner{}
	lines := 0
	for scanner.Scan() {
		text := scanner.Text()
		lines++
		line := strace_events.ParseLine(text)
		call, isCall := joiner.Join(line)
		for _, consumer := range consumers {
			consumerReading, consumerArguments, consumerIsCall := consumer.read(text)
			moduleReading := consumer.module(line, call, isCall)
			if consumerReading == moduleReading {
				continue
			}
			class := classify(text, line, call, isCall, consumerArguments, consumerIsCall)
			if class == unclassified && strings.Contains(moduleReading, `"malformed"`) {
				class = malformedText
			}
			if class == unclassified && consumer.spelled(line, call, isCall) == consumerReading {
				class = escapedPath
			}
			found(consumer, difference{line: text, oracle: consumerReading, module: moduleReading, class: class})
		}
	}
	return lines, scanner.Err()
}

// classify is why a consumer's reading of a line differs from this package's, or unclassified.
func classify(
	text string,
	line strace_events.Line,
	call strace_events.Call,
	isCall bool,
	oracleArguments string,
	oracleIsCall bool,
) differenceClass {
	switch {
	case isCall && oracleIsCall && call.Arguments != oracleArguments:
		if line.Kind == strace_events.ResumedLine && strings.TrimRight(oracleArguments, " \t") == call.Arguments {
			return resumedSpace
		}
		if strings.HasPrefix(call.Arguments, oracleArguments) && insideQuote(call.Arguments, len(oracleArguments)) {
			return quotedArguments
		}
		return unclassified
	case isCall && !oracleIsCall && call.Result.Kind == strace_events.UnknownResult:
		return unknownResult
	case (isCall != oracleIsCall || line.Kind == strace_events.ExitLine) && strangeForm(text, line):
		return lineForm
	case isCall && !oracleIsCall && line.Kind == strace_events.ResumedLine:
		// The consumer refused the call's unfinished half, so its resumption joins nothing: the earlier
		// line's form is why, `[pid 23] openat(... <unfinished ...>` resumed as `23 <... openat resumed>`.
		return lineForm
	case !isCall && oracleIsCall && insideQuote(oracleArguments, len(oracleArguments)):
		return quotedArguments
	case isCall && oracleIsCall && negativeDescriptorIn(call.Arguments):
		return negativeDescriptor
	}
	return unclassified
}

// strangeForm is a line strace prints that a consumer's grammar may refuse: a pid that isn't bare
// digits, whitespace around the line, or a call whose name holds a capital.
func strangeForm(text string, line strace_events.Line) bool {
	bare := line.Pid != "" && strings.HasPrefix(text, line.Pid)
	return !bare || text != strings.TrimSpace(text) || strings.ToLower(line.Name) != line.Name
}

// insideQuote is whether text's first length bytes leave a quoted string open.
func insideQuote(text string, length int) bool {
	quoted := false
	for index := 0; index < length; index++ {
		switch {
		case quoted && text[index] == '\\':
			index++
		case text[index] == '"':
			quoted = !quoted
		}
	}
	return quoted
}

// negativeDescriptorIn is whether any argument reads as a descriptor printed as a negative number.
func negativeDescriptorIn(arguments string) bool {
	for _, argument := range strings.Split(arguments, ",") {
		descriptor, _, found := strace_events.DescriptorArgument(strings.TrimSpace(argument))
		if found && strings.HasPrefix(descriptor.Name, "-") {
			return true
		}
	}
	return false
}

// inRepositoryCorpus is every trace in testdata, then the long lines longLines makes, by name. Each
// file is one trace:
//   - loom-strace-6.8-review.txt and loom-adam-gate-6-strace.txt are real strace 6.8 traces, from
//     loom's planner/testdata and ebpftrace/testdata at origin/release-0.8.1184 (9352c88758ab), and
//     loom-ebpf-rendered-trace.txt is its BPF tracer's rendering of strace's format, from the same ref.
//   - loom-fixtures.txt is every strace line in that ref's test files' string literals, and
//     adamic-fixtures.txt every one in adamic's internal/buildcache tests at a23ba096388c, its
//     straceLines first. A fixture line a test prefixes with its pid where it uses it is given one
//     (9 for Loom's, 12 for Adamic's), and Adamic's format verbs are filled; Loom's are left as text.
//   - adamic-synthetic.txt is adamic's syntheticTrace, 200 lines of its mix.
//   - edge-cases.txt is written by hand: every pid form, splits across pids, escapes, descriptors,
//     results of each kind, cut strings and garbage.
func inRepositoryCorpus(test testing.TB) map[string]string {
	test.Helper()
	corpus := map[string]string{}
	entries, readError := os.ReadDir("testdata")
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		content, fileError := os.ReadFile(filepath.Join("testdata", entry.Name()))
		if fileError != nil {
			test.Fatal(fileError)
		}
		corpus[entry.Name()] = string(content)
	}
	corpus["long lines"] = longLines()
	return corpus
}

// longLines is a trace of lines far longer than a terminal's: a path of 100,000 bytes, opened and
// decoded, and an execve whose argv holds 20,000 arguments, split around another process's call.
func longLines() string {
	path := "/w/" + strings.Repeat("directory/", 10_000) + "file.txt"
	arguments := make([]string, 20_000)
	for index := range arguments {
		arguments[index] = fmt.Sprintf(`"argument-%d"`, index)
	}
	return strings.Join([]string{
		fmt.Sprintf(`50 openat(AT_FDCWD</w>, %q, O_RDONLY|O_CLOEXEC) = 3<%s>`, path, path),
		fmt.Sprintf(
			`50 execve("/usr/bin/tool", [%s], 0x7ffd /* 30 vars */ <unfinished ...>`,
			strings.Join(arguments, ", "),
		),
		`51 newfstatat(AT_FDCWD</w>, "beside.txt", 0x7ffd, 0) = -1 ENOENT (No such file or directory)`,
		`50 <... execve resumed>) = 0`,
	}, "\n") + "\n"
}

// ledgerKey is what identifies a ledger row: its line and both readings of it.
func ledgerKey(line, oracle, module string) string {
	return line + "\x00" + oracle + "\x00" + module
}

// rowSource is a difference as a ledger row's Go source, for a reviewer to judge and paste.
func rowSource(difference difference) string {
	return fmt.Sprintf(
		"{\n\tline:   %s,\n\toracle: %s,\n\tmodule: %s,\n\tclass:  %s,\n\tdirection: ?,\n},",
		goString(difference.line), goString(difference.oracle), goString(difference.module), difference.class,
	)
}

// goString is text as a Go literal, raw where it can be.
func goString(text string) string {
	if !strings.Contains(text, "`") && strconv.CanBackquote(text) {
		return "`" + text + "`"
	}
	return strconv.Quote(text)
}

// Every line of the in-repo corpus reads as each consumer reads it, except the lines on that
// consumer's ledger, each of which still differs, as the ledger says, for the reason it names. A
// difference no ledger row holds fails, printed as a row to judge; so does a row the corpus no longer
// produces, since a ledger that outlives its differences would hide the next one.
func TestEveryDifferenceFromEachConsumerIsOnItsLedger(test *testing.T) {
	test.Parallel()
	corpus := inRepositoryCorpus(test)
	ledgers := map[string]map[string]ledgerRow{
		"loom":   keyedRows(test, loomLedger),
		"adamic": keyedRows(test, adamicLedger),
	}
	seen := map[string]map[string]bool{"loom": {}, "adamic": {}}
	counts := map[string]map[differenceClass]int{"loom": {}, "adamic": {}}
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, compareError := compareTrace(
			strings.NewReader(corpus[name]),
			[]consumer{newLoomOracle(), newAdamicOracle()},
			func(consumer consumer, difference difference) {
				counts[consumer.name][difference.class]++
				key := ledgerKey(difference.line, difference.oracle, difference.module)
				row, onLedger := ledgers[consumer.name][key]
				if !onLedger {
					test.Errorf(
						"%s: %s reads a line otherwise, on no ledger row:\n%s",
						name, consumer.name, rowSource(difference),
					)
					return
				}
				seen[consumer.name][key] = true
				if row.class != difference.class {
					test.Errorf(
						"%s: %s ledger row %q is classed %s, and the line classifies as %s",
						name, consumer.name, row.line, row.class, difference.class,
					)
				}
			},
		)
		if compareError != nil {
			test.Fatalf("%s: %v", name, compareError)
		}
	}
	for consumerName, ledger := range ledgers {
		for key, row := range ledger {
			if !seen[consumerName][key] {
				test.Errorf("%s ledger row %q no longer differs: remove it", consumerName, row.line)
			}
			if row.direction == 0 {
				test.Errorf("%s ledger row %q names no direction", consumerName, row.line)
			}
		}
		test.Logf("%s: %s", consumerName, classCounts(counts[consumerName]))
	}
}

// keyedRows is a ledger by its rows' keys, refusing a row twice.
func keyedRows(test *testing.T, ledger []ledgerRow) map[string]ledgerRow {
	test.Helper()
	keyed := map[string]ledgerRow{}
	for _, row := range ledger {
		key := ledgerKey(row.line, row.oracle, row.module)
		if _, twice := keyed[key]; twice {
			test.Errorf("ledger row %q is on its ledger twice", row.line)
		}
		keyed[key] = row
	}
	return keyed
}

// classCounts is each class's count, in the classes' order.
func classCounts(counts map[differenceClass]int) string {
	pieces := []string{}
	for class := unclassified; class <= malformedText; class++ {
		if counts[class] > 0 {
			pieces = append(pieces, fmt.Sprintf("%s %d", class, counts[class]))
		}
	}
	if len(pieces) == 0 {
		return "no differences"
	}
	return strings.Join(pieces, ", ")
}

// The large corpus: every file under COHERE_STRACE_CORPUS is a trace, compared against both consumers.
// It prints how many lines differ in each class and fails on any difference no class explains. Loom
// runs it on a box against real traces; unset, it reads nothing and says so.
func TestALargeCorpusDiffersFromEachConsumerOnlyInKnownClasses(test *testing.T) {
	test.Parallel()
	root := os.Getenv("COHERE_STRACE_CORPUS")
	if root == "" {
		test.Skip(
			"COHERE_STRACE_CORPUS is not set, so the large corpus was not read: " +
				"nothing here was checked against real traces",
		)
	}
	counts := map[string]map[differenceClass]int{"loom": {}, "adamic": {}}
	unexplained, traces, lines := []string{}, 0, 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil || !entry.Type().IsRegular() {
			return entryError
		}
		file, openError := os.Open(path)
		if openError != nil {
			return openError
		}
		// Ignored: the trace is only read, so a failed close loses nothing.
		defer file.Close()
		consumers := []consumer{newLoomOracle(), newAdamicOracle()}
		read, compareError := compareTrace(file, consumers, func(consumer consumer, difference difference) {
			counts[consumer.name][difference.class]++
			if difference.class == unclassified && len(unexplained) < 20 {
				unexplained = append(
					unexplained,
					fmt.Sprintf("%s, %s:\n%s", path, consumer.name, rowSource(difference)),
				)
			}
		})
		traces, lines = traces+1, lines+read
		return compareError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if lines == 0 {
		test.Fatalf("COHERE_STRACE_CORPUS (%s) holds no lines, so nothing was checked", root)
	}
	test.Logf("%s: %d traces, %d lines", root, traces, lines)
	for _, consumerName := range []string{"loom", "adamic"} {
		test.Logf("%s: %s", consumerName, classCounts(counts[consumerName]))
	}
	for _, line := range unexplained {
		test.Errorf("a difference no class explains: %s", line)
	}
}
