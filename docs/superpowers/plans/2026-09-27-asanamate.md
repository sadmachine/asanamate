# asanamate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build asanamate, a Go terminal UI that browses Asana tickets, runs configured shell actions with ticket data, resolves the ticket's git repo, and writes results back to Asana through subcommands.

**Architecture:** One binary, `cmd/asanamate`. Small internal packages each own one concern:
- `asana`: HTTP client
- `ticket`: ticket assembly and Markdown
- `filter`: query language
- `config` and `state`: TOML files
- `repo`: candidate listing and git validation
- `action`: environment variables, ticket files, and process launch
- `prompt` and `writeback`: confirmed Asana writes
- `setup`: first-run wizard
- `kitty`: image protocol
- `browser`: URL opener
- `listing`: `list`/`show` output formats for scripts and fzf
- `tui`: Bubble Tea v2 model (full or list-only)

The TUI owns only UI state and user flows. All side effects live in the packages above.

**Tech Stack:** Go 1.26, Bubble Tea v2 / Bubbles v2 / Lip Gloss v2 / Glamour v2 (`charm.land/*`), BurntSushi/toml, JohannesKaufmann/html-to-markdown v2, GoReleaser v2.

**Spec:** `docs/superpowers/specs/2026-09-27-asanamate-design.md`

## Global Constraints

- Module path: `github.com/sadmachine/asanamate`. `go 1.26` in go.mod.
- Token comes only from env var `ASANA_ACCESS_TOKEN`. It is never logged, written to disk, or sent to hosts other than the Asana API base URL.
- Direct dependencies are limited to: `charm.land/bubbletea/v2@v2.0.10`, `charm.land/bubbles/v2@v2.2.1`, `charm.land/lipgloss/v2@v2.0.6`, `charm.land/glamour/v2@v2.0.1`, `github.com/charmbracelet/x/ansi` (already a transitive dependency), `github.com/BurntSushi/toml@v1.6.0`, `github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.2`, `golang.org/x/sync` (errgroup; already transitive). Add nothing else.
- Platforms: darwin and linux only (background actions use `syscall.SysProcAttr{Setpgid: true}`).
- Ticket data reaches shell commands only through environment variables and files. Never interpolate ticket text into a command string.
- Files under the state dir use mode 0600. Directories use mode 0700.
- Config path: `$XDG_CONFIG_HOME/asanamate/config.toml`, falling back to `~/.config/asanamate/config.toml`. State dir: `$XDG_STATE_HOME/asanamate`, falling back to `~/.local/state/asanamate`.
- Workflow rules for this repo's owner: prefix every shell command with `rtk` (for example `rtk go test ./...`). Ask the user before staging or committing each task's changes.
- Style: Go doc comments on exported identifiers, few inline comments, `gofmt` clean, `go vet ./...` clean.

## Review Focus

- **Shell metacharacters in ticket titles or fields** (`$(...)`, quotes, `;`) must reach commands as literal text. Covered by `TestCommandIsInjectionSafe` in Task 7.
- **Terminal escape sequences in Asana text** (ESC, C1 controls) must be stripped before display or export. Covered by `TestMarkdownStripsControlCharacters` in Task 4.
- **Out-of-order async responses:** a detail fetch for a ticket that is no longer selected, or a task list for a project the user already left, must not replace what is on screen. Covered by `TestStaleTasksIgnored` and `TestDetailForOtherTicketCachedNotShown` in Task 12.
- **A saved repo link whose directory was deleted or is no longer a git repo** must re-prompt instead of running the action in the wrong place. Covered by `TestStaleRepoLinkReprompts` in Task 13.
- **A write needing confirmation with no terminal** (background action) must refuse without writing. Covered by `TestConfirmErrorBlocksWrite` in Task 8.

---

### Task 1: Module scaffold and config package

**Files:**
- Create: `go.mod`, `.gitignore`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `config.TokenEnv = "ASANA_ACCESS_TOKEN"`, `config.ErrNotConfigured`
  - `config.ModeForeground`, `config.ModeBackground`, `config.ModeExit` (strings `"foreground"`, `"background"`, `"exit"`)
  - `type Config struct { Workspace, Theme, Images, DefaultFilter string; ConfirmWrites bool; RepoSource RepoSource; Actions []Action }`
  - `type RepoSource struct { Command string }`
  - `type Action struct { Name, Key, Mode string; Repo bool; Command string; ConfirmWrites *bool }`
  - `func Default() Config`, `func Load(path string) (Config, error)`, `func (c Config) ConfirmWritesFor(a Action) bool`
  - `func Dir() (string, error)`, `func Path() (string, error)`, `func StateDir() (string, error)`, `func Token() (string, error)`

- [ ] **Step 1: Initialize the module**

```bash
rtk go mod init github.com/sadmachine/asanamate
rtk go mod edit -go=1.26
rtk go get github.com/BurntSushi/toml@v1.6.0
printf 'dist/\n' > .gitignore
```

- [ ] **Step 2: Write the failing test** `internal/config/config_test.go`

```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeFile(t, `workspace = "123"
[[actions]]
name = "View"
key = "v"
command = "less \"$ASANAMATE_TICKET_MD\""
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dark" || cfg.Images != "auto" || cfg.DefaultFilter != "is:open" || !cfg.ConfirmWrites {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.Actions[0].Mode != ModeForeground {
		t.Fatalf("mode = %q, want %q", cfg.Actions[0].Mode, ModeForeground)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown key":   "workspace = \"1\"\nworkspaec = \"2\"\n",
		"no workspace":  "theme = \"dark\"\n",
		"bad theme":     "workspace = \"1\"\ntheme = \"blue\"\n",
		"bad images":    "workspace = \"1\"\nimages = \"sixel\"\n",
		"bad mode":      "workspace = \"1\"\n[[actions]]\nname = \"a\"\nkey = \"a\"\nmode = \"later\"\ncommand = \"true\"\n",
		"long key":      "workspace = \"1\"\n[[actions]]\nname = \"a\"\nkey = \"ab\"\ncommand = \"true\"\n",
		"duplicate key": "workspace = \"1\"\n[[actions]]\nname = \"a\"\nkey = \"x\"\ncommand = \"true\"\n[[actions]]\nname = \"b\"\nkey = \"x\"\ncommand = \"true\"\n",
		"no command":    "workspace = \"1\"\n[[actions]]\nname = \"a\"\nkey = \"a\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeFile(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestConfirmWritesFor(t *testing.T) {
	off := false
	cfg := Config{ConfirmWrites: true}
	if !cfg.ConfirmWritesFor(Action{}) {
		t.Fatal("want the global default (true)")
	}
	if cfg.ConfirmWritesFor(Action{ConfirmWrites: &off}) {
		t.Fatal("want the action override (false)")
	}
}

func TestDirsHonorXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	p, _ := Path()
	s, _ := StateDir()
	if p != "/tmp/cfg/asanamate/config.toml" || s != "/tmp/state/asanamate" {
		t.Fatalf("got %s and %s", p, s)
	}
}

func TestDirsFallBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	p, _ := Path()
	s, _ := StateDir()
	if p != "/home/u/.config/asanamate/config.toml" || s != "/home/u/.local/state/asanamate" {
		t.Fatalf("got %s and %s", p, s)
	}
}

func TestToken(t *testing.T) {
	t.Setenv(TokenEnv, "  ")
	if _, err := Token(); err == nil || !strings.Contains(err.Error(), TokenEnv) {
		t.Fatalf("err = %v, want mention of %s", err, TokenEnv)
	}
	t.Setenv(TokenEnv, "abc")
	if tok, err := Token(); err != nil || tok != "abc" {
		t.Fatalf("token = %q, err = %v", tok, err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `rtk go test ./internal/config/`
Expected: FAIL, build error `undefined: Load`.

- [ ] **Step 4: Implement** `internal/config/config.go`

```go
// Package config loads the user-edited asanamate configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// TokenEnv names the environment variable that holds the Asana personal access token.
const TokenEnv = "ASANA_ACCESS_TOKEN"

// Action run modes.
const (
	ModeForeground = "foreground"
	ModeBackground = "background"
	ModeExit       = "exit"
)

// ErrNotConfigured means the config file does not exist yet.
var ErrNotConfigured = errors.New("asanamate is not configured; run `asanamate setup`")

// Config is the contents of config.toml.
type Config struct {
	Workspace     string     `toml:"workspace"`
	Theme         string     `toml:"theme"`
	Images        string     `toml:"images"`
	DefaultFilter string     `toml:"default_filter"`
	ConfirmWrites bool       `toml:"confirm_writes"`
	RepoSource    RepoSource `toml:"repo_source"`
	Actions       []Action   `toml:"actions"`
}

// RepoSource configures where repo picker candidates come from.
type RepoSource struct {
	Command string `toml:"command"`
}

// Action is a user-defined command run against the selected ticket.
type Action struct {
	Name          string `toml:"name"`
	Key           string `toml:"key"`
	Mode          string `toml:"mode"`
	Repo          bool   `toml:"repo"`
	Command       string `toml:"command"`
	ConfirmWrites *bool  `toml:"confirm_writes"`
}

// Default returns the values used for keys the config file omits.
func Default() Config {
	return Config{Theme: "dark", Images: "auto", DefaultFilter: "is:open", ConfirmWrites: true}
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, ErrNotConfigured
	}
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("%s: unknown keys: %v", path, undecoded)
	}
	for i := range cfg.Actions {
		if cfg.Actions[i].Mode == "" {
			cfg.Actions[i].Mode = ModeForeground
		}
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Workspace == "" {
		return errors.New("workspace is required; run `asanamate setup`")
	}
	if c.Theme != "dark" && c.Theme != "light" {
		return fmt.Errorf("theme must be \"dark\" or \"light\", got %q", c.Theme)
	}
	switch c.Images {
	case "auto", "kitty", "off":
	default:
		return fmt.Errorf("images must be \"auto\", \"kitty\", or \"off\", got %q", c.Images)
	}
	keys := map[string]string{}
	for _, a := range c.Actions {
		if a.Name == "" || a.Command == "" {
			return errors.New("every action needs a name and a command")
		}
		switch a.Mode {
		case ModeForeground, ModeBackground, ModeExit:
		default:
			return fmt.Errorf("action %q: mode must be foreground, background, or exit", a.Name)
		}
		if len([]rune(a.Key)) != 1 {
			return fmt.Errorf("action %q: key must be a single character", a.Name)
		}
		if other, ok := keys[a.Key]; ok {
			return fmt.Errorf("actions %q and %q share key %q", other, a.Name, a.Key)
		}
		keys[a.Key] = a.Name
	}
	return nil
}

// ConfirmWritesFor reports whether Asana writes made by action a need confirmation.
func (c Config) ConfirmWritesFor(a Action) bool {
	if a.ConfirmWrites != nil {
		return *a.ConfirmWrites
	}
	return c.ConfirmWrites
}

// Dir returns the asanamate config directory.
func Dir() (string, error) { return xdgDir("XDG_CONFIG_HOME", ".config") }

// Path returns the config file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// StateDir returns the directory for files asanamate writes itself.
func StateDir() (string, error) { return xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state")) }

func xdgDir(envKey, homeFallback string) (string, error) {
	base := os.Getenv(envKey)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, homeFallback)
	}
	return filepath.Join(base, "asanamate"), nil
}

// Token returns the Asana personal access token from the environment.
func Token() (string, error) {
	token := strings.TrimSpace(os.Getenv(TokenEnv))
	if token == "" {
		return "", fmt.Errorf("%s is not set; create a personal access token at https://app.asana.com/0/my-apps and export it", TokenEnv)
	}
	return token, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `rtk go test ./internal/config/ && rtk go vet ./...`
Expected: PASS, no vet output.

- [ ] **Step 6: Commit** (ask the user first)

```bash
rtk git add go.mod go.sum .gitignore internal/config
rtk git commit -m "feat(config): load and validate config.toml"
```

---

### Task 2: State package

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Produces:
  - `state.FileName = "state.toml"`
  - `type State struct { Repos map[string]string; RecentProjects []string }` (Repos maps project gid to repo path; RecentProjects holds gids, most recent first)
  - `func Load(path string) (*State, error)`, `func (s *State) Save() error`, `func (s *State) TouchProject(gid string)`, `func (s *State) LinkRepo(projectGID, path string)`

- [ ] **Step 1: Write the failing test** `internal/state/state_test.go`

```go
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Repos) != 0 || len(s.RecentProjects) != 0 {
		t.Fatalf("want empty state, got %+v", s)
	}
}

func TestSaveRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	s, _ := Load(path)
	s.LinkRepo("123", "/code/web")
	s.TouchProject("123")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Repos["123"] != "/code/web" || again.RecentProjects[0] != "123" {
		t.Fatalf("round trip lost data: %+v", again)
	}
}

func TestTouchProjectOrdersAndCaps(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), FileName))
	for i := 0; i < maxRecent+5; i++ {
		s.TouchProject(fmt.Sprint(i))
	}
	s.TouchProject("7")
	if len(s.RecentProjects) != maxRecent {
		t.Fatalf("len = %d, want %d", len(s.RecentProjects), maxRecent)
	}
	if s.RecentProjects[0] != "7" || s.RecentProjects[1] != fmt.Sprint(maxRecent+4) {
		t.Fatalf("order = %v", s.RecentProjects)
	}
	for i, gid := range s.RecentProjects[1:] {
		if gid == "7" {
			t.Fatalf("duplicate at %d: %v", i+1, s.RecentProjects)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/state/`
Expected: FAIL, `undefined: Load`.

- [ ] **Step 3: Implement** `internal/state/state.go`

```go
// Package state stores data asanamate learns while running: repo links and recent projects.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
)

// FileName is the state file's name inside the state directory.
const FileName = "state.toml"

const maxRecent = 20

// State is the contents of state.toml.
type State struct {
	// Repos maps an Asana project gid to a local git repository path.
	Repos map[string]string `toml:"repos"`
	// RecentProjects holds project gids, most recently opened first.
	RecentProjects []string `toml:"recent_projects"`

	path string
}

// Load reads the state file. A missing file yields empty state.
func Load(path string) (*State, error) {
	s := &State{path: path}
	if _, err := toml.DecodeFile(path, s); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Repos == nil {
		s.Repos = map[string]string{}
	}
	return s, nil
}

// Save writes the state atomically with owner-only permissions.
func (s *State) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// TouchProject moves gid to the front of the recent projects list.
func (s *State) TouchProject(gid string) {
	s.RecentProjects = slices.DeleteFunc(s.RecentProjects, func(g string) bool { return g == gid })
	s.RecentProjects = append([]string{gid}, s.RecentProjects...)
	if len(s.RecentProjects) > maxRecent {
		s.RecentProjects = s.RecentProjects[:maxRecent]
	}
}

// LinkRepo remembers the repository used for a project.
func (s *State) LinkRepo(projectGID, path string) {
	s.Repos[projectGID] = path
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `rtk go test ./internal/state/`
Expected: PASS.

- [ ] **Step 5: Commit** (ask the user first)

```bash
rtk git add internal/state
rtk git commit -m "feat(state): persist repo links and recent projects"
```

---

### Task 3: Asana client

**Files:**
- Create: `internal/asana/types.go`, `internal/asana/client.go`, `internal/asana/api.go`
- Test: `internal/asana/client_test.go`

**Interfaces:**
- Produces:
  - Types: `Ref{GID, Name}`, `Membership{Project Ref; Section *Ref}`, `EnumOption{GID, Name}`, `CustomField{GID, Name, ResourceSubtype string; DisplayValue *string; EnumOptions []EnumOption}`, `Task{GID, Name string; Completed bool; DueOn *string; Assignee, AssigneeSection, Parent *Ref; Memberships []Membership; Tags, Dependencies, Dependents []Ref; PermalinkURL, HTMLNotes, CreatedAt, ModifiedAt string; CustomFields []CustomField}`, `Story{GID, Type, CreatedAt, HTMLText string; CreatedBy *Ref}`, `Attachment{GID, Name, Host, PermanentURL, ViewURL string; DownloadURL *string}`, `User{GID, Name string; Workspaces []Ref}`, `Project{GID, Name string; Members []Ref}`
  - `func (t Task) SectionIn(projectGID string) string`
  - `func (t Task) SectionFor(projectGID string) string` (the My Tasks section when `projectGID == ""`, otherwise `SectionIn`)
  - `func ValidGID(s string) bool`
  - `func New(token string) *Client` (exported field `BaseURL`), `type APIError struct{ Status int; Message string }`
  - Methods on `*Client` (all take `ctx context.Context` first):
    - `Me() (User, error)`
    - `MyTasks(workspace string, since time.Time) ([]Task, error)`
    - `ProjectTasks(projectGID string, since time.Time) ([]Task, error)`
    - `MemberProjects(workspace, userGID string) ([]Project, error)`
    - `Task(gid string) (Task, error)`
    - `Comments(gid string) ([]Story, error)`
    - `Subtasks(gid string) ([]Task, error)`
    - `Attachments(taskGID string) ([]Attachment, error)`
    - `Attachment(gid string) (Attachment, error)`
    - `Sections(projectGID string) ([]Ref, error)`
    - `AddComment(taskGID, text string) error`
    - `AddToSection(sectionGID, taskGID string) error`
    - `SetCustomField(taskGID, fieldGID string, value any) error`

- [ ] **Step 1: Write the failing test** `internal/asana/client_test.go`

```go
package asana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var ctx = context.Background()

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("tok")
	c.BaseURL = srv.URL
	c.sleep = func(time.Duration) {}
	return c
}

func TestAuthHeaderAndErrorMessage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"errors":[{"message":"Not a member"}]}`)
	})
	_, err := c.Me(ctx)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Message != "Not a member" {
		t.Fatalf("err = %v", err)
	}
}

func TestPaginationFollowsOffset(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("offset") {
		case "":
			io.WriteString(w, `{"data":[{"gid":"1","name":"a"}],"next_page":{"offset":"p2"}}`)
		case "p2":
			io.WriteString(w, `{"data":[{"gid":"2","name":"b"}],"next_page":null}`)
		default:
			t.Errorf("unexpected offset %q", r.URL.Query().Get("offset"))
		}
	})
	secs, err := c.Sections(ctx, "9")
	if err != nil || len(secs) != 2 || secs[1].Name != "b" {
		t.Fatalf("secs = %+v, err = %v", secs, err)
	}
}

func TestRetriesOnceAfter429(t *testing.T) {
	calls := 0
	var slept time.Duration
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"data":{"gid":"1","name":"Me"}}`)
	})
	c.sleep = func(d time.Duration) { slept = d }
	me, err := c.Me(ctx)
	if err != nil || me.Name != "Me" || calls != 2 || slept != 2*time.Second {
		t.Fatalf("me = %+v, err = %v, calls = %d, slept = %v", me, err, calls, slept)
	}
}

func TestMyTasksResolvesTaskList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/user_task_list":
			if r.URL.Query().Get("workspace") != "77" {
				t.Errorf("workspace = %q", r.URL.Query().Get("workspace"))
			}
			io.WriteString(w, `{"data":{"gid":"55"}}`)
		case "/user_task_lists/55/tasks":
			if r.URL.Query().Get("completed_since") == "" {
				t.Error("missing completed_since")
			}
			io.WriteString(w, `{"data":[{"gid":"1","name":"Fix","assignee_section":{"gid":"s","name":"Today"},"memberships":[{"project":{"gid":"p","name":"Web"},"section":{"gid":"x","name":"Doing"}}]}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	tasks, err := c.MyTasks(ctx, "77", time.Now())
	if err != nil || len(tasks) != 1 || tasks[0].SectionFor("") != "Today" || tasks[0].SectionFor("p") != "Doing" || tasks[0].SectionIn("zz") != "" {
		t.Fatalf("tasks = %+v, err = %v", tasks, err)
	}
}

func TestMemberProjectsFiltersByUser(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("archived") != "false" {
			t.Errorf("archived = %q", r.URL.Query().Get("archived"))
		}
		io.WriteString(w, `{"data":[{"gid":"a","name":"Mine","members":[{"gid":"me"}]},{"gid":"b","name":"Other","members":[{"gid":"x"}]}]}`)
	})
	projects, err := c.MemberProjects(ctx, "w", "me")
	if err != nil || len(projects) != 1 || projects[0].Name != "Mine" {
		t.Fatalf("projects = %+v, err = %v", projects, err)
	}
}

