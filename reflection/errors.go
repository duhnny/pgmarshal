package reflection

import (
	"errors"
	"fmt"
)

var ErrInvalidStructPath = errors.New("path provided is not a valid struct path")
var ErrIndexOutOfRange = errors.New("index provided is out of range for struct path")
var ErrNoPtr = errors.New("path component is not a pointer")
var ErrNoneValueUnwrap = errors.New("reflection: tried to unwrap an undefined value")
var ErrNotAnOptional = errors.New("reflection: value provided is not an optional")
var ErrNoneValue = errors.New("reflection: optional has no value")

type InvalidValueErr struct {
	Value   any
	Message string
}

func (err *InvalidValueErr) Error() string {
	if (err.Message != "") {
        return fmt.Sprintf("reflection: %s", err.Message)
    }
    return fmt.Sprintf("reflection: value %v is not valid input", err.Value)
}

var EmptyFieldErr = errors.New("reflection: field should not be empty")

type FieldNotFoundErr struct {
	Field   string
	Value   any
	Message string
}

func (err *FieldNotFoundErr) Error() string {
	if (err.Message != "") {
        return fmt.Sprintf("reflection: %s", err.Message)
    }
    return fmt.Sprintf("reflection: field %s not found for value %v", err.Field, err.Value)
}
