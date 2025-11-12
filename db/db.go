package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/danielbetoret/organization/services/api/src/myerrors"
	"github.com/danielbetoret/organization/services/api/src/reflection"
	"github.com/danielbetoret/organization/services/api/src/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dbConfig struct {
	batchInsertOptimizationThreshold int
}

type DBWrapper struct {
	context *context.Context
	conn    *pgxpool.Pool
	schema  utils.CachedMap[Table, TableConfig]
	config  dbConfig
}

func (db *DBWrapper) GetTableConfig(table Table) (TableConfig, error) {
	// check first if schema is in cache
	cachedConfig := db.schema.Get(table)
	if cachedConfig.IsDefined() {
		return cachedConfig.Unwrap(), nil
	}

	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return TableConfig{}, ConnectionErr
	}
	defer conn.Release()

	columnsQueryResults, err := conn.Query(*db.context, `
		SELECT
			table_schema,
			table_name,
			column_name,
			ordinal_position,
			is_nullable,
			data_type
		FROM information_schema.columns
		WHERE
			table_schema = $1
			AND
			table_name = $2
	`, table.Schema, table.Name)
	if err != nil {
		panic(err)
	}
    defer columnsQueryResults.Close() // this is redundant, but good practice

	var columnsConfig = make([]ColumnConfig, 0)
	for columnsQueryResults.Next() {
		var table_schema string
		var table_name string
		var columnConfig ColumnConfig
		var nullable string
		err = columnsQueryResults.Scan(
			&table_schema,
			&table_name,
			&columnConfig.Name,
			&columnConfig.Position,
			&nullable,
			&columnConfig.Type,
		)
		if err != nil {
			panic(err)
		}

		switch nullable {
		case "YES":
			columnConfig.Nullable = true
		case "NO":
			columnConfig.Nullable = false
		}

		columnsConfig = append(columnsConfig, columnConfig)
	}
	
	// release connection for reuse
	columnsQueryResults.Close()

	// if no columns are found, we assume the table does not exist
	if len(columnsConfig) == 0 {
		return TableConfig{}, &TableNotFoundError{
			Table: table,
		}
	}

	constraintsQueryResults, err := conn.Query(*db.context, `
		SELECT
			ccu.column_name,
			tc.constraint_type
		FROM
			information_schema.table_constraints AS tc
		JOIN
			information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
		WHERE
			tc.table_schema = $1
			AND
			tc.table_name = $2
			AND
			tc.constraint_type != 'FOREIGN KEY'
	`, table.Schema, table.Name)
	if err != nil {
		panic(err)
	}
	defer constraintsQueryResults.Close() // this is redundant, but good practice

	for constraintsQueryResults.Next() {
		var column_name string
		var constraint_type string
		constraintsQueryResults.Scan(&column_name, &constraint_type)

		index := slices.IndexFunc(columnsConfig, func(columnConfig ColumnConfig) bool {
			return columnConfig.Name == column_name
		})
		if index == -1 {
			return TableConfig{}, fmt.Errorf("constraints: column %s not found in table %s\nTableConfig: %v", column_name, table.Name, columnsConfig)
		}

		switch constraint_type {
		case "PRIMARY KEY":
			columnsConfig[index].PrimaryKey = true
			columnsConfig[index].Unique = true
		case "UNIQUE":
			columnsConfig[index].Unique = true
		case "FOREIGN KEY":
		}
	}

	// release connection for reuse
	constraintsQueryResults.Close()

	// query from https://stackoverflow.com/a/1152321
	foreignKeyQueryResults, err := conn.Query(*db.context, `
		SELECT
			kcu.column_name,
			ccu.table_schema AS foreign_table_schema,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name 
		FROM information_schema.table_constraints AS tc 
		JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
	`, table.Schema, table.Name)
	if err != nil {
		panic(err)
	}
	defer foreignKeyQueryResults.Close()

	for foreignKeyQueryResults.Next() {
		var column_name string
		var foreign_table_schema string
		var foreign_table_name string
		var foreign_column_name string
		err = foreignKeyQueryResults.Scan(
			&column_name,
			&foreign_table_schema,
			&foreign_table_name,
			&foreign_column_name,
		)
		if err != nil {
			panic(err)
		}

		index := slices.IndexFunc(columnsConfig, func(columnConfig ColumnConfig) bool {
			return columnConfig.Name == column_name
		})
		if index == -1 {
			return TableConfig{}, fmt.Errorf("constraints: column not found")
		}

		columnsConfig[index].ForeignKey.IsForeignKey = true
		columnsConfig[index].ForeignKey.ReferencedTable = Table{Schema: foreign_table_schema, Name: foreign_table_name}
		columnsConfig[index].ForeignKey.ReferencedColumn = foreign_column_name
	}

	// sort columnsConfig slice by the order of the columns
	slices.SortStableFunc(columnsConfig, func(a ColumnConfig, b ColumnConfig) int {
		switch {
		case a.Position < b.Position:
			return -1
		case a.Position > b.Position:
			return 1
		default:
			return 0
		}
	})

	tc := TableConfig{
		Table:   table,
		Columns: columnsConfig,
	}

	// put in cache
	db.schema.Set(table, tc)

	return tc, nil
}

// crud methods
type insert struct {
	table   Table
	columns []Column
	values  [][]any
}

type insertTable struct {
	table   Table
	columns []Column
}