func TestCommentsKeepsOnlyComments(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"gid":"1","type":"system"},{"gid":"2","type":"comment","html_text":"<body>hi</body>"}]}`)
	})
	comments, err := c.Comments(ctx, "9")
	if err != nil || len(comments) != 1 || comments[0].GID != "2" {
		t.Fatalf("comments = %+v, err = %v", comments, err)
	}
}

func TestWritesWrapBodyInData(t *testing.T) {
	var got map[string]map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/1/stories" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"data":{}}`)
	})
	if err := c.AddComment(ctx, "1", "hi"); err != nil {
		t.Fatal(err)
	}
	if got["data"]["text"] != "hi" {
		t.Fatalf("body = %v", got)
	}
}

func TestValidGID(t *testing.T) {
	for s, want := range map[string]bool{"123": true, "": false, "12a": false, "../1": false} {
		if ValidGID(s) != want {
			t.Errorf("ValidGID(%q) = %v", s, !want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/asana/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Implement** `internal/asana/types.go`

```go
package asana

// Ref is a compact Asana object: a gid and a display name.
type Ref struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// Membership places a task in a project section.
type Membership struct {
	Project Ref  `json:"project"`
	Section *Ref `json:"section,omitempty"`
}

// EnumOption is one choice of an enum custom field.
type EnumOption struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

// CustomField is a custom field value on a task.
type CustomField struct {
	GID             string       `json:"gid"`
	Name            string       `json:"name"`
	ResourceSubtype string       `json:"resource_subtype"`
	DisplayValue    *string      `json:"display_value"`
	EnumOptions     []EnumOption `json:"enum_options,omitempty"`
}

// Task is an Asana task. List endpoints fill a subset of the fields.
type Task struct {
	GID             string        `json:"gid"`
	Name            string        `json:"name"`
	Completed       bool          `json:"completed"`
	DueOn           *string       `json:"due_on,omitempty"`
	Assignee        *Ref          `json:"assignee,omitempty"`
	AssigneeSection *Ref          `json:"assignee_section,omitempty"`
	Memberships     []Membership  `json:"memberships,omitempty"`
	Tags            []Ref         `json:"tags,omitempty"`
	PermalinkURL    string        `json:"permalink_url,omitempty"`
	HTMLNotes       string        `json:"html_notes,omitempty"`
	CustomFields    []CustomField `json:"custom_fields,omitempty"`
	Dependencies    []Ref         `json:"dependencies,omitempty"`
	Dependents      []Ref         `json:"dependents,omitempty"`
	Parent          *Ref          `json:"parent,omitempty"`
	CreatedAt       string        `json:"created_at,omitempty"`
	ModifiedAt      string        `json:"modified_at,omitempty"`
}

// SectionIn returns the task's section name in the given project, or "".
func (t Task) SectionIn(projectGID string) string {
	for _, m := range t.Memberships {
		if m.Project.GID == projectGID && m.Section != nil {
			return m.Section.Name
		}
	}
	return ""
}

// SectionFor returns the section shown for the task in a view: the My Tasks
// section when projectGID is empty, otherwise the section in that project.
func (t Task) SectionFor(projectGID string) string {
	if projectGID != "" {
		return t.SectionIn(projectGID)
	}
	if t.AssigneeSection != nil {
		return t.AssigneeSection.Name
	}
	return ""
}

// Story is an entry in a task's activity feed; comments have Type "comment".
type Story struct {
	GID       string `json:"gid"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	CreatedBy *Ref   `json:"created_by,omitempty"`
	HTMLText  string `json:"html_text"`
}

// Attachment is a file attached to a task.
type Attachment struct {
	GID          string  `json:"gid"`
	Name         string  `json:"name"`
	Host         string  `json:"host"`
	DownloadURL  *string `json:"download_url,omitempty"`
	PermanentURL string  `json:"permanent_url,omitempty"`
	ViewURL      string  `json:"view_url,omitempty"`
}

// User is the authenticated user.
type User struct {
	GID        string `json:"gid"`
	Name       string `json:"name"`
	Workspaces []Ref  `json:"workspaces"`
}

// Project is an Asana project with its members.
type Project struct {
	GID     string `json:"gid"`
	Name    string `json:"name"`
	Members []Ref  `json:"members"`
}
```

- [ ] **Step 4: Implement** `internal/asana/client.go`

```go
// Package asana is a minimal client for the Asana REST API.
package asana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// DefaultBaseURL is the Asana REST API root.
const DefaultBaseURL = "https://app.asana.com/api/1.0"

const maxRetryWait = 10 * time.Second

var gidPattern = regexp.MustCompile(`^[0-9]+$`)

// ValidGID reports whether s looks like an Asana gid.
func ValidGID(s string) bool { return gidPattern.MatchString(s) }

// Client calls the Asana API with a personal access token.
type Client struct {
	BaseURL string
	token   string
	http    *http.Client
	sleep   func(time.Duration)
}

// New returns a client that authenticates with token.
func New(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
		sleep:   time.Sleep,
	}
}

// APIError is a non-2xx response from Asana.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("asana: HTTP %d: %s", e.Status, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(map[string]any{"data": body})
		if err != nil {
			return err
		}
		payload = b
	}
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			if wait := retryAfter(resp.Header.Get("Retry-After")); wait <= maxRetryWait {
				c.sleep(wait)
				continue
			}
		}
		if resp.StatusCode >= 300 {
			return apiError(resp.StatusCode, data)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

func retryAfter(header string) time.Duration {
	if s, err := strconv.Atoi(header); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Second
}

func apiError(status int, body []byte) error {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	msg := http.StatusText(status)
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 && e.Errors[0].Message != "" {
		msg = e.Errors[0].Message
	}
	return &APIError{Status: status, Message: msg}
}

type nextPage struct {
	Offset string `json:"offset"`
}

func getAll[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, error) {
	q.Set("limit", "100")
	var all []T
	for {
		var page struct {
			Data     []T       `json:"data"`
			NextPage *nextPage `json:"next_page"`
		}
		if err := c.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.NextPage == nil || page.NextPage.Offset == "" {
			return all, nil
		}
		q.Set("offset", page.NextPage.Offset)
	}
}

func getOne[T any](ctx context.Context, c *Client, path string, q url.Values) (T, error) {
	var r struct {
		Data T `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, path, q, nil, &r)
	return r.Data, err
}
```

- [ ] **Step 5: Implement** `internal/asana/api.go`

```go
package asana

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const (
	listFields       = "name,completed,due_on,assignee.name,assignee_section.name,memberships.project.name,memberships.section.name,tags.name,permalink_url"
	detailFields     = listFields + ",html_notes,custom_fields.name,custom_fields.display_value,custom_fields.resource_subtype,custom_fields.enum_options.name,dependencies.name,dependents.name,parent.name,created_at,modified_at"
	attachmentFields = "name,host,download_url,permanent_url,view_url"
)

func fields(f string) url.Values { return url.Values{"opt_fields": {f}} }

// Me returns the authenticated user and their workspaces.
func (c *Client) Me(ctx context.Context) (User, error) {
	return getOne[User](ctx, c, "/users/me", fields("name,workspaces.name"))
}

// MyTasks returns the user's My Tasks list: incomplete tasks plus tasks completed after since.
func (c *Client) MyTasks(ctx context.Context, workspace string, since time.Time) ([]Task, error) {
	list, err := getOne[Ref](ctx, c, "/users/me/user_task_list", url.Values{"workspace": {workspace}})
	if err != nil {
		return nil, err
	}
	return c.tasksIn(ctx, "/user_task_lists/"+list.GID+"/tasks", since)
}

// ProjectTasks returns a project's incomplete tasks plus tasks completed after since.
func (c *Client) ProjectTasks(ctx context.Context, projectGID string, since time.Time) ([]Task, error) {
	return c.tasksIn(ctx, "/projects/"+projectGID+"/tasks", since)
}

func (c *Client) tasksIn(ctx context.Context, path string, since time.Time) ([]Task, error) {
	q := fields(listFields)
	q.Set("completed_since", since.UTC().Format(time.RFC3339))
	return getAll[Task](ctx, c, path, q)
}

// MemberProjects returns unarchived projects in workspace that userGID is a member of.
func (c *Client) MemberProjects(ctx context.Context, workspace, userGID string) ([]Project, error) {
	q := fields("name,members.gid")
	q.Set("workspace", workspace)
	q.Set("archived", "false")
	all, err := getAll[Project](ctx, c, "/projects", q)
	if err != nil {
		return nil, err
	}
	var mine []Project
	for _, p := range all {
		for _, m := range p.Members {
			if m.GID == userGID {
				mine = append(mine, p)
				break
			}
		}
	}
	return mine, nil
}

// Task returns one task with every field the reading pane shows.
func (c *Client) Task(ctx context.Context, gid string) (Task, error) {
	return getOne[Task](ctx, c, "/tasks/"+gid, fields(detailFields))
}

// Comments returns a task's comment stories, oldest first.
func (c *Client) Comments(ctx context.Context, gid string) ([]Story, error) {
	all, err := getAll[Story](ctx, c, "/tasks/"+gid+"/stories", fields("type,created_at,created_by.name,html_text"))
	if err != nil {
		return nil, err
	}
	var comments []Story
	for _, s := range all {
		if s.Type == "comment" {
			comments = append(comments, s)
		}
	}
	return comments, nil
}

// Subtasks returns a task's direct subtasks.
func (c *Client) Subtasks(ctx context.Context, gid string) ([]Task, error) {
	return getAll[Task](ctx, c, "/tasks/"+gid+"/subtasks", fields("name,completed"))
}

// Attachments returns a task's attachments.
func (c *Client) Attachments(ctx context.Context, taskGID string) ([]Attachment, error) {
	q := fields(attachmentFields)
	q.Set("parent", taskGID)
	return getAll[Attachment](ctx, c, "/attachments", q)
}

// Attachment returns one attachment with a fresh download URL.
func (c *Client) Attachment(ctx context.Context, gid string) (Attachment, error) {
	return getOne[Attachment](ctx, c, "/attachments/"+gid, fields(attachmentFields))
}

// Sections returns a project's sections.
func (c *Client) Sections(ctx context.Context, projectGID string) ([]Ref, error) {
	return getAll[Ref](ctx, c, "/projects/"+projectGID+"/sections", fields("name"))
}

// AddComment posts a plain-text comment on a task.
func (c *Client) AddComment(ctx context.Context, taskGID, text string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+taskGID+"/stories", nil, map[string]any{"text": text}, nil)
}

// AddToSection moves a task into a section.
func (c *Client) AddToSection(ctx context.Context, sectionGID, taskGID string) error {
	return c.do(ctx, http.MethodPost, "/sections/"+sectionGID+"/addTask", nil, map[string]any{"task": taskGID}, nil)
}

// SetCustomField sets one custom field on a task; a nil value clears it.
func (c *Client) SetCustomField(ctx context.Context, taskGID, fieldGID string, value any) error {
	body := map[string]any{"custom_fields": map[string]any{fieldGID: value}}
	return c.do(ctx, http.MethodPut, "/tasks/"+taskGID, nil, body, nil)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `rtk go test ./internal/asana/ && rtk go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add internal/asana
rtk git commit -m "feat(asana): REST client with pagination and retry"
```

---

### Task 4: Ticket assembly and Markdown

**Files:**
- Create: `internal/ticket/ticket.go`, `internal/ticket/markdown.go`
- Test: `internal/ticket/ticket_test.go`

**Interfaces:**
- Consumes: `asana.Client` methods `Task`, `Comments`, `Attachments`, `Subtasks`, and the asana types.
- Produces:
  - `type Ticket struct { asana.Task; Comments []asana.Story; Attachments []asana.Attachment; Subtasks []asana.Task }` (JSON keys `comments`, `attachments`, `subtasks`; Task fields are flattened)
  - `func Fetch(ctx context.Context, c *asana.Client, gid string) (Ticket, error)`
  - `const RecentlyCompleted = 7 * 24 * time.Hour`
  - `func List(ctx context.Context, c *asana.Client, workspace, projectGID string) ([]asana.Task, error)` (My Tasks when `projectGID == ""`; incomplete tasks plus tasks completed within `RecentlyCompleted`; used by both the TUI and `asanamate list`)
  - `func (t Ticket) Markdown() string`
  - `func Clean(s string) string` (removes control characters except `\n` and `\t`)
  - `func AttachmentURL(a asana.Attachment) string`

- [ ] **Step 1: Add the dependencies**

```bash
rtk go get github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.2 golang.org/x/sync
```

- [ ] **Step 2: Write the failing test** `internal/ticket/ticket_test.go`

```go
package ticket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

func sample() Ticket {
	due, cpa := "2026-10-01", "CPA-2491"
	return Ticket{
		Task: asana.Task{
			GID: "1", Name: "Fix \x1b[31mlogin\u009b", PermalinkURL: "https://app.asana.com/t/1", DueOn: &due,
			Memberships:  []asana.Membership{{Project: asana.Ref{GID: "p", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
			CustomFields: []asana.CustomField{{Name: "CPA", DisplayValue: &cpa}, {Name: "Branch Name ", DisplayValue: nil}},
			HTMLNotes:    "<body>Use <strong>SSO</strong>.</body>",
		},
		Comments:    []asana.Story{{CreatedAt: "2026-09-20T10:00:00Z", CreatedBy: &asana.Ref{Name: "Sam"}, HTMLText: "<body>Looks good</body>"}},
		Attachments: []asana.Attachment{{Name: "shot.png", PermanentURL: "https://app.asana.com/a/1"}},
		Subtasks:    []asana.Task{{Name: "Write test", Completed: true}},
	}
}

func TestMarkdownSections(t *testing.T) {
	md := sample().Markdown()
	for _, want := range []string{
		"- **Status:** open",
		"- **Due:** 2026-10-01",
		"- **Project:** Web / Doing",
		"## Fields",
		"- **CPA:** CPA-2491",
		"Use **SSO**.",
		"- [x] Write test",
		"1. [shot.png](https://app.asana.com/a/1)",
		"### Sam · 2026-09-20",
		"Looks good",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Branch Name") {
		t.Error("empty custom fields must be omitted")
	}
}

func TestMarkdownStripsControlCharacters(t *testing.T) {
	md := sample().Markdown()
	if strings.ContainsAny(md, "\x1b\u009b") {
		t.Fatalf("control characters leaked: %q", md)
	}
	if !strings.HasPrefix(md, "# Fix [31mlogin\n") {
		t.Fatalf("title line = %q", strings.SplitN(md, "\n", 2)[0])
	}
}

func TestJSONFlattensTaskAndIncludesExtras(t *testing.T) {
	b, err := json.Marshal(sample())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"gid":"1"`, `"comments":[`, `"attachments":[`, `"subtasks":[`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json missing %s: %s", want, b)
		}
	}
}

func TestFetchCombinesEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks/1":
			io.WriteString(w, `{"data":{"gid":"1","name":"T"}}`)
		case "/tasks/1/stories":
			io.WriteString(w, `{"data":[{"gid":"s","type":"comment","html_text":"<body>c</body>"}]}`)
		case "/attachments":
			io.WriteString(w, `{"data":[{"gid":"a","name":"f.pdf"}]}`)
		case "/tasks/1/subtasks":
			io.WriteString(w, `{"data":[{"gid":"2","name":"sub"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	tk, err := Fetch(context.Background(), c, "1")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Name != "T" || len(tk.Comments) != 1 || len(tk.Attachments) != 1 || len(tk.Subtasks) != 1 {
		t.Fatalf("ticket = %+v", tk)
	}
}

func TestListPicksViewEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/users/me/user_task_list" {
			io.WriteString(w, `{"data":{"gid":"55"}}`)
			return
		}
		if r.URL.Query().Get("completed_since") == "" {
			t.Error("missing completed_since")
		}
		io.WriteString(w, `{"data":[{"gid":"1","name":"T"}]}`)
	}))
	defer srv.Close()
	c := asana.New("tok")
	c.BaseURL = srv.URL
	if tasks, err := List(context.Background(), c, "w", ""); err != nil || len(tasks) != 1 {
		t.Fatalf("my tasks: %v %v", tasks, err)
	}
	if _, err := List(context.Background(), c, "w", "9"); err != nil {
		t.Fatal(err)
	}
	want := "/users/me/user_task_list,/user_task_lists/55/tasks,/projects/9/tasks"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("paths = %s, want %s", got, want)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `rtk go test ./internal/ticket/`
Expected: FAIL, `undefined: Ticket`.

- [ ] **Step 4: Implement** `internal/ticket/ticket.go`

```go
// Package ticket assembles an Asana task with its comments, attachments, and subtasks.
package ticket

import (
	"context"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/sadmachine/asanamate/internal/asana"
)

// RecentlyCompleted is how far back completed tasks are included in lists.
const RecentlyCompleted = 7 * 24 * time.Hour

// List returns the tasks for a view: My Tasks when projectGID is empty,
// otherwise the project's tasks. Incomplete tasks and recently completed ones
// are included.
func List(ctx context.Context, c *asana.Client, workspace, projectGID string) ([]asana.Task, error) {
	since := time.Now().Add(-RecentlyCompleted)
	if projectGID == "" {
		return c.MyTasks(ctx, workspace, since)
	}
	return c.ProjectTasks(ctx, projectGID, since)
}

// Ticket is a task plus everything the reading pane and actions need.
type Ticket struct {
	asana.Task
	Comments    []asana.Story      `json:"comments"`
	Attachments []asana.Attachment `json:"attachments"`
	Subtasks    []asana.Task       `json:"subtasks"`
}

// Fetch loads a ticket, calling the four endpoints in parallel.
func Fetch(ctx context.Context, c *asana.Client, gid string) (Ticket, error) {
	var t Ticket
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { t.Task, err = c.Task(ctx, gid); return err })
	g.Go(func() (err error) { t.Comments, err = c.Comments(ctx, gid); return err })
	g.Go(func() (err error) { t.Attachments, err = c.Attachments(ctx, gid); return err })
	g.Go(func() (err error) { t.Subtasks, err = c.Subtasks(ctx, gid); return err })
	return t, g.Wait()
}

// Clean removes control characters (except newline and tab) so untrusted
// Asana text cannot inject terminal escape sequences.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// AttachmentURL returns the best browser URL for an attachment.
func AttachmentURL(a asana.Attachment) string {
	if a.PermanentURL != "" {
		return a.PermanentURL
	}
	return a.ViewURL
}
```

- [ ] **Step 5: Implement** `internal/ticket/markdown.go`

```go
package ticket

import (
	"fmt"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/sadmachine/asanamate/internal/asana"
)

// Markdown renders the ticket for the reading pane and for $ASANAMATE_TICKET_MD.
func (t Ticket) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", Clean(t.Name))
	item := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, Clean(value))
		}
	}
	status := "open"
	if t.Completed {
		status = "done"
	}
	item("Status", status)
	item("URL", t.PermalinkURL)
	if t.Assignee != nil {
		item("Assignee", t.Assignee.Name)
	}
	if t.DueOn != nil {
		item("Due", *t.DueOn)
	}
	for _, m := range t.Memberships {
		value := m.Project.Name
		if m.Section != nil {
			value += " / " + m.Section.Name
		}
		item("Project", value)
	}
	if t.AssigneeSection != nil {
		item("My Tasks section", t.AssigneeSection.Name)
	}
	item("Tags", joinNames(t.Tags, ", "))
	if t.Parent != nil {
		item("Parent", t.Parent.Name)
	}

	var fields []string
	for _, f := range t.CustomFields {
		if f.DisplayValue != nil && *f.DisplayValue != "" {
			fields = append(fields, fmt.Sprintf("- **%s:** %s", Clean(strings.TrimSpace(f.Name)), Clean(*f.DisplayValue)))
		}
	}
	section(&b, "Fields", strings.Join(fields, "\n"))
	section(&b, "Description", htmlToMarkdown(t.HTMLNotes))

	var subtasks []string
	for _, s := range t.Subtasks {
		box := " "
		if s.Completed {
			box = "x"
		}
		subtasks = append(subtasks, fmt.Sprintf("- [%s] %s", box, Clean(s.Name)))
	}
	section(&b, "Subtasks", strings.Join(subtasks, "\n"))
	section(&b, "Blocked by", bulletNames(t.Dependencies))
	section(&b, "Blocking", bulletNames(t.Dependents))

	var attachments []string
	for i, a := range t.Attachments {
		attachments = append(attachments, fmt.Sprintf("%d. [%s](%s)", i+1, Clean(a.Name), Clean(AttachmentURL(a))))
	}
	section(&b, "Attachments", strings.Join(attachments, "\n"))

	var comments []string
	for _, c := range t.Comments {
		author := "Unknown"
		if c.CreatedBy != nil {
			author = Clean(c.CreatedBy.Name)
		}
		comments = append(comments, fmt.Sprintf("### %s · %s\n\n%s", author, day(c.CreatedAt), htmlToMarkdown(c.HTMLText)))
	}
	section(&b, "Comments", strings.Join(comments, "\n\n"))
	return b.String()
}

func section(b *strings.Builder, title, body string) {
	if body != "" {
		fmt.Fprintf(b, "\n## %s\n\n%s\n", title, body)
	}
}

func joinNames(refs []asana.Ref, sep string) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return strings.Join(names, sep)
}

func bulletNames(refs []asana.Ref) string {
	var lines []string
	for _, r := range refs {
		lines = append(lines, "- "+Clean(r.Name))
	}
	return strings.Join(lines, "\n")
}

func day(timestamp string) string {
	if len(timestamp) >= 10 {
		return timestamp[:10]
	}
	return timestamp
}

func htmlToMarkdown(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return Clean(html)
	}
	return Clean(strings.TrimSpace(md))
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `rtk go test ./internal/ticket/`
Expected: PASS. If `Use **SSO**.` fails only because of whitespace, check the converter's actual output with a one-off `t.Log(md)`. Fix the expectation only if the Markdown is still correct.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add go.mod go.sum internal/ticket
rtk git commit -m "feat(ticket): fetch full tickets and render markdown"
```

---

### Task 5: Filter query language

**Files:**
- Create: `internal/filter/filter.go`
- Test: `internal/filter/filter_test.go`

**Interfaces:**
- Consumes: `asana.Task`.
- Produces: `func Parse(query string) Filter`, `func (f Filter) Match(t asana.Task) bool`, `func (f Filter) Apply(tasks []asana.Task) []asana.Task` (keeps order).

- [ ] **Step 1: Write the failing test** `internal/filter/filter_test.go`

```go
package filter

import (
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

var (
	open = asana.Task{
		GID: "a", Name: "Fix login bug",
		Assignee:        &asana.Ref{Name: "Ann Lee"},
		Tags:            []asana.Ref{{Name: "bug"}},
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{Name: "Web"}, Section: &asana.Ref{Name: "In Progress"}}},
	}
	done = asana.Task{
		GID: "b", Name: "Write docs", Completed: true,
		Memberships: []asana.Membership{{Project: asana.Ref{Name: "Docs"}, Section: &asana.Ref{Name: "Done"}}},
	}
)

func TestMatch(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"", "ab"},
		{"LOGIN", "a"},
		{"is:open", "a"},
		{"is:done", "b"},
		{"-is:done", "a"},
		{`section:"in progress"`, "a"},
		{"section:today", "a"},
		{"-section:done", "a"},
		{"project:docs", "b"},
		{"assignee:ann", "a"},
		{"tag:bug", "a"},
		{"is:open fix", "a"},
		{"is:open docs", ""},
		{"is:bogus", ""},
		{"unknown:thing", ""},
	}
	for _, c := range cases {
		f := Parse(c.query)
		got := ""
		for _, task := range []asana.Task{open, done} {
			if f.Match(task) {
				got += task.GID
			}
		}
		if got != c.want {
			t.Errorf("Parse(%q) matched %q, want %q", c.query, got, c.want)
		}
	}
}

func TestApplyKeepsOrder(t *testing.T) {
	got := Parse("").Apply([]asana.Task{done, open})
	if len(got) != 2 || got[0].GID != "b" || got[1].GID != "a" {
		t.Fatalf("got %+v", got)
	}
	if got := Parse("is:done").Apply([]asana.Task{open}); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/filter/`
Expected: FAIL, `undefined: Parse`.

- [ ] **Step 3: Implement** `internal/filter/filter.go`

```go
// Package filter implements the ticket list query language.
//
// A query is space-separated terms that must all match. Bare words match the
// title. section:, project:, assignee:, and tag: match names by substring.
// is:open and is:done match completion. A leading "-" negates a term, and
// double quotes group words.
package filter

import (
	"strings"
	"unicode"

	"github.com/sadmachine/asanamate/internal/asana"
)

var keys = map[string]bool{"section": true, "project": true, "assignee": true, "tag": true, "is": true}

type term struct {
	key, value string
	negate     bool
}

// Filter is a parsed query.
type Filter struct {
	terms []term
}

// Parse parses a query. Unknown keys are treated as title words.
func Parse(query string) Filter {
	var f Filter
	for _, tok := range tokenize(query) {
		var t term
		if len(tok) > 1 && tok[0] == '-' {
			t.negate, tok = true, tok[1:]
		}
		if k, v, ok := strings.Cut(tok, ":"); ok && keys[strings.ToLower(k)] {
			t.key, t.value = strings.ToLower(k), strings.ToLower(v)
		} else {
			t.value = strings.ToLower(tok)
		}
		f.terms = append(f.terms, t)
	}
	return f
}

func tokenize(query string) []string {
	var toks []string
	var cur strings.Builder
	quoted := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

// Match reports whether t satisfies every term.
func (f Filter) Match(t asana.Task) bool {
	for _, tm := range f.terms {
		if tm.match(t) == tm.negate {
			return false
		}
	}
	return true
}

// Apply returns the tasks that match, in their original order.
func (f Filter) Apply(tasks []asana.Task) []asana.Task {
	var out []asana.Task
	for _, t := range tasks {
		if f.Match(t) {
			out = append(out, t)
		}
	}
	return out
}

func (tm term) match(t asana.Task) bool {
	switch tm.key {
	case "":
		return contains(t.Name, tm.value)
	case "is":
		switch tm.value {
		case "open":
			return !t.Completed
		case "done":
			return t.Completed
		}
		return false
	case "assignee":
		return t.Assignee != nil && contains(t.Assignee.Name, tm.value)
	case "tag":
		for _, tag := range t.Tags {
			if contains(tag.Name, tm.value) {
				return true
			}
		}
		return false
	case "project":
		for _, m := range t.Memberships {
			if contains(m.Project.Name, tm.value) {
				return true
			}
		}
		return false
	case "section":
		if t.AssigneeSection != nil && contains(t.AssigneeSection.Name, tm.value) {
			return true
		}
		for _, m := range t.Memberships {
			if m.Section != nil && contains(m.Section.Name, tm.value) {
				return true
			}
		}
		return false
	}
	return false
}

func contains(s, lowerSub string) bool {
	return strings.Contains(strings.ToLower(s), lowerSub)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `rtk go test ./internal/filter/`
Expected: PASS.

- [ ] **Step 5: Commit** (ask the user first)

```bash
rtk git add internal/filter
rtk git commit -m "feat(filter): ticket list query language"
```

---

### Task 6: Repo candidates and validation

**Files:**
- Create: `internal/repo/repo.go`
- Test: `internal/repo/repo_test.go`

**Interfaces:**
- Produces: `func ExpandHome(p string) string`, `func Candidates(ctx context.Context, command string) ([]string, error)`, `func Resolve(path string) (string, error)` (returns the git top-level directory).

- [ ] **Step 1: Write the failing test** `internal/repo/repo_test.go`

```go
package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCandidatesExpandsAndDedupes(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	got, err := Candidates(context.Background(), `printf '~/a\n/b\n/b\n\n  /c  \n'`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/home/u/a", "/b", "/c"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCandidatesReportsFailure(t *testing.T) {
	if _, err := Candidates(context.Background(), "exit 3"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolveGitRepo(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	got, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if gotReal, _ := filepath.EvalSymlinks(got); gotReal != want {
		t.Fatalf("Resolve = %s, want %s", got, want)
	}
}

func TestResolveRejectsNonRepo(t *testing.T) {
	_, err := Resolve(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/repo/`
Expected: FAIL, `undefined: Candidates`.

- [ ] **Step 3: Implement** `internal/repo/repo.go`

```go
// Package repo lists candidate repositories and validates git working trees.
package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Candidates runs command with /bin/sh and returns one path per output line.
func Candidates(ctx context.Context, command string) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).Output()
	if err != nil {
		return nil, fmt.Errorf("repo_source command failed: %w", err)
	}
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		p := ExpandHome(strings.TrimSpace(line))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths, nil
}

// Resolve returns the top-level directory of the git working tree containing path.
func Resolve(path string) (string, error) {
	p := ExpandHome(strings.TrimSpace(path))
	if p == "" {
		return "", errors.New("no path given")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", abs)
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `rtk go test ./internal/repo/`
Expected: PASS.

- [ ] **Step 5: Commit** (ask the user first)

```bash
rtk git add internal/repo
rtk git commit -m "feat(repo): list candidates and resolve git repos"
```

---

### Task 7: Action environment, ticket files, and launching

**Files:**
- Create: `internal/action/action.go`
- Test: `internal/action/action_test.go`

**Interfaces:**
- Consumes: `config.Action`, `ticket.Ticket`, `ticket.Clean`, `asana.ValidGID`, `asana.Task.SectionIn`.
- Produces:
  - `type Files struct { JSON, Markdown string }`
  - `type Context struct { Ticket ticket.Ticket; Project *asana.Ref; Repo string; ConfirmWrites bool; Files Files }`
  - `func WriteFiles(stateDir string, t ticket.Ticket) (Files, error)`
  - `func Env(c Context) []string`
  - `func Slug(s string) string`
  - `func DefaultProject(t asana.Task, viewProjectGID string) *asana.Ref`
  - `func Command(a config.Action, c Context) *exec.Cmd`
  - `func RunBackground(cmd *exec.Cmd, logPath string) error`

- [ ] **Step 1: Write the failing test** `internal/action/action_test.go`

```go
package action

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestEnvIncludesTicketFields(t *testing.T) {
	due, branch := "2026-10-01", "feat/x"
	tk := ticket.Ticket{Task: asana.Task{
		GID: "42", Name: "Fix Login!", DueOn: &due, PermalinkURL: "https://app.asana.com/t/42",
		Assignee:        &asana.Ref{Name: "Ann"},
		Tags:            []asana.Ref{{Name: "bug"}, {Name: "p1"}},
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{GID: "7", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
		CustomFields:    []asana.CustomField{{Name: "Branch Name ", DisplayValue: &branch}},
	}}
	env := Env(Context{Ticket: tk, Project: &asana.Ref{GID: "7", Name: "Web"}, Repo: "/r", ConfirmWrites: true, Files: Files{JSON: "/j", Markdown: "/m"}})
	for _, want := range []string{
		"ASANAMATE_GID=42", "ASANAMATE_TITLE=Fix Login!", "ASANAMATE_URL=https://app.asana.com/t/42",
		"ASANAMATE_SLUG=fix-login", "ASANAMATE_COMPLETED=false", "ASANAMATE_ASSIGNEE=Ann",
		"ASANAMATE_DUE=2026-10-01", "ASANAMATE_TAGS=bug,p1", "ASANAMATE_MY_SECTION=Today",
		"ASANAMATE_PROJECT=Web", "ASANAMATE_PROJECT_GID=7", "ASANAMATE_SECTION=Doing",
		"ASANAMATE_REPO=/r", "ASANAMATE_TICKET_JSON=/j", "ASANAMATE_TICKET_MD=/m",
		"ASANAMATE_CONFIRM_WRITES=1", "ASANAMATE_FIELD_BRANCH_NAME=feat/x",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Fix Login!":          "fix-login",
		"  Ünïcode & stuff ":  "n-code-stuff",
		strings.Repeat("ab-", 30): strings.TrimRight(strings.Repeat("ab-", 17)[:50], "-"),
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultProject(t *testing.T) {
	web := asana.Membership{Project: asana.Ref{GID: "1", Name: "Web"}}
	api := asana.Membership{Project: asana.Ref{GID: "2", Name: "API"}}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web, api}}, "2"); p == nil || p.GID != "2" {
		t.Errorf("viewed project: got %v", p)
	}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web}}, ""); p == nil || p.GID != "1" {
		t.Errorf("only project: got %v", p)
	}
	if p := DefaultProject(asana.Task{Memberships: []asana.Membership{web, api}}, ""); p != nil {
		t.Errorf("ambiguous: got %v, want nil", p)
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	files, err := WriteFiles(dir, ticket.Ticket{Task: asana.Task{GID: "9", Name: "T"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{files.JSON, files.Markdown} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: info = %v, err = %v", p, info, err)
		}
	}
	var decoded map[string]any
	data, _ := os.ReadFile(files.JSON)
	if err := json.Unmarshal(data, &decoded); err != nil || decoded["gid"] != "9" {
		t.Fatalf("json = %s, err = %v", data, err)
	}
	if _, err := WriteFiles(dir, ticket.Ticket{Task: asana.Task{GID: "../x"}}); err == nil {
		t.Fatal("expected rejection of a non-numeric gid")
	}
}

func TestCommandIsInjectionSafe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ASANAMATE_FIELD_STALE", "old")
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: `$(touch pwned) "; touch pwned2 '`}}
	cmd := Command(config.Action{Command: `printf '%s' "$ASANAMATE_TITLE"; env | grep -c ASANAMATE_FIELD_STALE || true`}, Context{Ticket: tk, Repo: dir})
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := tk.Name + "0\n"; string(out) != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
	for _, name := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s was created: ticket text was executed", name)
		}
	}
	if cmd.Dir != dir {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, dir)
	}
}

func TestRunBackgroundLogsOutput(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "actions.log")
	if err := RunBackground(exec.Command("/bin/sh", "-c", "echo hello"), logPath); err != nil {
		t.Fatal(err)
	}
	if err := RunBackground(exec.Command("/bin/sh", "-c", "exit 3"), logPath); err == nil {
		t.Fatal("want an error for a non-zero exit")
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("log = %q", data)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/action/`
Expected: FAIL, `undefined: Env`.

- [ ] **Step 3: Implement** `internal/action/action.go`

```go
// Package action runs configured commands against a ticket.
//
// Ticket data reaches commands only through ASANAMATE_* environment variables
// and files, never through the command string, so ticket text cannot inject
// shell syntax.
package action

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const envPrefix = "ASANAMATE_"

// Files are the ticket exports passed to an action.
type Files struct {
	JSON     string
	Markdown string
}

// Context is everything an action run knows about.
type Context struct {
	Ticket        ticket.Ticket
	Project       *asana.Ref
	Repo          string
	ConfirmWrites bool
	Files         Files
}

// WriteFiles exports the ticket to <stateDir>/tickets/<gid>/ticket.{json,md}.
// The paths are stable so detached actions can read them later.
func WriteFiles(stateDir string, t ticket.Ticket) (Files, error) {
	if !asana.ValidGID(t.GID) {
		return Files{}, fmt.Errorf("invalid task gid %q", t.GID)
	}
	dir := filepath.Join(stateDir, "tickets", t.GID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Files{}, err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return Files{}, err
	}
	f := Files{JSON: filepath.Join(dir, "ticket.json"), Markdown: filepath.Join(dir, "ticket.md")}
	if err := os.WriteFile(f.JSON, data, 0o600); err != nil {
		return Files{}, err
	}
	if err := os.WriteFile(f.Markdown, []byte(t.Markdown()), 0o600); err != nil {
		return Files{}, err
	}
	return f, nil
}

// Env returns the ASANAMATE_* variables for an action run, sorted.
func Env(c Context) []string {
	t := c.Ticket
	vars := map[string]string{
		"GID":            t.GID,
		"TITLE":          t.Name,
		"URL":            t.PermalinkURL,
		"SLUG":           Slug(t.Name),
		"COMPLETED":      strconv.FormatBool(t.Completed),
		"REPO":           c.Repo,
		"TICKET_JSON":    c.Files.JSON,
		"TICKET_MD":      c.Files.Markdown,
		"CONFIRM_WRITES": "0",
		"ASSIGNEE":       "",
		"DUE":            "",
		"MY_SECTION":     "",
		"PROJECT":        "",
		"PROJECT_GID":    "",
		"SECTION":        "",
	}
	if c.ConfirmWrites {
		vars["CONFIRM_WRITES"] = "1"
	}
	if t.Assignee != nil {
		vars["ASSIGNEE"] = t.Assignee.Name
	}
	if t.DueOn != nil {
		vars["DUE"] = *t.DueOn
	}
	if t.AssigneeSection != nil {
		vars["MY_SECTION"] = t.AssigneeSection.Name
	}
	if c.Project != nil {
		vars["PROJECT"] = c.Project.Name
		vars["PROJECT_GID"] = c.Project.GID
		vars["SECTION"] = t.SectionIn(c.Project.GID)
	}
	tags := make([]string, len(t.Tags))
	for i, tag := range t.Tags {
		tags[i] = tag.Name
	}
	vars["TAGS"] = strings.Join(tags, ",")
	for _, f := range t.CustomFields {
		name := envName(f.Name)
		if name == "" {
			continue
		}
		value := ""
		if f.DisplayValue != nil {
			value = *f.DisplayValue
		}
		vars["FIELD_"+name] = value
	}
	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, envPrefix+k+"="+ticket.Clean(v))
	}
	sort.Strings(env)
	return env
}

// Slug turns a title into a lowercase, dash-separated string of at most 50 characters.
func Slug(s string) string {
	return separated(strings.ToLower(s), '-', 50)
}

func envName(s string) string {
	return strings.ToUpper(separated(strings.ToLower(s), '_', 0))
}

// separated keeps [a-z0-9], collapses every other run into sep, and trims sep from both ends.
func separated(s string, sep byte, limit int) string {
	var b strings.Builder
	pending := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pending && b.Len() > 0 {
				b.WriteByte(sep)
			}
			b.WriteRune(r)
			pending = false
		} else {
			pending = true
		}
	}
	out := b.String()
	if limit > 0 && len(out) > limit {
		out = strings.TrimRight(out[:limit], string(sep))
	}
	return out
}

// DefaultProject is the active project for actions that do not resolve a repo:
// the viewed project if the ticket is in it, else the ticket's only project, else nil.
func DefaultProject(t asana.Task, viewProjectGID string) *asana.Ref {
	for _, m := range t.Memberships {
		if m.Project.GID == viewProjectGID {
			p := m.Project
			return &p
		}
	}
	if len(t.Memberships) == 1 {
		p := t.Memberships[0].Project
		return &p
	}
	return nil
}

// Command builds the /bin/sh invocation for an action. Inherited ASANAMATE_*
// variables are dropped so values from an outer run cannot leak in.
func Command(a config.Action, c Context) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", a.Command)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, envPrefix) {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, Env(c)...)
	if c.Repo != "" {
		cmd.Dir = c.Repo
	}
	return cmd
}

// RunBackground runs cmd in its own process group with output appended to
// logPath, and waits for it to exit. The process survives asanamate quitting.
func RunBackground(cmd *exec.Cmd, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	fmt.Fprintf(log, "--- %s %s\n", time.Now().Format(time.RFC3339), cmd.Args[len(cmd.Args)-1])
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Run()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `rtk go test ./internal/action/`
Expected: PASS. Check `TestSlug`'s long case by hand if it fails: `"ab-"*30` gives `ab-ab-…`, cut to 50 bytes with trailing `-` trimmed. The expected value is built the same way.

- [ ] **Step 5: Commit** (ask the user first)

```bash
rtk git add internal/action
rtk git commit -m "feat(action): safe env, ticket files, and process launch"
```

---

### Task 8: Confirmed write-back and the CLI entry point

**Files:**
- Create: `internal/prompt/prompt.go`, `internal/writeback/writeback.go`, `cmd/asanamate/main.go`
- Test: `internal/prompt/prompt_test.go`, `internal/writeback/writeback_test.go`

**Interfaces:**
- Consumes: `asana.Client` (`Task`, `Sections`, `AddComment`, `AddToSection`, `SetCustomField`), `asana.ValidGID`, `config.Load`, `config.Path`, `config.Token`.
- Produces:
  - `prompt.Line(in *bufio.Reader, out io.Writer, label, def string) (string, error)`
  - `prompt.Confirm(in *bufio.Reader, out io.Writer, question string) (bool, error)`
  - `writeback.ErrNoTTY`, `writeback.ErrDeclined`, `type Confirmer func(prompt string) (bool, error)`
  - `writeback.NeedsConfirm(yes bool, envValue string, configDefault bool) bool`
  - `writeback.TTYConfirm(prompt string) (bool, error)`
  - `type Service struct { Client *asana.Client; Confirm Confirmer }` with methods (each takes `ctx` first):
    - `Comment(gid, text string) error`
    - `Move(gid, section, projectGID string) error`
    - `SetField(gid, field, value string) error`
  - `main.run(args []string) int`, `main.loadConfig()`, `main.newClient()`

- [ ] **Step 1: Write the failing tests**

`internal/prompt/prompt_test.go`:

```go
package prompt

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func TestLineUsesDefault(t *testing.T) {
	var out strings.Builder
	got, err := Line(reader("\n"), &out, "Dir", "~/code")
	if err != nil || got != "~/code" || out.String() != "Dir [~/code]: " {
		t.Fatalf("got %q, err %v, prompt %q", got, err, out.String())
	}
}

func TestLineEOF(t *testing.T) {
	if _, err := Line(reader(""), io.Discard, "Dir", "x"); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestConfirm(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "\n": false, "n\n": false, "yes": true} {
		got, err := Confirm(reader(in), io.Discard, "Go?")
		if err != nil || got != want {
			t.Errorf("Confirm(%q) = %v, %v", in, got, err)
		}
	}
}
```

`internal/writeback/writeback_test.go`:

```go
package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
)

const taskJSON = `{"data":{"gid":"1","name":"Fix login",
 "memberships":[{"project":{"gid":"p1","name":"Web"}},{"project":{"gid":"p2","name":"API"}}],
 "custom_fields":[
  {"gid":"f1","name":"Branch Name ","resource_subtype":"text"},
  {"gid":"f2","name":"Points","resource_subtype":"number"},
  {"gid":"f3","name":"Stage","resource_subtype":"enum","enum_options":[{"gid":"o1","name":"Review"}]},
  {"gid":"f4","name":"When","resource_subtype":"date"}]}}`

type write struct {
	method, path string
	body         map[string]any
}

func fake(t *testing.T) (*asana.Client, *[]write) {
	t.Helper()
	var writes []write
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/1":
			io.WriteString(w, taskJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/p1/sections":
			io.WriteString(w, `{"data":[{"gid":"s1","name":"To Do"},{"gid":"s2","name":"In Review"}]}`)
		case r.Method != http.MethodGet:
			var body struct {
				Data map[string]any `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			writes = append(writes, write{r.Method, r.URL.Path, body.Data})
			io.WriteString(w, `{"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return c, &writes
}

var ctx = context.Background()

func TestNeedsConfirm(t *testing.T) {
	cases := []struct {
		yes      bool
		env      string
		def, out bool
	}{
		{true, "1", true, false},
		{false, "0", true, false},
		{false, "1", false, true},
		{false, "", true, true},
		{false, "", false, false},
	}
	for _, c := range cases {
		if got := NeedsConfirm(c.yes, c.env, c.def); got != c.out {
			t.Errorf("NeedsConfirm(%v, %q, %v) = %v", c.yes, c.env, c.def, got)
		}
	}
}

func TestCommentPosts(t *testing.T) {
	c, writes := fake(t)
	if err := (Service{Client: c}).Comment(ctx, "1", "  PR: https://x  \n"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/tasks/1/stories" || (*writes)[0].body["text"] != "PR: https://x" {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestDeclineBlocksWrite(t *testing.T) {
	c, writes := fake(t)
	var asked string
	svc := Service{Client: c, Confirm: func(p string) (bool, error) { asked = p; return false, nil }}
	if err := svc.Comment(ctx, "1", "hi"); !errors.Is(err, ErrDeclined) {
		t.Fatalf("err = %v", err)
	}
	if len(*writes) != 0 || !strings.Contains(asked, "Fix login") {
		t.Fatalf("writes = %+v, prompt = %q", *writes, asked)
	}
}

func TestConfirmErrorBlocksWrite(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c, Confirm: func(string) (bool, error) { return false, ErrNoTTY }}
	if err := svc.SetField(ctx, "1", "points", "3"); !errors.Is(err, ErrNoTTY) {
		t.Fatalf("err = %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}

func TestMoveNeedsProjectWhenAmbiguous(t *testing.T) {
	c, _ := fake(t)
	err := (Service{Client: c}).Move(ctx, "1", "In Review", "")
	if err == nil || !strings.Contains(err.Error(), "--project") || !strings.Contains(err.Error(), "Web (p1)") {
		t.Fatalf("err = %v", err)
	}
}

func TestMoveMatchesSectionCaseInsensitively(t *testing.T) {
	c, writes := fake(t)
	if err := (Service{Client: c}).Move(ctx, "1", "in review", "p1"); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].path != "/sections/s2/addTask" || (*writes)[0].body["task"] != "1" {
		t.Fatalf("writes = %+v", *writes)
	}
	if err := (Service{Client: c}).Move(ctx, "1", "Shipped", "p1"); err == nil || !strings.Contains(err.Error(), "To Do") {
		t.Fatalf("err = %v, want list of sections", err)
	}
}

func TestSetFieldValues(t *testing.T) {
	cases := []struct {
		field, value string
		want         any
	}{
		{"branch name", "feat/x", "feat/x"},
		{"Points", "3.5", 3.5},
		{"stage", "review", "o1"},
		{"Points", "", nil},
	}
	for _, tc := range cases {
		c, writes := fake(t)
		if err := (Service{Client: c}).SetField(ctx, "1", tc.field, tc.value); err != nil {
			t.Fatalf("%s=%q: %v", tc.field, tc.value, err)
		}
		fields := (*writes)[0].body["custom_fields"].(map[string]any)
		for _, got := range fields {
			if got != tc.want {
				t.Errorf("%s=%q: sent %v, want %v", tc.field, tc.value, got, tc.want)
			}
		}
	}
}

func TestSetFieldRejectsBadInput(t *testing.T) {
	c, writes := fake(t)
	svc := Service{Client: c}
	for _, args := range [][2]string{{"Points", "many"}, {"Stage", "Shipped"}, {"When", "2026-01-01"}, {"Nope", "x"}} {
		if err := svc.SetField(ctx, "1", args[0], args[1]); err == nil {
			t.Errorf("SetField(%q, %q) succeeded", args[0], args[1])
		}
	}
	if len(*writes) != 0 {
		t.Fatalf("writes = %+v", *writes)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk go test ./internal/prompt/ ./internal/writeback/`
Expected: FAIL, `undefined: Line` and `undefined: Service`.

- [ ] **Step 3: Implement** `internal/prompt/prompt.go`

```go
// Package prompt reads answers to line-based terminal questions.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Line asks for one line of input. An empty answer returns def.
func Line(in *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	s, err := in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", err
	}
	if s = strings.TrimSpace(s); s == "" {
		return def, nil
	}
	return s, nil
}

// Confirm asks a yes/no question. Only "y" or "yes" count as yes.
func Confirm(in *bufio.Reader, out io.Writer, question string) (bool, error) {
	answer, err := Line(in, out, question+" [y/N]", "")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}
```

- [ ] **Step 4: Implement** `internal/writeback/writeback.go`

```go
// Package writeback performs confirmed updates to Asana tasks.
package writeback

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/prompt"
)

var (
	// ErrNoTTY means confirmation was required but no terminal was available.
	ErrNoTTY = errors.New("confirmation required but no terminal is available; pass --yes or set confirm_writes = false")
	// ErrDeclined means the user answered no.
	ErrDeclined = errors.New("cancelled")
)

// Confirmer asks the user to approve a write.
type Confirmer func(prompt string) (bool, error)

// NeedsConfirm resolves whether to confirm: --yes wins, then
// ASANAMATE_CONFIRM_WRITES ("1"/"0"), then the config default.
func NeedsConfirm(yes bool, envValue string, configDefault bool) bool {
	if yes {
		return false
	}
	switch envValue {
	case "0":
		return false
	case "1":
		return true
	}
	return configDefault
}

// TTYConfirm asks on /dev/tty, so it works even when stdin is a pipe.
func TTYConfirm(question string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, ErrNoTTY
	}
	defer tty.Close()
	return prompt.Confirm(bufio.NewReader(tty), tty, question)
}

// Service writes to Asana, asking Confirm first when it is set.
type Service struct {
	Client  *asana.Client
	Confirm Confirmer
}

func (s Service) confirm(question string) error {
	if s.Confirm == nil {
		return nil
	}
	ok, err := s.Confirm(question)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDeclined
	}
	return nil
}

// Comment posts text as a comment on the task.
func (s Service) Comment(ctx context.Context, gid, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("comment text is empty")
	}
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	if err := s.confirm(fmt.Sprintf("Comment on %q?\n%s\n", t.Name, text)); err != nil {
		return err
	}
	return s.Client.AddComment(ctx, gid, text)
}

// Move puts the task in the named section of one of its projects.
func (s Service) Move(ctx context.Context, gid, sectionName, projectGID string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	project, err := pickProject(t, projectGID)
	if err != nil {
		return err
	}
	sections, err := s.Client.Sections(ctx, project.GID)
	if err != nil {
		return err
	}
	var names []string
	for _, sec := range sections {
		if strings.EqualFold(strings.TrimSpace(sec.Name), strings.TrimSpace(sectionName)) {
			if err := s.confirm(fmt.Sprintf("Move %q to %s / %s?", t.Name, project.Name, sec.Name)); err != nil {
				return err
			}
			return s.Client.AddToSection(ctx, sec.GID, gid)
		}
		names = append(names, sec.Name)
	}
	return fmt.Errorf("no section %q in %s; sections: %s", sectionName, project.Name, strings.Join(names, ", "))
}

func pickProject(t asana.Task, projectGID string) (asana.Ref, error) {
	var choices []string
	for _, m := range t.Memberships {
		if m.Project.GID == projectGID {
			return m.Project, nil
		}
		choices = append(choices, fmt.Sprintf("%s (%s)", m.Project.Name, m.Project.GID))
	}
	switch {
	case projectGID != "":
		return asana.Ref{}, fmt.Errorf("task is not in project %s", projectGID)
	case len(t.Memberships) == 0:
		return asana.Ref{}, errors.New("task is not in any project")
	case len(t.Memberships) == 1:
		return t.Memberships[0].Project, nil
	}
	return asana.Ref{}, fmt.Errorf("task is in several projects; pass --project with one of: %s", strings.Join(choices, ", "))
}

// SetField sets a text, number, or enum custom field. An empty value clears it.
func (s Service) SetField(ctx context.Context, gid, fieldName, value string) error {
	t, err := s.Client.Task(ctx, gid)
	if err != nil {
		return err
	}
	for _, f := range t.CustomFields {
		if !strings.EqualFold(strings.TrimSpace(f.Name), strings.TrimSpace(fieldName)) {
			continue
		}
		v, err := fieldValue(f, value)
		if err != nil {
			return err
		}
		if err := s.confirm(fmt.Sprintf("Set %q on %q to %q?", strings.TrimSpace(f.Name), t.Name, value)); err != nil {
			return err
		}
		return s.Client.SetCustomField(ctx, gid, f.GID, v)
	}
	return fmt.Errorf("task has no custom field %q", fieldName)
}

func fieldValue(f asana.CustomField, value string) (any, error) {
	name := strings.TrimSpace(f.Name)
	if value == "" {
		return nil, nil
	}
	switch f.ResourceSubtype {
	case "text":
		return value, nil
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("%s needs a number, got %q", name, value)
		}
		return n, nil
	case "enum":
		var options []string
		for _, o := range f.EnumOptions {
			if strings.EqualFold(o.Name, value) {
				return o.GID, nil
			}
			options = append(options, o.Name)
		}
		return nil, fmt.Errorf("%s has no option %q; options: %s", name, value, strings.Join(options, ", "))
	}
	return nil, fmt.Errorf("%s: custom field type %q is not supported", name, f.ResourceSubtype)
}
```

- [ ] **Step 5: Implement** `cmd/asanamate/main.go`

```go
// Command asanamate is a terminal UI for Asana tickets with scriptable actions.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/writeback"
)

var version = "dev"

const usage = `usage:
  asanamate                                        open the TUI
  asanamate setup                                  create the config file
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate move    [--yes] [--project <gid>] <gid> <section>
  asanamate field   [--yes] <gid> <field> <value>  set a custom field ("" clears it)
  asanamate version
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	var err error
	switch name {
	case "comment", "move", "field":
		err = writeCommand(name, args[1:])
	case "version":
		fmt.Println(currentVersion())
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "asanamate:", err)
		return 1
	}
	return 0
}

func currentVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return version
}

func loadConfig() (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

func newClient() (*asana.Client, error) {
	token, err := config.Token()
	if err != nil {
		return nil, err
	}
	return asana.New(token), nil
}

func writeCommand(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	yes := fs.Bool("yes", false, "write without asking for confirmation")
	project := new(string)
	if name == "move" {
		project = fs.String("project", "", "project gid; required when the task is in several projects")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	want := map[string]int{"comment": 2, "move": 2, "field": 3}[name]
	if len(rest) != want {
		return fmt.Errorf("%s: wrong number of arguments\n%s", name, usage)
	}
	gid := rest[0]
	if !asana.ValidGID(gid) {
		return fmt.Errorf("task gid must be numeric, got %q", gid)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	svc := writeback.Service{Client: client}
	if writeback.NeedsConfirm(*yes, os.Getenv("ASANAMATE_CONFIRM_WRITES"), cfg.ConfirmWrites) {
		svc.Confirm = writeback.TTYConfirm
	}
	ctx := context.Background()
	switch name {
	case "comment":
		text := rest[1]
		if text == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = string(data)
		}
		return svc.Comment(ctx, gid, text)
	case "move":
		return svc.Move(ctx, gid, rest[1], *project)
	default:
		return svc.SetField(ctx, gid, rest[1], rest[2])
	}
}
```

- [ ] **Step 6: Run the tests and a CLI smoke check**

Run: `rtk go test ./... && rtk go vet ./... && rtk go run ./cmd/asanamate move 12x "Done"`
Expected: tests PASS. The last command prints `asanamate: task gid must be numeric, got "12x"` and exits 1.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add internal/prompt internal/writeback cmd/asanamate
rtk git commit -m "feat(writeback): comment, move, and field subcommands"
```

---

### Task 9: Setup wizard

**Files:**
- Create: `internal/setup/setup.go`
- Modify: `cmd/asanamate/main.go` (add the `setup` case and `runSetup`)
- Test: `internal/setup/setup_test.go`

**Interfaces:**
- Consumes: `asana.Client.Me`, `prompt.Line`, `prompt.Confirm`, `repo.ExpandHome`, `config.Load` (in the test), `config.StateDir`, `state.FileName`.
- Produces: `type Options struct { In *bufio.Reader; Out io.Writer; Client *asana.Client; ConfigPath, StatePath string }`, `func Run(ctx context.Context, o Options) error`, `func Render(workspace asana.Ref, repoRoot string) string`.

- [ ] **Step 1: Write the failing test** `internal/setup/setup_test.go`

```go
package setup

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
)

func fakeClient(t *testing.T) *asana.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"gid":"u","name":"Ann","workspaces":[{"gid":"w1","name":"Home"},{"gid":"w2","name":"Work"}]}}`)
	}))
	t.Cleanup(srv.Close)
	c := asana.New("tok")
	c.BaseURL = srv.URL
	return c
}

func options(t *testing.T, input string) (Options, *strings.Builder) {
	out := &strings.Builder{}
	dir := t.TempDir()
	return Options{
		In: bufio.NewReader(strings.NewReader(input)), Out: out, Client: fakeClient(t),
		ConfigPath: filepath.Join(dir, "cfg", "config.toml"), StatePath: filepath.Join(dir, "state.toml"),
	}, out
}

func TestRunWritesLoadableConfig(t *testing.T) {
	root := t.TempDir()
	o, out := options(t, "2\n/definitely/missing\n"+root+"\n")
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.Workspace != "w2" || !strings.Contains(cfg.RepoSource.Command, root) || len(cfg.Actions) != 1 {
		t.Fatalf("cfg = %+v", cfg)
	}
	info, _ := os.Stat(o.ConfigPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	for _, want := range []string{"Authenticated as Ann", "is not a directory", "Wrote " + o.ConfigPath} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunKeepsExistingConfigWhenDeclined(t *testing.T) {
	o, _ := options(t, "n\n")
	os.MkdirAll(filepath.Dir(o.ConfigPath), 0o700)
	os.WriteFile(o.ConfigPath, []byte("keep"), 0o600)
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected cancellation error")
	}
	if data, _ := os.ReadFile(o.ConfigPath); string(data) != "keep" {
		t.Fatalf("config overwritten: %q", data)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/setup/`
Expected: FAIL, `undefined: Run`.

- [ ] **Step 3: Implement** `internal/setup/setup.go`

```go
// Package setup creates the asanamate config file interactively.
package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/prompt"
	"github.com/sadmachine/asanamate/internal/repo"
)

const defaultRepoRoot = "~/code"

// Options wires setup to its input, output, and file locations.
type Options struct {
	In         *bufio.Reader
	Out        io.Writer
	Client     *asana.Client
	ConfigPath string
	StatePath  string
}

// Run asks for the workspace and repo directory, then writes the config file.
func Run(ctx context.Context, o Options) error {
	if _, err := os.Stat(o.ConfigPath); err == nil {
		ok, err := prompt.Confirm(o.In, o.Out, o.ConfigPath+" already exists. Overwrite it?")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("setup cancelled; existing config kept")
		}
	}
	me, err := o.Client.Me(ctx)
	if err != nil {
		return fmt.Errorf("could not authenticate with %s: %w", config.TokenEnv, err)
	}
	fmt.Fprintf(o.Out, "Authenticated as %s.\n", me.Name)
	workspace, err := chooseWorkspace(o, me.Workspaces)
	if err != nil {
		return err
	}
	root, err := chooseRepoRoot(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(o.ConfigPath, []byte(Render(workspace, root)), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(o.ConfigPath, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, summary, o.ConfigPath, root, o.StatePath)
	return nil
}

func chooseWorkspace(o Options, workspaces []asana.Ref) (asana.Ref, error) {
	switch len(workspaces) {
	case 0:
		return asana.Ref{}, errors.New("your Asana account has no workspaces")
	case 1:
		fmt.Fprintf(o.Out, "Using workspace %s.\n", workspaces[0].Name)
		return workspaces[0], nil
	}
	fmt.Fprintln(o.Out, "Workspaces:")
	for i, w := range workspaces {
		fmt.Fprintf(o.Out, "  %d) %s\n", i+1, w.Name)
	}
	for {
		answer, err := prompt.Line(o.In, o.Out, "Workspace number", "1")
		if err != nil {
			return asana.Ref{}, err
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(workspaces) {
			return workspaces[n-1], nil
		}
		fmt.Fprintln(o.Out, "Enter a number from the list.")
	}
}

func chooseRepoRoot(o Options) (string, error) {
	for {
		answer, err := prompt.Line(o.In, o.Out, "Directory that contains your git repositories", defaultRepoRoot)
		if err != nil {
			return "", err
		}
		dir, err := filepath.Abs(repo.ExpandHome(answer))
		if err != nil {
			return "", err
		}
		if strings.Contains(dir, "'") {
			fmt.Fprintln(o.Out, "Paths containing ' are not supported.")
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(o.Out, "%s is not a directory.\n", dir)
			continue
		}
		return dir, nil
	}
}

// Render returns the commented config file for a workspace and repo directory.
func Render(workspace asana.Ref, repoRoot string) string {
	name := strings.Join(strings.Fields(workspace.Name), " ")
	return fmt.Sprintf(configTemplate, name, workspace.GID, repoRoot, repoRoot)
}

const summary = `
Wrote %s.

Defaults:
  - View: My Tasks, filtered by "is:open". Press p to switch projects, / to filter.
  - Repo picker: git repositories directly inside %s.
    Change [repo_source] command to use sesh, zoxide, or anything else.
  - Writes to Asana ask for confirmation (confirm_writes = true).
  - Repo links and recent projects are stored in %s.

Run asanamate to start.
`

const configTemplate = `# asanamate configuration.
# Reference: https://github.com/sadmachine/asanamate#configuration

# Asana workspace: %s
workspace = %q

# Markdown style for the reading pane: "dark" or "light".
theme = "dark"

# Images: "auto" detects kitty-protocol terminals, "kitty" forces on, "off" disables.
images = "auto"

# Filter applied at startup. Terms: words, section:, project:, assignee:, tag:,
# is:open, is:done. Prefix a term with "-" to negate it; quote multi-word values.
default_filter = "is:open"

# Ask before "asanamate comment/move/field" writes to Asana.
# Override per action with confirm_writes, or per call with --yes.
confirm_writes = true

[repo_source]
# Prints one git repository path per line for the repo picker.
# The default lists repositories directly inside %s.
# Alternatives: "sesh list -z", "zoxide query -l".
command = '''find '%s' -mindepth 2 -maxdepth 2 -name .git -exec dirname {} \;'''

# Actions run with /bin/sh -c. Ticket data arrives in ASANAMATE_* environment
# variables and in the files $ASANAMATE_TICKET_JSON and $ASANAMATE_TICKET_MD.
# Never paste ticket text into the command; always use the variables.
# mode: "foreground" (suspend the TUI), "background" (detached, logged), or
# "exit" (quit asanamate, then run). repo = true resolves the ticket's repo first.

[[actions]]
name = "View ticket in pager"
key = "v"
mode = "foreground"
command = '${PAGER:-less} "$ASANAMATE_TICKET_MD"'

# [[actions]]
# name = "Start Claude in a new tmux window"
# key = "c"
# mode = "background"
# repo = true
# # tmux new-window does not inherit this environment; pass variables with -e.
# command = '''tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
`
```

- [ ] **Step 4: Wire the `setup` subcommand** in `cmd/asanamate/main.go`

Add `"bufio"`, `"path/filepath"`, `"github.com/sadmachine/asanamate/internal/setup"`, and `"github.com/sadmachine/asanamate/internal/state"` to the imports. Add this case to the `switch name` in `run`, before `case "comment", "move", "field":`

```go
	case "setup":
		err = runSetup()
```

Append this function:

```go
func runSetup() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	return setup.Run(context.Background(), setup.Options{
		In: bufio.NewReader(os.Stdin), Out: os.Stdout, Client: client,
		ConfigPath: configPath, StatePath: filepath.Join(stateDir, state.FileName),
	})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `rtk go test ./... && rtk go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit** (ask the user first)

```bash
rtk git add internal/setup cmd/asanamate/main.go
rtk git commit -m "feat(setup): interactive config wizard"
```

---

### Task 10: Kitty images and browser opener

**Files:**
- Create: `internal/kitty/kitty.go`, `internal/browser/browser.go`
- Test: `internal/kitty/kitty_test.go`, `internal/browser/browser_test.go`

**Interfaces:**
- Produces:
  - `kitty.Supported(mode string, getenv func(string) string, passthrough func() bool) bool`
  - `kitty.TmuxPassthrough() bool`
  - `kitty.IsImage(name string) bool`
  - `kitty.Download(ctx context.Context, rawURL string) ([]byte, error)`
  - `kitty.Encode(data []byte, cols, rows int, inTmux bool) (string, error)`
  - `kitty.Clear(inTmux bool) string`
  - `kitty.NewViewer(payload string, inTmux bool) *kitty.Viewer` (implements `tea.ExecCommand`: `Run`, `SetStdin`, `SetStdout`, `SetStderr`)
  - `browser.Open(rawURL string) error`

- [ ] **Step 1: Write the failing tests**

`internal/kitty/kitty_test.go`:

```go
package kitty

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestSupported(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		name string
		mode string
		vars map[string]string
		pass func() bool
		want bool
	}{
		{"off", "off", map[string]string{"KITTY_WINDOW_ID": "1"}, yes, false},
		{"forced", "kitty", nil, no, true},
		{"kitty direct", "auto", map[string]string{"KITTY_WINDOW_ID": "1"}, no, true},
		{"ghostty in tmux with passthrough", "auto", map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "x"}, yes, true},
		{"ghostty in tmux without passthrough", "auto", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "TMUX": "x"}, no, false},
		{"plain xterm", "auto", map[string]string{"TERM": "xterm-256color"}, yes, false},
	}
	for _, c := range cases {
		if got := Supported(c.mode, env(c.vars), c.pass); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsImage(t *testing.T) {
	if !IsImage("Shot.PNG") || !IsImage("a.jpeg") || IsImage("doc.pdf") {
		t.Fatal("IsImage misclassified")
	}
}

func pngBytes(t *testing.T, w, h int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := color.RGBA{A: 255}
			if noisy {
				c.R, c.G, c.B = uint8(rand.Intn(256)), uint8(rand.Intn(256)), uint8(rand.Intn(256))
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncodeSmall(t *testing.T) {
	out, err := Encode(pngBytes(t, 10, 400, false), 80, 24, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,q=2,r=24,m=0;") || !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("unexpected payload prefix/suffix: %q", out[:40])
	}
}

func TestEncodeChunksWideImagesAndWrapsForTmux(t *testing.T) {
	out, err := Encode(pngBytes(t, 400, 100, true), 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "\x1bPtmux;\x1b\x1b_Ga=T,f=100,q=2,c=80,m=1;") {
		t.Fatalf("prefix = %q", out[:48])
	}
	if strings.Count(out, "\x1bPtmux;") < 2 || !strings.Contains(out, "m=0;") {
		t.Fatal("want several chunks, the last with m=0")
	}
}

func TestEncodeRejectsNonImage(t *testing.T) {
	if _, err := Encode([]byte("not an image"), 80, 24, false); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization header must not be sent to download hosts")
		}
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	oldClient, oldMax := httpClient, maxImageBytes
	httpClient, maxImageBytes = srv.Client(), 10
	defer func() { httpClient, maxImageBytes = oldClient, oldMax }()

	if data, err := Download(context.Background(), srv.URL); err != nil || string(data) != "0123456789" {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	maxImageBytes = 5
	if _, err := Download(context.Background(), srv.URL); err == nil {
		t.Fatal("expected a size-limit error")
	}
	if _, err := Download(context.Background(), "http://example.com/a.png"); err == nil {
		t.Fatal("expected rejection of a non-https URL")
	}
}

func TestViewerWritesPayloadAndClears(t *testing.T) {
	v := NewViewer("PAYLOAD", false)
	var out bytes.Buffer
	v.SetStdin(strings.NewReader("\n"))
	v.SetStdout(&out)
	if err := v.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PAYLOAD") || !strings.HasSuffix(out.String(), Clear(false)) {
		t.Fatalf("out = %q", out.String())
	}
}
```

`internal/browser/browser_test.go`:

```go
package browser

import "testing"

func TestValidateRejectsUnsafeURLs(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "-a Calculator", "javascript:alert(1)", ""} {
		if err := validate(u); err == nil {
			t.Errorf("validate(%q) accepted", u)
		}
	}
	if err := validate("https://app.asana.com/0/1/2"); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk go test ./internal/kitty/ ./internal/browser/`
Expected: FAIL, `undefined: Supported` and `undefined: validate`.

- [ ] **Step 3: Implement** `internal/kitty/kitty.go`

```go
// Package kitty shows images with the kitty terminal graphics protocol.
package kitty

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const chunkSize = 4096

var (
	httpClient    = &http.Client{Timeout: 30 * time.Second}
	maxImageBytes = 20 << 20
)

// Supported reports whether images can be shown. mode is "auto", "kitty", or "off".
func Supported(mode string, getenv func(string) string, passthrough func() bool) bool {
	switch mode {
	case "off":
		return false
	case "kitty":
		return true
	}
	if !kittyTerminal(getenv) {
		return false
	}
	return getenv("TMUX") == "" || passthrough()
}

func kittyTerminal(getenv func(string) string) bool {
	if getenv("KITTY_WINDOW_ID") != "" || getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	switch getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	term := getenv("TERM")
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty")
}

// TmuxPassthrough reports whether tmux forwards escape sequences to the outer terminal.
func TmuxPassthrough() bool {
	out, err := exec.Command("tmux", "show", "-gv", "allow-passthrough").Output()
	v := strings.TrimSpace(string(out))
	return err == nil && (v == "on" || v == "all")
}

// IsImage reports whether a file name has an image extension we can decode.
func IsImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// Download fetches an image over https without Asana credentials.
func Download(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, errors.New("attachment download URL is not https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxImageBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("image is larger than %d bytes", maxImageBytes)
	}
	return data, nil
}

// Encode converts an image to kitty graphics escape sequences sized to fit
// cols x rows cells, assuming cells are about twice as tall as wide.
func Encode(data []byte, cols, rows int, inTmux bool) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	b := img.Bounds()
	size := fmt.Sprintf("r=%d", max(rows, 1))
	if b.Dx()*2*rows > cols*b.Dy() {
		size = fmt.Sprintf("c=%d", max(cols, 1))
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	var out strings.Builder
	for i := 0; i < len(encoded); i += chunkSize {
		end := min(i+chunkSize, len(encoded))
		more := 1
		if end == len(encoded) {
			more = 0
		}
		control := fmt.Sprintf("m=%d", more)
		if i == 0 {
			control = "a=T,f=100,q=2," + size + "," + control
		}
		out.WriteString(wrap("\x1b_G"+control+";"+encoded[i:end]+"\x1b\\", inTmux))
	}
	return out.String(), nil
}

// Clear deletes every image placed on screen.
func Clear(inTmux bool) string { return wrap("\x1b_Ga=d,q=2\x1b\\", inTmux) }

func wrap(seq string, inTmux bool) string {
	if !inTmux {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Viewer shows an encoded image full screen until Enter is pressed.
// It implements tea.ExecCommand so Bubble Tea releases the terminal first.
type Viewer struct {
	payload, clear string
	stdin          io.Reader
	stdout         io.Writer
}

// NewViewer returns a viewer for a payload produced by Encode.
func NewViewer(payload string, inTmux bool) *Viewer {
	return &Viewer{payload: payload, clear: Clear(inTmux)}
}

func (v *Viewer) SetStdin(r io.Reader)  { v.stdin = r }
func (v *Viewer) SetStdout(w io.Writer) { v.stdout = w }
func (v *Viewer) SetStderr(io.Writer)   {}

// Run draws the image, waits for Enter, then removes the image.
func (v *Viewer) Run() error {
	fmt.Fprint(v.stdout, "\x1b[2J\x1b[H", v.payload, "\r\n\r\nPress Enter to return.")
	_, err := bufio.NewReader(v.stdin).ReadString('\n')
	fmt.Fprint(v.stdout, v.clear)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Implement** `internal/browser/browser.go`

```go
// Package browser opens http(s) URLs in the default browser.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens rawURL with open (macOS) or xdg-open (Linux).
func Open(rawURL string) error {
	if err := validate(rawURL); err != nil {
		return err
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, rawURL).Run()
}

func validate(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", rawURL)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `rtk go test ./internal/kitty/ ./internal/browser/`
Expected: PASS.

- [ ] **Step 6: Commit** (ask the user first)

```bash
rtk git add internal/kitty internal/browser
rtk git commit -m "feat: kitty image viewer and safe browser opener"
```

---

### Task 11: TUI picker component

**Files:**
- Create: `internal/tui/styles.go`, `internal/tui/picker.go`
- Test: `internal/tui/picker_test.go`

**Interfaces:**
- Produces (package-internal, used by Tasks 12–13):
  - Kinds: `type pickKind int` with values `pickProject`, `pickAction`, `pickTicketProject`, `pickRepo`, `pickAttachment`
  - `type pickItem struct { Label, Hint, Key string; Value any }`
  - `type pickResult struct { done, cancelled bool; item *pickItem; free string }`
  - `type picker struct { kind pickKind; title string; items []pickItem; matches []int; cursor int; input textinput.Model; keySelect, allowFree bool; err string }`
  - `func newPicker(kind pickKind, title string, items []pickItem) *picker`
  - `func (p *picker) update(msg tea.KeyPressMsg) (pickResult, tea.Cmd)`
  - `func (p *picker) view(width, height int) string`
  - Styles: `titleStyle`, `dimStyle`, `selectedStyle`, `errorStyle`, `modalStyle`
  - Test helper: `key(s string) tea.KeyPressMsg` and `typeText(p *picker, s string)` in `picker_test.go`, reused by later TUI tests

- [ ] **Step 1: Add the UI dependencies**

```bash
rtk go get charm.land/bubbletea/v2@v2.0.10 charm.land/bubbles/v2@v2.2.1 charm.land/lipgloss/v2@v2.0.6 charm.land/glamour/v2@v2.0.1 github.com/charmbracelet/x/ansi
```

- [ ] **Step 2: Write the failing test** `internal/tui/picker_test.go`

```go
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeText(p *picker, s string) {
	for _, r := range s {
		p.update(key(string(r)))
	}
}

func items(labels ...string) []pickItem {
	out := make([]pickItem, len(labels))
	for i, l := range labels {
		out[i] = pickItem{Label: l, Value: l}
	}
	return out
}

func TestPickerFiltersByWords(t *testing.T) {
	p := newPicker(pickProject, "Projects", items("Web App", "Mobile", "Web Site"))
	typeText(p, "site web")
	if len(p.matches) != 1 || p.items[p.matches[0]].Label != "Web Site" {
		t.Fatalf("matches = %v", p.matches)
	}
}

func TestPickerEnterSelectsHighlighted(t *testing.T) {
	p := newPicker(pickProject, "Projects", items("A", "B"))
	p.update(key("down"))
	res, _ := p.update(key("enter"))
	if !res.done || res.item == nil || res.item.Label != "B" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerFreeText(t *testing.T) {
	p := newPicker(pickRepo, "Repo", items("/code/web"))
	p.allowFree = true
	typeText(p, "/tmp/x")
	res, _ := p.update(key("enter"))
	if !res.done || res.item != nil || res.free != "/tmp/x" {
		t.Fatalf("enter with no matches: res = %+v", res)
	}
	p = newPicker(pickRepo, "Repo", items("/code/web"))
	p.allowFree = true
	typeText(p, "web")
	res, _ = p.update(key("tab"))
	if !res.done || res.free != "web" {
		t.Fatalf("tab: res = %+v", res)
	}
}

func TestPickerKeySelect(t *testing.T) {
	p := newPicker(pickAction, "Actions", []pickItem{{Label: "Claude", Key: "c"}, {Label: "View", Key: "v"}})
	p.keySelect = true
	if res, _ := p.update(key("z")); res.done {
		t.Fatal("unbound key must not select")
	}
	res, _ := p.update(key("v"))
	if !res.done || res.item.Label != "View" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickerEscCancels(t *testing.T) {
	p := newPicker(pickProject, "Projects", items("A"))
	if res, _ := p.update(key("esc")); !res.cancelled {
		t.Fatalf("res = %+v", res)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `rtk go test ./internal/tui/`
Expected: FAIL, `undefined: newPicker`.

- [ ] **Step 4: Implement** `internal/tui/styles.go`

```go
package tui

import "charm.land/lipgloss/v2"

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)
```

- [ ] **Step 5: Implement** `internal/tui/picker.go`

```go
package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type pickKind int

const (
	pickProject pickKind = iota
	pickAction
	pickTicketProject
	pickRepo
	pickAttachment
)

type pickItem struct {
	Label string
	Hint  string
	Key   string
	Value any
}

type pickResult struct {
	done      bool
	cancelled bool
	item      *pickItem
	free      string
}

// picker is a modal list. It filters by typed words, or with keySelect it
// selects by each item's Key. With allowFree, typed text can be returned as is.
type picker struct {
	kind      pickKind
	title     string
	items     []pickItem
	matches   []int
	cursor    int
	input     textinput.Model
	keySelect bool
	allowFree bool
	err       string
}

func newPicker(kind pickKind, title string, items []pickItem) *picker {
	in := textinput.New()
	in.Prompt = "> "
	in.Focus()
	p := &picker{kind: kind, title: title, items: items, input: in}
	p.refilter()
	return p
}

func matchesWords(label, query string) bool {
	l := strings.ToLower(label)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(l, w) {
			return false
		}
	}
	return true
}

func (p *picker) refilter() {
	p.matches = p.matches[:0]
	for i, it := range p.items {
		if matchesWords(it.Label, p.input.Value()) {
			p.matches = append(p.matches, i)
		}
	}
	p.cursor = max(min(p.cursor, len(p.matches)-1), 0)
}

func (p *picker) choose(i int) pickResult {
	it := p.items[i]
	return pickResult{done: true, item: &it}
}

func (p *picker) freeText() (pickResult, bool) {
	text := strings.TrimSpace(p.input.Value())
	if !p.allowFree || text == "" {
		return pickResult{}, false
	}
	return pickResult{done: true, free: text}, true
}

func (p *picker) update(msg tea.KeyPressMsg) (pickResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return pickResult{cancelled: true}, nil
	case "up", "ctrl+p":
		p.cursor = max(p.cursor-1, 0)
		return pickResult{}, nil
	case "down", "ctrl+n":
		p.cursor = max(min(p.cursor+1, len(p.matches)-1), 0)
		return pickResult{}, nil
	case "enter":
		if len(p.matches) > 0 {
			return p.choose(p.matches[p.cursor]), nil
		}
		res, _ := p.freeText()
		return res, nil
	case "tab":
		res, _ := p.freeText()
		return res, nil
	}
	if p.keySelect {
		for i := range p.items {
			if p.items[i].Key == msg.String() {
				return p.choose(i), nil
			}
		}
		return pickResult{}, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.err = ""
	p.refilter()
	return pickResult{}, cmd
}

func (p *picker) view(width, height int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(p.title) + "\n")
	if !p.keySelect {
		b.WriteString(p.input.View() + "\n")
	}
	rows := max(height-4, 1)
	start := max(p.cursor-rows+1, 0)
	if len(p.matches) == 0 {
		b.WriteString(dimStyle.Render("no matches") + "\n")
	}
	for i := start; i < len(p.matches) && i < start+rows; i++ {
		it := p.items[p.matches[i]]
		line := it.Label
		if p.keySelect {
			line = "[" + it.Key + "] " + line
		}
		if it.Hint != "" {
			line += "  " + dimStyle.Render(it.Hint)
		}
		line = ansi.Truncate(line, max(width-2, 1), "…")
		if i == p.cursor {
			line = selectedStyle.Render("▸ " + ansi.Strip(line))
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	if p.err != "" {
		b.WriteString(errorStyle.Render(p.err) + "\n")
	}
	hint := "enter select · esc cancel"
	if p.allowFree {
		hint += " · tab use typed path"
	}
	b.WriteString(dimStyle.Render(hint))
	return b.String()
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `rtk go test ./internal/tui/`
Expected: PASS. If typing does not reach the input, check that `textinput` v2 inserts `KeyPressMsg.Text` for focused inputs. `newPicker` calls `Focus()`.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add go.mod go.sum internal/tui
rtk git commit -m "feat(tui): reusable modal picker"
```

---

### Task 12: TUI model: list, reader, filter, project switching

**Files:**
- Create: `internal/tui/cmds.go`, `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: picker (Task 11), `asana.Client`, `asana.Task.SectionFor`, `ticket.Fetch`, `ticket.List`, `ticket.Clean`, `ticket.AttachmentURL`, `filter.Parse`, `Filter.Apply`, `repo.Candidates`, `browser.Open`, `kitty.Download`, `kitty.Encode`, `kitty.NewViewer`, `state.State`, `config.Config`.
- Produces:
  - `type Deps struct { Config config.Config; State *state.State; Client *asana.Client; StateDir string; Images, InTmux bool }`
  - `func New(d Deps) *Model`, `func (m *Model) ExitCommand() *exec.Cmd`, plus the `tea.Model` methods
  - Messages and commands used by Task 13: `candidatesMsg`, `actionDoneMsg{name, log string; err error}`, `imageMsg`, `loadCandidates`, `loadImage`, `openURL`
  - Model fields `run *pendingRun`, `modal *picker`, `exitCmd *exec.Cmd`
  - Methods `handlePick`, `openActionMenu`, `openAttachments`, `openRepoPicker` (the last three are defined in Task 13)

This task compiles only together with Task 13's `flow.go`, because `handlePick` dispatches to flow methods. Implement Step 3 (`cmds.go`) and Step 4 (`model.go`) here. Then add the Task 13 `flow.go` file before running the tests. Task 13 adds its own tests.

- [ ] **Step 1: Write the failing test** `internal/tui/model_test.go`

```go
package tui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func testModel(t *testing.T, cfg config.Config) (*Model, *state.State) {
	t.Helper()
	st, err := state.Load(filepath.Join(t.TempDir(), state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme == "" {
		cfg.Theme = "dark"
	}
	return New(Deps{Config: cfg, State: st, StateDir: t.TempDir()}), st
}

var (
	openTask = asana.Task{GID: "1", Name: "Fix login"}
	doneTask = asana.Task{GID: "2", Name: "Write docs", Completed: true}
	sideTask = asana.Task{GID: "3", Name: "Fix footer"}
)

func TestTasksFilteredByDefault(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	_, cmd := m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask}})
	if len(m.visible) != 1 || m.visible[0].GID != "1" || m.loading {
		t.Fatalf("visible = %+v, loading = %v", m.visible, m.loading)
	}
	if cmd == nil {
		t.Fatal("want a detail fetch to be scheduled")
	}
}

func TestStaleTasksIgnored(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.viewProject = &asana.Ref{GID: "p2"}
	m.Update(tasksMsg{project: &asana.Ref{GID: "p1"}, tasks: []asana.Task{openTask}})
	if m.tasks != nil || !m.loading {
		t.Fatalf("stale response applied: tasks = %+v", m.tasks)
	}
}

func TestDetailForOtherTicketCachedNotShown(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, sideTask}})
	m.Update(detailMsg{gid: "3", ticket: ticket.Ticket{Task: sideTask}})
	if _, ok := m.details["3"]; !ok || m.shownGID != "" {
		t.Fatalf("details = %v, shown = %q", m.details, m.shownGID)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	if m.shownGID != "1" {
		t.Fatalf("shown = %q, want 1", m.shownGID)
	}
}

func TestFilterTyping(t *testing.T) {
	m, _ := testModel(t, config.Config{DefaultFilter: "is:open"})
	m.Update(tasksMsg{tasks: []asana.Task{openTask, doneTask, sideTask}})
	m.Update(key("/"))
	for _, r := range " footer" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if m.filtering || len(m.visible) != 1 || m.visible[0].GID != "3" {
		t.Fatalf("filtering = %v, visible = %+v", m.filtering, m.visible)
	}
}

func TestProjectPickerOrder(t *testing.T) {
	m, st := testModel(t, config.Config{})
	st.RecentProjects = []string{"c", "gone"}
	m.projects = []asana.Project{{GID: "b", Name: "beta"}, {GID: "a", Name: "Alpha"}, {GID: "c", Name: "Gamma"}}
	m.openProjectPicker()
	var labels []string
	for _, it := range m.modal.items {
		labels = append(labels, it.Label)
	}
	if want := []string{"My Tasks", "Gamma", "Alpha", "beta"}; !slices.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

func TestPickingProjectRecordsRecent(t *testing.T) {
	m, st := testModel(t, config.Config{})
	m.projects = []asana.Project{{GID: "b", Name: "Beta"}}
	m.openProjectPicker()
	m.Update(key("down"))
	m.Update(key("enter"))
	if m.modal != nil || m.viewProject == nil || m.viewProject.GID != "b" || !m.loading {
		t.Fatalf("modal = %v, view = %v, loading = %v", m.modal, m.viewProject, m.loading)
	}
	again, _ := state.Load(st.Path())
	if len(again.RecentProjects) == 0 || again.RecentProjects[0] != "b" {
		t.Fatalf("recent = %v", again.RecentProjects)
	}
}
```

This test uses `st.Path()`. Add this accessor to `internal/state/state.go`:

```go
// Path returns the file the state is saved to.
func (s *State) Path() string { return s.path }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/tui/`
Expected: FAIL, `undefined: New` / `undefined: tasksMsg`.

- [ ] **Step 3: Implement** `internal/tui/cmds.go`

```go
package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/browser"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const (
	detailDelay    = 200 * time.Millisecond
	requestTimeout = time.Minute
)

type tasksMsg struct {
	project *asana.Ref
	tasks   []asana.Task
	err     error
}

type detailTickMsg struct{ gid string }

type detailMsg struct {
	gid    string
	ticket ticket.Ticket
	err    error
}

type projectsMsg struct {
	projects []asana.Project
	err      error
}

type candidatesMsg struct {
	paths []string
	err   error
}

type actionDoneMsg struct {
	name string
	log  string
	err  error
}

type imageMsg struct {
	payload string
	url     string
	err     error
}

type statusMsg string

func loadTasks(c *asana.Client, workspace string, project *asana.Ref) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		tasks, err := ticket.List(ctx, c, workspace, gidOf(project))
		return tasksMsg{project: project, tasks: tasks, err: err}
	}
}

func scheduleDetail(gid string) tea.Cmd {
	return tea.Tick(detailDelay, func(time.Time) tea.Msg { return detailTickMsg{gid: gid} })
}

func loadDetail(c *asana.Client, gid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		t, err := ticket.Fetch(ctx, c, gid)
		return detailMsg{gid: gid, ticket: t, err: err}
	}
}

func loadProjects(c *asana.Client, workspace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		me, err := c.Me(ctx)
		if err != nil {
			return projectsMsg{err: err}
		}
		projects, err := c.MemberProjects(ctx, workspace, me.GID)
		return projectsMsg{projects: projects, err: err}
	}
}

func loadCandidates(command string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		paths, err := repo.Candidates(ctx, command)
		return candidatesMsg{paths: paths, err: err}
	}
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		if err := browser.Open(url); err != nil {
			return statusMsg(err.Error())
		}
		return nil
	}
}

func loadImage(c *asana.Client, a asana.Attachment, cols, rows int, inTmux bool) tea.Cmd {
	url := ticket.AttachmentURL(a)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		fresh, err := c.Attachment(ctx, a.GID)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		if fresh.DownloadURL == nil {
			return imageMsg{url: url, err: errors.New("attachment has no download URL")}
		}
		data, err := kitty.Download(ctx, *fresh.DownloadURL)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		payload, err := kitty.Encode(data, cols, rows, inTmux)
		return imageMsg{payload: payload, url: url, err: err}
	}
}
```

- [ ] **Step 4: Implement** `internal/tui/model.go`

```go
// Package tui is the asanamate terminal interface.
package tui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/filter"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

const narrowWidth = 100

// Deps are the services the TUI uses.
type Deps struct {
	Config   config.Config
	State    *state.State
	Client   *asana.Client
	StateDir string
	Images   bool
	InTmux   bool
}

// Model is the Bubble Tea model for asanamate.
type Model struct {
	deps          Deps
	width, height int

	viewProject *asana.Ref
	tasks       []asana.Task
	visible     []asana.Task
	cursor      int
	loading     bool

	filterInput textinput.Model
	filtering   bool

	focusReader   bool
	reader        viewport.Model
	renderer      *glamour.TermRenderer
	rendererWidth int
	details       map[string]ticket.Ticket
	shownGID      string

	projects []asana.Project
	modal    *picker
	run      *pendingRun
	status   string
	exitCmd  *exec.Cmd
}

// New returns a model that starts on My Tasks with the configured default filter.
func New(d Deps) *Model {
	in := textinput.New()
	in.Prompt = "/"
	in.SetValue(d.Config.DefaultFilter)
	return &Model{
		deps:        d,
		filterInput: in,
		reader:      viewport.New(),
		details:     map[string]ticket.Ticket{},
		loading:     true,
	}
}

// ExitCommand is the command an exit-mode action left to run after the TUI quits.
func (m *Model) ExitCommand() *exec.Cmd { return m.exitCmd }

func (m *Model) Init() tea.Cmd {
	return loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case tasksMsg:
		if !sameProject(msg.project, m.viewProject) {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.status = "loading tasks: " + msg.err.Error()
			return m, nil
		}
		m.tasks = msg.tasks
		m.applyFilter()
		return m, m.selectionChanged()
	case detailTickMsg:
		if t, ok := m.selected(); ok && t.GID == msg.gid {
			if _, cached := m.details[msg.gid]; !cached {
				return m, loadDetail(m.deps.Client, msg.gid)
			}
		}
	case detailMsg:
		t, ok := m.selected()
		isSelected := ok && t.GID == msg.gid
		if msg.err != nil {
			if isSelected {
				m.status = "loading ticket: " + msg.err.Error()
			}
			return m, nil
		}
		m.details[msg.gid] = msg.ticket
		if isSelected {
			m.showDetail()
		}
	case projectsMsg:
		if msg.err != nil {
			m.status = "loading projects: " + msg.err.Error()
			return m, nil
		}
		m.status = ""
		m.projects = msg.projects
		m.openProjectPicker()
	case candidatesMsg:
		m.openRepoPicker(msg)
	case actionDoneMsg:
		m.status = actionStatus(msg)
	case imageMsg:
		if msg.err != nil {
			m.status = "image: " + msg.err.Error() + "; opening in browser"
			return m, openURL(msg.url)
		}
		m.status = ""
		viewer := kitty.NewViewer(msg.payload, m.deps.InTmux)
		return m, tea.Exec(viewer, func(err error) tea.Msg { return actionDoneMsg{name: "image viewer", err: err} })
	case statusMsg:
		m.status = string(msg)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func sameProject(a, b *asana.Ref) bool {
	return gidOf(a) == gidOf(b)
}

// gidOf returns the project's gid, or "" for My Tasks (nil).
func gidOf(project *asana.Ref) string {
	if project == nil {
		return ""
	}
	return project.GID
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.modal != nil {
		return m.updateModal(msg)
	}
	m.status = ""
	if m.filtering {
		return m.updateFilter(msg)
	}
	switch k {
	case "q":
		return tea.Quit
	case "tab":
		m.focusReader = !m.focusReader
		return nil
	case "/":
		m.filtering = true
		return m.filterInput.Focus()
	case "p":
		if m.projects != nil {
			m.openProjectPicker()
			return nil
		}
		m.status = "loading projects…"
		return loadProjects(m.deps.Client, m.deps.Config.Workspace)
	case "r":
		m.details = map[string]ticket.Ticket{}
		m.shownGID = ""
		return m.reload()
	case "o":
		if t, ok := m.selected(); ok {
			return openURL(t.PermalinkURL)
		}
		return nil
	case "a":
		m.openActionMenu()
		return nil
	case "f":
		m.openAttachments()
		return nil
	}
	if m.focusReader {
		var cmd tea.Cmd
		m.reader, cmd = m.reader.Update(msg)
		return cmd
	}
	switch k {
	case "j", "down":
		m.moveTo(m.cursor + 1)
	case "k", "up":
		m.moveTo(m.cursor - 1)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.visible) - 1)
	default:
		return nil
	}
	return m.selectionChanged()
}

func (m *Model) updateModal(msg tea.KeyPressMsg) tea.Cmd {
	p := m.modal
	res, cmd := p.update(msg)
	switch {
	case res.cancelled:
		m.modal, m.run = nil, nil
	case res.done:
		return tea.Batch(cmd, m.handlePick(p.kind, res))
	}
	return cmd
}

func (m *Model) handlePick(kind pickKind, res pickResult) tea.Cmd {
	switch kind {
	case pickProject:
		return m.pickedProject(res.item.Value.(*asana.Ref))
	case pickAction:
		return m.pickedAction(res.item.Value.(int))
	case pickTicketProject:
		return m.pickedTicketProject(res.item.Value.(asana.Ref))
	case pickRepo:
		path := res.free
		if res.item != nil {
			path = res.item.Value.(string)
		}
		return m.pickedRepo(path)
	case pickAttachment:
		return m.pickedAttachment(res.item.Value.(asana.Attachment))
	}
	return nil
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
		m.filterInput.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	return tea.Batch(cmd, m.selectionChanged())
}

func (m *Model) reload() tea.Cmd {
	m.loading = true
	m.tasks, m.visible, m.cursor = nil, nil, 0
	return loadTasks(m.deps.Client, m.deps.Config.Workspace, m.viewProject)
}

func (m *Model) applyFilter() {
	m.visible = filter.Parse(m.filterInput.Value()).Apply(m.tasks)
	m.moveTo(m.cursor)
}

func (m *Model) moveTo(i int) {
	m.cursor = max(min(i, len(m.visible)-1), 0)
}

func (m *Model) selected() (asana.Task, bool) {
	if m.cursor < len(m.visible) {
		return m.visible[m.cursor], true
	}
	return asana.Task{}, false
}

func (m *Model) selectedDetail() (ticket.Ticket, bool) {
	t, ok := m.selected()
	if !ok {
		return ticket.Ticket{}, false
	}
	d, ok := m.details[t.GID]
	return d, ok
}

func (m *Model) selectionChanged() tea.Cmd {
	t, ok := m.selected()
	if !ok {
		m.shownGID = ""
		m.reader.SetContent("")
		return nil
	}
	if t.GID == m.shownGID {
		return nil
	}
	if _, cached := m.details[t.GID]; cached {
		m.showDetail()
		return nil
	}
	m.shownGID = ""
	m.reader.SetContent(dimStyle.Render("Loading " + ticket.Clean(t.Name) + "…"))
	return scheduleDetail(t.GID)
}

func (m *Model) showDetail() {
	t, ok := m.selectedDetail()
	if !ok {
		return
	}
	_, readerW, _ := m.paneWidths()
	m.reader.SetContent(m.renderMarkdown(t.Markdown(), max(readerW-2, 20)))
	m.reader.GotoTop()
	m.shownGID = t.GID
}

func (m *Model) renderMarkdown(md string, width int) string {
	if m.renderer == nil || m.rendererWidth != width {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(m.deps.Config.Theme), glamour.WithWordWrap(width))
		if err != nil {
			return md
		}
		m.renderer, m.rendererWidth = r, width
	}
	out, err := m.renderer.Render(md)
	if err != nil {
		return md
	}
	return out
}

