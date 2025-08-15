package reflection

import (
	"os"
	"reflect"
	"testing"

	"github.com/danielbetoret/organization/services/api/src/utils"
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
