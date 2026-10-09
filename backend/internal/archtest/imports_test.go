// Package archtest: pagar arsitektur. Tes ini GAGAL bila aturan ketergantungan antar paket dilanggar
// (lihat backend/ARCHITECTURE.md dan .golangci.yml). Hanya berkas non-tes yang diperiksa; tes boleh
// mengimpor paket lain untuk menyiapkan data.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const module = "mihanstore/"

// allowed: paket internal yang BOLEH diimpor tiap paket (selain pustaka standar & pihak ketiga).
// Pengecualian antar-modul yang disengaja ditandai komentar (lihat ARCHITECTURE.md).
var allowed = map[string][]string{
	"internal/platform":            {},
	"internal/platform/imaging":    {"internal/platform"},
	"internal/audit":               {"internal/platform"},
	"internal/notify":              {},
	"internal/push":                {"internal/platform", "internal/notify"}, // jenis kejadian bersama dengan notify
	"internal/regions":             {"internal/platform", "internal/audit"},
	"internal/regions/regionstest": {},
	"internal/catalog":             {"internal/platform", "internal/platform/imaging", "internal/audit", "internal/catalog/media"},
	"internal/catalog/media":       {"internal/platform", "internal/platform/imaging"},
	"internal/identity":            {"internal/platform", "internal/audit"},
	"internal/cart":                {"internal/platform", "internal/catalog"},                                                // aturan harga murni (pricing.go)
	"internal/admin":               {"internal/platform", "internal/audit", "internal/identity"},                             // prinsipal admin = identity.User
	"internal/orders":              {"internal/platform", "internal/platform/imaging", "internal/audit", "internal/regions"}, // tipe data wilayah saja
	"cmd/server":                   {"internal/app", "internal/platform", "internal/identity", "internal/regions"},
	"internal/app":                 nil, // composition root: boleh semua
}

// domainForbidden: berkas aturan murni tidak boleh tahu database, HTTP, atau modul lain.
var domainFiles = map[string]bool{"domain.go": true, "pricing.go": true}
var domainForbidden = []string{"gorm.io/", "net/http", module}

type goFile struct {
	pkgDir  string // relatif terhadap backend/, mis. internal/orders
	name    string
	imports []string
}

func backendRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod tidak ditemukan di %s", root)
	}
	return root
}

func sourceFiles(t *testing.T) []goFile {
	t.Helper()
	root := backendRoot(t)
	var out []goFile
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			gf := goFile{pkgDir: filepath.ToSlash(rel), name: filepath.Base(p)}
			for _, im := range f.Imports {
				v, _ := strconv.Unquote(im.Path.Value)
				gf.imports = append(gf.imports, v)
			}
			out = append(out, gf)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) == 0 {
		t.Fatal("tidak ada berkas sumber yang diperiksa")
	}
	return out
}

func TestImportRules(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range sourceFiles(t) {
		seen[f.pkgDir] = true
		allow, known := allowed[f.pkgDir]
		if !known {
			t.Errorf("paket %s belum terdaftar di aturan impor (archtest) — tambahkan beserta alasannya", f.pkgDir)
			continue
		}
		for _, im := range f.imports {
			if !strings.HasPrefix(im, module) {
				continue
			}
			dep := strings.TrimPrefix(im, module)
			if dep == "internal/app" && f.pkgDir != "cmd/server" {
				t.Errorf("%s/%s: hanya cmd/server yang boleh mengimpor internal/app", f.pkgDir, f.name)
			}
			if allow == nil || dep == f.pkgDir {
				continue
			}
			ok := false
			for _, a := range allow {
				if dep == a {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s/%s mengimpor %s (tidak diizinkan; definisikan antarmuka di sisi pemakai, dirakit di internal/app)", f.pkgDir, f.name, dep)
			}
		}
		if domainFiles[f.name] {
			for _, im := range f.imports {
				for _, bad := range domainForbidden {
					if strings.HasPrefix(im, bad) {
						t.Errorf("%s/%s (aturan murni) tidak boleh mengimpor %s", f.pkgDir, f.name, im)
					}
				}
			}
		}
	}
	var missing []string
	for p, a := range allowed {
		if !seen[p] && a != nil {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("paket di aturan tidak ditemukan (hapus/ganti nama?): %v", missing)
	}
}
