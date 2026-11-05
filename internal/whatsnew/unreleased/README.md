# Unreleased highlights

Each change that earns a What's new highlight adds its own file here, named
for the change, such as `command-palette.md`, so pull requests never edit the
same lines. A file holds `### New` and/or `### Changed`, each followed by its
entries; see AGENTS.md "What's new":

```markdown
### New

- **Command palette.** Find and run any command without remembering its
  key. Try it: press `:` and type `due`.
```

Development builds show these files as the Unreleased section. At release,
`go run ./internal/whatsnew/release vX.Y.Z` moves them into
`../whats-new.md` under `## vX.Y.Z` and deletes them. This README stays.
