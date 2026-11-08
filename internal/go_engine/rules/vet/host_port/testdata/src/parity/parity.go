// Package parity holds every branch of hostport's judgment, a finding and a clean case beside each, and the shapes
// most likely to set the port apart from the original: renamed and dot imports, parentheses, method values and
// expressions, embedded and aliased dialers, generics, closures, and declarations elsewhere in the package or outside
// it. Both sides must report exactly the same findings with exactly the same fixes; the comments say what each case
// expects.
package parity

import (
	"context"
	"fmt"
	formatting "fmt"
	"net"
	network "net"
	"time"

	"parity/address"
)

// Both formats, written as the dial's own argument: found, with no line in the message.
func direct(host string, port int, portText string) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%s", host, portText))
}

// Formats that are neither of the two, or not constant: clean.
func otherFormats(host string, port int, format string) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%v:%d", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("[%s]:%d", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d ", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf(format, host, port))
}

// hostAndPort is a constant format, so its value is read through its name: found.
const hostAndPort = "%s:%d"

// A constant format reached by name, concatenation, a raw string and parentheses: each found, the format deleted as
// written.
func constantFormats(host string, port int, portText string) {
	_, _ = net.Dial("tcp", fmt.Sprintf(hostAndPort, host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:"+"%d", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf(`%s:%s`, host, portText))
	_, _ = net.Dial("tcp", fmt.Sprintf(("%s:%d"), host, port))
}

// Sprintf with other than three arguments: clean, though the format is one of the two.
func argumentCounts(host string, port int, rest []any) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", rest...))
	// Three arguments with the last spread do not type-check, since only Sprintf's own variadic slice may be spread,
	// so that shape cannot be a fixture.
}

// Ports of every shape: an integer literal in each base rewritten as a decimal string, and anything else wrapped.
func ports(host string, port uint16) {
	const named = 80
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 8080))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 0x1F90))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 0o17620))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 0b1111))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 017620))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 8_080))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 0))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, named))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, -1))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, int(port)+1))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 'P'))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 80.0))
	// A literal past int64's range does not type-check as an argument to Sprintf, so it cannot be a fixture; a
	// literal within it but past uint16's is still a literal, and is rewritten.
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, 9223372036854775807))
	// "%s:%s" keeps its port as written, a literal or not.
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%s", host, 8080))
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%s", host, "http"))
}

// The address in parentheses is neither a call nor a name to the original: clean. The dial in parentheses still
// calls net.Dial, and Sprintf in parentheses still calls Sprintf: found, the fix replacing the parentheses too.
func parentheses(host string, port int) {
	_, _ = net.Dial("tcp", (fmt.Sprintf("%s:%d", host, port)))
	_, _ = (net.Dial)("tcp", fmt.Sprintf("%s:%d", host, port))
	_, _ = net.Dial("tcp", (fmt.Sprintf)("%s:%d", host, port))
}

// Renamed imports name the same functions: found, and the fix still writes net.JoinHostPort, as the original's does.
func renamed(host string, port int) {
	_, _ = network.Dial("tcp", formatting.Sprintf("%s:%d", host, port))
	address := formatting.Sprintf("%s:%s", host, "80")
	_, _ = network.DialTimeout("tcp", address, time.Second)
}

// DialTimeout takes a third argument, so a Sprintf written as its argument is left alone, as the original leaves it;
// passed through a variable it is found.
func timeouts(host string, port int) {
	_, _ = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), time.Second)
	address := fmt.Sprintf("%s:%d", host, port)
	_, _ = net.DialTimeout("tcp", address, time.Second)
}

