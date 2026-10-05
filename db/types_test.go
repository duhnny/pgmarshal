package db

import (
	"testing"

	"github.com/duhnny/pgmarshal/reflection"
)

func TestMarshaler(t *testing.T) {
	op := reflection.NewOptional(true)

	_, ok := any(op).(DbMarshaler)
	if !ok {
		t.Errorf("db: optionals should be marshalers")
	}
}
