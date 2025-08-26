package reflection

import (
	"reflect"
)

func NewOptional[T any](v T) Optional[T] {
	return Optional[T]{
		Value: &v,
		Defined: true,
	}
}

func IsOptional(v any) bool {
	rt := reflect.TypeOf(v)

	// check IsDefined method
	isDefined, ok := rt.MethodByName("IsDefined")
	if !ok {
		return false
	}

	if isDefined.Type.NumIn() != 1 ||
		isDefined.Type.NumOut() != 1 ||
		isDefined.Type.Out(0).Kind() != reflect.Bool {
		return false
	}

	// check Unwrap method
	unwrap, ok := rt.MethodByName("Unwrap")
	if !ok {
		return false
	}

	if unwrap.Type.NumIn() != 1 ||
		unwrap.Type.NumOut() != 1 {
		return false
	}

	// check GetInnerType
	getInnerType, ok := rt.MethodByName("GetInnerType")
	if !ok {
		return false
	}

	if getInnerType.Type.NumIn() != 1 ||
		getInnerType.Type.NumOut() != 1 ||
		getInnerType.Type.Out(0) != reflect.TypeFor[reflect.Type]() {
		return false
	}

	return true
}

func StripOptionals(v any) (any, error) {
	rv := reflect.ValueOf(v)
	rt := reflect.TypeOf(v)

	switch (rv.Kind()) {
	case reflect.Struct:
		if IsOptional(v) {
			isDefined, ok := rt.MethodByName("IsDefined")
			if !ok {
				panic(ErrNotAnOptional)
			}

			hasValue := isDefined.Func.Call([]reflect.Value{rv})[0].Interface().(bool)
			if !hasValue {
				return nil, ErrNoneValue
			}

			unwrap, ok := rt.MethodByName("Unwrap")
			if !ok {
				panic(ErrNotAnOptional)
			}

			unwrappedValue := unwrap.Func.Call([]reflect.Value{rv})[0]
			return unwrappedValue.Interface(), nil
		}


		values := make([]any, 0)
		fields := make([]reflect.StructField, 0)
		for i := range rt.NumField() {
			field := rv.Field(i)

			unwrappedValue, err := StripOptionals(field.Interface())
			if err != nil {
				switch (err) {
				case ErrNoneValue:
					continue
				default:
					return nil, err
				}
			}

			values = append(values, unwrappedValue)
			fields = append(fields, reflect.StructField{
				Name: rt.Field(i).Name,
				Type: reflect.ValueOf(unwrappedValue).Type(),
				Tag: rt.Field(i).Tag,
			})
		}

		out := reflect.New(reflect.StructOf(fields)).Elem()
		for i := range values {
			out.FieldByName(fields[i].Name).Set(reflect.ValueOf(values[i]))
		}

		return out.Interface(), nil
	default:
		return v, nil
	}
}
