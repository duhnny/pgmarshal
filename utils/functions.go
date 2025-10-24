package utils

import (
	"fmt"
	"os"
)

// slices
func Filter[T any](s []T, testFunc func(T) bool) []T {
	var ret = make([]T, 0)
	for _, v := range s {
		if testFunc(v) {
			ret = append(ret, v)
		}
	}

	return ret
}

func Map[T any, U any](s []T, mapFunc func(T) U) []U {
	ret := make([]U, 0)
	for _, v := range s {
		ret = append(ret, mapFunc(v))
	}

	return ret
}

func IndexMap[T any, U any](s []T, mapFunc func(int, T) U) []U{
    ret := make([]U, 0)
    for i, v := range s {
        ret = append(ret, mapFunc(i, v))
    }

    return ret
}

func Pop[T any](s []T, i int) ([]T, T) {
	return append(s[:i], s[i+1:]...), s[i]
}

func NewSlice[T any](x T) []T {
	s := make([]T, 0)
	s = append(s, x)

	return s
}

// maps

// returns the first index
func KeyFunc[T comparable, U any](m map[T]U, f func(T, U) bool) *T {
	for k, v := range m {
		if f(k, v) {
			return &k
		}
	}

	return nil
}

func PrintInOut(f os.File, input any, output any) {
	f.Write([]byte(fmt.Sprintln("input: ", input)))
	f.Write([]byte(fmt.Sprintln("output: ", output)))
}
