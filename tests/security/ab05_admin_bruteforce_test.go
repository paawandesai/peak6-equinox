package security

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestAB05_AdminBruteForceLockout: after 10 failed basic-auth attempts from one
// IP within 10 minutes, that IP gets 429 even with the right password, other
// IPs are unaffected, and the lockout lifts once the window passes.
func TestAB05_AdminBruteForceLockout(t *testing.T) {
	h, ing, ck := newAdminHandler(t, "ci")
	attacker, admin := "198.51.100.1", "203.0.113.5"

	for i := 1; i <= 10; i++ {
		rec := send(h, http.MethodGet, "/admin", attacker, basic(adminUser, "guess-"+strconv.Itoa(i)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("attempt %d: no WWW-Authenticate challenge", i)
		}
	}
	// The 11th attempt is refused before the password is even checked.
	for _, pass := range []string{"guess-11", adminPass} {
		rec := send(h, http.MethodPost, "/admin/ingest", attacker, basic(adminUser, pass))
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("locked out attempt with %q: status %d, want 429", pass, rec.Code)
		}
		if ra, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || ra <= 0 || ra > 601 {
			t.Errorf("Retry-After = %q, want seconds until the window passes", rec.Header().Get("Retry-After"))
		}
	}
	if ing.calls.Load() != 0 {
		t.Error("a locked-out request ran a round")
	}

	// The real admin, from another IP, is unaffected.
	if rec := send(h, http.MethodGet, "/admin", admin, basic(adminUser, adminPass)); rec.Code != http.StatusOK {
		t.Errorf("admin from another IP: status %d, want 200", rec.Code)
	}

	// 9 minutes later the attacker is still locked out; after 10 the oldest failure expires.
	ck.t = ck.t.Add(9 * time.Minute)
	if rec := send(h, http.MethodGet, "/admin", attacker, basic(adminUser, adminPass)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("at 9 minutes: status %d, want 429", rec.Code)
	}
	ck.t = ck.t.Add(time.Minute + time.Second)
	if rec := send(h, http.MethodGet, "/admin", attacker, basic(adminUser, "guess-12")); rec.Code != http.StatusUnauthorized {
		t.Errorf("after the window: status %d, want 401 again", rec.Code)
	}
	if rec := send(h, http.MethodGet, "/admin", attacker, basic(adminUser, adminPass)); rec.Code != http.StatusOK {
		t.Errorf("after the window with the right password: status %d, want 200", rec.Code)
	}
}

// TestAB05_LockoutUsesForwardedIPOnlyBehindCloudRun: locally the peer address
// counts, so an attacker cannot spread attempts over forged X-Forwarded-For
// values; in prod the front end's appended (last) entry is the client.
func TestAB05_LockoutUsesForwardedIPOnlyBehindCloudRun(t *testing.T) {
	local, _, _ := newAdminHandler(t, "local")
	for i := 0; i < 10; i++ {
		send(local, http.MethodGet, "/admin", "198.51.100.1", func(r *http.Request) {
			r.SetBasicAuth(adminUser, "bad")
			r.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i))
		})
	}
	if rec := send(local, http.MethodGet, "/admin", "198.51.100.1", basic(adminUser, adminPass)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("local: forged X-Forwarded-For evaded the lockout (status %d)", rec.Code)
	}

	prod, _, _ := newAdminHandler(t, "prod")
	for i := 0; i < 10; i++ {
		send(prod, http.MethodGet, "/admin", "10.1.1.1", func(r *http.Request) {
			r.SetBasicAuth(adminUser, "bad")
			r.Header.Set("X-Forwarded-For", "1.2.3."+strconv.Itoa(i)+", 198.51.100.1")
		})
	}
	rec := send(prod, http.MethodGet, "/admin", "10.1.1.1", func(r *http.Request) {
		r.SetBasicAuth(adminUser, adminPass)
		r.Header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.1")
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("prod: client-supplied leading entries evaded the lockout (status %d)", rec.Code)
	}
}

// TestAB05_CredentialsUseConstantTimeCompare parses the auth middleware and
// fails if the password check does not go through subtle.ConstantTimeCompare,
// or if any == comparison touches the configured credentials.
func TestAB05_CredentialsUseConstantTimeCompare(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "web", "middleware_auth.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var constantTime, plainCompare bool
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "subtle" && x.Sel.Name == "ConstantTimeCompare" {
				constantTime = true
			}
		case *ast.BinaryExpr:
			if x.Op == token.EQL || x.Op == token.NEQ {
				for _, side := range []ast.Expr{x.X, x.Y} {
					if sel, ok := side.(*ast.SelectorExpr); ok && (sel.Sel.Name == "AdminPassword" || sel.Sel.Name == "AdminUser") {
						if lit, ok := otherSide(x, side).(*ast.BasicLit); !ok || lit.Value != `""` {
							plainCompare = true
						}
					}
				}
			}
		}
		return true
	})
	if !constantTime {
		t.Error("middleware_auth.go does not use subtle.ConstantTimeCompare")
	}
	if plainCompare {
		t.Error("middleware_auth.go compares a credential with == or !=")
	}
}

func otherSide(b *ast.BinaryExpr, side ast.Expr) ast.Expr {
	if b.X == side {
		return b.Y
	}
	return b.X
}
