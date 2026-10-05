package reflection

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/duhnny/pgmarshal/utils"
)

func TestNewOptional(t *testing.T) {
	in1 := 4

	expOut1 := Optional[int]{
		Value: &in1,
		Defined: true,
	}

	out1 := NewOptional(in1)

	if !reflect.DeepEqual(out1, expOut1) {
		t.Errorf("reflection.NewOptional output is not expected")
		utils.PrintInOut(*os.Stdout, in1, out1)
		return
	}
}

func TestIsOptional(t *testing.T) {
	in := []any{NewOptional(1), "string"}
	expOut := []bool{true, false}

	for i := range len(in) {
		out := IsOptional(in[i])

		if !reflect.DeepEqual(out, expOut[i]) {
			t.Errorf("reflection.NewOptional output is not expected")
			utils.PrintInOut(*os.Stdout, in[i], out)
			return
		}
	}
}

func TestStripOptionals(t *testing.T) {
	// first test
	in1 := struct{
		A Optional[int]    `tag:"a"`
		B Optional[string] `tag:"b"`
	}{
		A: NewOptional(4),
		B: NewOptional("test"),
	}

	expOut1 := struct{
		A int    `tag:"a"`
		B string `tag:"b"`
	}{
		A: 4,
		B: "test",
	}

	out1, err := StripOptionals(in1)
	if err != nil {
		t.Errorf("reflection.StripOptionals should not error with input %v", in1)
		return
	}

	if !reflect.DeepEqual(out1, expOut1) {
		t.Errorf("reflection.StripOptionals output is not expected")
		utils.PrintInOut(*os.Stdout, in1, out1)
		return
	}

	// second test
	type substruct struct {
		E int `tag:"e"`
		F Optional[int] `tag:"f"`
		G Optional[time.Time] `tag:"g"`
	}

	now := time.Now()

	in2 := struct{
		A int `tag:"a"`
		B Optional[string] `tag:"b"`
		C Optional[time.Time] `tag:"c"`
		D substruct `tag:"d"`
	}{
		A: 9,
		C: NewOptional(now),
		D: substruct{
			E: 10,
			F: NewOptional(11),
			G: NewOptional(now),
		},
	}

	expOut2 := struct{
		A int `tag:"a"`
		C time.Time `tag:"c"`
		D struct{
			E int `tag:"e"`
			F int `tag:"f"`
			G time.Time `tag:"g"`
		} `tag:"d"`
	}{
		A: 9,
		C: now,
		D: struct{
			E int `tag:"e"`
			F int `tag:"f"`
			G time.Time `tag:"g"`
		}{
			E: 10,
			F: 11,
			G: now,
		},
	}

	out2, err := StripOptionals(in2)
	if err != nil {
		t.Errorf("reflection.StripOptionals should not error with input %v", in2)
		return
	}

	if !reflect.DeepEqual(out2, expOut2) {
		t.Errorf("reflection.StripOptionals output is not expected")
		utils.PrintInOut(*os.Stdout, in2, out2)
		return
	}
}
