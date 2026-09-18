package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/caigee-cmd/cli2api"

type goPackage struct {
	ImportPath string
	Dir        string
	Name       string
	Imports    []string
}

// Production import allowlist. Each entry is a remaining historical edge with
// an explicit deletion condition. Do not add new rows without review.
var importAllowlist = map[string]map[string]string{
	modulePath + "/internal/api": {
		modulePath + "/internal/app": "S15: api is the remaining test-only compatibility facade (api.New → app.New).",
	},
	modulePath + "/internal/executor": {
		modulePath + "/internal/providers/qoder": "S09 leftover: Qoder chat still uses worker HTTP via qoder.NewChatRequest until production chat switches to Adapter.",
	},
	modulePath + "/internal/runtime": {
		modulePath + "/internal/providers/qoder": "S08/S09 leftover: Qoder child spawn, HOME, catalog, and quota still call providers/qoder until remaining capabilities go through Adapter.",
	},
	modulePath + "/internal/console": {
		modulePath + "/internal/providers/devin":     "Account import still validates/decodes Devin credential blobs in console HTTP.",
		modulePath + "/internal/providers/trae":      "Account import still validates/decodes Trae credential blobs in console HTTP.",
		modulePath + "/internal/providers/workbuddy": "Account import still validates/decodes WorkBuddy credential blobs in console HTTP.",
	},
}

var allowedAPIProductionFiles = map[string]struct{}{
	"server.go":          {},
	"bootstrap.go":       {},
	"compat_wrappers.go": {},
	"gateway_aliases.go": {},
	"system_settings.go": {},
}

