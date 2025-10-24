package myerrors

import "errors"

func Contains[T error](err error) bool {
	var inner T

	return errors.As(err, &inner)
}

func Get[T error](err error) T {
	var inner T

	errors.As(err, &inner)

	return inner
}
