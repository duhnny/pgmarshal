package db

import (
	"testing"

	"github.com/danielbetoret/organization/services/api/src/reflection"
)

func TestMarshaler(t *testing.T) {
	op := reflection.NewOptional(true)

	_, ok := any(op).(DbMarshaler)
	if !ok {
		t.Errorf("db: optionals should be marshalers")
	}
}
