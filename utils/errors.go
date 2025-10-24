package utils

import (
	"fmt"
	"reflect"
)

type MultiError[T error] struct {
	Errors []T
}

func (err MultiError[T]) Error() string {
	message := "multiple errors occurred:"

	for _, inner := range err.Errors {
		message += " " + inner.Error() + ";"
	}

	return message
}

type NoneValueUnwrapError struct {}

func (err NoneValueUnwrapError) Error() string {
	return "attempted to unwrap undefined optional"
}

type NoValueError struct {}

func (err NoValueError) Error() string {
	return "no value in struct"
}

type WrongTypeError struct {
	Value   any
	Type    reflect.Type
	Message string
}

func (err WrongTypeError) Error() string {
	message := fmt.Sprintf("reflection: value %v is of wrong type for optional of type %v", err.Value, err.Type)

	if message != "" {
		message += ": " + err.Message
	}

	return message
}
