package db

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/danielbetoret/organization/services/api/src/reflection"
	"github.com/danielbetoret/organization/services/api/src/utils"
)

type DBType uint

const (
	SmallInt                 DBType = iota // 2 byte signed integer
	Int                                    // 4 byte signed integer
	BigInt                                 // 8 byte signed integer
	Double                                 // 8 byte float
	Real                                   // 4 byte float
	Bit                                    // bit string of fixed length
	VarBit                                 // bit string of varying length
	Char                                   // character string of fixed length
	VarChar                                // character string of varying length
	Text                                   // variable length character string
	JSON                                   // variable length JSON string
	XML                                    // variable length XML string
	Date                                   // calendar date (year, month, day)
	TimeWithoutTimezone                    // time of the day, no timezone
	TimeWithTimezone                       // time of the day, with timezone
	TimestampWithoutTimezone               // date and time, no timezone
	TimestampWithTimezone                  // date and time, with timezone
)

type conjunction uint

const (
	And conjunction = iota // represents AND in sql statements
	Or                     // represents OR in sql statements
)

func (conj *conjunction) write() string {
	switch *conj {
	case And:
		return "AND"
	case Or:
		return "OR"
	default:
		return "AND"
	}
}

type operator uint

const (
	Eq  operator = iota // represents "=" in sql statements
	Neq                 // represents "<>" in sql statements
	Gt                  // represents ">" in sql statements
	Gte                 // represents ">=" in sql statements
	Lt                  // represents "<" in sql statements
	Lte                 // represents "<=" in sql statements
	In                  // represents "IN" in sql statements
	Any                 // represents "= ANY " in sql statements
)

func (op *operator) write() string {
	switch *op {
	case Eq:
		return "="
	case Neq:
		return "<>"
	case Gt:
		return ">"
	case Gte:
		return ">="
	case Lt:
		return "<"
	case Lte:
		return "<="
	case In:
		return "IN"
	case Any:
		return "= ANY"
	default:
		return "="
	}
}

type DB interface {
	GetTableConfig(table Table) (TableConfig, error)

	Create(values any, table Table, opts QueryOpts) (int, error)
	Query(values any, table Table, opts QueryOpts) (int, error)
	Update(values any, table Table, opts QueryOpts) (int, error)
	Delete(table Table, opts QueryOpts) (int, error)

	CreateRow(value any, table Table, opts QueryOpts) (int, error)
	QueryRow(value any, table Table, opts QueryOpts) error

	QuerySql(values any, sql string, parameters ...any) (int, error)
	ExecuteSql(sql string, parameters ...any) (int, error)
}

type TableConfig struct {
	Table   Table
	Columns []ColumnConfig
}

type ColumnConfig struct {
	Name       string
	Type       string
	Position   int
	PrimaryKey bool
	Unique     bool
	Nullable   bool
	ForeignKey ForeignKey
}

func (columnConfig *ColumnConfig) IsForeignKey() bool {
	return columnConfig.ForeignKey.IsForeignKey
}

type ForeignKey struct {
	IsForeignKey     bool
	ReferencedTable  Table
	ReferencedColumn string
}

type Row interface {
	Unmarshall(v any) error
}

type identifier string

// filter
type filter struct {
	column   Column
	operator operator
	value    any
}

type Filters struct {
	filter   filter
	conj     conjunction
	children []*Filters
}

func (f *Filters) Args() []any {
	if len(f.children) == 0 {
		// if right side of comparison is an identifier, use it as an identifier, instead of a value
		// this allows for using columns in the right side, for example
		if reflect.ValueOf(f.filter.value).Type() == reflect.TypeFor[identifier]() {
			return []any{}
		}
		return []any{f.filter.value}
	}

	return slices.Concat(utils.Map(f.children, func(c *Filters) []any {
		return c.Args()
	})...)
}

// TODO: this can be optimized
func (f *Filters) Sql() string {
	if len(f.children) == 0 {
		// if right side of comparison is an identifier, use it as an identifier, instead of a value
		// this allows for using columns in the right side, for example
		rside := ""
		if reflect.ValueOf(f.filter.value).Type() == reflect.TypeFor[identifier]() {
			rside = f.filter.value.(string)
		} else {
			rside = "$1"
		}

		if f.filter.operator == Any && reflect.ValueOf(f.filter.value).Kind() == reflect.Slice {
			rside = "(" + rside + ")"
		}

		return fmt.Sprintf(
			"%s %s %s",
			SanitizeIdentifier(
				f.filter.column.Table.Schema,
				f.filter.column.Table.Name,
				f.filter.column.Name,
			),
			f.filter.operator.write(),
			rside,
		)
	}

	sql := f.children[0].Sql()
	argCount := len(f.children[0].Args())
	for i := 1; i < len(f.children); i++ {
		sql += " " + f.conj.write() + " " + ShiftArgs(f.children[i].Sql(), argCount)
		argCount += len(f.children[i].Args())
	}

	return sql
}

// returns all columns involved in the filters
func (f *Filters) Columns() []Column {
	if len(f.children) == 0 {
		return []Column{f.filter.column}
	}

	return slices.Concat(utils.Map(f.children, func(c *Filters) []Column {
		return c.Columns()
	})...)
}

type OrderBy struct {
	Column Column
	Desc   bool
}

type Table struct {
	Schema string
	Name   string
}

func (t *Table) Sanitize() identifier {
	return SanitizeIdentifier(t.Schema, t.Name)
}

type Column struct {
	Table Table
	Name  string
}

type Opt struct {
	Name  string
	Value any
}

type QueryOpts struct {
	Filters   *Filters
	OrderBy   *OrderBy
	Limit     int
	Unique    bool
	AllowNone bool
	Recursive bool
	Fields    []reflection.Path
	AllFields bool
}

type WrappedType interface {
	GetInnerType() reflect.Type
}

type DbModel interface {
	GetDbType() reflect.Type
}

type DbMarshaler interface {
	DbModel
	MarshalDb() (any, error)
}

type DbUnmarshaler interface {
	DbModel
	UnmarshalDb(v any) error
}

type DbMarshalOpts struct {
	Name   string
	Spread reflection.Optional[bool]
	Prefix *string
}
