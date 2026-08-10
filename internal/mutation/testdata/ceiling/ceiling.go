// Package ceiling is a FIXTURE, not shipped code. It exists so the
// gremlins-attribution-ceiling category rests on a demonstration rather than on
// an assertion: a switch-case condition that the test suite provably executes,
// which gremlins still reports NOT COVERED.
//
// It lives under testdata/ so `go build ./...` and `go test ./...` skip it. The
// proof test drives it by explicit path.
package ceiling

// Greeting is a const initializer whose only operator is string concatenation.
// Evaluated entirely at compile time, so there is no runtime evaluation for any
// caller to observe — see TestConstInitializerArithmeticIsUnobservable.
const Greeting = "hello" + ", " + "world"

// Classify is the attribution-ceiling subject: the CASE CONDITIONS below are
// what gremlins mutates, and what go-cover attributes to the enclosing block
// instead of to their own lines.
func Classify(r rune) string {
	switch {
	case r >= 'a' && r <= 'z':
		return "lower"
	case r >= '0' && r <= '9':
		return "digit"
	}
	return "other"
}

// Describe builds its result by string concatenation — the ARITHMETIC_BASE
// subject. Every operand is a string, so the `+` → `-` swap gremlins applies
// here is not a behavioural mutation at all; it does not compile.
func Describe(name string) string {
	return "name: " + name + " (" + Greeting + ")"
}
