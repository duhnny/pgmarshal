package utils

import (
	"encoding/json"
	"reflect"
	"sync"
)

// optionals
type Optional[T any] struct {
	value   *T
	defined bool
}

func NewOptional[T any](v T) Optional[T] {
	return Optional[T]{
		value: &v,
		defined: true,
	}
}

func (op *Optional[T]) IsDefined() bool {
	return op.defined
}

func (op Optional[T]) Unwrap() T {
	if !op.defined {
		panic(NoneValueUnwrapError{})
	}

	return *op.value
}

func (op Optional[T]) GetInnerType() reflect.Type {
	var v T
	return reflect.TypeOf(v)
}

// for json marshalling
func (op Optional[T]) IsZero() bool {
	return !op.defined
}

func (op Optional[T]) MarshalJSON() ([]byte, error) {
	if !op.defined {
		return nil, NoValueError{}
	}

	return json.Marshal(op.Unwrap())
}

func (op *Optional[T]) UnmarshalJSON(data []byte) error {
	var val *T

	err := json.Unmarshal(data, &val)
	if err != nil {
		return err
	}

	op.value = val
	op.defined = true

	return nil
}

func (op Optional[T]) GetDbType() reflect.Type {
	return reflect.TypeFor[T]()
}

func (op Optional[T]) MarshalDb() (any, error) {

	if !op.IsDefined() {
		var v T
		return v, NoValueError{}
	}

	return op.Unwrap(), nil
}

func (op *Optional[T]) Scan(src any) error {
	if src == nil {
		op.defined = false
		return nil
	}

	newValue, ok := src.(*T)
	if !ok {
		return WrongTypeError{
			Value: src,
			Type: reflect.TypeFor[T](),
		}
	}

	op.defined = true
	op.value = newValue

	return nil
}

func (op *Optional[T]) UnmarshalDb(v any) error {
	if v == nil {
		op.defined = false
		return nil
	}

	newValue, ok := v.(*T)
	if !ok {
		return WrongTypeError{
			Value: v,
			Type: reflect.TypeFor[T](),
		}
	}

	op.defined = true
	op.value = newValue

	return nil
}

// thread safe map
type SafeMap[T comparable, U any] struct {
	data map[T]U
	sync sync.RWMutex
}

func NewSafeMap[T comparable, U any]() SafeMap[T, U] {
	return SafeMap[T, U]{
		data: make(map[T]U),
	}
}

func (m *SafeMap[T, U]) Get(key T) Optional[U] {
	m.sync.Lock()
	defer m.sync.Unlock()
	
	v, ok := m.data[key]
	if !ok {
		return Optional[U]{}
	}

	return NewOptional(v)
}

func (m *SafeMap[T, U]) Set(key T, val U) {
	m.sync.RLock()
	defer m.sync.RUnlock()

	m.data[key] = val
}

func (m *SafeMap[T, U]) Del(key T) {
	m.sync.RLock()
	defer m.sync.RUnlock()

	delete(m.data, key)
}