func (ins insert) write() (string, []any, error) {
	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES\n",
		ins.table.Sanitize(),
		strings.Join(utils.Map(ins.columns, func(c Column) string {
			return string(SanitizeIdentifier(c.Name))
		}), ", "),
	)

	valuesSql, args := JoinSql(
		utils.Map(ins.values, func(values []any) string {
			placeholders := make([]string, 0)

			for i := range values {
				placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
			}

			return "(" + strings.Join(placeholders, ", ") + ")"
		}),
		ins.values,
		",\n",
	)

	return sql + valuesSql + ";", args, nil
}

func (db *DBWrapper) Create(values any, table Table, opts QueryOpts) (int, error) {
	// check types first
    s := reflect.ValueOf(values)

	if s.Kind() != reflect.Slice || s.Type().Elem().Kind() != reflect.Struct {
		return 0, &InvalidValueError{
			Value: values,
			Message: "values should be a pointer to a slice of structs",
		}
	}

    // define useful variables
    t := s.Type().Elem()

	// require at least one value
	if s.Len() == 0 {
		if opts.AllowNone {
			return 0, nil
		}
		return 0, NoValuesErr
	}

	// first of all, gather necessary data
	structMap := structTreeNode{}

	err := createStructMap(
		db,
		table,
		Column{},
		t,
		reflection.NewPath(),
		&structMap,
		DbMarshalOpts{},
		opts,
	)
	if err != nil {
		return 0, fmt.Errorf("db: found the following error while gathering data for the query: %w", err)
	}

	columns := make([]Column, 0)
	columnMap := make(map[Column]*reflection.Path)
	joins := make([]join, 0)

	err = structMap.gatherQueryData(reflection.NewPath(""), &columns, &columnMap, &joins)
	if err != nil {
		return 0, err
	}

	// fill in both the joined column and the original column
	for _, join := range joins {
		if slices.Contains(columns, join.joined) && !slices.Contains(columns, join.original) {
			columns = append(columns, join.original)
			columnMap[join.original] = columnMap[join.joined]
		} else if slices.Contains(columns, join.original) && !slices.Contains(columns, join.joined) {
			columns = append(columns, join.joined)
			columnMap[join.joined] = columnMap[join.original]
		}
	}

	// collect tables for insert
	insertTables := make([]insertTable, 0)

	// first, the original table
	originalTable := insertTable{
		table: table,
		columns: make([]Column, 0),
	}
	for _, col := range columns {
		if col.Table == table {
			originalTable.columns = append(originalTable.columns, col)
		}
	}
	insertTables = append(insertTables, originalTable)

	// now, the jois
	for _, join := range joins {
		joinedTable := insertTable{
			table: join.joined.Table,
			columns: make([]Column, 0),
		}

		for _, col := range columns {
			if col.Table == joinedTable.table {
				joinedTable.columns = append(joinedTable.columns, col)
			}
		}
	}

	// create inserts for each table
	// we run it in the reverse order of the joins, because
	rawInserts := make([]insert, 0)
	for i := len(insertTables)-1; i >= 0; i-- {
		tb := insertTables[i]

		for j := range s.Len() {
			ins := insert{
				table: tb.table,
				columns: make([]Column, 0),
				values: make([][]any, 0),
			}

			val := make([]any, 0)

			columnLoop:
			for _, col := range tb.columns {
				fieldValue, err := GetFieldMarshal(
					s.Index(j),
					*columnMap[col],
					false,
				)
				if err != nil {
					// in case of an optional with no value, do not insert column
					if myerrors.Contains[utils.NoValueError](err) {
						continue columnLoop
					}

					return 0, err
				}

				val = append(val, fieldValue.Interface())
				ins.columns = append(ins.columns, col)
			}

			ins.values = append(ins.values, val)
			rawInserts = append(rawInserts, ins)
		}
	}

	// the following is not necessary, but it is a nice optimization for batch inserts:
	// instead of generating sql for each row to be inserted, we will collect the
	// inserts with the same columns, and create one insert statement for each one
	inserts := make([]insert, 0)
	if (s.Len() > db.config.batchInsertOptimizationThreshold) {
		for _, rawInsert := range rawInserts {
			i := slices.IndexFunc(inserts, func(insert insert) bool {
				return slices.Equal(insert.columns, rawInsert.columns)
			})

			// if group of columns does not exist, append
			// append only values otherwise
			// obs.: the order of the columns will always be the same
			// in virtue of the process taken to build the inserts
			if i == -1 {
				inserts = append(inserts, rawInsert)
			} else if len(rawInsert.values) != 0 {
				inserts[i].values = append(inserts[i].values, rawInsert.values...)
			}
		}
	} else {
		inserts = rawInserts
	}

	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	defer conn.Release()

	// begin transaction
	tx, err := conn.Begin(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	
	rowsAffected := 0
	for _, ins := range inserts {
		sql, args, err := ins.write()
		if err != nil {
			return 0, fmt.Errorf("db: failed to write sql with error: %w", err)
		}

		ctag, err := tx.Exec(*db.context, sql, args...)
		if err != nil {
			tx.Rollback(*db.context)
			return 0, SqlError{
				Sql: sql,
				Args: args,
				Err: err,
			}
		}

		rowsAffected += int(ctag.RowsAffected())
	}

	err = tx.Commit(*db.context)
	if err != nil {
		tx.Rollback(*db.context)
		return 0, err
	}
	
	return rowsAffected, nil
}

func (db *DBWrapper) CreateRow(value any, table Table, opts QueryOpts) (int, error) {
	rv := reflect.ValueOf(value)
	
	if rv.Kind() != reflect.Struct {
		return 0, &InvalidValueError{
			Value: value,
			Message: "value should be a struct",
		}
	}

	s := reflect.New(reflect.SliceOf(rv.Type()))
	s.Elem().Set(reflect.Append(s.Elem(), rv))

	return db.Create(s.Elem().Interface(), table, opts)
}

func (db *DBWrapper) Query(values any, table Table, opts QueryOpts) (int, error) {
	// check types first
    rv := reflect.ValueOf(values)

	if rv.Kind() != reflect.Pointer {
		return 0, &InvalidValueError{
			Value: values,
			Message: "values should be a pointer to a slice of structs",
		}
	}

	if rv.IsNil() {
		return 0, &InvalidValueError{
			Value: values,
			Message: "nil pointer provided",
		}
	}

	if rv.Type().Elem().Kind() != reflect.Slice || rv.Type().Elem().Elem().Kind() != reflect.Struct {
		return 0, &InvalidValueError{
			Value: values,
			Message: "values should be a pointer to a slice of structs",
		}
	}

    // define useful variables
	s := rv.Elem()
    t := rv.Type().Elem().Elem()

    // get all data necessary to make the query in one go
	structMap := structTreeNode{}

    err := createStructMap(
        db,
        table,
		Column{},
        t,
        reflection.NewPath(),
		&structMap,
		DbMarshalOpts{},
        opts,
    )
    if err != nil {
        return 0, err
    }

	columns := make([]Column, 0)
	columnMap := make(map[Column]*reflection.Path)
	joins := make([]join, 0)
	err = structMap.gatherQueryData(
		reflection.NewPath(""),
		&columns,
		&columnMap,
		&joins,
	)
	if err != nil {
		return 0, fmt.Errorf("db: failed to gather data for query with error: %w", err)
	}
    if len(columns) == 0 {
        return 0, NoMatchingColumnErr
    }

    // build sql
    selectSql := "SELECT " + strings.Join(
        utils.Map(
            columns,
            func(c Column) string {
                return pgx.Identifier([]string{
                    c.Table.Schema,
                    c.Table.Name,
                    c.Name,
                }).Sanitize()
            },
        ),
        ", ",
    )

    fromSql := fmt.Sprintf(
        "FROM %s %s",
        pgx.Identifier([]string{table.Schema, table.Name}).Sanitize(),
        strings.Join(
            utils.IndexMap(
                joins,
                func(i int, j join) string {
                    return fmt.Sprintf(
                        "LEFT JOIN %s ON %s = %s",
                        pgx.Identifier([]string{
                            j.joined.Table.Schema,
                            j.joined.Table.Name,
                        }).Sanitize(),
                        pgx.Identifier([]string{
                            j.original.Table.Schema,
                            j.original.Table.Name,
                            j.original.Name,
                        }).Sanitize(),
                        pgx.Identifier([]string{
                            j.joined.Table.Schema,
                            j.joined.Table.Name,
                            j.joined.Name,
                        }).Sanitize(),
                    )
                },
            ),
            " ",
        ),
    )

    whereSql := ""
    whereArgs := make([]any, 0)
    if opts.Filters != nil {
        whereSql = fmt.Sprintf("WHERE %s", opts.Filters.Sql())
        whereArgs = opts.Filters.Args()
    }

    orderBySql := ""
    if opts.OrderBy != nil {
		order := ""
        if opts.OrderBy.Desc {
			order = "DESC"
        } else {
			order = "ASC"
        }
        orderBySql = fmt.Sprintf(
			"ORDER BY %s %s",
			pgx.Identifier([]string{
            	opts.OrderBy.Column.Table.Schema,
            	opts.OrderBy.Column.Table.Name,
            	opts.OrderBy.Column.Name,
        	}).Sanitize(),
			order,
		)
    }

    limitSql := ""
    limitArgs := make([]any, 0)
    if opts.Limit != 0 {
        limitSql = "LIMIT $1"
        limitArgs = append(limitArgs, opts.Limit)
    }

    finalSql, finalArgs := JoinSql(
        []string{
            selectSql,
            fromSql,
            whereSql,
            orderBySql,
            limitSql,
        },
        [][]any{
            {},
            {},
            whereArgs,
			{},
            limitArgs,
        },
		" ",
    )

    finalSql += ";"

	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	defer conn.Release()

    // actually query db
    rows, err := conn.Query(*db.context, finalSql, finalArgs...)
    if err != nil {
		return 0, SqlError{
			Sql: finalSql,
			Args: finalArgs,
			Err: err,
		}
    }

    i := 0
    for rows.Next() {
        addres := make([]any, 0)
		valueMap := make(map[string]reflect.Value)

		newValue := reflect.New(t).Elem()

        for _, column := range columns {
            valueType, err := reflection.GetFieldType(t, *columnMap[column])
            if err != nil {
                panic(err)
            }
			valueMap[(columnMap[column]).ToString()] = reflect.New(reflect.PointerTo(valueType))
            addres = append(addres, valueMap[columnMap[column].ToString()].Interface())
        }

        err = rows.Scan(addres...)
        if err != nil {
            return 0, err
        }

		for _, column := range columns {
			// if is is nil, we find out if the value itself is a pointer or
			// if the parent is a pointer (in case of a foreign key)
			if valueMap[columnMap[column].ToString()].Elem().IsNil() {
				path := slices.Clone(*columnMap[column])
				l := len(path)

				// check first if the value is a foreign key
				path = slices.Delete(path, l-1, l)
				l = l-1
				path[l-1] = strings.ReplaceAll(path.Get(-1), "*", "")

				parent, err := structMap.Get(path)
				if err != nil {
					panic(err)
				}

				parentValue, err := reflection.GetField(newValue, path, false)
				if err != nil {
					panic(err)
				}

				child, err := structMap.Get(*columnMap[column])
				if err != nil {
					panic(err)
				}

				if !reflect.ValueOf(parent.joinedColumn).IsZero() {
					if parent.joinedColumn != child.column {
						// in case the column is not the primary key
						continue
					}

					// if it is, check if parent is nullable
					if parentValue.Kind() != reflect.Pointer {
						return 0, &InvalidValueError{
							Value: parentValue,
							Message: fmt.Sprintf(
								"db: value at path %s cannot be set to <nil>",
								strings.Join(path, "."),
							),
						}
					}

					// if it is nullable, set to nil
					if !parentValue.CanSet() {
						return 0, &InvalidValueError{
							Value: parentValue,
							Message: fmt.Sprintf("Cannot set value in struct path %s", path),
						}
					}
					parentValue.Set(reflect.Zero(parentValue.Type()))
					continue
				}

				// this only happens if there is no join, but the column is a foreign key
				if parent.column == child.column {
					// in this case, check if parent is nullable
					if parentValue.Kind() != reflect.Pointer {
						return 0, &InvalidValueError{
							Value: parentValue,
							Message: fmt.Sprintf(
								"db: value at path %s cannot be set to <nil>",
								strings.Join(path, "."),
							),
						}
					}

					// if it is nullable, set to nil
					if !parentValue.CanSet() {
						return 0, &InvalidValueError{
							Value: parentValue,
							Message: fmt.Sprintf("Cannot set value in struct path %s", path),
						}
					}
					parentValue.SetZero()
					continue
				}

				// if it is not a foreign key, we see whether the value itself is nullable
				path = slices.Clone(*columnMap[column])
				l = len(path)
				path[l-1] = strings.ReplaceAll(path.Get(-1), "*", "")

				wrappedValue, err := reflection.GetField(newValue, path, false)
				if err != nil {
					panic(err)
				}

				// if it is not a pointer, return an error
				if wrappedValue.Kind() != reflect.Pointer {
					return 0, &InvalidValueError{
						Value: wrappedValue,
						Message: fmt.Sprintf(
							"db: value at path %s cannot be set to <nil>",
							strings.Join(path, "."),
						),
					}
				} 
				
				// otherwise, set it to <nil>
				if !wrappedValue.CanSet() {
					return 0, &InvalidValueError{
						Value: parentValue,
						Message: fmt.Sprintf("Cannot set value in struct path %s", path),
					}
				}
				wrappedValue.SetZero()
				continue
			} else {
				value, err := reflection.GetField(newValue, *columnMap[column], true)
				if err != nil {
					panic(err)
				}

				if !value.CanSet() {
					return 0, &InvalidValueError{
						Value: value,
						Message: fmt.Sprintf("Cannot set value in struct path %s", columnMap[column]),
					}
				}

				value.Set(valueMap[columnMap[column].ToString()].Elem().Elem())
			}
		}

		s.Set(reflect.Append(s, newValue))
        i++
    }

	if i == 0 && !opts.AllowNone {
		return 0, &RowNotFoundError{}
	}

    return i, nil
}

func (db *DBWrapper) QueryRow(value any, table Table, opts QueryOpts) error {
	// check value
	rv := reflect.ValueOf(value)
	
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidValueError{
			Value: value,
			Message: "value should be a non-nil pointer",
		}
	}

	if !rv.Elem().CanSet() {
		return &InvalidValueError{
			Value: value,
			Message: "value provided is not settable",
		}
	}

	// create new slice for query
	s := reflect.New(reflect.SliceOf(rv.Elem().Type()))

	// query db
	opts.Limit = 1

	n, err := db.Query(s.Interface(), table, opts)
	if err != nil {
		return err
	}
	if n == 0 && !opts.AllowNone {
		return &RowNotFoundError{}
	}
	if n > 1 {
		panic(fmt.Errorf("db: something went wrong, the query with limit 1 returned more than 1 item"))
	}

	if n == 1 {
		rv.Elem().Set(s.Elem().Index(0))
	}
	
	return nil
}

