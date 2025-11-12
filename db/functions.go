package db

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielbetoret/organization/services/api/src/reflection"
	"github.com/danielbetoret/organization/services/api/src/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var reflectInt = reflect.TypeOf(0)
var reflectUint = reflect.TypeOf(uint(0))
var reflectString = reflect.TypeOf("")
var reflectFloat32 = reflect.TypeOf(float32(0))
var reflectFloat64 = reflect.TypeOf(float64(0))
var reflectByteSlice = reflect.TypeOf([]byte(""))
var reflectTime = reflect.TypeOf(time.Now())

type ConnectionOpts struct {
	// default: 1 minute
	SchemaCacheInterval              utils.Optional[time.Duration]
	// default: 100
	BatchInsertOptimizationThreshold utils.Optional[int]
}

func NewDBWrapper(ctx *context.Context, conn *pgxpool.Pool, opts ConnectionOpts) DBWrapper {
	schemaCacheInterval := K_DefaultSchemaCacheInterval // default value is a minute
	if opts.SchemaCacheInterval.IsDefined() {
		schemaCacheInterval = opts.SchemaCacheInterval.Unwrap()
	}

	batchInsertOptimizationThreshold := K_DefaultBatchInsertOptimizationThreshold
	if opts.BatchInsertOptimizationThreshold.IsDefined() {
		batchInsertOptimizationThreshold = opts.BatchInsertOptimizationThreshold.Unwrap()
	}

	return DBWrapper{
		context: ctx,
		conn:    conn,
		schema:  utils.NewCachedMap[Table, TableConfig](schemaCacheInterval),
		config:  dbConfig{
			batchInsertOptimizationThreshold: batchInsertOptimizationThreshold,
		},
	}
}

func SanitizeIdentifier(ids ...string) identifier {
	return identifier(pgx.Identifier(ids).Sanitize())
}

func isCastableToDBType(v any, t DBType) bool {
	valueType := reflect.TypeOf(v)
	var allowed []reflect.Type

	switch t {
	case SmallInt, Int, BigInt:
		allowed = []reflect.Type{
			reflectInt,
			reflectUint,
			reflectString,
		}
	case Double, Real:
		allowed = []reflect.Type{
			reflectInt,
			reflectUint,
			reflectString,
			reflectFloat32,
			reflectFloat64,
		}
	case Bit, VarBit:
		allowed = []reflect.Type{
			reflectByteSlice,
		}
	case Char, VarChar, Text, JSON, XML:
		allowed = []reflect.Type{
			reflectString,
		}
	case Date, TimeWithoutTimezone, TimeWithTimezone, TimestampWithoutTimezone, TimestampWithTimezone:
		allowed = []reflect.Type{
			reflectTime,
		}
	default:
		return false
	}

	return slices.Contains(allowed, valueType)
}

func NewFilter(column Column, op operator, value any) *Filters {
	if len(column.Name) == 0 {
		panic(fmt.Errorf("db: column must not be empty"))
	}

	return &Filters{
		filter: filter{
			column: column,
			operator: op,
			value: value,
		},
	}
}

func JoinFilters(conj conjunction, filterSlice ...*Filters) *Filters {
	return &Filters{
		conj:     conj,
		children: filterSlice,
	}
}

func ShiftArgs(sql string, n int) string {
	re := regexp.MustCompile(`\$[0-9]+`)
	newSqlb := re.ReplaceAllFunc([]byte(sql), func(match []byte) []byte {
		agrPlaceholder := string(match)
		agrNumber, err := strconv.Atoi(strings.TrimPrefix(agrPlaceholder, "$"))
		if err != nil {
			panic(err)
		}

		return []byte(fmt.Sprintf("$%d", agrNumber+n))
	})

	return string(newSqlb)
}

func JoinSql(sql []string, args [][]any, sep string) (string, []any) {
    if (len(sql) != len(args)) {
        panic(fmt.Errorf("sql lines and slice of args should have the same length"))
    }

    newSql := ""
    newArgs := make([]any, 0)
    for i := 0; i < len(sql)-1; i++ {
        newSql += ShiftArgs(sql[i], len(newArgs)) + sep
        newArgs = append(newArgs, args[i]...)
    }

	// last item
	if len(sql) > 0 {
		newSql += ShiftArgs(sql[len(sql)-1], len(newArgs))
		newArgs = append(newArgs, args[len(args)-1]...)
	}

    return newSql, newArgs
}

