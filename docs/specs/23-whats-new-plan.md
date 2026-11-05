# What's New Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the first TUI launch after an upgrade, show a curated "What's new" modal that the user can turn off, backed by an embedded Markdown file that also feeds the GitHub release notes.

**Architecture:** A new `internal/whatsnew` package embeds `whats-new.md`, splits it into per-version sections, and answers "which sections are new since version X". `startTUI` passes the parsed sections to the TUI through `Deps`. The TUI decides at startup whether to open a scrollable modal (modeled on `extensionDetail`) and records `last_seen_version` in state. `whats_new.on_startup` is a settings key, so the modal and the comma menu can turn it off.

**Tech Stack:** Go, `embed`, Bubble Tea v2, `charm.land/bubbles` viewport, the TUI's existing glamour-based `markdownRenderer`, BurntSushi TOML.

**Spec:** `docs/specs/23-whats-new.md`

## Global Constraints

- Setting key: `whats_new.on_startup`, bool, default `true`, in `settings.toml` and `config.SettingKeys`.
- State key: `last_seen_version` in `state.toml`, written only by the app.
- Version rule: only stable `vX.Y.Z` builds take part (the `updatecheck` rule); `dev`, prerelease, and pseudo-versions never open the modal or write state.
- `## Unreleased` never shows in a stable build.
- The modal opens only from the main TUI (`Deps.Ticket == ""`), never from `asanamate read`.
- Content file: `internal/whatsnew/whats-new.md`; headings `## Unreleased` or `## vX.Y.Z`, newest first; subsections `### New` and optional `### Changed`; absolute links.
- Commit subjects follow Conventional Commits; each commit carries its own tests and docs (AGENTS.md).

## Review Focus

- **Hand-edited or garbage `last_seen_version`** (such as `"latest"`): treat it as missing, so the user sees the running version's entries and the value is overwritten. Tested in Task 1 (`TestNewerAndSince`).
- **State save fails** (unwritable state directory): the modal still closes, an error status appears, and nothing panics; the modal may reappear next launch. Tested in Task 3 (`TestWhatsNewSaveErrorStillCloses`).
- **Tiny pane** (tmux sidebar, 20×6): the modal renders without panicking and keeps the footer visible. Tested in Task 3 (`TestWhatsNewTinyPane`).
- **`go install` pseudo-versions** (`v0.5.1-0.20261105120000-abcdef123456`): treated as not stable, so they skip everything. Tested in Task 1 (`TestNewerAndSince`).
- **Malformed content file** (out-of-order versions, a typo in a heading, two `Unreleased` sections): caught in CI, not by users. Tested in Task 1 (`TestEmbeddedContentIsWellFormed`).

---

### Task 1: `whatsnew` package and version helpers

**Files:**
- Modify: `internal/updatecheck/updatecheck.go:93-115` (export the helpers; update callers in the same file)
- Create: `internal/whatsnew/whatsnew.go`
- Create: `internal/whatsnew/whats-new.md`
- Test: `internal/whatsnew/whatsnew_test.go`

**Interfaces:**
- Produces:
  - `updatecheck.ParseStable(raw string) ([3]uint64, bool)` (renamed from `stableVersion`)
  - `updatecheck.Compare(a, b [3]uint64) int` (renamed from `compare`)
  - `type whatsnew.Section struct { Version string; Body string }`, where `Version` is `"Unreleased"` or `"vX.Y.Z"` and `Body` is the trimmed Markdown under the heading
  - `whatsnew.Parse(src string) []Section`: file order; drops sections whose body is empty
  - `whatsnew.Embedded() []Section`: `Parse` of the embedded file
  - `whatsnew.Newer(current, lastSeen string) bool`: current is stable, and lastSeen is not stable or is older
  - `whatsnew.Since(all []Section, current, lastSeen string) []Section`: stable sections in `(lastSeen, current]`; when lastSeen is not stable, only the section equal to current; nil when current is not stable
  - `whatsnew.All(all []Section, current string) []Section`: stable sections `<= current`; every section, including `Unreleased`, when current is not stable (lets dev builds preview)
  - `whatsnew.Markdown(sections []Section) string`: `"## " + Version + "\n\n" + Body`, sections joined by a blank line

- [ ] **Step 1: Write the failing tests**

