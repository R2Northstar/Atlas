package origin

import (
	"reflect"
	"strings"
	"testing"
)

func TestUserInfoResponse(t *testing.T) {
	ui, err := parseUserInfo(strings.NewReader(`<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?><users><user><userId>1001111111111</userId><personaId>1001111111111</personaId><EAID>test</EAID></user><user><userId>1001111111112</userId><personaId>1001111111112</personaId><EAID>test1</EAID></user></users>`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ui, []UserInfo{
		{UserID: 1001111111111, PersonaID: "1001111111111", EAID: "test"},
		{UserID: 1001111111112, PersonaID: "1001111111112", EAID: "test1"},
	}) {
		t.Errorf("unexpected result %#v", ui)
	}
}
