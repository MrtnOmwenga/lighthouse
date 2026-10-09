package site

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Architecture is one project's system as data: its parts, how they connect, and flows that walk
// through them. The file is written and tested in the project's own repository (so it can't name
// code that is gone) and copied to architecture/<slug>.yaml by tools/sync-architecture.sh. This
// site only draws it.
type Architecture struct {
	Title       string       `yaml:"title"`
	Summary     string       `yaml:"summary"`
	Repository  string       `yaml:"repository"`
	Lanes       []Lane       `yaml:"lanes"`
	Parts       []Part       `yaml:"parts"`
	Connections []Connection `yaml:"connections"`
	Flows       []Flow       `yaml:"flows"`
}

type Lane struct {
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
}

type Part struct {
	ID     string   `yaml:"id"`
	Lane   string   `yaml:"lane"`
	Name   string   `yaml:"name"`
	What   string   `yaml:"what"`
	Detail string   `yaml:"detail"`
	Code   []string `yaml:"code"`
	// Handbook names the handbook page that explains the part. The assistant uses it; the page
	// doesn't show it.
	Handbook string `yaml:"handbook"`
}

type Connection struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Label string `yaml:"label"`
}

type Flow struct {
	ID      string     `yaml:"id"`
	Title   string     `yaml:"title"`
	Summary string     `yaml:"summary"`
	Steps   []FlowStep `yaml:"steps"`
	Proof   string     `yaml:"proof"`
}

type FlowStep struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Title string `yaml:"title"`
	Text  string `yaml:"text"`
}

// In returns the lane's parts, in file order.
func (a *Architecture) In(lane string) []Part {
	var out []Part
	for _, p := range a.Parts {
		if p.Lane == lane {
			out = append(out, p)
		}
	}
	return out
}

// Name is the display name of the part with that id.
func (a *Architecture) Name(id string) string {
	for _, p := range a.Parts {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// CodeLink is where a part's file can be read. HEAD follows the repository's default branch.
func (a *Architecture) CodeLink(path string) string {
	return strings.TrimSuffix(a.Repository, "/") + "/blob/HEAD/" + path
}

var (
	partPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	codePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]{0,199}$`)
)

// validate reports what is wrong with the definition. Ids end up in element ids and in addresses
// the assistant points at, and code paths end up in links, so their shapes are fixed here.
func (a *Architecture) validate(where string) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, where+": "+fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Summary) == "" {
		add("title and summary are required")
	}
	if u, err := url.Parse(a.Repository); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		add("repository: an https URL")
	}
	lanes := map[string]bool{}
	for _, l := range a.Lanes {
		if !partPattern.MatchString(l.ID) || lanes[l.ID] || strings.TrimSpace(l.Label) == "" {
			add("lane %q: needs a unique id and a label", l.ID)
		}
		lanes[l.ID] = true
	}
	parts := map[string]bool{}
	for _, p := range a.Parts {
		switch {
		case !partPattern.MatchString(p.ID):
			add("part %q: id must be lowercase letters, digits and dashes", p.ID)
		case parts[p.ID]:
			add("part %s: duplicate id", p.ID)
		case !lanes[p.Lane]:
			add("part %s: lane %q isn't defined", p.ID, p.Lane)
		case strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.What) == "" || strings.TrimSpace(p.Detail) == "":
			add("part %s: name, what and detail are required", p.ID)
		}
		parts[p.ID] = true
		for _, c := range p.Code {
			if !codePattern.MatchString(c) || strings.Contains(c, "..") {
				add("part %s: code %q must be a path inside the repository", p.ID, c)
			}
		}
	}
	if len(a.Parts) == 0 {
		add("no parts")
	}
	drawn := map[string]bool{}
	for _, c := range a.Connections {
		if !parts[c.From] || !parts[c.To] || c.From == c.To {
			add("connection %s to %s: both ends must be parts", c.From, c.To)
		}
		drawn[c.From+" "+c.To], drawn[c.To+" "+c.From] = true, true
	}
	flows := map[string]bool{}
	for _, f := range a.Flows {
		if !partPattern.MatchString(f.ID) || flows[f.ID] || strings.TrimSpace(f.Title) == "" || len(f.Steps) == 0 {
			add("flow %q: needs a unique id, a title and steps", f.ID)
		}
		flows[f.ID] = true
		for i, s := range f.Steps {
			if !drawn[s.From+" "+s.To] {
				add("flow %s step %d: %s to %s isn't a drawn connection", f.ID, i+1, s.From, s.To)
			}
			if strings.TrimSpace(s.Title) == "" || strings.TrimSpace(s.Text) == "" {
				add("flow %s step %d: title and text are required", f.ID, i+1)
			}
		}
	}
	return problems
}
