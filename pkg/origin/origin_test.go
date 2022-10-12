package origin

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestUserInfoResponse(t *testing.T) {
	testUserInfoResponse(t,
		"SuccessNew",
		200, "text/xml", `<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?><users><user><userId>1001111111111</userId><personaId>1001111111111</personaId><EAID>test</EAID></user><user><userId>1001111111112</userId><personaId>1001111111112</personaId><EAID>test1</EAID></user></users>`,
		[]UserInfo{
			{UserID: 1001111111111, PersonaID: "1001111111111", EAID: "test"},
			{UserID: 1001111111112, PersonaID: "1001111111112", EAID: "test1"},
		}, nil,
	)
	testUserInfoResponse(t,
		"SuccessOld",
		200, "text/xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><users><user><userId>2291234567</userId><personaId>328123456</personaId><EAID>blahblah</EAID></user></users>`,
		[]UserInfo{
			{UserID: 2291234567, PersonaID: "328123456", EAID: "blahblah"},
		}, nil,
	)
	testUserInfoResponse(t,
		"EmptyToken",
		200, "text/xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><error code="10044"><failure value="" field="authToken" cause="MISSING_AUTHTOKEN"/></error>`,
		nil, ErrOrigin,
	)
	testUserInfoResponse(t,
		"InvalidExpiredToken",
		200, "text/xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><error code="10044"><failure value="" field="authToken" cause="invalid_token"/></error>`,
		nil, ErrAuthRequired,
	)
	testUserInfoResponse(t,
		"FakeWrongRootElement",
		200, "text/xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><fake/></error>`,
		nil, ErrInvalidResponse,
	)
	testUserInfoResponse(t,
		"FakeError",
		200, "text/xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><error code="12345"><failure value="" field="dummy" cause="fake"/></error>`,
		nil, ErrOrigin,
	)
	testUserInfoResponse(t,
		"FakeBadResponse",
		500, "text/plain", `Fake Internal Server Error`,
		nil, ErrOrigin,
	)
	testUserInfoResponse(t,
		"FakeInvalidXML",
		200, "text/xml", `fake`,
		nil, ErrInvalidResponse,
	)
}

func testUserInfoResponse(t *testing.T, name string, status int, mime, xml string, v []UserInfo, err error) {
	t.Run(name, func(t *testing.T) {
		buf, root, err1 := checkResponseXML(&http.Response{
			Status:     strconv.Itoa(status) + " " + http.StatusText(status),
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(xml)),
			Header: http.Header{
				"Content-Type": {mime},
			},
		})
		if err1 != nil {
			if err == nil {
				t.Fatalf("expected no error, got %q", err1)
			}
			if !errors.Is(err1, err) {
				t.Fatalf("expected error %q, got %q", err, err1)
			}
			return
		}

		ui, err1 := parseUserInfo(buf, root)
		if err1 != nil {
			if err == nil {
				t.Fatalf("expected no error, got %q", err1)
			}
			if !errors.Is(err1, err) {
				t.Fatalf("expected error %q, got %q", err, err1)
			}
			return
		}
		if err != nil {
			t.Fatalf("expected error %q, got nothing", err)
		}

		if !reflect.DeepEqual(ui, v) {
			t.Errorf("unexpected result %#v", ui)
		}
	})
}