// Every way the address reaches a dial through a variable.
func variables(host string, port int, parameter string) {
	// Declared just before, in a nested block: found, naming the dial's line.
	{
		address := fmt.Sprintf("%s:%d", host, port)
		_, _ = net.Dial("tcp", address)
	}
	// Declared with var, typed and untyped: found.
	var typed string = fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial("tcp", typed)
	var untyped = fmt.Sprintf("%s:%s", host, "80")
	_, _ = net.Dial("tcp", untyped)
	// Declared in an if's initializer: found.
	if initialized := fmt.Sprintf("%s:%d", host, port); host != "" {
		_, _ = net.Dial("tcp", initialized)
	}
	// Dialed twice, from two lines: found twice, once for each dial's line.
	twice := fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial("tcp", twice)
	_, _ = net.Dial("udp", twice)
	// Dialed twice from one line: found twice, the same finding.
	again := fmt.Sprintf("%s:%d", host, port)
	_ = []error{dialError(net.Dial("tcp", again)), dialError(net.Dial("udp", again))}
	// Declared with another name and value: clean, since the declaration holds two values.
	first, second := "tcp", fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial(first, second)
	var third, fourth = "tcp", fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial(third, fourth)
	// Declared with another name from one call: clean, since that call is not Sprintf.
	pair, pairError := split(host, port)
	_, _ = net.Dial("tcp", pair)
	_ = pairError
	// Assigned after its declaration: judged by its declaration, as the original judges it, whatever is assigned
	// later. Declared as Sprintf and then reassigned: found.
	reassigned := fmt.Sprintf("%s:%d", host, port)
	reassigned = net.JoinHostPort(host, "80")
	_, _ = net.Dial("tcp", reassigned)
	// Declared as something else and then assigned Sprintf: clean.
	assignedLater := ""
	assignedLater = fmt.Sprintf("%s:%d", host, port)
	_, _ = net.Dial("tcp", assignedLater)
	// Redeclared in a short declaration beside a new name: judged by its first declaration, Sprintf, and found,
	// since a redeclaration declares nothing.
	redeclared := fmt.Sprintf("%s:%d", host, port)
	redeclared, fresh := split(host, port)
	_, _ = net.Dial("tcp", redeclared)
	_ = fresh
	// A parameter has no declaring value: clean.
	_, _ = net.Dial("tcp", parameter)
	// A package-level variable, here and in another file: found, naming the dial's line.
	_, _ = net.Dial("tcp", packageAddress)
	_, _ = net.Dial("tcp", elsewhereAddress)
	_, _ = net.Dial("tcp", groupedAddress)
	// A variable of another package, reached by its selector: clean, as the address is neither a call nor a name.
	_, _ = net.Dial("tcp", address.Remote)
	// A constant address is not a variable: clean.
	_, _ = net.Dial("tcp", constantAddress)
	// Declared in parentheses: clean, as the declared value is not a call to the original.
	parenthesized := (fmt.Sprintf("%s:%d", host, port))
	_, _ = net.Dial("tcp", parenthesized)
	// Received in a select's case: clean, as the value is a receive.
	addresses := make(chan string, 1)
	addresses <- host
	select {
	case received := <-addresses:
		_, _ = net.Dial("tcp", received)
	default:
	}
	// Bound by a type switch: clean, as its symbolic variable declares nothing.
	var anything any = fmt.Sprintf("%s:%d", host, port)
	switch bound := anything.(type) {
	case string:
		_, _ = net.Dial("tcp", bound)
	}
	// Ranged over: clean, as range declares the variable.
	for _, ranged := range []string{fmt.Sprintf("%s:%d", host, port)} {
		_, _ = net.Dial("tcp", ranged)
	}
}

// dialError is the error of a dial.
func dialError(_ net.Conn, dialFailure error) error {
	return dialFailure
}

// split is a call of two results that is not Sprintf.
func split(host string, port int) (string, error) {
	return fmt.Sprintf("%s:%d", host, port), nil
}

// packageAddress is a package-level variable passed to a dial below its declaration: found, naming the dial's line.
var packageAddress = fmt.Sprintf("%s:%d", "localhost", 80)

// groupedAddress is declared in a group beside a clean one.
var (
	groupedAddress = fmt.Sprintf("%s:%s", "localhost", "80")
	groupedClean   = net.JoinHostPort("localhost", "80")
)

// constantAddress is a constant, never a variable.
const constantAddress = "localhost:80"

// Package-level dials, in a var's initializer.
var (
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", "localhost", 80))
	_, _ = net.Dial("tcp", groupedClean)
)