type join struct {
	original Column
	joined   Column
}

type structTreeNode struct {
	ptrCount      int
	path          string
	table         Table
	column        Column
	joinedColumn  Column
	children      []*structTreeNode
}

func (s *structTreeNode) Print() {
	fmt.Println("ptrCount", s.ptrCount)
	fmt.Println("path", s.path)
	fmt.Println("table", s.table)
	fmt.Println("column", s.column)
	fmt.Println("joinedColumn", s.joinedColumn)
	
	fmt.Println("children: [")
	for _, child := range s.children {
		child.Print()
	}
	fmt.Println("]")
}

func (s *structTreeNode) Get(p reflection.Path) (*structTreeNode, error) {
	if len(p) == 1 {
		return s, nil
	}

	field := strings.ReplaceAll(p.Get(1), "*", "")
	for _, child := range s.children {
		if child.path == field {
			return child.Get(append([]string{""}, p[2:]...))
		}
	}

	return nil, &reflection.FieldNotFoundErr{
		Field: field,
	}
}

func (s *structTreeNode) gatherQueryData(
	currentPath reflection.Path,
	columns *[]Column,
	columnMap *map[Column]*reflection.Path,
	joins *[]join,
) error {
	if !reflection.IsNull(reflect.ValueOf(s.joinedColumn)) {
		(*joins) = append((*joins), join{
			original: s.column,
			joined: s.joinedColumn,
		})
	}

	p := slices.Clone(currentPath)
	p[len(p)-1] = strings.Repeat("*", s.ptrCount) + p.Get(-1)

	if len(s.children) == 0 {
		if reflection.IsNull(reflect.ValueOf(s.column)) == true {
			return &ColumnNotFoundError{}
		}

		(*columns) = append((*columns), s.column)
		(*columnMap)[s.column] = &p

		return nil
	}

	for _, child := range s.children {
		newPath := slices.Clone(p)
		newPath = append(newPath, child.path)

		err := child.gatherQueryData(newPath, columns, columnMap, joins)
		if err != nil {
			return err
		}
	}

	return nil
}

