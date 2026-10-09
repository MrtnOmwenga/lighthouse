// Package site is the portfolio's content: the profile, the projects, and a long-form story for
// each project. It is read from a folder the owner maintains (SITE_DIR):
//
//	site.yaml            profile and projects
//	stories/<slug>.yaml  one story per project
//	media/               images and files the pages link to (the portrait, the CV)
//
// The content is trusted configuration, but it is validated at startup so a mistake fails there,
// with every problem listed, rather than on a page.
package site

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Site struct {
	Profile  Profile           `yaml:"profile"`
	Projects []Project         `yaml:"projects"`
	Stories  map[string]*Story `yaml:"-"` // by project slug
	// Architectures are the projects' system diagrams, by project slug.
	Architectures map[string]*Architecture `yaml:"-"`
	Dir           string                   `yaml:"-"`
}

// Fact is a label and a value: a line of a fact box, a statistic, a skill area.
type Fact struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
}

// Item is a titled paragraph: a step, a job, an education entry.
type Item struct {
	Title string `yaml:"title"`
	Body  string `yaml:"body"`
	When  string `yaml:"when"`
	Note  string `yaml:"note"`
}

type Links struct {
	Email    string `yaml:"email"`
	LinkedIn string `yaml:"linkedin"`
	GitHub   string `yaml:"github"`
	CV       string `yaml:"cv"` // a URL or a file in media/; empty hides the button
}

// CVURL is where the Download CV button points, or "" to hide it.
func (l Links) CVURL() string {
	if l.CV == "" || strings.HasPrefix(l.CV, "http") {
		return l.CV
	}
	return "/media/" + l.CV
}

type Profile struct {
	Strip           string    `yaml:"strip"` // the line across the top of every page
	Portrait        string    `yaml:"portrait"`
	PortraitCaption string    `yaml:"portrait_caption"`
	Preview         string    `yaml:"preview"` // the image link previews show, in media/ (1200×630)
	Links           Links     `yaml:"links"`
	Lead            Article   `yaml:"lead"`      // the front page's story about the owner
	Delivered       []Item    `yaml:"delivered"` // named outcomes, under the lead
	Works           []Item    `yaml:"works"`     // how the owner works, in a row under the lead
	Glance          []Fact    `yaml:"glance"`    // "At a glance"
	HowTo           []Item    `yaml:"how_to"`    // "How to read this site"
	About           AboutPage `yaml:"about"`
}

type Article struct {
	Kicker     string   `yaml:"kicker"`
	Headline   string   `yaml:"headline"`
	Standfirst string   `yaml:"standfirst"`
	Paragraphs []string `yaml:"paragraphs"`
}

type AboutPage struct {
	Article   `yaml:",inline"`
	Stats     []Fact `yaml:"stats"`
	Now       string `yaml:"now"`
	Career    []Item `yaml:"career"` // Title = role, Body = what, When, Note = stack
	Toolbox   []Fact `yaml:"toolbox"`
	Education []Item `yaml:"education"`
	Contact   string `yaml:"contact"`
}

// Placement is where a project sits on the front page.
type Placement string

const (
	Lead    Placement = "lead"    // the main story of the projects section
	Side    Placement = "side"    // beside the lead
	Feature Placement = "feature" // the wide story with the systems data beside it
	Brief   Placement = "brief"   // a short item under the systems data
)

type Project struct {
	Slug       string    `yaml:"slug"`
	Name       string    `yaml:"name"`
	Kicker     string    `yaml:"kicker"`
	Headline   string    `yaml:"headline"`
	Standfirst string    `yaml:"standfirst"`
	Plain      string    `yaml:"plain"` // "In plain terms"
	Hood       string    `yaml:"hood"`  // "Under the hood"
	Figure     string    `yaml:"figure"`
	Placement  Placement `yaml:"placement"`
	Facts      []Fact    `yaml:"facts"`
	Repo       string    `yaml:"repo"`
	Docs       string    `yaml:"docs"` // design notes
	// ArchitectureDraft keeps the project's architecture page out of the story, the sitemap and
	// search engines until it has been read through. The page itself still answers.
	ArchitectureDraft bool `yaml:"architecture_draft"`
	// Demo is where the live demo is; empty for projects without one.
	Demo string `yaml:"demo"`
	// Health is polled until it answers, which also wakes a demo that sleeps when idle. Usually
	// an address inside the cluster, so it is never shown publicly.
	Health     string  `yaml:"health"`
	Monitor    string  `yaml:"monitor"` // the slug of the monitor whose status the project shows
	Tour       string  `yaml:"tour"`
	NoDemoNote string  `yaml:"no_demo_note"`
	Intro      []Slide `yaml:"intro"`
}