```go
func TestParse(t *testing.T) {
	got := Parse("# What's new\n\n## Unreleased\n\n### New\n\n- **A.** a\n\n## v0.5.0\n\n### New\n\n- **B.** b\n\n## v0.4.0\n\n")
	want := []Section{{"Unreleased", "### New\n\n- **A.** a"}, {"v0.5.0", "### New\n\n- **B.** b"}}
	// v0.4.0 has an empty body and is dropped.
	if !reflect.DeepEqual(got, want) { t.Fatalf("got %#v", got) }
}

func TestNewerAndSince(t *testing.T) {
	all := []Section{{"Unreleased", "u"}, {"v0.5.0", "c"}, {"v0.4.1", "b"}, {"v0.4.0", "a"}}
	for _, tc := range []struct {
		current, lastSeen string
		newer             bool
		since             []string // versions
	}{
		{"v0.5.0", "v0.4.0", true, []string{"v0.5.0", "v0.4.1"}},
		{"v0.5.0", "", true, []string{"v0.5.0"}},
		{"v0.5.0", "latest", true, []string{"v0.5.0"}},
		{"v0.5.0", "v0.5.0", false, nil},
		{"v0.4.0", "v0.5.0", false, nil},
		{"dev", "v0.4.0", false, nil},
		{"v0.5.1-0.20261105120000-abcdef123456", "v0.4.0", false, nil},
		{"v0.4.2", "v0.4.1", true, nil}, // newer, but no entries
	} { /* assert Newer(...) == tc.newer and versions of Since(all, ...) == tc.since */ }
}

func TestAll(t *testing.T) {
	// All(all, "v0.4.1") versions == [v0.4.1 v0.4.0]
	// All(all, "dev") versions == [Unreleased v0.5.0 v0.4.1 v0.4.0]
}

func TestMarkdown(t *testing.T) {
	// Markdown([]Section{{"v0.5.0","c"},{"v0.4.1","b"}}) == "## v0.5.0\n\nc\n\n## v0.4.1\n\nb"
}

func TestEmbeddedContentIsWellFormed(t *testing.T) {
	// Every "## " heading in the raw embedded file is "Unreleased" or ParseStable-valid;
	// "Unreleased" appears at most once and only first; versions strictly descend.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/whatsnew/`
Expected: FAIL (the package does not compile: `undefined: Parse`).

- [ ] **Step 3: Export `ParseStable` and `Compare` in `internal/updatecheck/updatecheck.go`, then implement the `whatsnew` functions above in `internal/whatsnew/whatsnew.go`**

