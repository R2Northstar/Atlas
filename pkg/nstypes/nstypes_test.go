package nstypes

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMaps(t *testing.T) {
	testNSType(t, Maps)
}

func TestPlaylists(t *testing.T) {
	testNSType(t, Playlists)
}

func testNSType[T interface {
	fmt.GoStringer
	fmt.Stringer
	SourceString() string
	Known() bool
	Title() (string, bool)
}](t *testing.T, all func() []T) {
	var dummy T
	if reflect.TypeOf(dummy).Kind() != reflect.String {
		t.Error("enum type must be a string")
	}
	reflect.ValueOf(&dummy).Elem().SetString("__nonexistent__")

	name := reflect.TypeOf(dummy).Name()
	if strings.Contains(name, ".") {
		panic("wtf")
	}

	if dummy.GoString() != fmt.Sprintf("%s(%q)", name, "__nonexistent__") {
		t.Error("incorrect GoString output for nonexistent enum value")
	}
	if dummy.String() != "__nonexistent__" {
		t.Error("incorrect String output for nonexistent enum value")
	}
	if dummy.SourceString() != "__nonexistent__" {
		t.Error("incorrect SourceString output for nonexistent enum value")
	}
	if dummy.Known() {
		t.Error("known should not return true for nonexistent enum value")
	}
	if x, ok := dummy.Title(); x != "" || ok {
		t.Error("incorrect Title output for nonexistent enum value")
	}

	for _, v := range all() {
		val := reflect.ValueOf(v).String()
		if v.GoString() != fmt.Sprintf("%s(%q)", name, val) {
			t.Error("incorrect GoString output for existing enum value")
		}
		if v.String() == val {
			t.Error("incorrect String output for existing enum value (should be the title, not the raw value)")
		}
		if v.SourceString() != val {
			t.Error("incorrect SourceString output for existing enum value")
		}
		if !v.Known() {
			t.Error("known should return true for existing enum value")
		}
		if x, ok := v.Title(); x != v.String() || !ok {
			t.Error("incorrect Title output for existing enum value")
		}
	}
}