func TestImportConstraints(t *testing.T) {
	root := moduleRoot(t)
	pkgs := listProductionPackages(t, root)
	var violations []string

	for _, pkg := range pkgs {
		rel := strings.TrimPrefix(pkg.ImportPath, modulePath+"/")
		imports := pkg.Imports
		allow := importAllowlist[pkg.ImportPath]

		for _, imp := range imports {
			if allowed, ok := allow[imp]; ok && allowed != "" {
				continue
			}
			if bad, reason := forbiddenImport(rel, imp); bad {
				violations = append(violations, pkg.ImportPath+" imports "+imp+": "+reason)
			}
		}

		if rel == "internal/api" {
			entries, err := os.ReadDir(pkg.Dir)
			if err != nil {
				t.Fatalf("read api dir: %v", err)
			}
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
					continue
				}
				if _, ok := allowedAPIProductionFiles[name]; !ok {
					violations = append(violations, "internal/api production file "+name+" is not on the facade allowlist")
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("import constraints failed:\n%s", strings.Join(violations, "\n"))
	}
}

func forbiddenImport(importer, imp string) (bool, string) {
	if !strings.HasPrefix(importer, "internal/") && !strings.HasPrefix(importer, "cmd/") {
		return false, ""
	}

	switch {
	case importer == "internal/gateway" || strings.HasPrefix(importer, "internal/gateway/"):
		if isAny(imp, "internal/store", "database/sql", "modernc.org/sqlite", "internal/runtime", "internal/api") {
			return true, "gateway must not import store, SQL, runtime Manager, or api"
		}
		if isConcreteProvider(imp) {
			return true, "gateway must not import a concrete provider"
		}
	case importer == "internal/console" || strings.HasPrefix(importer, "internal/console/"):
		if isAny(imp, "internal/store", "database/sql", "modernc.org/sqlite", "internal/runtime", "internal/api") {
			return true, "console must not import store, SQL, runtime Manager, or api"
		}
	case importer == "internal/server" || strings.HasPrefix(importer, "internal/server/"):
		if isAny(imp, "internal/store", "database/sql", "modernc.org/sqlite", "internal/runtime", "internal/api") {
			return true, "server must not import store, SQL, runtime Manager, or api"
		}
		if isConcreteProvider(imp) {
			return true, "server must not import a concrete provider"
		}
	case importer == "internal/executor" || strings.HasPrefix(importer, "internal/executor/"):
		if isAny(imp, "internal/store", "internal/app", "internal/server", "internal/gateway", "internal/console", "internal/api") {
			return true, "executor must not import store or HTTP/app packages"
		}
		if isConcreteProvider(imp) {
			return true, "executor must not import a concrete provider"
		}
	case importer == "internal/control" || strings.HasPrefix(importer, "internal/control/"):
		if isAny(imp, "internal/store", "database/sql", "modernc.org/sqlite", "internal/runtime", "internal/app", "internal/server", "internal/gateway", "internal/console", "internal/api") {
			return true, "control must not import SQL, runtime, app, or HTTP packages"
		}
		if isConcreteProvider(imp) {
			return true, "control must not import a concrete provider"
		}
	case importer == "internal/runtime" || strings.HasPrefix(importer, "internal/runtime/"):
		if isAny(imp, "internal/control", "internal/app", "internal/server", "internal/gateway", "internal/console", "internal/api") {
			return true, "runtime must not import control, app, or HTTP packages"
		}
		if isConcreteProvider(imp) && imp != modulePath+"/internal/providers/qoder" {
			return true, "runtime must not import a non-Qoder concrete provider"
		}
	case strings.HasPrefix(importer, "internal/providers/") && importer != "internal/providers":
		if isAny(imp, "internal/executor", "internal/runtime", "internal/gateway", "internal/console", "internal/server", "internal/store", "internal/app", "internal/api") {
			return true, "provider packages must not import executor, runtime Manager, HTTP, store, or api"
		}
	case importer == "internal/store" || strings.HasPrefix(importer, "internal/store/"):
		if isAny(imp, "internal/control", "internal/runtime", "internal/app", "internal/server", "internal/gateway", "internal/console", "internal/api") {
			return true, "store must not import control, runtime, app, or HTTP packages"
		}
		if isConcreteProvider(imp) {
			return true, "store must not import a concrete provider"
		}
	case importer == "internal/logs" || strings.HasPrefix(importer, "internal/logs/"):
		if isAny(imp, "internal/store", "internal/gateway", "internal/console", "internal/app", "internal/api") {
			return true, "logs must not import store, HTTP, or app"
		}
	case importer == "internal/accounts" || strings.HasPrefix(importer, "internal/accounts/"):
		if isAny(imp, "internal/runtime", "internal/executor", "internal/store", "internal/app", "internal/api") {
			return true, "accounts must not import runtime, executor, store, or app"
		}
	case importer == "cmd/server":
		if isAny(imp, "internal/api") {
			return true, "cmd/server must construct app.New, not the api facade"
		}
	}

	if isAny(imp, "internal/app") && importer != "cmd/server" && importer != "internal/api" && !strings.HasPrefix(importer, "internal/app") {
		return true, "app must not be imported by lower packages"
	}
	if isAny(imp, "internal/api") && importer != "internal/api" {
		return true, "only the api facade package may import itself; production packages must not import api"
	}
	return false, ""
}

func isConcreteProvider(imp string) bool {
	prefix := modulePath + "/internal/providers/"
	if !strings.HasPrefix(imp, prefix) {
		return false
	}
	rest := strings.TrimPrefix(imp, prefix)
	root, _, _ := strings.Cut(rest, "/")
	switch root {
	case "qoder", "workbuddy", "trae", "devin":
		return true
	default:
		return false
	}
}

func isAny(imp string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if imp == suffix || imp == modulePath+"/"+suffix || strings.HasSuffix(imp, "/"+suffix) {
			if strings.Contains(suffix, "/") || strings.Contains(suffix, ".") {
				if imp == suffix || imp == modulePath+"/"+suffix {
					return true
				}
				if strings.HasPrefix(imp, modulePath+"/"+suffix+"/") {
					return true
				}
			} else if imp == suffix || imp == modulePath+"/"+suffix {
				return true
			}
		}
		if strings.HasPrefix(suffix, "internal/") && (imp == modulePath+"/"+suffix || strings.HasPrefix(imp, modulePath+"/"+suffix+"/")) {
			return true
		}
		if suffix == "database/sql" && imp == "database/sql" {
			return true
		}
		if suffix == "modernc.org/sqlite" && (imp == "modernc.org/sqlite" || strings.HasPrefix(imp, "modernc.org/sqlite/")) {
			return true
		}
	}
	return false
}

func listProductionPackages(t *testing.T, root string) []goPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./internal/...", "./cmd/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list: %v\n%s", err, stderr)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []goPackage
	for dec.More() {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list: %v", err)
		}
		if !strings.HasPrefix(pkg.ImportPath, modulePath+"/") {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list returned no module packages")
	}
	return pkgs
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
