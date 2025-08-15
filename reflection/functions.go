package reflection

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func GetFields(t reflect.Type) []reflect.StructField {
	fields := make([]reflect.StructField, 0)
	for i := 0; i < t.NumField(); i++ {
		fields = append(fields, t.Field(i))
	}

	return fields
}

func IsNull(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return true
		} else {
			return IsNull(v.Elem())
		}
	default:
		return v.IsZero()
	}
}

func PathFromString(s string) (Path, error) {
	path := NewPath(strings.Split(s, ".")...)

	if !path.IsValid() {
		return Path{}, ErrInvalidStructPath
	}

	return path, nil
}

func PointsToKind(v reflect.Type, k reflect.Kind, recursive bool) bool {
	if v.Kind() != reflect.Pointer {
		return false
	}

	if v.Elem().Kind() != k {
		if v.Elem().Kind() == reflect.Pointer && recursive {
			return PointsToKind(v.Elem(), k, recursive)
		}

		return false
	}

	return true
}

func IsSubpath(sub Path, path Path, ignorePointers bool) bool {
	// clone to avoid changing original paths
	csub := slices.Clone(sub)
	cpath := slices.Clone(path)

	if ignorePointers {
		for i, comp := range csub {
			csub[i] = strings.ReplaceAll(comp, "*", "")
		}
		for i, comp := range cpath {
			cpath[i] = strings.ReplaceAll(comp, "*", "")
		}
	}

	if csub[0] == "" {
		csub = slices.Delete(csub, 0, 1)
	}
	if cpath[0] == "" {
		cpath = slices.Delete(cpath, 0, 1)
	}

	if len(csub) > len(cpath) {
		return false
	}

	for i, part := range csub {
		if part != cpath[i] {
			return false
		}
	}

	return true
}

func GetField(v reflect.Value, path Path, initializeNilPointers bool) (reflect.Value, error) {
	// create copy
	cpath := slices.Clone(path)

    if cpath[0] == "" {
        cpath = slices.Delete(cpath, 0, 1)
    }

    if len(cpath) == 0 {
        return v, nil
    }

    // check for field
	nextField := strings.ReplaceAll(cpath[0], "*", "")
	pointerCount := strings.Repeat("*", strings.Count(cpath[0], "*"))
    if len(nextField) > 0 {
		switch (v.Kind()) {
		case reflect.Map:
			_, ok := v.Interface().(map[string]any)
			if !ok {
				return reflect.Value{}, &InvalidValueErr{
					Value: v,
					Message: "if field is a map, it should be indexed by a string",
				}
			}

			return GetField(
				v.MapIndex(reflect.ValueOf(nextField)),
				append([]string{pointerCount}, cpath[1:]...),
				initializeNilPointers,
			)
		case reflect.Struct:
			return GetField(
				v.FieldByName(nextField),
				append([]string{pointerCount}, cpath[1:]...),
				initializeNilPointers,
			)
		default:
			return reflect.Value{}, &InvalidValueErr{
				Value: v,
				Message: fmt.Sprintf("value %v does not have field %s", v, cpath[0]),
			}
		}
    }

	// if we arrive here, it means there is no field name in path[0], and
	// therefore, because path[0] != "", pointerCount != 0
	if len(pointerCount) == 0 {
		panic(fmt.Errorf("something went wrong, we should not be here"))
	}

	if v.Kind() != reflect.Pointer {
		return reflect.Value{}, &InvalidValueErr{
			Value: v,
			Message: fmt.Sprintf("value %v should be a pointer", v),
		}
	}

	if v.IsNil() {
		if !initializeNilPointers {
			return v, nil
		}

		if !v.CanSet() {
			return reflect.Value{}, &InvalidValueErr{
				Value: v,
				Message: fmt.Sprintf("pointer value %v is nil and not settable. If you want GetField to return <nil>, set <initializeNilPointers> to false", v),
			}
		}

		v.Set(reflect.New(v.Type().Elem()))
	}

	return GetField(
		v.Elem(),
		append([]string{strings.Replace(cpath[0], "*", "", 1)}, cpath[1:]...),
		initializeNilPointers,
	)
}