// wrappedDialer embeds a dialer, whose Dial it promotes; pointerHolder holds a pointer to one.
type wrappedDialer struct {
	net.Dialer
}

type pointerHolder struct {
	dialer *net.Dialer
}

// aliasedDialer is net.Dialer under another name.
type aliasedDialer = net.Dialer

// Every way to reach (*net.Dialer).Dial.
func dialers(host string, port int, holder pointerHolder) {
	var dialer net.Dialer
	_, _ = dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	pointer := &net.Dialer{}
	_, _ = pointer.Dial("tcp", fmt.Sprintf("%s:%s", host, "80"))
	_, _ = holder.dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	var wrapped wrappedDialer
	_, _ = wrapped.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	var aliased aliasedDialer
	_, _ = aliased.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	_, _ = (&net.Dialer{}).Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	address := fmt.Sprintf("%s:%d", host, port)
	_, _ = wrapped.Dial("tcp", address)
	// DialContext is not judged: clean.
	_, _ = dialer.DialContext(context.Background(), "tcp", fmt.Sprintf("%s:%d", host, port))
	// A method value is a variable when called: clean.
	dial := dialer.Dial
	_, _ = dial("tcp", fmt.Sprintf("%s:%d", host, port))
	// A method expression takes the receiver first, so the address is its third argument and the second, "tcp", is
	// what is judged: clean as a literal, and, as in the original, judged as an address when it is a variable.
	_, _ = (*net.Dialer).Dial(&dialer, "tcp", fmt.Sprintf("%s:%d", host, port))
	networkName := fmt.Sprintf("%s:%d", host, port)
	_, _ = (*net.Dialer).Dial(&dialer, networkName, "localhost:80")
	// A function value of net.Dial is a variable when called: clean.
	dialFunction := net.Dial
	_, _ = dialFunction("tcp", fmt.Sprintf("%s:%d", host, port))
}

// A dial nested in another's receiver: both found, in an order that differs between the sides and does not matter.
func nestedDial(host string, port int) {
	_, _ = dialerOf(dialerOf(nil).Dial("tcp", fmt.Sprintf("%s:%d", host, port))).Dial("tcp", fmt.Sprintf("%s:%d", host, port))
}

// dialerOf is a dialer, whatever it is handed.
func dialerOf(any, ...any) *net.Dialer {
	return &net.Dialer{}
}

// A dial in a closure and in a deferred call: found.
func closures(host string, port int) {
	address := fmt.Sprintf("%s:%d", host, port)
	dial := func() {
		_, _ = net.Dial("tcp", address)
	}
	dial()
	defer func() {
		_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	}()
}

// A generic function dials as any other: found. A type parameter's Dial is not net's: clean.
func generic[Port int | uint16](host string, port Port) {
	_, _ = net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
}

type dialing interface {
	Dial(network string, address string) (net.Conn, error)
}

func constrained[Dialer dialing](dialer Dialer, host string, port int) {
	_, _ = dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
}

// An interface satisfied by net.Dialer dispatches through the interface's method: clean.
func throughInterface(dialer dialing, host string, port int) {
	_, _ = dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
}

// A generic type holding a dialer reaches net's own method: found.
type genericHolder[Kind any] struct {
	dialer net.Dialer
	kind   Kind
}

func (holder *genericHolder[Kind]) dial(host string, port int) {
	_, _ = holder.dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	_ = holder.kind
}

// Functions of this package named Dial and Sprintf are not net's or fmt's: clean.
func Dial(network string, address string) (net.Conn, error) {
	return nil, nil
}

func Sprintf(format string, arguments ...any) string {
	return format
}

func lookalikes(host string, port int) {
	_, _ = Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	_, _ = net.Dial("tcp", Sprintf("%s:%d", host, port))
}

// Too few arguments to a dial: clean, and a spread call supplies them all.
func spread() {
	_, _ = net.Dial(networkAndAddress())
}

func networkAndAddress() (string, string) {
	return "tcp", fmt.Sprintf("%s:%d", "localhost", 80)
}

// Formats the pass never reads, outside a dial: clean.
func notDialed(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