func createStructMap(
    db DB,
    table Table,
	column Column,
    t reflect.Type,
    structPath reflection.Path,
	structMap *structTreeNode,
	marshalOpts DbMarshalOpts,
    opts QueryOpts,
) error {
	if t.Implements(reflect.TypeFor[DbMarshaler]()) {
		v, ok := reflect.Zero(t).Interface().(DbMarshaler)
		if !ok {
			panic(fmt.Errorf("db: type %v implements DbMarshaler but cannot be cast to it", t))
		}

		return createStructMap(
			db,
			table,
			column,
			v.GetDbType(),
			structPath,
			structMap,
			marshalOpts,
			opts,
		)
	}

	switch t.Kind() {
	case
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.Bool,
		reflect.String,
		reflect.Map:

		if len(opts.Fields) > 0 {
			return &reflection.FieldNotFoundErr{
				Field: strings.Join(opts.Fields[0], "."),
				Value: t,
				Message: fmt.Sprintf(
					"value of type %v at path %s does not have field %s",
					t,
					strings.Join(structPath, "."),
					strings.Join(opts.Fields[0], "."),
				),
			}
		}

		structMap.ptrCount = strings.Count(structPath.Get(-1), "*")
		structMap.path = strings.ReplaceAll(structPath.Get(-1), "*", "")
		structMap.column = column

		return nil

	case reflect.Struct:
		// if it is time
		if t == reflection.Time {
			structMap.ptrCount = strings.Count(structPath.Get(-1), "*")
			structMap.path = strings.ReplaceAll(structPath.Get(-1), "*", "")
			structMap.column = column

			return nil
		}

		if !opts.AllFields && len(opts.Fields) == 0 {
			return &ConflictingOptsError{
				Opts: []Opt{
					{Name: "Fields", Value: opts.Fields},
					{Name: "AllFields", Value: opts.AllFields},
				},
			}
		}

		structMap.table = table
		structMap.ptrCount = strings.Count(structPath.Get(-1), "*")
		structMap.path = strings.ReplaceAll(structPath.Get(-1), "*", "")
		structMap.column = column

	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() != reflect.Uint8 {
			return &InvalidValueError{Value: t, Message: "fields should be either values, maps, structs or pointers to one of the three"}
		}

		if len(opts.Fields) > 0 {
			return &reflection.FieldNotFoundErr{
				Field: strings.Join(opts.Fields[0], "."),
				Value: t,
				Message: fmt.Sprintf(
					"value of type %v at path %s does not have field %s",
					t,
					strings.Join(structPath, "."),
					strings.Join(opts.Fields[0], "."),
				),
			}
		}

		structMap.ptrCount = strings.Count(structPath.Get(-1), "*")
		structMap.path = strings.ReplaceAll(structPath.Get(-1), "*", "")
		structMap.column = column

		return nil

	case reflect.Pointer:
        path := slices.Clone(structPath)
        n := len(path)
        newPath := utils.IndexMap(path, func(i int, f string) string {
            if i == n - 1 {
                return "*" + f
            }
            return f
        })

		structMap.ptrCount += 1

        return createStructMap(
            db,
            table,
			column,
            t.Elem(),
            newPath,
			structMap,
			marshalOpts,
            opts,
        )

	default:
		return &InvalidValueError{Value: t, Message: "fields should be either values, maps, structs or pointers to one of the three"}
	}

    tc, err := db.GetTableConfig(table)
    if err != nil {
        return err
    }

	// now, if the structure is a map, we loop over columns of a table,
	// and, if it is a struct, we loop over its fields
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

        // initialize next struct path to keep track of where we are in the struct
        newStructPath := slices.Clone(structPath)
		newStructPath = append(newStructPath, field.Name)

		// ignore marshal opts if invalid
		newMarshalOpts, err := parseDbMarshalOpts(field.Tag.Get("db"))
		if err != nil {
			continue
		}

		// if field is embedded, go inside
		isAnonymousAndSpreadNotDefined := field.Anonymous && !newMarshalOpts.Spread.IsDefined()
		isSetToSpread := newMarshalOpts.Spread.IsDefined() && newMarshalOpts.Spread.Unwrap()
		if isAnonymousAndSpreadNotDefined || isSetToSpread {
			// right now, we only support embedded structs
			if field.Type.Kind() == reflect.Struct {
				newStructMap := structTreeNode{}

                err = createStructMap(
                    db,
                    table,
					column,
                    field.Type,
                    newStructPath,
					&newStructMap,
					newMarshalOpts,
                    opts,
                )
                if err != nil {
                    return err
                }

				if len(newStructMap.children) == 0 {
					continue
				}

				newStructMap.path = strings.ReplaceAll(newStructPath.Get(-1), "*", "")
				structMap.children = append(structMap.children, &newStructMap)
			}

			continue
		}

		columnName := newMarshalOpts.Name

		if columnName == "" {
			continue
		}

		if marshalOpts.Prefix != nil {
			columnName = *marshalOpts.Prefix + "_" + columnName
		}

		// find column the field refers to
		var col ColumnConfig

		found := false
		for j := 0; j < len(tc.Columns) && !found; j++ {
			if tc.Columns[j].Name == columnName {
				col = tc.Columns[j]
				found = true
			}
		}

		if !found {
			continue
		}

        fieldType := field.Type
		fieldName := field.Name

        // filter columns
        if !opts.AllFields && !slices.ContainsFunc(opts.Fields, func(f reflection.Path) bool {
			return reflection.IsSubpath(reflection.NewPath(fieldName), f, true)
        }) {
            continue
        }

		// prepare for recursion
		newFields := make([]reflection.Path, 0)
		for j := range opts.Fields {
			oldField := slices.Clone(opts.Fields[j])
			if len(oldField) > 2 && strings.ReplaceAll(oldField[1], "*", "") == fieldName {
				newFields = append(newFields, reflection.NewPath(append([]string{""}, oldField[2:]...)...))
			}
		}
		newOpts := opts
		newOpts.Fields = newFields
		if len(newOpts.Fields) == 0 && opts.Recursive {
			newOpts.AllFields = true
		}

        // handle nested case
        if col.IsForeignKey() && pointsToKind(fieldType, reflect.Struct, true, true) {
            if opts.Recursive {
				// this join must be made before recursion, otherwise we end up
				// with the opposite order
				newStructMap := structTreeNode{}

                err = createStructMap(
                    db,
                    col.ForeignKey.ReferencedTable,
					Column{Table: table, Name: col.Name},
                    fieldType,
                    newStructPath,
					&newStructMap,
					newMarshalOpts,
                    newOpts,
                )
                if err != nil {
                    return err
                }

				newStructMap.column = Column{
					Table: table,
					Name: col.Name,
				}
				newStructMap.joinedColumn = Column{
					Table: col.ForeignKey.ReferencedTable,
					Name: col.ForeignKey.ReferencedColumn,
				}
				newStructMap.path = strings.ReplaceAll(newStructPath.Get(-1), "*", "")
				structMap.children = append(structMap.children, &newStructMap)

            } else {
				// get rid of pointers, in order to be able to use .NumField()
				newOutterStructMap := structTreeNode{}

				underlyingStruct, ptrCount, err := unwrapUntilKind(fieldType, reflect.Struct, true, true)
				if err != nil {
					panic(err)
				}

				for range ptrCount {
					newStructPath.AddPtr(len(newStructPath)-1)
					newOutterStructMap.ptrCount += 1
				}

				// find field that maps to referenced column
                j := 0
                found = false
                for j < underlyingStruct.NumField() && !found {
                    if underlyingStruct.Field(j).Tag.Get("db") == col.ForeignKey.ReferencedColumn {
                        found = true
                    } else {
                        j++
                    }
                }

                if !found {
                    return &InvalidValueError{
                        Value: fieldType,
                        Message: fmt.Sprintf(
                            "nested struct does not have field for foreign key referenced column %s of table %s",
                            col.ForeignKey.ReferencedColumn,
                            col.ForeignKey.ReferencedTable.Schema + "." + col.ForeignKey.ReferencedTable.Name,
                        ),
                    }
                }

				if len(newOpts.Fields) > 1 ||
					(len(newOpts.Fields) == 1 &&
					strings.ReplaceAll(newOpts.Fields[0][1], "*", "") != underlyingStruct.Field(j).Name) {

					return &ConflictingOptsError{
						Opts: []Opt{
							{Name: "Recursive", Value: false},
							{Name: "Fields", Value: opts.Fields},
						},
						Message: fmt.Sprintf(
							"cannot access field %s in type %v at path %s. If you need access to it, set opts.Recursive=true",
							strings.Join(newOpts.Fields[0], "."),
							underlyingStruct,
							strings.Join(newStructPath, "."),
						),
					}
				}

				newOpts2 := newOpts
				newOpts2.Fields = make([]reflection.Path, 0)

				newStructMap := structTreeNode{}
				newStructPath = append(newStructPath, underlyingStruct.Field(j).Name)
				err = createStructMap(
					db,
					table,
					Column{Table: table, Name: col.Name},
                    underlyingStruct.Field(j).Type,
                    newStructPath,
					&newStructMap,
					newMarshalOpts,
                    newOpts2,
				)
				if err != nil {
					return err
				}

				newOutterStructMap.path = fieldName
				newOutterStructMap.column = Column{Table: table, Name: col.Name}
				newOutterStructMap.children = []*structTreeNode{
					&newStructMap,
				}

				structMap.children = append(structMap.children, &newOutterStructMap)
            }
        } else {
			newStructMap := structTreeNode{}

			err = createStructMap(
				db,
				table,
				Column{Table: table, Name: col.Name},
				fieldType,
				newStructPath,
				&newStructMap,
				newMarshalOpts,
				newOpts,
			)
			if err != nil {
				return err
			}

			structMap.children = append(structMap.children, &newStructMap)
        }
	}

	return nil
}