Embed with `//go:embed whats-new.md`. `Parse` splits on lines that start with `"## "`; text before the first such heading (a title or intro) is ignored. Create `whats-new.md` with a one-line intro and an empty `## Unreleased` heading; Task 5 fills it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/whatsnew/ ./internal/updatecheck/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/updatecheck/updatecheck.go internal/whatsnew/
git commit -m "feat(whatsnew): parse curated release highlights by version"
```

### Task 2: `whats_new.on_startup` setting and `last_seen_version` state

**Files:**
- Modify: `internal/config/config.go` (`Config` struct near line 85; `Default()` near line 395)
- Modify: `internal/config/settings.go:17-21` (`SettingKeys`)
- Modify: `internal/setup/setup.go` (config template: one comment line)
- Modify: `internal/state/state.go` (`State` struct; `Load` at line 127)
- Modify: `README.md` (settings table near line 804)
- Test: `internal/config/settings_test.go`, `internal/state/state_test.go`

**Interfaces:**
- Produces:
  - `config.Config.WhatsNew WhatsNew` with tag `toml:"whats_new"`; `type WhatsNew struct { OnStartup bool \`toml:"on_startup"\` }`; doc comment: "WhatsNew controls the what's-new modal shown after an upgrade. OnStartup opens it on the first launch of a newer release."
  - `Default().WhatsNew.OnStartup == true`
  - `"whats_new.on_startup"` appended to `config.SettingKeys`
  - `state.State.LastSeenVersion string` with tag `toml:"last_seen_version,omitempty"`
  - `func (s *State) Fresh() bool`: true when `Load` found no file

- [ ] **Step 1: Write the failing tests**

In `TestSettingsLoadAndValidation`, add the case `{"whats_new.on_startup = 1\n", "whats_new.on_startup"}`. Add:

```go
func TestSaveSettingWhatsNew(t *testing.T) {
	path := writeSettingsFixture(t, "workspace = \"1\"\n", string(DefaultSettings()))
	if err := SaveSetting(path, "whats_new.on_startup", false); err != nil { t.Fatal(err) }
	cfg, err := Load(path)
	if err != nil || cfg.WhatsNew.OnStartup { t.Fatalf("on_startup = %v, err = %v", cfg.WhatsNew.OnStartup, err) }
}
```

In `state_test.go`:

```go
func TestFreshAndLastSeenVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, _ := Load(path)
	if !s.Fresh() { t.Fatal("missing file is not fresh") }
	s.LastSeenVersion = "v0.5.0"
	if err := s.Save(); err != nil { t.Fatal(err) }
	again, _ := Load(path)
	if again.Fresh() || again.LastSeenVersion != "v0.5.0" { t.Fatalf("fresh = %v, last = %q", again.Fresh(), again.LastSeenVersion) }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ ./internal/state/`
Expected: FAIL (`cfg.WhatsNew undefined`, `s.Fresh undefined`).

- [ ] **Step 3: Implement the fields, the default, the `SettingKeys` entry, and `Fresh`**

`validate()` needs no check; TOML decoding already rejects a non-boolean and names the key. In the `config.toml` template in `internal/setup/setup.go`, add `# What's new on startup lives in settings.toml (whats_new.on_startup).` next to the other "lives in settings.toml" comments. Add this README settings table row after `layout.focus.dim`:
`| \`whats_new.on_startup\` | \`true\` | open What's new on the first launch after an upgrade; the modal's \`d\` or \`,\`, then \`u\` toggles |`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/state/ ./internal/setup/`
Expected: PASS, including `TestMergeExtractsEverySetting`, which picks up the new key through `DefaultSettings()`.

- [ ] **Step 5: Commit**

```bash
git add internal/config internal/setup/setup.go internal/state README.md
git commit -m "feat(config): add whats_new.on_startup and last seen version state"
```

### Task 3: TUI modal, startup decision, and menu rows

**Files:**
- Create: `internal/tui/whatsnew.go`
- Modify: `internal/tui/model.go` (`Deps` struct near line 64; modal field near line 149; key routing near line 727; overlay rendering near line 1268; `New` near line 238, after `ticketOnly` is known)
- Modify: `internal/tui/mouse.go:360` (wheel scroll), `internal/tui/notifications.go:170` and `internal/tui/hooks.go:84` (the "a modal is open" checks)
- Modify: `internal/tui/settings.go` (`openTools` items)
- Modify: `cmd/asanamate/main.go` (`startTUI`: `d.WhatsNew = whatsnew.Embedded()`)
- Test: `internal/tui/whatsnew_test.go`

**Interfaces:**
- Consumes: everything Task 1 and Task 2 produce.
- Produces:
  - `tui.Deps.WhatsNew []whatsnew.Section` (nil in tests unless set)
  - `type whatsNewView struct { markdown string; version string; reader viewport.Model }`, held in `Model.whatsNew *whatsNewView`
  - `func (m *Model) startupWhatsNew()`, called from `New` when `!m.ticketOnly()`
  - `func (m *Model) openWhatsNew(sections []whatsnew.Section)`
  - `func (m *Model) updateWhatsNew(msg tea.KeyPressMsg) tea.Cmd`
  - `func (m *Model) whatsNewView(width, height int) string`
  - `func (m *Model) closeWhatsNew()`: clears the modal; records the version when the running version is stable and newer

Startup decision in `startupWhatsNew`:

```go
v, st := m.deps.Version, m.deps.State
if !whatsnew.Newer(v, st.LastSeenVersion) { return }
var sections []whatsnew.Section
if !st.Fresh() { sections = whatsnew.Since(m.deps.WhatsNew, v, st.LastSeenVersion) }
if m.deps.Config.WhatsNew.OnStartup && len(sections) > 0 { m.openWhatsNew(sections); return }
m.recordWhatsNewSeen() // sets LastSeenVersion = v and saves; a save error becomes an error status
```

Modal behavior: the header reads `What's new in asanamate`. The body is `whatsnew.Markdown(sections)` rendered by `newMarkdownRenderer(m.palette, width, false, m.sym.codeWrap)` in a viewport. The footer lists scroll keys, `enter`/`esc` "got it", and `d` "don't show on startup". When the version is stable, a second footer line reads `Full notes: https://github.com/sadmachine/asanamate/releases/tag/<version>`. `d` is matched on the raw key before `bound("picker", ...)`; it sets `m.deps.Config.WhatsNew.OnStartup = false`, calls `m.saveSetting("whats_new.on_startup", false)`, then `closeWhatsNew`. Routing, rendering, mouse, and the blocking checks mirror `m.extension` at each listed line; use the `overlayExtension` sizing.

`openTools` gains two rows: `{Label: "What's new", Key: "e"}` opens `whatsnew.All(m.deps.WhatsNew, m.deps.Version)`; `{Label: "Show what's new on startup", Hint: onOff(m.deps.Config.WhatsNew.OnStartup), Key: "u"}` flips the value, saves it with `saveSetting`, and reopens `openTools`.

- [ ] **Step 1: Write the failing tests**

Use a helper `whatsNewModel(t, version, stateBody string, cfg config.Config) (*Model, *state.State)`. It writes `stateBody` to a temp `state.toml` when the body is non-empty (so `""` means a fresh install), loads it, and builds the model the way `testModel` does, with `Version: version` and `WhatsNew: fixture`. The fixture is `[{v0.5.0, "### New\n\n- **Five.** five"}, {v0.4.1, "### New\n\n- **Four-one.** four-one"}, {v0.4.0, "### New\n\n- **Four.** four"}]`.

```go
func TestWhatsNewOpensOnceAfterUpgrade(t *testing.T)      // state "last_seen_version = \"v0.4.0\"", v0.5.0: m.whatsNew != nil; view contains "Five" and "Four-one", not "Four." ; esc closes; reloaded state LastSeenVersion == "v0.5.0"; a second model from that state has whatsNew == nil
func TestWhatsNewExistingUserFirstRelease(t *testing.T)   // state "repos = {}" (exists, no last seen), v0.5.0: view has "Five" only
func TestWhatsNewFreshInstallRecordsSilently(t *testing.T) // state "", v0.5.0: whatsNew == nil; reloaded LastSeenVersion == "v0.5.0"
func TestWhatsNewSkipsDevAndDowngrade(t *testing.T)       // versions "", "dev", and v0.4.0 over last seen v0.5.0: whatsNew == nil, LastSeenVersion unchanged
func TestWhatsNewSettingOffRecordsSilently(t *testing.T)  // OnStartup false, upgrade: nil modal, version recorded
func TestWhatsNewNoEntriesRecordsSilently(t *testing.T)   // v0.4.2 over v0.4.1 (no v0.4.2 section): nil modal, version recorded
func TestWhatsNewDontShowTurnsOffSetting(t *testing.T)    // press "d": modal closed, m.deps.Config.WhatsNew.OnStartup false, reloadSettings(t, m).WhatsNew.OnStartup false, version recorded
func TestWhatsNewReadModeSkips(t *testing.T)              // Deps.Ticket = "1", upgrade: nil modal, LastSeenVersion unchanged
func TestWhatsNewToolsMenu(t *testing.T)                  // ",", "w": modal shows all three versions; esc; ",", "u": OnStartup false and saved
func TestWhatsNewSaveErrorStillCloses(t *testing.T)       // state path under a regular file: esc closes; m.status is an error mentioning the state path
func TestWhatsNewTinyPane(t *testing.T)                   // window 20x6: View() does not panic, contains "got it"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run WhatsNew`
Expected: FAIL (`Deps.WhatsNew undefined`).

- [ ] **Step 3: Implement `internal/tui/whatsnew.go`, then wire it into the files listed above**

- [ ] **Step 4: Run the full TUI suite**

Run: `go test ./internal/tui/ ./cmd/...`
Expected: PASS. Existing tests never open the modal, because `testModel` uses a fresh state and an empty version.

- [ ] **Step 5: Commit**

```bash
git add internal/tui cmd/asanamate/main.go
git commit -m "feat(tui): show what's new after an upgrade"
```

### Task 4: `asanamate whats-new` command and README

**Files:**
- Modify: `cmd/asanamate/main.go` (the `usage` string near line 95; a `run` switch case)
- Modify: `README.md` (Upgrading section, line 709)
- Test: `cmd/asanamate/main_test.go` (or the file holding existing `run` tests)

**Interfaces:**
- Consumes: `whatsnew.Embedded`, `whatsnew.All`, `whatsnew.Markdown`, `currentVersion()`.
- Produces: `func runWhatsNew(args []string, out io.Writer, sections []whatsnew.Section, version string) error`

- [ ] **Step 1: Write the failing test**

```go
func TestRunWhatsNew(t *testing.T) {
	var b bytes.Buffer
	sections := []whatsnew.Section{{"v0.5.0", "five"}, {"v0.4.0", "four"}}
	if err := runWhatsNew(nil, &b, sections, "v0.4.0"); err != nil { t.Fatal(err) }
	if got := b.String(); got != "## v0.4.0\n\nfour\n" { t.Fatalf("got %q", got) }
	if err := runWhatsNew([]string{"x"}, &b, sections, "v0.4.0"); err == nil { t.Fatal("extra argument accepted") }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/asanamate/ -run WhatsNew`
Expected: FAIL (`undefined: runWhatsNew`).

- [ ] **Step 3: Implement `runWhatsNew` and wire it up**

Add `case "whats-new": err = runWhatsNew(args[1:], os.Stdout, whatsnew.Embedded(), currentVersion())` and the usage line `asanamate whats-new  print release highlights up to this version`. When no section applies, print `No highlights for <version>.` In README "Upgrading", add a paragraph covering the modal: when it opens, `d` or `whats_new.on_startup`, the `,` then `e` row, `asanamate whats-new`, and that nothing is fetched over the network.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/asanamate/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/asanamate README.md
git commit -m "feat(cli): add whats-new command"
```

### Task 5: Release notes, maintenance rules, and first content

**Files:**
- Modify: `.github/workflows/release.yml` (the "Upgrade notes" step)
- Modify: `AGENTS.md` (new "What's new" section; "Version bumps" release step 1)
- Modify: `internal/whatsnew/whats-new.md` (`## Unreleased` entries)

- [ ] **Step 1: Extend the release step**

Rename the step "Release notes header". Run the same `awk` against `internal/whatsnew/whats-new.md` into `whatsnew.md`. Build the header from whichever sections are non-empty: `## What's new` plus the what's-new section, then `## Upgrading` plus the migrations section. Set `flags` only when the header is non-empty.

- [ ] **Step 2: Verify the header locally**

Run the step's shell body with `GITHUB_REF_NAME=v9.9.9` and `RUNNER_TEMP=$(mktemp -d)` against temporary copies of both files containing a `## v9.9.9` section, then `cat "$RUNNER_TEMP/header.md"`.
Expected: `## What's new` and its entries, followed by `## Upgrading` and its steps. With neither section present, there is no `header.md` and no `flags` output.

- [ ] **Step 3: Add the AGENTS.md rules**

Add a "## What's new" section with the spec's maintenance rules, verbatim in substance:
- Every `feat` and every `fix` commit gets a what's-new decision; an entry goes under `## Unreleased` in `internal/whatsnew/whats-new.md` in the same commit.
- **New:** something users could not do before. **Changed:** something they rely on that now behaves or looks different; this can come from a `feat`, a `fix`, or a breaking change. **No entry:** small polish nobody would look for, fixes that restore intended behavior, and internal work.
- Entry format: a bold title, one or two sentences, and how to try it; absolute links.

In "Version bumps", step 1 also renames `## Unreleased` in `whats-new.md` (deleting the heading when empty), and the release commit becomes `docs(release): release vX.Y.Z`. Add a step before tagging: compare `## Unreleased` with every `feat` and `fix` commit in `git log --oneline <tag>..main`.

- [ ] **Step 4: Write the first entries**

Apply the rule to every `feat` and `fix` in `git log --oneline v0.4.3..HEAD`: at least "Fold group sections" (New) and "What's new" itself (New). Run `go test ./internal/whatsnew/`; `TestEmbeddedContentIsWellFormed` must pass.

- [ ] **Step 5: Commit (two commits)**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): put what's new at the top of release notes"
git add AGENTS.md internal/whatsnew/whats-new.md
git commit -m "docs(agents): require a what's-new decision for feat and fix commits"
```

### Final check

- [ ] Run `go vet ./... && go test ./...`. Expected: PASS.
- [ ] Run `go run ./cmd/asanamate doctor`. Expected: the config and the settings with `whats_new.on_startup` load clean.
- [ ] Run `go run ./cmd/asanamate whats-new`. Expected: the dev build prints `## Unreleased` and its entries.
- [ ] Manual: build with `-ldflags "-X main.version=v9.9.9"`, set `last_seen_version = "v0.4.3"` in a copy of `state.toml` (with `XDG_STATE_HOME` pointed at a temp directory), launch, and see the modal; `d` turns it off.
