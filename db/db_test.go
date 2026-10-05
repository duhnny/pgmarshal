package db

import (
	"reflect"
	"testing"

	"github.com/duhnny/pgmarshal/reflection"
)

type MockDB struct {
	DB
}
func (db MockDB) GetTableConfig(table Table) (TableConfig, error) {
	return TableConfig{
		Table: table,
		Columns: []ColumnConfig{
			{
				Name: "a",
			},
			{
				Name: "b",
			},
			{
				Name: "c",
			},
		},
	}, nil
}

func TestStructMap(t *testing.T) {
	type s1 struct {
		A string `db:"a"`
		B int    `db:"b"`
	}

	type s2 struct {
		s1
		C bool `db:"c"`
	}

	typ := reflect.TypeFor[s2]()


	structMap := structTreeNode{}
	err := createStructMap(
		MockDB{},
		Table{Schema: "exemple", Name: "table"},
		Column{},
		typ,
		reflection.NewPath(""),
		&structMap,
		QueryOpts{ AllFields: true },
	)
	if err != nil {
		t.Errorf("createStructMap failed with error %s", err.Error())
	}

	if len(structMap.children) != 2 {
		t.Errorf("wrong number of children")
	}

	if len(structMap.children[0].children) != 2 {
		t.Errorf("wrong number of children for embedded struct")
	}

	// test gatherQueryData
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
		t.Errorf("failed to gather query data")
	}

	// structMap.Print()
	// for column, path := range columnMap {
	// 	fmt.Printf(
	// 		"%s.%s.%s: %s\n",
	// 		column.Table.Schema,
	// 		column.Table.Name,
	// 		column.Name,
	// 		strings.Join(*path, "."),
	// 	)
	// }
	// t.Errorf("it's actually nothing")
}