func GetFieldMarshal(v reflect.Value, path reflection.Path, initializeNilPointers bool) (reflect.Value, error) {
	// check first if it is a marshaler
	marsh, ok := v.Interface().(DbMarshaler)
	if ok {
		newValue, err := marsh.MarshalDb()
		if err != nil {
			switch err.(type) {
			case utils.NoValueError:
				return reflect.Value{}, err
			default:
				panic(err)
			}
		}

		return GetFieldMarshal(
			reflect.ValueOf(newValue),
			path,
			initializeNilPointers,
		)
	}

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
				return reflect.Value{}, &reflection.InvalidValueErr{
					Value: v,
					Message: "if field is a map, it should be indexed by a string",
				}
			}

			return GetFieldMarshal(
				v.MapIndex(reflect.ValueOf(nextField)),
				append([]string{pointerCount}, cpath[1:]...),
				initializeNilPointers,
			)
		case reflect.Struct:
			return GetFieldMarshal(
				v.FieldByName(nextField),
				append([]string{pointerCount}, cpath[1:]...),
				initializeNilPointers,
			)
		default:
			return reflect.Value{}, &reflection.InvalidValueErr{
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
		return reflect.Value{}, &reflection.InvalidValueErr{
			Value: v,
			Message: fmt.Sprintf("value %v should be a pointer", v),
		}
	}

	if v.IsNil() {
		if !initializeNilPointers {
			return v, nil
		}

		if !v.CanSet() {
			return reflect.Value{}, &reflection.InvalidValueErr{
				Value: v,
				Message: fmt.Sprintf("pointer value %v is nil and not settable. If you want GetField to return <nil>, set <initializeNilPointers> to false", v),
			}
		}

		v.Set(reflect.New(v.Type().Elem()))
	}

	return GetFieldMarshal(
		v.Elem(),
		append([]string{strings.Replace(cpath[0], "*", "", 1)}, cpath[1:]...),
		initializeNilPointers,
	)
}

func pointsToKind(v reflect.Type, k reflect.Kind, recursive bool, ignoreOptionals bool) bool {
	if v.Implements(reflect.TypeFor[WrappedType]()) && ignoreOptionals {
		newValue, ok := reflect.Zero(v).Interface().(WrappedType)
		if !ok {
			panic(fmt.Errorf("db: type %v implements WrappedType but cannot be cast", v))
		}

		return pointsToKind(newValue.GetInnerType(), k, recursive, ignoreOptionals)
	}

	if v.Kind() == k {
		return true
	}

	if v.Kind() != reflect.Pointer {
		return false
	}

	if !recursive {
		return false
	}

	return pointsToKind(v.Elem(), k, recursive, ignoreOptionals)
}

func unwrapUntilKind(
	v reflect.Type,
	k reflect.Kind,
	recursive bool,
	ignoreOptionals bool,
) (typ reflect.Type, ptrCount int, err error) {
	if v.Implements(reflect.TypeFor[WrappedType]()) && ignoreOptionals {
		newValue, ok := reflect.Zero(v).Interface().(WrappedType)
		if !ok {
			panic(fmt.Errorf("db: type %v implements WrappedType but cannot be cast", v))
		}

		return unwrapUntilKind(newValue.GetInnerType(), k, recursive, ignoreOptionals)
	}

	if v.Kind() == k {
		return v, 0, nil
	}

	if v.Kind() != reflect.Pointer {
		return nil, 0, &InvalidValueError{
			Value: v,
			Message: fmt.Sprintf("db: value %v is not of kind %s and is not a pointer", v, k.String()),
		}
	}

	if !recursive {
		return nil, 0, &InvalidValueError{
			Value: v,
			Message: fmt.Sprintf("db: value %v is a pointer, but recursive is false", v),
		}
	}

	newTyp, count, err := unwrapUntilKind(v.Elem(), k, recursive, ignoreOptionals)
	if err != nil {
		return nil, 0, err
	}

	return newTyp, count+1, err
}

func parseDbMarshalOpts(tag string) (DbMarshalOpts, error) {
	optSlice := strings.Split(tag, ",")

	name := optSlice[0]

	spread := reflection.Optional[bool]{}
	i := slices.IndexFunc(optSlice, func(opt string) bool {
		return strings.HasPrefix(opt, "spread")
	})
	if i != -1 {
		if strings.HasPrefix(optSlice[i], "spread=") {
			val := strings.Replace(optSlice[i], "spread=", "", 1)

			switch (val) {
			case "true":
				spread = reflection.NewOptional(true)
			case "false":
				spread = reflection.NewOptional(false)
			default:
				return DbMarshalOpts{}, InvalidMarshalOptsError{
					Opt: "spread",
					Value: val,
				}
			}
		} else if (optSlice[i] == "spread") {
			spread = reflection.NewOptional(true)
		} else {
			return DbMarshalOpts{}, InvalidMarshalOptsError{
				Opt: "spread",
				Value: optSlice[i],
			}
		}
	}

	var prefix *string
	i = slices.IndexFunc(optSlice, func(opt string) bool {
		return strings.HasPrefix(opt, "prefix=")
	})
	if i != -1 {
		*prefix = strings.Replace(optSlice[i], "prefix=", "", 1)
	}

	return DbMarshalOpts{
		Name: name,
		Spread: spread,
		Prefix: prefix,
	}, nil
}
