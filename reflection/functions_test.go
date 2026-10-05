package reflection

import (
	"os"
	"reflect"
	"testing"

	"github.com/duhnny/pgmarshal/utils"
)

func TestDeepEqual(t *testing.T) {
	in1 := struct{
		A int `tag:"a"`
	}{
		A: 2,
	}

	in2 := struct{
		A int `tag:"b"`
	}{
		A: 2,
	}

	if reflect.DeepEqual(in1, in2) {
		t.Errorf("tags are not equal")
		return
	}
}

func TestStripNil(t *testing.T) {
	in1 := struct{
		A int     `tag:"a"`
		B *string `tag:"b"`
	}{
		A: 4,
	}

	expOut1 := struct{
		A int     `tag:"a"`
	}{
		A: 4,
	}

	out1, err := StripNil(in1)
	if err != nil {
		t.Errorf("reflection.StripNil should not error with input %v", in1)
		return
	}

	if !reflect.DeepEqual(out1, expOut1) {
		t.Errorf("reflection.StripNil output is not expected")
		utils.PrintInOut(*os.Stdout, in1, out1)
		return
	}
}

func TestGetDeepFields(t *testing.T) {
	type s1 struct {
		A string
		B int
	}

	t1 := reflect.TypeFor[s1]()
	f1 := GetDeepFields(t1)

	if len(f1) != 2 {
		t.Errorf("GetDeepFields is not getting all fields for basic structs")
		return
	}

	if f1[0].Name != "A" || f1[1].Name != "B" {
		t.Errorf("GetDeepFields is not getting fields properly for basic structs")
	}

	t2 := reflect.TypeFor[struct{
		s1
		C bool
	}]()
	f2 := GetDeepFields(t2)

	if len(f2) != 3 {
		t.Errorf("GetDeepFields is not getting all fields for embedded structs")
		return
	}
	
	if f2[0].Name != "A" || f2[1].Name != "B" || f2[2].Name != "C" {
		t.Errorf("GetDeepFields is not getting fields properly for embedded structs")
	}
}
