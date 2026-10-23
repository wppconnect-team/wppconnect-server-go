package auth

import (
	"encoding/json"
	"os"
	"testing"
)

func TestNodeBcryptFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/node-bcrypt.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Fixtures []struct{ Session, Secret, Raw, Token, Full string }
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Fixtures) < 3 {
		t.Fatal("missing cross-runtime fixtures")
	}
	for _, f := range data.Fixtures {
		t.Run(f.Session, func(t *testing.T) {
			for _, token := range []string{f.Raw, f.Token} {
				if err := Verify(f.Session, f.Secret, token); err != nil {
					t.Fatal(err)
				}
				if Verify("another-session", f.Secret, token) == nil {
					t.Fatal("accepted another session")
				}
				if Verify(f.Session, "rotated-secret", token) == nil {
					t.Fatal("accepted rotated secret")
				}
			}
			if f.Full != f.Session+":"+f.Token {
				t.Fatal("invalid Node full token")
			}
		})
	}
}