func (m *Model) openProjectPicker() {
	items := []pickItem{{Label: "My Tasks", Value: (*asana.Ref)(nil)}}
	byGID := map[string]asana.Project{}
	for _, p := range m.projects {
		byGID[p.GID] = p
	}
	seen := map[string]bool{}
	for _, gid := range m.deps.State.RecentProjects {
		if p, ok := byGID[gid]; ok {
			items = append(items, pickItem{Label: ticket.Clean(p.Name), Hint: "recent", Value: &asana.Ref{GID: p.GID, Name: p.Name}})
			seen[gid] = true
		}
	}
	rest := make([]asana.Project, 0, len(m.projects))
	for _, p := range m.projects {
		if !seen[p.GID] {
			rest = append(rest, p)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return strings.ToLower(rest[i].Name) < strings.ToLower(rest[j].Name) })
	for _, p := range rest {
		items = append(items, pickItem{Label: ticket.Clean(p.Name), Value: &asana.Ref{GID: p.GID, Name: p.Name}})
	}
	m.modal = newPicker(pickProject, "Switch project", items)
}

func (m *Model) pickedProject(ref *asana.Ref) tea.Cmd {
	m.modal = nil
	m.viewProject = ref
	if ref != nil {
		m.deps.State.TouchProject(ref.GID)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving state: " + err.Error()
		}
	}
	return m.reload()
}

