// Package parity holds every branch of httpresponse's judgment, each with a clean case beside it, and the shapes
// most likely to read differently between the original and the port. A comment saying "reported" marks a line both
// are expected to report; the test compares the two sides, not these comments.
package parity

import (
	"errors"
	"log"
	"net/http"
	"os"
)

// Each function of net/http that returns (*http.Response, error) is judged.
func packageFunctions(url string) {
	response, err := http.Get(url)
	defer response.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
	posted, err := http.Post(url, "text/plain", nil)
	defer posted.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
	form, err := http.PostForm(url, nil)
	defer form.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// A function of net/http that returns something other than a response is passed over.
func notAResponse(url string) {
	request, err := http.NewRequest("GET", url, nil)
	defer log.Println(request.URL)
	if err != nil {
		log.Fatal(err)
	}
}

// Checking the error first is right.
func checkedFirst(url string) {
	response, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
}

// The methods of a client, its value and a pointer to it, reached by a field of net/http, through aliases, and as a
// method expression.
type clientAlias = http.Client

type clientPointerAlias = *http.Client

func clients(url string, request *http.Request, value http.Client, pointer *http.Client, aliased clientAlias,
	aliasedPointer clientPointerAlias) {
	byValue, err := value.Do(request)
	defer byValue.Body.Close() // reported
	byPointer, err := pointer.Head(url)
	defer byPointer.Body.Close() // reported
	byDefault, err := http.DefaultClient.Get(url)
	defer byDefault.Body.Close() // reported
	byAlias, err := aliased.Get(url)
	defer byAlias.Body.Close() // reported
	byPointerAlias, err := aliasedPointer.Get(url)
	defer byPointerAlias.Body.Close() // reported
	byExpression, err := (*http.Client).Get(pointer, url)
	defer byExpression.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// A method value is called by a name, not a selector, and is passed over.
func methodValue(url string) {
	get := http.DefaultClient.Get
	response, err := get(url)
	defer response.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
}

// A client of another type, embedding net/http's or of a type parameter, is passed over, its methods returning a
// response all the same.
type embedding struct {
	http.Client
}

type ownClient struct{}

func (client ownClient) Get(url string) (*http.Response, error) {
	return http.Get(url)
}

func otherClients[C *http.Client](url string, embedded embedding, own ownClient, parameter C) {
	fromEmbedded, err := embedded.Get(url)
	defer fromEmbedded.Body.Close()
	fromOwn, err := own.Get(url)
	defer fromOwn.Body.Close()
	fromParameter, err := (*http.Client)(parameter).Get(url)
	defer fromParameter.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// Functions of this package are called by a name, not a selector, and are passed over, as is a generic one.
func get(url string) (*http.Response, error) {
	return http.Get(url)
}

func fetch[T any](url string) (*http.Response, error) {
	return http.Get(url)
}

func ownFunctions(url string) {
	response, err := get(url)
	defer response.Body.Close()
	generic, err := fetch[int](url)
	defer generic.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
}

// Results of other shapes: three of them, an error of another type, a response of a defined type over net/http's,
// and one through an alias of net/http's, which is net/http's.
type responseAlias = http.Response

type definedResponse http.Response

type wrongError struct{}

func (wrongError) Error() string { return "wrong" }

type shapes struct{}

func (shapes) three(url string) (*http.Response, error, int) { return nil, nil, 0 }

func (shapes) ownError(url string) (*http.Response, *wrongError) { return nil, nil }

func (shapes) defined(url string) (*definedResponse, error) { return nil, nil }

func (shapes) value(url string) (http.Response, error) { return http.Response{}, nil }

func resultShapes(url string, shaped shapes, client *http.Client) {
	three, err, _ := shaped.three(url)
	defer three.Body.Close()
	own, wrong := shaped.ownError(url)
	defer own.Body.Close()
	defined, err := shaped.defined(url)
	defer defined.Body.Close()
	value, err := shaped.value(url)
	defer value.Body.Close()
	var aliased *responseAlias
	aliased, err = client.Get(url)
	defer aliased.Body.Close() // reported
	if err != nil || wrong != nil {
		log.Fatal(err)
	}
}

// The call as the last statement of its block has no defer after it.
func lastStatement(url string) (response *http.Response, err error) {
	{
		response, err = http.Get(url)
	}
	return response, err
}

// A call wrapped in another has its error checked by the wrapper, and a call wrapping one is still judged, as is a
// call within an argument of a call that is not wrapped.
func checked(response *http.Response, err error) *http.Response {
	if err != nil {
		panic(err)
	}
	return response
}

func wrapped(url string) {
	response := checked(http.Get(url))
	defer response.Body.Close()
	outer, err := http.Get(checked(http.Get(url)).Status)
	defer outer.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// The first statement must be an assignment: a declaration, an expression, or an if with the call in its
// initializer is passed over.
func notAnAssignment(url string) {
	var declared, err = http.Get(url)
	defer declared.Body.Close()
	http.Get(url)
	defer declared.Body.Close()
	if response, err := http.Get(url); err == nil {
		defer response.Body.Close()
	}
	if err != nil {
		log.Fatal(err)
	}
}

// The assignment's first target must be rooted in a name: one through a pointer or an index is passed over, and one
// rooted in a struct's name is judged by that name.
type holder struct {
	response *http.Response
}

func targets(url string, pointer **http.Response, responses []*http.Response, held holder) {
	var err error
	*pointer, err = http.Get(url)
	defer (*pointer).Body.Close()
	responses[0], err = http.Get(url)
	defer responses[0].Body.Close()
	held.response, err = http.Get(url)
	defer held.response.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// The next statement must be a defer, and its function rooted in a name.
func notADefer(url string) {
	response, err := http.Get(url)
	log.Println(response.Status)
	defer response.Body.Close()
	again, err := http.Get(url)
	defer func() { again.Body.Close() }()
	parenthesized, err := http.Get(url)
	defer (parenthesized.Body.Close)()
	if err != nil {
		log.Fatal(err)
	}
}

// The deferred function must be rooted in the response's own name: a function handed the response is another name.
func closeBody(response *http.Response) {
	response.Body.Close()
}

func otherRoots(url string) {
	response, err := http.Get(url)
	defer closeBody(response)
	other, err := http.Get(url)
	defer response.Body.Close()
	if err != nil || other == nil {
		log.Fatal(err)
	}
}

// The blank identifier declared with `:=` is an object of its own, the same as no other name. Assigned with `=`, it
// has no object, and differences.go holds that case, which the original reports.
func blankTargets(url string) {
	_, declared := http.Get(url)
	defer log.Println("deferred")
	if declared != nil {
		log.Fatal(declared)
	}
}

// A package-level variable is the same variable in the defer, whether declared in this file, which the parser
// resolves, or in another, which it leaves unresolved, and is reported either way. Beside an imported package's name
// it is not; differences.go holds the unresolved cases the original reports.
var sameFile *http.Response

func packageLevel(url string) {
	var err error
	sameFile, err = http.Get(url)
	defer log.Println("deferred")
	sameFile, err = http.Get(url)
	defer sameFile.Body.Close() // reported
	otherFile, err = http.Get(url)
	defer otherFile.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}

// A type switch's variable is an object of each clause, the same throughout a block within the clause.
func typeSwitch(url string, value any) {
	var err error
	switch held := value.(type) {
	case *holder:
		{
			held.response, err = http.Get(url)
			defer held.response.Body.Close() // reported
		}
	}
	if err != nil {
		log.Fatal(err)
	}
}

// Statements in a case clause are not in a block statement, so the innermost block is the switch's, whose
// statement is the clause, and the defer is passed over.
func inClauses(url string, choice int, ready chan struct{}) {
	switch choice {
	case 0:
		response, err := http.Get(url)
		defer response.Body.Close()
		if err != nil {
			log.Fatal(err)
		}
	}
	select {
	case <-ready:
		response, err := http.Get(url)
		defer response.Body.Close()
		if err != nil {
			log.Fatal(err)
		}
	}
}

// Nested blocks, closures and the bodies of statements are judged, each in its own block.
func nested(url string, values []int) {
	{
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if err != nil {
			log.Fatal(err)
		}
	}
	for range values {
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if err != nil {
			log.Fatal(err)
		}
	}
	if len(values) > 0 {
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if err != nil {
			log.Fatal(err)
		}
	} else {
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if err != nil {
			log.Fatal(err)
		}
	}
	func() {
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if err != nil {
			log.Fatal(err)
		}
	}()
	go func() {
		response, err := http.Get(url)
		defer response.Body.Close() // reported
		if errors.Is(err, os.ErrClosed) {
			log.Fatal(err)
		}
	}()
}

// A generic function is judged as any other, its type parameters unused by the call.
func generic[T any, P *T](url string, pointer P) {
	response, err := http.Get(url)
	defer response.Body.Close() // reported
	if err != nil || pointer == nil {
		log.Fatal(err)
	}
}

// A call in a package-level initializer is in no block, and is passed over.
var initialized, initializedError = http.Get("http://foo.com")