func GetFieldType(t reflect.Type, path Path) (reflect.Type, error) {
	// create copy
	cpath := slices.Clone(path)

    if cpath[0] == "" {
        cpath = slices.Delete(cpath, 0, 1)
    }

    if len(cpath) == 0 {
        return t, nil
    }

    // check for field
	nextField := strings.ReplaceAll(cpath[0], "*", "")
	pointerCount := strings.Repeat("*", strings.Count(cpath[0], "*"))
    if len(nextField) > 0 {
		switch (t.Kind()) {
		case reflect.Map:
			return GetFieldType(
				t.Elem(),
				append([]string{pointerCount}, cpath[1:]...),
			)
		case reflect.Struct:
			nextStructField, ok := t.FieldByName(nextField)
			if !ok {
				return reflect.TypeOf(nil), &InvalidValueErr{
					Value: t,
					Message: fmt.Sprintf("type %v does not have field %s", t, cpath[0]),
				}
			}

			return GetFieldType(
				nextStructField.Type,
				append([]string{pointerCount}, cpath[1:]...),
			)
		default:
			return reflect.TypeOf(nil), &InvalidValueErr{
				Value: t,
				Message: fmt.Sprintf("type %v does not have field %s", t, cpath[0]),
			}
		}
    }

	// if we arrive here, it means there is no field name in path[0], and
	// therefore, because path[0] != "", pointerCount != 0
	if len(pointerCount) == 0 {
		panic(fmt.Errorf("something went wrong, we should not be here"))
	}

	if t.Kind() != reflect.Pointer {
		return reflect.TypeOf(nil), &InvalidValueErr{
			Value: t,
			Message: fmt.Sprintf("type %v should be a pointer", t),
		}
	}

	return GetFieldType(
		t.Elem(),
		append([]string{strings.Replace(cpath[0], "*", "", 1)}, cpath[1:]...),
	)

}

func GetStructField(t reflect.Type, path Path) (reflect.StructField, error) {
	if !path.IsValid() || len(path) == 1 {
		return reflect.StructField{}, ErrInvalidStructPath
	}

	// create copy
	cpath := slices.Clone(path)

	switch (t.Kind()) {
	case reflect.Pointer:
		if !strings.Contains(cpath[0], "*") {
			return reflect.StructField{}, &InvalidValueErr{
				Value: t,
				Message: fmt.Sprintf("type %v should not be a pointer", t),
			}
		}

		cpath[0] = strings.Replace(cpath[0], "*", "", 1)
		return GetStructField(t.Elem(), cpath)
	case reflect.Struct:
		// deal with it below
	default:
		return reflect.StructField{}, &InvalidValueErr{
			Value: t,
			Message: fmt.Sprintf("type %v is not a pointer or a struct", t),
		}
	}

	// if we get here, t is a struct type
	if strings.Contains(cpath[0], "*") {
		return reflect.StructField{}, &InvalidValueErr{
			Value: t,
			Message: fmt.Sprintf("type %v should be a pointer", t),
		}
	}

	nextField := strings.ReplaceAll(cpath[1], "*", "")
	nextPath := NewPath(append([]string{strings.Repeat("*", strings.Count(cpath[1], "*"))}, cpath[2:]...)...)
	for i := range t.NumField() {
		if t.Field(i).Name == nextField {
			switch (t.Field(i).Type.Kind()) {
			case reflect.Struct, reflect.Pointer:
				if t.Field(i).Type == Time {
					break
				}
				return GetStructField(t.Field(i).Type, nextPath)
			}

			if len(nextPath) > 1 {
				return reflect.StructField{}, &InvalidValueErr{
					Value: t.Field(i).Type,
					Message: fmt.Sprintf("type %v should be a pointer or a struct", t),
				}
			}

			return t.Field(i), nil
		}
	}

	return reflect.StructField{}, &FieldNotFoundErr{
		Field: nextField,
		Value: t,
	}
}

func StripNil(v any) (any, error) {
	rv := reflect.ValueOf(v)
	rt := reflect.TypeOf(v)

	if rt.Kind() != reflect.Struct {
		return nil, &InvalidValueErr{
			Value: v,
			Message: fmt.Sprintf("value %v sould be a struct", v),
		}
	}

	fields := make([]reflect.StructField, 0)
	for i := range rt.NumField() {
		if rv.Field(i).Kind() != reflect.Pointer {
			fields = append(fields, rt.Field(i))
		} else if !rv.Field(i).IsNil() {
			fields = append(fields, rt.Field(i))
		}
	}

	newStructType := reflect.StructOf(fields)
	newStruct := reflect.New(newStructType)

	for _, field := range fields {
		newStruct.Elem().FieldByName(field.Name).Set(rv.FieldByName(field.Name))
	}

	return newStruct.Elem().Interface(), nil
}