func (m *Model) paneWidths() (listW, readerW int, split bool) {
	if m.width < narrowWidth {
		return m.width, m.width, false
	}
	listW = m.width * 2 / 5
	return listW, m.width - listW - 1, true
}

func (m *Model) bodyHeight() int { return max(m.height-2, 1) }

func (m *Model) layout() {
	_, readerW, _ := m.paneWidths()
	m.reader.SetWidth(readerW)
	m.reader.SetHeight(m.bodyHeight())
	m.filterInput.SetWidth(max(m.width/3, 10))
	if m.shownGID != "" {
		m.showDetail()
	}
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.header() + "\n" + m.body() + "\n" + m.footer())
	v.AltScreen = true
	return v
}

func (m *Model) header() string {
	name := "My Tasks"
	if m.viewProject != nil {
		name = ticket.Clean(m.viewProject.Name)
	}
	f := dimStyle.Render("filter: " + m.filterInput.Value())
	if m.filtering {
		f = m.filterInput.View()
	}
	count := dimStyle.Render(fmt.Sprintf("%d/%d", len(m.visible), len(m.tasks)))
	return ansi.Truncate(titleStyle.Render("asanamate · "+name)+"  "+f+"  "+count, m.width, "…")
}

func (m *Model) footer() string {
	s := m.status
	if s == "" {
		s = dimStyle.Render("j/k move · tab focus · / filter · p projects · a actions · f files · o open · r reload · q quit")
	}
	return ansi.Truncate(s, m.width, "…")
}