type update struct {
	sets    []string
	sql     string
	args    []any
}

func (db *DBWrapper) updateInternal(value any, table Table, opts QueryOpts) (int, pgx.Tx, *pgxpool.Conn, error) {
	// gather data necessary for the query
	structMap := structTreeNode{}

	err := createStructMap(
		db,
		table,
		Column{},
		reflect.TypeOf(value),
		reflection.NewPath(),
		&structMap,
		DbMarshalOpts{},
		opts,
	)
	if err != nil {
		return 0, nil, nil, err
	}

	columns := make([]Column, 0)
	columnMap := make(map[Column]*reflection.Path)
	joins := make([]join, 0)

	structMap.gatherQueryData(reflection.NewPath(""), &columns, &columnMap, &joins)

	for _, join := range joins {
		// if we update a column that references another, we should update
		// both in the referenced table and in the original table

		if slices.Contains(columns, join.joined) {
			columns = append(columns, join.original)
			columnMap[join.original] = columnMap[join.joined]
		}
	}

	// collect all tables involved in the update
	tables := make([]Table, 0)
	tables = append(tables, table)
	tables = append(tables, utils.Map(joins, func(j join) Table {return j.joined.Table})...)

	// create sql for the updates
	statements := make([]update, 0)

	// WHERE part of the update statement
	whereParts := make([]string, 0)
	for _, join := range joins {
		whereParts = append(whereParts, fmt.Sprintf(
			"%s = %s",
			SanitizeIdentifier(
				join.joined.Table.Schema,
				join.joined.Table.Name,
				join.joined.Name,
			),
			SanitizeIdentifier(
				join.original.Table.Schema,
				join.original.Table.Name,
				join.original.Name,
			),
		))
	}
	if opts.Filters != nil {
		whereParts = append(whereParts, opts.Filters.Sql())
	}

	whereSql := ""
	if len(whereParts) > 0 {
		whereSql = fmt.Sprintf(
			"WHERE %s\n",
			strings.Join(whereParts, " AND "),
		)
	}

	// for the FROM part of sql
	fromTables := slices.Clone(tables)
	if opts.Filters != nil {
		for _, c := range opts.Filters.Columns() {
			if (!slices.Contains(fromTables, c.Table)) {
				fromTables = append(fromTables, c.Table)
			}
		}
	}

	for i := len(tables)-1; i >= 0; i-- {
		tb := tables[i]
		upd := update{}

		upd.sql = fmt.Sprintf(
			"UPDATE %s\n",
			pgx.Identifier([]string{tb.Schema, tb.Name}).Sanitize(),
		)

		j := 1
		for _, c := range columns {
			if c.Table == tb {
				fieldValue, err := GetFieldMarshal(
					reflect.ValueOf(value),
					*columnMap[c],
					false,
				)
				if err != nil {
					switch err.(type) {
					case utils.NoValueError:
						continue
					default:
						return 0, nil, nil, err
					}
				}

				upd.sets = append(upd.sets, fmt.Sprintf(
					"%s = $%d",
					pgx.Identifier([]string{
						c.Name,
					}).Sanitize(),
					j,
				))
				upd.args = append(upd.args, fieldValue.Interface())

				j++
			}
		}

		// if there is nothing to update in a table, do not include statement
		if len(upd.sets) == 0 {
			continue
		}
		upd.sql += "SET " + strings.Join(upd.sets, ",") + "\n"

		if len(fromTables) > 1 {
			upd.sql += fmt.Sprintf("FROM %s\n", strings.Join(
				utils.Map(
					utils.Filter(fromTables, func(t Table) bool {
						return t != tb
					}),
					func(t Table) string {
						return pgx.Identifier([]string{t.Schema, t.Name}).Sanitize()
					},
				),
				", ",
			))
		}

		upd.sql += ShiftArgs(whereSql, j-1)
		if opts.Filters != nil {
			upd.args = append(upd.args, opts.Filters.Args()...)
		}

		upd.sql += ";"

		statements = append(statements, upd)
	}

	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, nil, nil, ConnectionErr
	}

	// begin transaction
	tx, err := conn.Begin(*db.context)
	if err != nil {
		return 0, nil, nil, ConnectionErr
	}

	// actually run the sql
	rowsAffected := 0
	for _, statement := range statements {
		cmdTag, err := tx.Exec(*db.context, statement.sql, statement.args...)
		if err != nil {
			tx.Rollback(*db.context)
			conn.Release()
			return 0, nil, nil, SqlError{
				Sql: statement.sql,
				Args: statement.args,
				Err: err,
			}
		}

		rowsAffected += int(cmdTag.RowsAffected())
	}

	return rowsAffected, tx, conn, nil
}

