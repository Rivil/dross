package secretpath

import "testing"

func TestPattern(t *testing.T) {
	pats := []string{"*.env", ".env*", "*.pem", "config/creds.json"}
	cases := []struct{ p, want string }{
		{".env", "*.env"},
		{".env.local", ".env*"},
		{"deploy/prod.env", "*.env"},
		{".env.example", ""},
		{"svc/.env.local.template", ""},
		{"certs/server.pem", "*.pem"},
		{"app/config/creds.json", "config/creds.json"},
		{"config/creds.json", "config/creds.json"},
		{"myconfig/creds.json", ""},
		{"main.go", ""},
		{"./a/../.env", "*.env"},
	}
	for _, c := range cases {
		if got := Pattern(c.p, pats); got != c.want {
			t.Errorf("Pattern(%q) = %q, want %q", c.p, got, c.want)
		}
	}
}
