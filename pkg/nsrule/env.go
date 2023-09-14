package nsrule

import (
	"maps"
	"reflect"

	"github.com/antonmedv/expr"
)

// Env contains data used to evaluate rules.
type Env map[string]any

var (
	dummyEnv     = Env{}
	defaultEnv   = Env{}
	extraOptions []expr.Option
)

// NewEnv shallow-copies the default values into a new Env.
func NewEnv() Env {
	return maps.Clone(defaultEnv)
}

func define[T any](name string, def T, optional bool) func(Env, T) {
	if name == "" {
		panic("define: name is required")
	}
	if _, ok := dummyEnv[name]; ok {
		panic("define: name is already used")
	}
	dummyEnv[name] = def
	if !optional {
		defaultEnv[name] = def
	}
	return func(e Env, v T) { e[name] = v }
}

// Define registers a variable with the provided name defaulting to the zero
// value. For parse-time expression type-checking to work, T should not be any.
func Define[T any](name string) func(Env, T) {
	var zero T
	return define[T](name, zero, false)
}

// DefineOptional registers an optional variable with the provided name. For parse-time
// expression type-checking to work, T should not be any.
func DefineOptional[T any](name string) func(Env, T) {
	var zero T
	return define[T](name, zero, true)
}

// DefineDefault registers a variable with the provided name. For parse-time
// expression type-checking to work, T should not be any.
func DefineDefault[T any](name string, def T) func(Env, T) {
	return define[T](name, def, false)
}

// DefineOperator overloads an operator.
func DefineOperator[T any](op string, fn T) string {
	name := op + " " + reflect.TypeOf(fn).String()
	extraOptions = append(extraOptions, expr.Operator(op, name))
	define[T](name, fn, false)
	return name
}

// DefineOperatorCompare is shorthand for using DefineOperator to override
// comparison operators.
func DefineOperatorCompare[A, B any](cmp func(a A, b B) int) []string {
	var ops []string
	ops = append(ops, DefineOperator("<", func(a A, b B) bool {
		return cmp(a, b) < 0
	}))
	ops = append(ops, DefineOperator("<=", func(a A, b B) bool {
		return cmp(a, b) <= 0
	}))
	ops = append(ops, DefineOperator("==", func(a A, b B) bool {
		return cmp(a, b) == 0
	}))
	ops = append(ops, DefineOperator("!=", func(a A, b B) bool {
		return cmp(a, b) != 0
	}))
	ops = append(ops, DefineOperator(">=", func(a A, b B) bool {
		return cmp(a, b) >= 0
	}))
	ops = append(ops, DefineOperator(">", func(a A, b B) bool {
		return cmp(a, b) > 0
	}))
	return ops
}
