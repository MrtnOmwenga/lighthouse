package site

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTheShippedDiagramsAreValid(t *testing.T) {
	s, err := Load("../../deploy/site", "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	a := s.Architectures["redacted"]
	if a == nil {
		t.Fatal("redacted has no diagram")
	}
	if len(a.Parts) < 5 || len(a.Connections) < 5 || len(a.Flows) == 0 {
		t.Errorf("a diagram needs parts, connections and a flow: %d %d %d", len(a.Parts), len(a.Connections), len(a.Flows))
	}
	if got := a.CodeLink("src/policy/policy.ts"); got != "https://github.com/MrtnOmwenga/RBAC-API/blob/HEAD/src/policy/policy.ts" {
		t.Errorf("code link: %s", got)
	}
	if Anchor("Fail closed, then recover!") != "fail-closed-then-recover" {
		t.Errorf("anchor: %q", Anchor("Fail closed, then recover!"))
	}
	for _, p := range a.Parts {
		if p.Story != "" && !s.Stories["redacted"].Anchors()[p.Story] {
			t.Errorf("part %s points at a heading the story doesn't have: %s", p.ID, p.Story)
		}
	}
	if len(a.In("database")) == 0 || a.Name("api") != "REST API" || a.Name("nobody") != "nobody" {
		t.Error("parts are found by lane and named by id")
	}
}

// Lighthouse's own diagram is written here, not copied from another repository, so this is where
// the files it names are checked.
func TestLighthousesOwnDiagramNamesFilesThatExist(t *testing.T) {
	s, err := Load("../../deploy/site", "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	a := s.Architectures["lighthouse"]
	if a == nil {
		t.Fatal("lighthouse has no diagram")
	}
	for _, p := range a.Parts {
		if len(p.Code) == 0 {
			t.Errorf("part %s names no code", p.ID)
		}
		for _, c := range p.Code {
			if _, err := os.Stat(filepath.Join("../..", c)); err != nil {
				t.Errorf("part %s: %s doesn't exist", p.ID, c)
			}
		}
	}
	for _, f := range a.Flows {
		for _, path := range regexp.MustCompile(`\binternal/[\w./-]+\.go\b`).FindAllString(f.Proof, -1) {
			if _, err := os.Stat(filepath.Join("../..", path)); err != nil {
				t.Errorf("flow %s: proof names %s, which doesn't exist", f.ID, path)
			}
		}
	}
}

const validDiagram = `
title: T
summary: S
repository: https://github.com/example/x
lanes: [{ id: one, label: One }]
parts:
  - { id: a, lane: one, name: A, what: w, detail: d, code: [src/a.ts] }
  - { id: b, lane: one, name: B, what: w, detail: d, code: [src/b.ts] }
connections: [{ from: a, to: b, label: calls }]
flows:
  - id: f
    title: F
    steps: [{ from: b, to: a, title: back, text: a step may travel a connection either way }]
`

func project(t *testing.T, diagram string) error {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "site.yaml", "projects:\n  - { slug: x, name: X, headline: h, plain: p, hood: h }\n")
	write(t, dir, "architecture/x.yaml", diagram)
	_, err := Load(dir, "example.dev")
	return err
}

func TestADiagramIsCheckedBeforeItIsDrawn(t *testing.T) {
	if err := project(t, validDiagram); err != nil {
		t.Fatalf("a valid diagram was refused: %v", err)
	}
	// Ids become element ids and addresses, and code paths become links: each is held to a shape.
	for name, c := range map[string]struct{ from, to, want string }{
		"an id with markup":         {"id: a, lane: one, name: A", `id: "a\"><script>", lane: one, name: A`, "id must be lowercase"},
		"a path out of the repo":    {"code: [src/a.ts]", "code: [../../etc/passwd]", "must be a path inside the repository"},
		"a path that is an address": {"code: [src/b.ts]", `code: ["javascript:alert(1)"]`, "must be a path inside the repository"},
		"a repository over http":    {"https://github.com/example/x", "http://github.com/example/x", "repository: an https URL"},
		"an unknown lane":           {"id: b, lane: one", "id: b, lane: two", `lane "two" isn't defined`},
		"a connection to nowhere":   {"from: a, to: b, label", "from: a, to: zz, label", "both ends must be parts"},
		"a step nothing carries":    {"from: b, to: a, title", "from: a, to: a, title", "isn't a drawn connection"},
		"a part twice":              {"id: b, lane: one, name: B", "id: a, lane: one, name: B", "duplicate id"},
	} {
		changed := strings.Replace(validDiagram, c.from, c.to, 1)
		if changed == validDiagram {
			t.Fatalf("%s: the case changes nothing", name)
		}
		if err := project(t, changed); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", name, c.want, err)
		}
	}
	// A part may point into the story, but only at a heading that is there; this project has no story.
	if err := project(t, strings.Replace(validDiagram, "code: [src/a.ts]", "code: [src/a.ts], story: solution", 1)); err == nil || !strings.Contains(err.Error(), `the story has no heading "solution"`) {
		t.Errorf("a part pointing at a missing heading: %v", err)
	}
	if err := project(t, validDiagram+"colour: red\n"); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Errorf("an unknown key must fail loudly: %v", err)
	}
}
