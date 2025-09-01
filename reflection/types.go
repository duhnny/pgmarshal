package reflection

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type Path []string

func NewPath(path ...string) Path {
	if len(path) == 0 {
		path = []string{""}
	}

	if len(strings.ReplaceAll(path[0], "*", "")) > 0 {
		path = append([]string{""}, path...)
	}

	out := Path(path)

	if !out.IsValid() {
		panic(ErrInvalidStructPath)
	}

	return out
}

func (p *Path) IsValid() bool {
	if p == nil || len(*p) == 0 {
		return false
	}

	if strings.ReplaceAll((*p)[0], "*", "") != "" {
		return false
	}

	for i := 1; i < len(*p); i++ {
		if !PathComponentRegex.Match([]byte((*p)[i])) {
			return false
		}
	}

	return true
}

func (p *Path) Get(pos int) string {
	if (pos < 0) {
		pos = len(*p)+pos
	}

	if (pos < 0 || pos >= len(*p)) {
		panic(ErrIndexOutOfRange)
	}

	return (*p)[pos]
}

func (p *Path) AddPtr(pos int) error {
	if !p.IsValid() {
		return ErrInvalidStructPath
	}

	if pos < 0 || pos >= len(*p) {
		return ErrIndexOutOfRange
	}

	(*p)[pos] = "*" + (*p)[pos]
	return nil
}

func (p *Path) DelPtr(pos int) error {
	if !p.IsValid() {
		return ErrInvalidStructPath
	}

	if pos < 0 || pos >= len(*p) {
		return ErrIndexOutOfRange
	}

	if !strings.Contains((*p)[pos], "*") {
		return ErrNoPtr
	}

	(*p)[pos] = strings.Replace((*p)[pos], "*", "", 1)
	return nil
}

func (p Path) ToString() string {
	return strings.Join(p, ".")
}

var Time = reflect.TypeOf(time.Time{})

// optionals
type Optional[T any] struct {
	Value   *T
	Defined bool
}

func (op Optional[T]) IsDefined() bool {
	return op.Defined
}

func (op Optional[T]) Unwrap() T {
	if !op.Defined {
		panic(ErrNoneValueUnwrap)
	}

	return *op.Value
}

func (op Optional[T]) GetInnerType() reflect.Type {
	var v T
	return reflect.TypeOf(v)
}

func (op *Optional[T]) UnmarshalJSON(data []byte) error {
	var val *T

	err := json.Unmarshal(data, &val)
	if err != nil {
		return err
	}

	op.Value = val
	op.Defined = true

	return nil
}

func (op Optional[T]) MarshalDb() (any, error) {

	if !op.IsDefined() {
		var v T
		return v, ErrNoneValue
	}

	return op.Unwrap(), nil
}

func (op *Optional[T]) UnmarshalDb(v any) error {
	if v == nil {
		op.Defined = false
		return nil
	}

	newValue, ok := v.(*T)
	if !ok {
		return WrongTypeError{
			Value: v,
			Type: reflect.TypeFor[T](),
		}
	}

	op.Defined = true
	op.Value = newValue

	return nil
}