func (m *Model) body() string {
	h := m.bodyHeight()
	if m.modal != nil {
		box := modalStyle.Render(m.modal.view(max(min(m.width-4, 80), 10), max(h-2, 3)))
		return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
	}
	listW, _, split := m.paneWidths()
	list := lipgloss.NewStyle().Width(listW).Height(h).Render(m.listView(listW, h))
	switch {
	case split:
		sep := dimStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", h), "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, list, sep, m.reader.View())
	case m.focusReader:
		return m.reader.View()
	default:
		return list
	}
}

func (m *Model) listView(width, height int) string {
	switch {
	case m.loading:
		return dimStyle.Render("Loading tasks…")
	case len(m.visible) == 0:
		return dimStyle.Render("No tasks match the filter.")
	}
	start := max(m.cursor-height+1, 0)
	var lines []string
	for i := start; i < len(m.visible) && i < start+height; i++ {
		t := m.visible[i]
		mark := "○"
		if t.Completed {
			mark = "✓"
		}
		line := mark + " " + ticket.Clean(t.Name)
		if sec := t.SectionFor(gidOf(m.viewProject)); sec != "" {
			line += "  " + dimStyle.Render(ticket.Clean(sec))
		}
		line = ansi.Truncate(line, width, "…")
		if i == m.cursor {
			style := selectedStyle
			if m.focusReader {
				style = titleStyle
			}
			line = style.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 5: Continue with Task 13 Steps 1–3**, then run this task's tests

Task 13 defines `pendingRun`, `openActionMenu`, `openAttachments`, `openRepoPicker`, `pickedAction`, `pickedTicketProject`, `pickedRepo`, `pickedAttachment`, and `actionStatus`. Once Task 13's `flow.go` exists:

Run: `rtk go test ./internal/tui/ -run 'Tasks|Stale|Detail|Filter|Project'`
Expected: PASS.

- [ ] **Step 6: Commit** (ask the user first; commit together with Task 13's `flow.go` if the package only compiles with both files)

```bash
rtk git add internal/state/state.go internal/tui/cmds.go internal/tui/model.go internal/tui/model_test.go
rtk git commit -m "feat(tui): ticket list, reader, filter, and project picker"
```

---

### Task 13: TUI action flow, repo resolution, attachments, and TUI entry point

**Files:**
- Create: `internal/tui/flow.go`
- Modify: `cmd/asanamate/main.go` (run the TUI by default and propagate the exit-mode status)
- Test: `internal/tui/flow_test.go`

**Interfaces:**
- Consumes: `action.DefaultProject`, `action.WriteFiles`, `action.Command`, `action.RunBackground`, `action.Context`, `repo.Resolve`, `config.ModeForeground/ModeBackground/ModeExit`, `Config.ConfirmWritesFor`, `kitty.IsImage`, `ticket.AttachmentURL`, and Task 12's model and commands.
- Produces: `type pendingRun struct { action config.Action; ticket ticket.Ticket; project *asana.Ref; projectChosen bool }` and the methods listed in Task 12 Step 5.

- [ ] **Step 1: Write the failing test** `internal/tui/flow_test.go`

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/state"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	resolved, err := repo.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

var exitRepoAction = config.Action{Name: "Go", Key: "x", Mode: config.ModeExit, Repo: true, Command: "true"}

func flowModel(t *testing.T, memberships ...asana.Membership) (*Model, *state.State) {
	t.Helper()
	m, st := testModel(t, config.Config{
		Actions:    []config.Action{exitRepoAction},
		RepoSource: config.RepoSource{Command: `printf '/definitely/missing\n'`},
	})
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "Fix", Memberships: memberships}}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["1"] = tk
	m.openActionMenu()
	return m, st
}

var (
	web = asana.Membership{Project: asana.Ref{GID: "p1", Name: "Web"}}
	api = asana.Membership{Project: asana.Ref{GID: "p2", Name: "API"}}
)

func TestSeveralProjectsAskWhichOne(t *testing.T) {
	m, _ := flowModel(t, web, api)
	m.pickedAction(0)
	if m.modal == nil || m.modal.kind != pickTicketProject || len(m.modal.items) != 2 {
		t.Fatalf("modal = %+v", m.modal)
	}
}

func TestLinkedRepoRunsImmediately(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t, web)
	st.LinkRepo("p1", dir)
	m.pickedAction(0)
	cmd := m.ExitCommand()
	if cmd == nil || cmd.Dir != dir || !slices.Contains(cmd.Env, "ASANAMATE_REPO="+dir) || !slices.Contains(cmd.Env, "ASANAMATE_PROJECT=Web") {
		t.Fatalf("exit command = %+v", cmd)
	}
	if _, err := os.Stat(filepath.Join(m.deps.StateDir, "tickets", "1", "ticket.md")); err != nil {
		t.Fatalf("ticket.md not written: %v", err)
	}
}

func TestUnlinkedRepoPromptsAndSaves(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t, web)
	msg := m.pickedAction(0)().(candidatesMsg)
	m.Update(msg)
	if m.modal == nil || m.modal.kind != pickRepo || !m.modal.allowFree {
		t.Fatalf("modal = %+v", m.modal)
	}
	m.modal.input.SetValue(t.TempDir())
	m.Update(key("tab"))
	if m.modal == nil || !strings.Contains(m.modal.err, "not a git repository") {
		t.Fatalf("invalid path must keep the picker open with an error; modal = %+v", m.modal)
	}
	m.modal.input.SetValue(dir)
	m.Update(key("tab"))
	if m.modal != nil || m.ExitCommand() == nil || m.ExitCommand().Dir != dir {
		t.Fatalf("modal = %+v, exit = %+v", m.modal, m.ExitCommand())
	}
	again, _ := state.Load(st.Path())
	if again.Repos["p1"] != dir {
		t.Fatalf("saved repos = %v", again.Repos)
	}
}

