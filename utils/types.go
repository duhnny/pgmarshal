package utils

import "encoding/json"

type Optional[T any] struct {
	Value   *T
	Defined bool
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