type Slide struct {
	Kicker string `yaml:"kicker"`
	Title  string `yaml:"title"`
	Plain  string `yaml:"plain"`
	Body   string `yaml:"body"`
	Facts  []Fact `yaml:"facts"`
}

// Story is a project's long-form write-up, aimed at a hiring team: the problem, how it was
// solved, the decisions that shaped it, how it is tested, and what it doesn't do yet.
type Story struct {
	ReadMinutes   int        `yaml:"read_minutes"`
	Updated       string     `yaml:"updated"`
	FigureCaption string     `yaml:"figure_caption"`
	Problem       Section    `yaml:"problem"`
	Solution      Section    `yaml:"solution"`
	Quote         string     `yaml:"quote"`
	Steps         *Steps     `yaml:"steps"`
	Decisions     []Decision `yaml:"decisions"`
	Testing       Section    `yaml:"testing"`
	Limits        Section    `yaml:"limits"`
	Numbers       []Fact     `yaml:"numbers"`
	FactFile      []Fact     `yaml:"fact_file"`
	CallToAction  string     `yaml:"call_to_action"`
}

type Section struct {
	Title      string   `yaml:"title"`
	Paragraphs []string `yaml:"paragraphs"`
}

type Steps struct {
	Caption string `yaml:"caption"`
	Items   []Item `yaml:"items"`
}

// Decision is one architectural choice: what was chosen, why, and what it costs.
type Decision struct {
	Title    string `yaml:"title"`
	Choice   string `yaml:"choice"`
	Why      string `yaml:"why"`
	TradeOff string `yaml:"trade_off"`
}

func (s *Site) Find(slug string) (Project, bool) {
	for _, p := range s.Projects {
		if p.Slug == slug {
			return p, true
		}
	}
	return Project{}, false
}

// ByPlacement returns the projects placed at p on the front page, in file order.
func (s *Site) ByPlacement(p Placement) []Project {
	var out []Project
	for _, pr := range s.Projects {
		if pr.Placement == p {
			out = append(out, pr)
		}
	}
	return out
}

// Next is the project whose story follows slug's, wrapping around.
func (s *Site) Next(slug string) (Project, bool) {
	for i, p := range s.Projects {
		if p.Slug != slug {
			continue
		}
		for j := 1; j < len(s.Projects); j++ {
			n := s.Projects[(i+j)%len(s.Projects)]
			if s.Stories[n.Slug] != nil {
				return n, true
			}
		}
	}
	return Project{}, false
}

// Load reads a site folder. An empty dir is an empty site. In site.yaml, ${DOMAIN} stands for
// domain (the host of the site's public URL), so demo addresses are configured in one place.
func Load(dir, domain string) (*Site, error) {
	s := &Site{Stories: map[string]*Story{}, Architectures: map[string]*Architecture{}, Dir: dir}
	if dir == "" {
		return s, nil
	}
	if err := decode(filepath.Join(dir, "site.yaml"), s, domain); err != nil {
		return nil, err
	}
	var problems []string
	for _, p := range s.Projects {
		if !slugPattern.MatchString(p.Slug) {
			continue // reported by validate; never used to build a path
		}
		path := filepath.Join(dir, "stories", p.Slug+".yaml")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue // a project may have no story yet
		}
		st := &Story{}
		if err := decode(path, st, domain); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		s.Stories[p.Slug] = st
	}
	for _, p := range s.Projects {
		if !slugPattern.MatchString(p.Slug) {
			continue
		}
		path := filepath.Join(dir, "architecture", p.Slug+".yaml")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue // a project may have no diagram yet
		}
		a := &Architecture{}
		if err := decode(path, a, domain); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		problems = append(problems, a.validate("architecture/"+p.Slug+".yaml")...)
		// A part may point into the story; the heading it names has to be there.
		for _, part := range a.Parts {
			if part.Story == "" {
				continue
			}
			if st := s.Stories[p.Slug]; st == nil || !st.Anchors()[part.Story] {
				problems = append(problems, fmt.Sprintf("architecture/%s.yaml: part %s: the story has no heading %q", p.Slug, part.ID, part.Story))
			}
		}
		s.Architectures[p.Slug] = a
	}
	problems = append(problems, s.validate()...)
	if len(problems) > 0 {
		return nil, errors.New("site content:\n  " + strings.Join(problems, "\n  "))
	}
	return s, nil
}