func TestStaleRepoLinkReprompts(t *testing.T) {
	m, st := flowModel(t, web)
	st.LinkRepo("p1", t.TempDir())
	cmd := m.pickedAction(0)
	if cmd == nil || m.ExitCommand() != nil || !strings.Contains(m.status, "no longer a git repository") {
		t.Fatalf("status = %q, exit = %v", m.status, m.ExitCommand())
	}
	if _, ok := cmd().(candidatesMsg); !ok {
		t.Fatal("want the repo picker to load candidates")
	}
}

func TestNoProjectRepoIsNotSaved(t *testing.T) {
	dir := gitRepo(t)
	m, st := flowModel(t)
	m.Update(m.pickedAction(0)())
	m.pickedRepo(dir)
	if m.ExitCommand() == nil || len(st.Repos) != 0 {
		t.Fatalf("exit = %v, repos = %v", m.ExitCommand(), st.Repos)
	}
}

func TestNonRepoActionUsesViewedProject(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "Fix", Memberships: []asana.Membership{web, api}}}
	m.viewProject = &asana.Ref{GID: "p2", Name: "API"}
	m.tasks, m.visible = []asana.Task{tk.Task}, []asana.Task{tk.Task}
	m.details["1"] = tk
	m.openActionMenu()
	m.Update(key("x"))
	if cmd := m.ExitCommand(); cmd == nil || !slices.Contains(cmd.Env, "ASANAMATE_PROJECT_GID=p2") || cmd.Dir != "" {
		t.Fatalf("exit command = %+v", cmd)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `rtk go test ./internal/tui/`
Expected: FAIL, `undefined: pendingRun` / `m.openActionMenu undefined`.

- [ ] **Step 3: Implement** `internal/tui/flow.go`

```go
package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/action"
	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/kitty"
	"github.com/sadmachine/asanamate/internal/repo"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// pendingRun tracks an action between picking it and launching it.
type pendingRun struct {
	action        config.Action
	ticket        ticket.Ticket
	project       *asana.Ref
	projectChosen bool
}

func (m *Model) openActionMenu() {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return
	}
	if len(m.deps.Config.Actions) == 0 {
		m.status = "no actions configured; add [[actions]] to config.toml"
		return
	}
	items := make([]pickItem, len(m.deps.Config.Actions))
	for i, a := range m.deps.Config.Actions {
		items[i] = pickItem{Label: a.Name, Hint: a.Mode, Key: a.Key, Value: i}
	}
	p := newPicker(pickAction, "Run on: "+ticket.Clean(t.Name), items)
	p.keySelect = true
	m.modal = p
	m.run = &pendingRun{ticket: t}
}

func (m *Model) pickedAction(i int) tea.Cmd {
	m.modal = nil
	m.run.action = m.deps.Config.Actions[i]
	return m.continueRun()
}

func (m *Model) pickedTicketProject(ref asana.Ref) tea.Cmd {
	m.modal = nil
	m.run.project, m.run.projectChosen = &ref, true
	return m.continueRun()
}

// continueRun advances the run: choose the project, use its linked repo if
// still valid, otherwise load candidates for the repo picker.
func (m *Model) continueRun() tea.Cmd {
	r := m.run
	if !r.action.Repo {
		r.project = action.DefaultProject(r.ticket.Task, gidOf(m.viewProject))
		return m.execute("")
	}
	if !r.projectChosen {
		switch len(r.ticket.Memberships) {
		case 0:
			r.projectChosen = true
		case 1:
			p := r.ticket.Memberships[0].Project
			r.project, r.projectChosen = &p, true
		default:
			items := make([]pickItem, len(r.ticket.Memberships))
			for i, mb := range r.ticket.Memberships {
				items[i] = pickItem{Label: ticket.Clean(mb.Project.Name), Value: mb.Project}
			}
			m.modal = newPicker(pickTicketProject, "Which project's repo?", items)
			return nil
		}
	}
	if r.project != nil {
		if path, ok := m.deps.State.Repos[r.project.GID]; ok {
			if resolved, err := repo.Resolve(path); err == nil {
				return m.execute(resolved)
			}
			m.status = fmt.Sprintf("linked repo %s is no longer a git repository; pick again", path)
		}
	}
	return loadCandidates(m.deps.Config.RepoSource.Command)
}

func (m *Model) openRepoPicker(msg candidatesMsg) {
	if m.run == nil {
		return
	}
	items := make([]pickItem, len(msg.paths))
	for i, p := range msg.paths {
		items[i] = pickItem{Label: p, Value: p}
	}
	title := "Repo for this ticket"
	if m.run.project != nil {
		title = "Repo for " + ticket.Clean(m.run.project.Name)
	}
	p := newPicker(pickRepo, title, items)
	p.allowFree = true
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	m.modal = p
}

func (m *Model) pickedRepo(path string) tea.Cmd {
	resolved, err := repo.Resolve(path)
	if err != nil {
		if m.modal != nil {
			m.modal.err = err.Error()
		}
		return nil
	}
	m.modal = nil
	if p := m.run.project; p != nil {
		m.deps.State.LinkRepo(p.GID, resolved)
		if err := m.deps.State.Save(); err != nil {
			m.status = "saving repo link: " + err.Error()
		}
	}
	return m.execute(resolved)
}

func (m *Model) execute(repoPath string) tea.Cmd {
	r := m.run
	m.run = nil
	files, err := action.WriteFiles(m.deps.StateDir, r.ticket)
	if err != nil {
		m.status = "writing ticket files: " + err.Error()
		return nil
	}
	cmd := action.Command(r.action, action.Context{
		Ticket:        r.ticket,
		Project:       r.project,
		Repo:          repoPath,
		ConfirmWrites: m.deps.Config.ConfirmWritesFor(r.action),
		Files:         files,
	})
	name := r.action.Name
	switch r.action.Mode {
	case config.ModeForeground:
		return tea.ExecProcess(cmd, func(err error) tea.Msg { return actionDoneMsg{name: name, err: err} })
	case config.ModeBackground:
		m.status = name + ": running…"
		logPath := filepath.Join(m.deps.StateDir, "actions.log")
		return func() tea.Msg {
			return actionDoneMsg{name: name, log: logPath, err: action.RunBackground(cmd, logPath)}
		}
	default:
		m.exitCmd = cmd
		return tea.Quit
	}
}

func actionStatus(msg actionDoneMsg) string {
	if msg.err == nil {
		return msg.name + ": done"
	}
	s := msg.name + " failed: " + msg.err.Error()
	if msg.log != "" {
		s += " (see " + msg.log + ")"
	}
	return s
}

func (m *Model) openAttachments() {
	t, ok := m.selectedDetail()
	if !ok {
		m.status = "ticket details are still loading"
		return
	}
	if len(t.Attachments) == 0 {
		m.status = "no attachments"
		return
	}
	items := make([]pickItem, len(t.Attachments))
	for i, a := range t.Attachments {
		hint := "open in browser"
		if m.showsInline(a) {
			hint = "view image"
		}
		items[i] = pickItem{Label: fmt.Sprintf("%d. %s", i+1, ticket.Clean(a.Name)), Hint: hint, Value: a}
	}
	m.modal = newPicker(pickAttachment, "Attachments", items)
}

func (m *Model) showsInline(a asana.Attachment) bool {
	return m.deps.Images && a.Host == "asana" && kitty.IsImage(a.Name)
}

func (m *Model) pickedAttachment(a asana.Attachment) tea.Cmd {
	m.modal = nil
	if m.showsInline(a) {
		m.status = "loading image…"
		return loadImage(m.deps.Client, a, max(m.width, 1), max(m.height-3, 1), m.deps.InTmux)
	}
	return openURL(ticket.AttachmentURL(a))
}
```

- [ ] **Step 4: Run all TUI tests (Tasks 11–13)**

Run: `rtk go test ./internal/tui/ && rtk go vet ./...`
Expected: PASS.

- [ ] **Step 5: Make the TUI the default command** in `cmd/asanamate/main.go`

Add these imports: `"errors"`, `"os/exec"`, `tea "charm.land/bubbletea/v2"`, `"github.com/sadmachine/asanamate/internal/kitty"`, `"github.com/sadmachine/asanamate/internal/tui"`.

In `run`, replace the `default:` case

```go
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
```

with

```go
	case "":
		err = runTUI()
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
```

and replace the error handling at the end of `run`

```go
	if err != nil {
		fmt.Fprintln(os.Stderr, "asanamate:", err)
		return 1
	}
	return 0
```

with

```go
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "asanamate:", err)
		return 1
	}
	return 0
```

Append:

```go
func runTUI() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	stateDir, err := config.StateDir()
	if err != nil {
		return err
	}
	st, err := state.Load(filepath.Join(stateDir, state.FileName))
	if err != nil {
		return err
	}
	m := tui.New(tui.Deps{
		Config:   cfg,
		State:    st,
		Client:   client,
		StateDir: stateDir,
		Images:   kitty.Supported(cfg.Images, os.Getenv, kitty.TmuxPassthrough),
		InTmux:   os.Getenv("TMUX") != "",
	})
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	cmd := m.ExitCommand()
	if cmd == nil {
		return nil
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
```

This only returns `*exec.ExitError` from an exit-mode action, so the exit-code branch applies only to that case.

- [ ] **Step 6: Manual verification against the real Asana account** (needs `ASANA_ACCESS_TOKEN`)

Run: `rtk go build -o /tmp/asanamate ./cmd/asanamate && XDG_CONFIG_HOME=/tmp/am-cfg XDG_STATE_HOME=/tmp/am-state /tmp/asanamate setup`, then `XDG_CONFIG_HOME=/tmp/am-cfg XDG_STATE_HOME=/tmp/am-state /tmp/asanamate`.

Check each item and report the results to the user:
- My Tasks loads. `/` filtering updates the list live.
- `p` lists "My Tasks", recents, then projects. Picking one loads its tasks. Reopening `p` shows it as recent.
- The reading pane shows fields, description, comments, subtasks, and attachments.
- `a` → `v` (pager action) suspends and resumes the TUI.
- A `repo = true` action prompts for a repo on first use. The second use skips the prompt.
- `f` on an image attachment shows the image (kitty/Ghostty, with `allow-passthrough on` in tmux) or opens the browser.
- Width below 100 columns shows one pane. `tab` switches panes.
- `rtk go run ./cmd/asanamate comment <gid> "test"` prompts before posting. Only run this on a scratch task the user names.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add internal/tui/flow.go internal/tui/flow_test.go cmd/asanamate/main.go
rtk git commit -m "feat(tui): actions, repo resolution, attachments, and TUI entry"
```

---

### Task 14: List-only TUI and scripting output (`--no-preview`, `list`, `show`)

**Files:**
- Create: `internal/listing/listing.go`
- Modify: `internal/tui/model.go`, `internal/tui/flow.go`, `cmd/asanamate/main.go`
- Test: `internal/listing/listing_test.go`, `internal/tui/listonly_test.go`

**Interfaces:**
- Consumes: `ticket.List`, `ticket.Fetch`, `ticket.Clean`, `Ticket.Markdown`, `filter.Parse`, `Filter.Apply`, `asana.Task.SectionFor`, `asana.ValidGID`, and the Task 12–13 model.
- Produces:
  - Format constants: `listing.FormatTSV = "tsv"`, `listing.FormatJSONL = "jsonl"`, `listing.FormatMarkdown = "md"`, `listing.FormatJSON = "json"`
  - `listing.Tasks(w io.Writer, format string, tasks []asana.Task, projectGID string) error`
  - `listing.Ticket(w io.Writer, format string, t ticket.Ticket) error`
  - TUI: `Deps.NoPreview bool`; Enter opens the action menu, fetching details first if needed
  - CLI: `asanamate [--no-preview]`, `asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]`, `asanamate show [--format md|json] <gid>`

- [ ] **Step 1: Write the failing tests**

`internal/listing/listing_test.go`:

```go
package listing

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