func (db *DBWrapper) Update(value any, table Table, opts QueryOpts) (int, error) {
	if opts.Fields == nil && !opts.AllFields {
		return 0, &ConflictingOptsError{
			Opts: []Opt{
				{Name: "Fields", Value: opts.Fields},
				{Name: "AllFields", Value: opts.AllFields},
			},
			Message: "either set 'AllFields' to true, or provide 'Fields' slice",
		}
	}

	rowsAffected, tx, conn, err := db.updateInternal(value, table, opts)
	if err != nil {
		// at this point, tx is already closed, so no need to rollback
		return 0, err
	}

	// this is to avoid leaks in worst case scenario
	defer conn.Release()

	if rowsAffected == 0 && !opts.AllowNone {
		return 0, &RowNotFoundError{}
	}

	if opts.Limit != 0 && rowsAffected > opts.Limit {
		tx.Rollback(*db.context)
		return 0, &ExceededLimitError{RowCount: rowsAffected, Limit: opts.Limit}
	}

	// commit changes
	err = tx.Commit(*db.context)
	if err != nil {
		tx.Rollback(*db.context)
		return 0, ConnectionErr
	}

	// close connection
	conn.Release()

	return rowsAffected, nil
}

func (db *DBWrapper) Delete(table Table, opts QueryOpts) (int, error) {
	if opts.Filters == nil {
		if opts.AllowNone {
			return 0, nil
		}
		return 0, &RowNotFoundError{}
	}

	sql := fmt.Sprintf(
		"DELETE FROM %s WHERE %s",
		pgx.Identifier([]string{table.Schema, table.Name}).Sanitize(),
		opts.Filters.Sql(),
	)

	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	defer conn.Release()

	// begin transaction
	tx, err := conn.Begin(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}

	// execute sql
	cmdTag, err := tx.Exec(*db.context, sql, opts.Filters.Args()...)
	if err != nil {
		tx.Rollback(*db.context)
		return 0, SqlError{
			Sql: sql,
			Args: opts.Filters.Args(),
			Err: err,
		}
	}

	// perform checks and rollback if needed
	if cmdTag.RowsAffected() == 0 && !opts.AllowNone {
		return 0, &RowNotFoundError{}
	}

	if opts.Limit != 0 && int(cmdTag.RowsAffected()) > opts.Limit {
		tx.Rollback(*db.context)
		return 0, &ExceededLimitError{
			RowCount: int(cmdTag.RowsAffected()),
			Limit: opts.Limit,
		}
	}

	// commit and close transaction
	err = tx.Commit(*db.context)
	if err != nil {
		tx.Rollback(*db.context)
		return 0, ConnectionErr
	}

	// close connection
	conn.Release()

	// return number of rows affected
	return int(cmdTag.RowsAffected()), nil
}