// decode reads YAML strictly: unknown keys are errors, so typos surface.
func decode(path string, v any, domain string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw = bytes.ReplaceAll(raw, []byte("${DOMAIN}"), []byte(domain))
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

var (
	slugPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)
	mediaPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$`)
	figures      = map[string]bool{"": true, "redacted": true, "chain": true, "architecture": true, "devices": true}
	placements   = map[Placement]bool{Lead: true, Side: true, Feature: true, Brief: true, "": true}
)

// MediaName reports whether name is an acceptable file name in media/: a plain name, no path.
func MediaName(name string) bool {
	return mediaPattern.MatchString(name) && !strings.Contains(name, "..")
}

func (s *Site) validate() []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	link := func(what, v string, relative bool) {
		if v == "" || (relative && strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//")) {
			return
		}
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			add("%s: an absolute http(s) URL without credentials", what)
		}
	}
	media := func(what, name string) {
		if name == "" {
			return
		}
		if !MediaName(name) {
			add("%s: a file name in media/, not a path", what)
			return
		}
		if _, err := os.Stat(filepath.Join(s.Dir, "media", name)); err != nil {
			add("%s: media/%s is missing", what, name)
		}
	}

	p := s.Profile
	media("profile.portrait", p.Portrait)
	media("profile.preview", p.Preview)
	if p.Links.Email != "" && !strings.Contains(p.Links.Email, "@") {
		add("profile.links.email: an email address")
	}
	link("profile.links.linkedin", p.Links.LinkedIn, false)
	link("profile.links.github", p.Links.GitHub, false)
	if cv := p.Links.CV; cv != "" && !strings.HasPrefix(cv, "http") {
		media("profile.links.cv", cv)
	} else {
		link("profile.links.cv", cv, false)
	}

	seen := map[string]bool{}
	for i, pr := range s.Projects {
		where := fmt.Sprintf("projects[%d]", i)
		if pr.Slug != "" {
			where = "project " + pr.Slug
		}
		if !slugPattern.MatchString(pr.Slug) {
			add("%s: slug must be lowercase letters, digits and dashes", where)
		}
		if seen[pr.Slug] {
			add("%s: duplicate slug", where)
		}
		seen[pr.Slug] = true
		for _, f := range []struct{ name, v string }{{"name", pr.Name}, {"headline", pr.Headline}, {"plain", pr.Plain}, {"hood", pr.Hood}} {
			if strings.TrimSpace(f.v) == "" {
				add("%s: %s is required", where, f.name)
			}
		}
		if !figures[pr.Figure] {
			add("%s: figure must be redacted, chain, architecture or devices", where)
		}
		if !placements[pr.Placement] {
			add("%s: placement must be lead, side, feature or brief", where)
		}
		link(where+": repo", pr.Repo, false)
		link(where+": docs", pr.Docs, false)
		link(where+": demo", pr.Demo, true)
		link(where+": health", pr.Health, false)
		link(where+": tour", pr.Tour, true)
		if pr.Monitor != "" && !slugPattern.MatchString(pr.Monitor) {
			add("%s: monitor must be a monitor's slug", where)
		}
		if pr.Demo == "" && (pr.Health != "" || pr.Tour != "" || len(pr.Intro) > 0) {
			add("%s: health, tour and intro need a demo", where)
		}
		if pr.Demo != "" && (len(pr.Intro) == 0 || len(pr.Intro) > 6) {
			add("%s: a demo needs an intro of 1 to 6 slides to show while it starts", where)
		}
		for j, sl := range pr.Intro {
			if sl.Title == "" || sl.Body == "" {
				add("%s: intro[%d] needs a title and a body", where, j)
			}
		}
	}
	for slug, st := range s.Stories {
		if st.Problem.Title == "" || st.Solution.Title == "" || len(st.Decisions) == 0 {
			add("story %s: needs a problem, a solution and at least one decision", slug)
		}
		for j, d := range st.Decisions {
			if d.Title == "" || d.Choice == "" || d.Why == "" {
				add("story %s: decisions[%d] needs a title, a choice and a why", slug, j)
			}
		}
	}
	return problems
}
