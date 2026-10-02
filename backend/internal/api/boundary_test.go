package api

import (
	"os/exec"
	"strings"
	"testing"
)

// The binary that ships to production only reads and streams finished runs.
// It must not pull in the model (internal/model/...), directly or through
// another package, or the image grows and the serving code starts to depend on
// how runs are computed.
func TestServedBinaryDoesNotDependOnTheModel(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	// this package, the root entry point that Docker builds, and the shared shapes
	for _, pkg := range []string{".", "../..", "../wire"} {
		out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for _, path := range strings.Fields(string(out)) {
			if strings.Contains(path, "/internal/model/") || strings.HasSuffix(path, "/internal/worker") {
				t.Errorf("%s depends on %s; the API side must not import the model or the worker, it calls the worker over HTTP", pkg, path)
			}
		}
	}
}

// internal/wire is plain data: it may only import the standard library.
func TestWireImportsOnlyTheStandardLibrary(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", "../wire").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, path := range strings.Fields(string(out)) {
		if strings.Contains(path, "climate-hacktion-curtailment") && !strings.HasSuffix(path, "/internal/wire") {
			t.Errorf("internal/wire imports %s", path)
		}
	}
}