func (db *DBWrapper) QuerySql(values any, sql string, parameters ...any) (int, error) {
	// check types
	rv := reflect.ValueOf(values)

	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Slice || rv.Elem().Type().Elem().Kind() != reflect.Struct {
		return 0, &InvalidValueError{
			Value: values,
			Message: "values should be a pointer to a slice of structs",
		}
	}

	// define some userful variables
	s := rv.Elem()
	typ := rv.Elem().Type().Elem()

	fields := reflection.GetDeepFields(typ)

	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	defer conn.Release()

	// make query
	rows, err := conn.Query(*db.context, sql, parameters...)
	if err != nil {
		return 0, SqlError{
			Sql: sql,
			Args: parameters,
			Err: err,
		}
	}
	defer rows.Close()

	// decide in which order to scan into structs
	fieldOrder := make([]string, 0)

	descriptions := rows.FieldDescriptions()
	for _, desc := range descriptions {
		p := utils.KeyFunc(fields, func(path string, field reflect.StructField) bool {
			return field.Tag.Get("db") == desc.Name
		})

		if p != nil {
			fieldOrder = append(fieldOrder, *p)
		}
	}

	// scan values
	n := 0
	for rows.Next() {
		s.Set(reflect.Append(s, reflect.Zero(typ)))

		// collect pointers
		pointers := make([]any, 0)

		for _, pathStr := range fieldOrder {
			path, err := reflection.PathFromString(pathStr)
			if err != nil {
				panic(fmt.Errorf("db: path should definitely be a valid path: %w", err))
			}

			field, err := reflection.GetField(s.Index(s.Len()-1), path, false)
			if err != nil {
				return 0, fmt.Errorf("db: failed to access field at path %s", path.ToString())
			}

			pointers = append(pointers, field.Addr().Interface())
		}

		err = rows.Scan(pointers...)
		if err != nil {
			var scanErr *pgx.ScanArgError
			switch {
			case errors.As(err, &scanErr):
				return 0, fmt.Errorf(
					"db scan: failed to scan column at index %d into value[%d][%d]: %w",
					scanErr.ColumnIndex,
					s.Len()-1,
					scanErr.ColumnIndex,
					err,
				)
			default:
				return 0, fmt.Errorf("db: %w", err)
			}
		}

		n++
	}

	return n, nil
}

func (db *DBWrapper) ExecuteSql(sql string, parameters ...any) (int, error) {
	// acquire connection
	conn, err := db.conn.Acquire(*db.context)
	if err != nil {
		return 0, ConnectionErr
	}
	defer conn.Release()

	// execute sql
	tag, err := conn.Exec(*db.context, sql, parameters...)
	if err != nil {
		return 0, SqlError{
			Sql: sql,
			Args: parameters,
			Err: err,
		}
	}

	return int(tag.RowsAffected()), nil
}