var due = "2026-10-01"

var tasks = []asana.Task{
	{
		GID: "1", Name: "Fix\tlogin\nnow\x1b", DueOn: &due, PermalinkURL: "https://app.asana.com/t/1",
		AssigneeSection: &asana.Ref{Name: "Today"},
		Memberships:     []asana.Membership{{Project: asana.Ref{GID: "p", Name: "Web"}, Section: &asana.Ref{Name: "Doing"}}},
	},
	{GID: "2", Name: "Plain"},
}

func TestTasksTSV(t *testing.T) {
	var b strings.Builder
	if err := Tasks(&b, FormatTSV, tasks, ""); err != nil {
		t.Fatal(err)
	}
	want := "1\tToday\t2026-10-01\tFix login now\thttps://app.asana.com/t/1\n2\t\t\tPlain\t\n"
	if b.String() != want {
		t.Fatalf("got %q\nwant %q", b.String(), want)
	}
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n") {
		if n := len(strings.Split(line, "\t")); n != 5 {
			t.Fatalf("line %q has %d columns", line, n)
		}
	}
	b.Reset()
	Tasks(&b, FormatTSV, tasks[:1], "p")
	if !strings.HasPrefix(b.String(), "1\tDoing\t") {
		t.Fatalf("project section not used: %q", b.String())
	}
}

func TestTasksJSONL(t *testing.T) {
	var b strings.Builder
	if err := Tasks(&b, FormatJSONL, tasks, ""); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	var first asana.Task
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first.GID != "1" || first.SectionFor("p") != "Doing" {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
}

func TestTicketFormats(t *testing.T) {
	tk := ticket.Ticket{Task: asana.Task{GID: "1", Name: "T"}}
	var md, js strings.Builder
	if err := Ticket(&md, FormatMarkdown, tk); err != nil || !strings.HasPrefix(md.String(), "# T\n") {
		t.Fatalf("md = %q, err = %v", md.String(), err)
	}
	if err := Ticket(&js, FormatJSON, tk); err != nil || !strings.Contains(js.String(), `"comments"`) {
		t.Fatalf("json = %q, err = %v", js.String(), err)
	}
}

func TestUnknownFormats(t *testing.T) {
	if err := Tasks(io.Discard, "csv", tasks, ""); err == nil {
		t.Error("Tasks accepted csv")
	}
	if err := Ticket(io.Discard, "html", ticket.Ticket{}); err == nil {
		t.Error("Ticket accepted html")
	}
}
```

`internal/tui/listonly_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/config"
	"github.com/sadmachine/asanamate/internal/ticket"
)

