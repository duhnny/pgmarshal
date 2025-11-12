package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danielbetoret/organization/services/api/src/utils"
)

type RowNotFoundError struct {
	Sql string
}

func (err *RowNotFoundError) Error() string {
	return fmt.Sprintf("db: No row found for query: %s", err.Sql)
}

type ColumnNotNullableError struct {
	column    string
	fieldName string
}

func (err *ColumnNotNullableError) Error() string {
	return fmt.Sprintf("db: Field %s must not be null", err.fieldName)
}

type TableNotFoundError struct {
	Table Table
}

func (err *TableNotFoundError) Error() string {
	return fmt.Sprintf("db: table %s not found in schema %s", err.Table.Name, err.Table.Schema)
}

type ColumnNotFoundError struct {
	Table  Table
	Column string
}

func (err *ColumnNotFoundError) Error() string {
	return fmt.Sprintf("db: Column %s not found in table %s of schema %s", err.Column, err.Table.Name, err.Table.Schema)
}

type InvalidValueError struct {
	Value   any
    Message string
}

func (err *InvalidValueError) Error() string {
    if err.Message != "" {
        return fmt.Sprintf("db: %s", err.Message)
    }
	return fmt.Sprintf("db: Invalid value %v", err.Value)
}

var NoMatchingColumnErr = errors.New("db: Value must have at least one field matching a column in the table")

type ConflictingOptsError struct {
    Opts []Opt
    Message string
}

func (err *ConflictingOptsError) Error() string {
    if err.Message != "" {
        return fmt.Sprintf("db: %s", err.Message)
    }
    return fmt.Sprintf("db: conflicting options: {%s}",
        strings.Join(utils.Map(err.Opts, func(opt Opt) string {
            return fmt.Sprintf("\"%s\": %v", opt.Name, opt.Value)
        }), ", "),
    )
}

var NoValuesErr = errors.New("db: You must pass in at least one value")

var ConnectionErr = errors.New("db: Failed to stablish a connection to the database")

type ExceededLimitError struct {
	RowCount int
	Limit    int
}

func (err *ExceededLimitError) Error() string {
	return fmt.Sprintf(
		"db: row count in query exceeded specified limit: %d > %d",
		err.RowCount,
		err.Limit,
	)
}

type InvalidMarshalOptsError struct {
	Opt   string
	Value string
}

func (err InvalidMarshalOptsError) Error() string {
	if err.Opt == "" {
		return "db: invalid marshal opts passed"
	}

	return fmt.Sprintf("db: invalid value for option %s: %s", err.Opt, err.Value)
}

type SyntaxError struct {
	Sql     string
	Args    []any
	Message string
}

func (err SyntaxError) Error() string {
	return err.Message
}
