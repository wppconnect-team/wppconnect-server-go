package directory

import "testing"

func TestParseContactAndChatTypes(t *testing.T) {
	for _, tc := range []struct {
		value      string
		group, lid bool
		want       string
	}{
		{"5511999999999@c.us", false, false, "5511999999999@s.whatsapp.net"},
		{"123", false, true, "123@lid"},
		{"123@lid", false, false, "123@lid"},
		{"123-456@g.us", false, false, "123-456@g.us"},
		{"123-456", true, false, "123-456@g.us"},
		{"123@g.us", false, true, ""},
		{"123@c.us", false, true, ""},
		{"123", true, true, ""},
	} {
		t.Run(tc.value+tc.want, func(t *testing.T) {
			jid, err := Parse(tc.value, tc.group, tc.lid)
			if tc.want == "" {
				if err == nil {
					t.Fatal("conflicting flags accepted")
				}
			} else if err != nil || jid.String() != tc.want {
				t.Fatalf("got %s, %v; want %s", jid, err, tc.want)
			}
		})
	}
}
