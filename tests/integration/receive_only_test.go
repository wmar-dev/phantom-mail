package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
)

// FR-020: the service never sends mail. This test fails if production code
// imports an SMTP client or dials out, apart from an explicit allowlist.
var dialAllowlist = map[string]bool{
	"cmd/phantom-mail/healthcheck.go": true,
}

func TestNoOutboundMailCodePaths(t *testing.T) {
	root := filepath.Join("..", "..")
	var violations []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				t.Errorf("parse %s: %v", rel, perr)
				return nil
			}
			for _, imp := range f.Imports {
				if strings.Trim(imp.Path.Value, `"`) == "net/smtp" {
					violations = append(violations, rel+": imports net/smtp")
				}
			}
			if dialAllowlist[rel] {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch pkg.Name {
				case "net", "textproto", "tls":
					if strings.HasPrefix(sel.Sel.Name, "Dial") {
						violations = append(violations, rel+": uses "+pkg.Name+"."+sel.Sel.Name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("outbound network code found:\n  %s", strings.Join(violations, "\n  "))
	}
}

func TestServerNeverRelaysOrOffersAuth(t *testing.T) {
	addr, _ := startIngest(t, t.TempDir())
	c, err := textproto.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, _, err := c.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	_ = c.PrintfLine("EHLO test")
	_, msg, err := c.ReadResponse(250)
	if err != nil {
		t.Fatal(err)
	}
	if up := strings.ToUpper(msg); strings.Contains(up, "AUTH") || strings.Contains(up, "STARTTLS") {
		t.Fatalf("EHLO offers %q", msg)
	}
	_ = c.PrintfLine("MAIL FROM:<a@example.com>")
	if _, _, err := c.ReadResponse(250); err != nil {
		t.Fatal(err)
	}
	_ = c.PrintfLine("RCPT TO:<victim@gmail.com>")
	code, _, err := c.ReadResponse(0)
	if code != 550 {
		t.Fatalf("relay attempt answered %d (%v), want 550", code, err)
	}
	_ = c.PrintfLine("AUTH LOGIN")
	if code, _, _ := c.ReadResponse(0); code != 502 {
		t.Fatalf("AUTH answered %d, want 502", code)
	}
}
