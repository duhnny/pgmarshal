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

func NewDBWrapper(ctx *context.Context, conn *pgxpool.Pool) DBWrapper {
	return DBWrapper{
		context: ctx,
		conn:    conn,
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

func EmptyFilterSlice() []filter {
	return []filter{}
}

func GetOrderBy() OrderBy {
	return OrderBy{}
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

func JoinSql(sql []string, args [][]any) (string, []any) {
    if (len(sql) != len(args)) {
        panic(fmt.Errorf("sql lines and slice of args should have the same length"))
    }

    newSql := ""
    newArgs := make([]any, 0)
    for i := 0; i < len(sql); i++ {
        newSql += ShiftArgs(sql[i], len(newArgs)) + " "
        newArgs = append(newArgs, args[i]...)
    }

    return newSql, newArgs
}