func TestNoPreviewShowsOnlyTheList(t *testing.T) {
	m, _ := testModel(t, config.Config{})
	m.deps.NoPreview = true
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(key("tab"))
	if listW, _, split := m.paneWidths(); listW != 160 || split || m.focusReader {
		t.Fatalf("listW = %d, split = %v, focusReader = %v", listW, split, m.focusReader)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	body := m.body()
	if m.shownGID != "1" || !strings.Contains(body, "Fix login") || strings.Contains(body, "Loading") {
		t.Fatalf("shown = %q, body = %q", m.shownGID, body)
	}
	if strings.Contains(m.footer(), "tab focus") {
		t.Fatal("footer must not advertise tab in list-only mode")
	}
}

func TestEnterOpensMenuAfterDetailLoads(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	_, cmd := m.Update(key("enter"))
	if cmd == nil || m.modal != nil || m.menuFor != "1" {
		t.Fatalf("cmd = %v, modal = %v, menuFor = %q", cmd, m.modal, m.menuFor)
	}
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	if m.modal == nil || m.modal.kind != pickAction || m.menuFor != "" {
		t.Fatalf("modal = %+v, menuFor = %q", m.modal, m.menuFor)
	}
}

func TestEnterWithCachedDetailOpensMenuImmediately(t *testing.T) {
	m, _ := testModel(t, config.Config{Actions: []config.Action{{Name: "Go", Key: "x", Mode: config.ModeExit, Command: "true"}}})
	m.Update(tasksMsg{tasks: []asana.Task{openTask}})
	m.Update(detailMsg{gid: "1", ticket: ticket.Ticket{Task: openTask}})
	m.Update(key("enter"))
	if m.modal == nil || m.modal.kind != pickAction {
		t.Fatalf("modal = %+v", m.modal)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk go test ./internal/listing/ ./internal/tui/`
Expected: FAIL, `undefined: Tasks`, `m.deps.NoPreview undefined`, `m.menuFor undefined`.

- [ ] **Step 3: Implement** `internal/listing/listing.go`

```go
// Package listing formats tickets for scripts and tools such as fzf.
package listing

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sadmachine/asanamate/internal/asana"
	"github.com/sadmachine/asanamate/internal/ticket"
)

// Output formats.
const (
	FormatTSV      = "tsv"
	FormatJSONL    = "jsonl"
	FormatMarkdown = "md"
	FormatJSON     = "json"
)

var flatten = strings.NewReplacer("\t", " ", "\n", " ")

// Tasks writes one line per task. TSV columns are gid, section, due, title,
// url; section is the My Tasks section, or the section in projectGID.
func Tasks(w io.Writer, format string, tasks []asana.Task, projectGID string) error {
	switch format {
	case FormatTSV:
		for _, t := range tasks {
			due := ""
			if t.DueOn != nil {
				due = *t.DueOn
			}
			cols := []string{t.GID, t.SectionFor(projectGID), due, t.Name, t.PermalinkURL}
			for i, c := range cols {
				cols[i] = flatten.Replace(ticket.Clean(c))
			}
			if _, err := fmt.Fprintln(w, strings.Join(cols, "\t")); err != nil {
				return err
			}
		}
		return nil
	case FormatJSONL:
		enc := json.NewEncoder(w)
		for _, t := range tasks {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown list format %q; use %s or %s", format, FormatTSV, FormatJSONL)
}

// Ticket writes one ticket as Markdown (same as ticket.md) or indented JSON.
func Ticket(w io.Writer, format string, t ticket.Ticket) error {
	switch format {
	case FormatMarkdown:
		_, err := io.WriteString(w, t.Markdown())
		return err
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(t)
	}
	return fmt.Errorf("unknown show format %q; use %s or %s", format, FormatMarkdown, FormatJSON)
}
```

- [ ] **Step 4: Add list-only mode and Enter-to-act to the TUI**

In `internal/tui/model.go`:

Add a field to `Deps` after `InTmux bool`:

```go
	// NoPreview hides the reading pane and gives the list the full width.
	NoPreview bool
```

Add a field to `Model` after `run *pendingRun`:

```go
	menuFor string // gid whose action menu opens once its details arrive
```

In `Update`, replace the whole `case detailMsg:` block with:

```go
	case detailMsg:
		t, ok := m.selected()
		isSelected := ok && t.GID == msg.gid
		if msg.err != nil {
			if isSelected {
				m.status = "loading ticket: " + msg.err.Error()
			}
			if m.menuFor == msg.gid {
				m.menuFor = ""
			}
			return m, nil
		}
		m.details[msg.gid] = msg.ticket
		if isSelected {
			m.showDetail()
			if m.menuFor == msg.gid {
				m.menuFor = ""
				m.openActionMenu()
			}
		}
```

In `handleKey`, replace

```go
	case "tab":
		m.focusReader = !m.focusReader
		return nil
```

with

```go
	case "tab":
		if !m.deps.NoPreview {
			m.focusReader = !m.focusReader
		}
		return nil
```

and replace

```go
	case "a":
		m.openActionMenu()
		return nil
```

with

```go
	case "a", "enter":
		return m.requestActionMenu()
```

In `showDetail`, after the `if !ok { return }` guard, insert:

```go
	if m.deps.NoPreview {
		m.shownGID = t.GID
		return
	}
```

In `paneWidths`, replace `if m.width < narrowWidth {` with `if m.deps.NoPreview || m.width < narrowWidth {`.

Replace `footer` with:

```go
func (m *Model) footer() string {
	s := m.status
	if s == "" {
		hints := "j/k move · enter actions · tab focus · / filter · p projects · f files · o open · r reload · q quit"
		if m.deps.NoPreview {
			hints = strings.Replace(hints, " · tab focus", "", 1)
		}
		s = dimStyle.Render(hints)
	}
	return ansi.Truncate(s, m.width, "…")
}
```

In `internal/tui/flow.go`, add after `openActionMenu`:

```go
// requestActionMenu opens the action menu, first fetching the ticket's
// details if they have not arrived yet.
func (m *Model) requestActionMenu() tea.Cmd {
	t, ok := m.selected()
	if !ok {
		return nil
	}
	if _, cached := m.details[t.GID]; cached {
		m.openActionMenu()
		return nil
	}
	m.menuFor = t.GID
	m.status = "loading ticket…"
	return loadDetail(m.deps.Client, t.GID)
}
```

- [ ] **Step 5: Add the `--no-preview` flag and the `list` and `show` subcommands** in `cmd/asanamate/main.go`

Add these imports: `"strings"`, `"github.com/sadmachine/asanamate/internal/filter"`, `"github.com/sadmachine/asanamate/internal/listing"`, `"github.com/sadmachine/asanamate/internal/ticket"`.

Replace the `usage` constant with:

```go
const usage = `usage:
  asanamate [--no-preview]                         open the TUI (--no-preview: list only)
  asanamate list [--project <gid>] [--filter <query>] [--format tsv|jsonl]
                                                   print tickets (tsv: gid, section, due, title, url)
  asanamate show [--format md|json] <gid>          print one ticket
  asanamate setup                                  create the config file
  asanamate comment [--yes] <gid> <text | ->       comment on a task ("-" reads stdin)
  asanamate move    [--yes] [--project <gid>] <gid> <section>
  asanamate field   [--yes] <gid> <field> <value>  set a custom field ("" clears it)
  asanamate version
`
```

In `run`, replace

```go
	case "":
		err = runTUI()
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
```

with

```go
	case "list":
		err = runList(args[1:])
	case "show":
		err = runShow(args[1:])
	default:
		if name != "" && !strings.HasPrefix(name, "-") {
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		err = runTUI(args)
```

Change `func runTUI() error {` to `func runTUI(args []string) error {`, and insert these lines at the start of its body:

```go
	fs := flag.NewFlagSet("asanamate", flag.ContinueOnError)
	noPreview := fs.Bool("no-preview", false, "show only the ticket list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
```

In the `tui.Deps{...}` literal inside `runTUI`, add `NoPreview: *noPreview,`.

Append:

```go
func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	project := fs.String("project", "", "project gid (default: My Tasks)")
	query := fs.String("filter", "", "filter query (default: default_filter from the config)")
	format := fs.String("format", listing.FormatTSV, "tsv or jsonl")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %v\n%s", fs.Args(), usage)
	}
	if *project != "" && !asana.ValidGID(*project) {
		return fmt.Errorf("project gid must be numeric, got %q", *project)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	filterSet := false
	fs.Visit(func(f *flag.Flag) { filterSet = filterSet || f.Name == "filter" })
	if !filterSet {
		*query = cfg.DefaultFilter
	}
	tasks, err := ticket.List(context.Background(), client, cfg.Workspace, *project)
	if err != nil {
		return err
	}
	return listing.Tasks(os.Stdout, *format, filter.Parse(*query).Apply(tasks), *project)
}

func runShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	format := fs.String("format", listing.FormatMarkdown, "md or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("show needs exactly one task gid\n%s", usage)
	}
	gid := fs.Arg(0)
	if !asana.ValidGID(gid) {
		return fmt.Errorf("task gid must be numeric, got %q", gid)
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	t, err := ticket.Fetch(context.Background(), client, gid)
	if err != nil {
		return err
	}
	return listing.Ticket(os.Stdout, *format, t)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `rtk go test ./... && rtk go vet ./...`
Expected: PASS.

- [ ] **Step 7: Manual verification** (needs `ASANA_ACCESS_TOKEN` and the config from Task 13 Step 6)

- `rtk go run ./cmd/asanamate --no-preview`: a full-width list. `tab` does nothing. `enter` opens the action menu, even when pressed right after moving to a ticket.
- `rtk go run ./cmd/asanamate list | head`: every line has 5 tab-separated columns.
- `rtk go run ./cmd/asanamate list --filter "" --format jsonl | head -1`: one JSON object.
- `rtk go run ./cmd/asanamate list | fzf --delimiter '\t' --with-nth 2,4 --preview 'go run ./cmd/asanamate show {1}' | cut -f1`: the preview shows the ticket, and the chosen gid is printed.
- `rtk go run ./cmd/asanamate bogus`: prints usage and exits 2.

- [ ] **Step 8: Commit** (ask the user first)

```bash
rtk git add internal/listing internal/tui cmd/asanamate/main.go
rtk git commit -m "feat: list-only TUI and list/show output for scripts"
```

---

### Task 15: Distribution and README

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`
- Modify: `README.md` (replace the placeholder)

**Interfaces:**
- Consumes: `main.version` (set with `-X main.version`).

- [ ] **Step 1: Write** `.goreleaser.yaml`

```yaml
version: 2
project_name: asanamate

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - main: ./cmd/asanamate
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{ .Version }}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: checksums.txt

changelog:
  use: github

homebrew_casks:
  - name: asanamate
    repository:
      owner: sadmachine
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: https://github.com/sadmachine/asanamate
    description: Terminal UI for Asana tickets with scriptable actions
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/asanamate"]
          end
```

- [ ] **Step 2: Write** `.github/workflows/ci.yml`

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test ./...
```

- [ ] **Step 3: Write** `.github/workflows/release.yml`

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

- [ ] **Step 4: Validate the release config locally**

Run: `rtk brew list goreleaser || rtk brew install goreleaser` (ask the user before installing), then `rtk goreleaser check` and `rtk goreleaser release --snapshot --clean --skip=publish`.
Expected: `check` reports no errors. The snapshot build produces four archives in `dist/`, and `dist/asanamate_darwin_arm64*/asanamate version` prints a snapshot version. If `check` flags `homebrew_casks` fields for the installed GoReleaser version, fix them following its message and the GoReleaser docs. Do not switch to the deprecated `brews` key.

- [ ] **Step 5: Write** `README.md`

````markdown
# asanamate

A terminal UI for reading Asana tickets and running your own commands on them.
Browse My Tasks or any project, read a ticket with its fields, comments, and
attachments, then press a key to start a coding agent in the ticket's repo with
the ticket as context. Agents and scripts report back with `asanamate comment`,
`move`, and `field`.

asanamate runs in any terminal and works well in tmux panes, windows, and popups.
macOS and Linux only.

## Install

```sh
brew install --cask sadmachine/tap/asanamate
# or
go install github.com/sadmachine/asanamate/cmd/asanamate@latest
```

Prebuilt binaries are attached to each [GitHub release](https://github.com/sadmachine/asanamate/releases).

## Setup

1. Create a personal access token at <https://app.asana.com/0/my-apps> and export it:
   `export ASANA_ACCESS_TOKEN=...`
2. Run `asanamate setup`. It checks the token, asks for your workspace and the
   directory that holds your git repositories, and writes
   `~/.config/asanamate/config.toml` with every default explained.
3. Run `asanamate`.

## Keys

| Key | Action |
|---|---|
| `j`/`k`, arrows | move (list) or scroll (reader) |
| `g`/`G` | first/last ticket |
| `tab` | switch focus between the list and the reader |
| `/` | edit the filter (Enter or Esc to finish) |
| `p` | switch project (recent first) |
| `enter`, `a` | run an action on the selected ticket |
| `f` | attachments: view images inline or open in the browser |
| `o` | open the ticket in the browser |
| `r` | reload |
| `q` | quit |

## Filtering

Space-separated terms, all of which must match:

- words match the title
- `section:`, `project:`, `assignee:`, `tag:` match names
- `is:open`, `is:done`
- `-term` negates a term; `"double quotes"` group words

Example: `is:open section:"in progress" -tag:blocked`. Set the startup filter
with `default_filter`.

## List-only mode and scripting

- `asanamate --no-preview` shows only the ticket list at full width. It is handy
  in a small popup: pick a ticket, press `enter`, and run an action.
- `asanamate list` prints tickets for other tools. The default is My Tasks with
  `default_filter` applied.
  - `--project <gid>` lists a project instead. The gid is the number after
    `/project/` in the project's URL.
  - `--filter "<query>"` overrides the filter. `--filter ""` shows everything.
  - `--format tsv` (default) prints `gid, section, due, title, url` separated
    by tabs. `--format jsonl` prints one JSON object per task.
- `asanamate show <gid>` prints one ticket as Markdown. `--format json` prints
  JSON instead.

Pick a ticket with fzf:

```sh
asanamate list | fzf --delimiter '\t' --with-nth 2,4 --preview 'asanamate show {1}' | cut -f1
```

## Configuration

`~/.config/asanamate/config.toml` (or `$XDG_CONFIG_HOME/asanamate/config.toml`):

| Key | Default | Meaning |
|---|---|---|
| `workspace` | set by setup | Asana workspace gid |
| `theme` | `"dark"` | reading pane style: `dark` or `light` |
| `images` | `"auto"` | `auto`, `kitty` (force on), or `off` |
| `default_filter` | `"is:open"` | filter applied at startup |
| `confirm_writes` | `true` | write-back subcommands ask before writing |
| `repo_source.command` | lists repos in your setup directory | prints one repo path per line |

To pick repos from sesh or zoxide instead:

```toml
[repo_source]
command = "sesh list -z"
```

## Actions

```toml
[[actions]]
name = "Start Claude"   # shown in the menu
key = "c"               # one character
mode = "background"     # foreground | background | exit
repo = true             # resolve the ticket's repo and run inside it
command = '''tmux new-window -c "$ASANAMATE_REPO" -n "$ASANAMATE_SLUG" -e "ASANAMATE_TICKET_MD=$ASANAMATE_TICKET_MD" -e "ASANAMATE_GID=$ASANAMATE_GID" 'claude "$(cat "$ASANAMATE_TICKET_MD")"' '''
confirm_writes = false  # optional per-action override
```

Commands run with `/bin/sh -c`. Ticket data is available only through
environment variables and files. Always reference the variables, and never
paste ticket text into the command, because ticket content is untrusted.

| Variable | Value |
|---|---|
| `ASANAMATE_GID`, `ASANAMATE_TITLE`, `ASANAMATE_URL` | task gid, name, permalink |
| `ASANAMATE_SLUG` | branch-friendly title, e.g. `fix-login-bug` |
| `ASANAMATE_COMPLETED`, `ASANAMATE_ASSIGNEE`, `ASANAMATE_DUE`, `ASANAMATE_TAGS` | task details |
| `ASANAMATE_MY_SECTION` | My Tasks section |
| `ASANAMATE_PROJECT`, `ASANAMATE_PROJECT_GID`, `ASANAMATE_SECTION` | active project and the ticket's section in it |
| `ASANAMATE_REPO` | resolved repo (`repo = true` actions) |
| `ASANAMATE_TICKET_JSON`, `ASANAMATE_TICKET_MD` | full ticket as JSON / Markdown |
| `ASANAMATE_FIELD_<NAME>` | custom field display values, e.g. `ASANAMATE_FIELD_BRANCH_NAME` |
| `ASANAMATE_CONFIRM_WRITES` | `1`/`0`, read by the write-back subcommands |

Modes:

- `foreground`: suspends the TUI, runs in the same terminal, and resumes.
- `background`: runs detached. Output goes to `~/.local/state/asanamate/actions.log`.
- `exit`: quits asanamate, then runs the command. Best for popups.

**tmux note:** `tmux new-window` and `split-window` run their command in the
tmux server's environment, so `ASANAMATE_*` variables do not reach them. Pass
the ones you need with `-e NAME="$NAME"`, as in the example above.

The active project is the one picked for the repo when `repo = true`.
Otherwise it is the project you are viewing (if the ticket is in it), or the
ticket's only project.

Repo resolution: the first time a project's ticket runs a `repo = true` action,
you pick a repo from `repo_source.command` or type any path (Tab). asanamate
remembers it. Tickets in several projects always ask which project to use.

More examples:

```toml
[[actions]]
name = "Create branch and record it"
key = "b"
mode = "foreground"
repo = true
command = '''git switch -c "feature/$ASANAMATE_SLUG" && asanamate field "$ASANAMATE_GID" "Branch Name" "feature/$ASANAMATE_SLUG"'''

[[actions]]
name = "Jump to repo session"
key = "s"
mode = "exit"
repo = true
command = 'sesh connect "$ASANAMATE_REPO"'
```

## Writing back to Asana

```sh
asanamate comment [--yes] <gid> "Opened PR https://..."   # "-" reads stdin
asanamate move    [--yes] [--project <gid>] <gid> "In Review"
asanamate field   [--yes] <gid> "Branch Name" feature/fix-login
```

Each command asks on the terminal before writing unless `--yes` is given,
`ASANAMATE_CONFIRM_WRITES=0` is set, or `confirm_writes = false`. With no
terminal available (for example, a background action), a write that needs
confirmation is refused.

## tmux

```tmux
# Popup (pair with exit-mode actions)
bind-key A display-popup -E -w 90% -h 90% asanamate
# Quick-pick popup: list only
bind-key T display-popup -E -w 60% -h 60% "asanamate --no-preview"
# Pane (pair with background actions)
bind-key a split-window -h asanamate
# Needed for inline images inside tmux
set -g allow-passthrough on
```

## Files

- Config: `~/.config/asanamate/config.toml`
- State (repo links, recent projects): `~/.local/state/asanamate/state.toml`
- Ticket exports: `~/.local/state/asanamate/tickets/<gid>/`
- Background action log: `~/.local/state/asanamate/actions.log`

`$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` are honored.
````

- [ ] **Step 6: Final checks**

Run: `rtk go test ./... && rtk go vet ./... && rtk gofmt -l .`
Expected: PASS, and `gofmt -l` prints nothing.

- [ ] **Step 7: Commit** (ask the user first)

```bash
rtk git add .goreleaser.yaml .github README.md
rtk git commit -m "chore: release pipeline, CI, and README"
```

- [ ] **Step 8: Hand the release checklist to the user.** These steps are outward-facing. Do not perform them without explicit approval.
  1. Create the GitHub repos `sadmachine/asanamate` and `sadmachine/homebrew-tap` (public).
  2. Create a fine-grained token with Contents read/write on `homebrew-tap`. Add it as the `HOMEBREW_TAP_TOKEN` secret on `sadmachine/asanamate`.
  3. Push `main`, then tag and push the first release: `git tag v0.1.0 && git push origin main v0.1.0`.
  4. Verify: `brew install --cask sadmachine/tap/asanamate` and `go install github.com/sadmachine/asanamate/cmd/asanamate@v0.1.0`.
