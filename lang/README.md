# SqlBackup language files

Every text shown by SqlBackup (command line, user interface, error messages, log files) comes from the
`<code>.lng` files in this folder. At runtime the program reads them from the `lang` folder next to
`SqlBackup.exe`. English (`en.lng`) is also embedded in the executable, so the program works in English
even if this folder is missing.

## Adding a language

1. Copy `en.lng` to `<code>.lng`, e.g. `de.lng` (lowercase letters, `-` or `_`, 2–10 characters).
2. Change the header: `@code = de` (must match the file name) and `@name = Deutsch` (shown in the language list).
3. Translate the text **after** the `=` sign. Do not change the keys on the left.
4. Keep every placeholder in curly braces (`{db}`, `{path}`, `{err}` …) exactly as written. You may move
   them within the sentence.
5. Save the file as UTF-8.
6. Put the file in the `lang` folder next to `SqlBackup.exe` and select the language in Settings, or
   use `SqlBackup.exe <command> --lang de` on the command line.

## Language selection

`--lang` on the command line → the language chosen in Settings → the Windows display language → English.
Until a language is chosen in Settings, the program follows the Windows display language: for `pt-BR` it
looks for `pt-br.lng` first, then `pt.lng`; if no file matches, English is used.

## File format

```
# comment
key = value
multi.line = first line\nsecond line
path.example = C:\\Backups
```

- One `key = value` per line. Spaces around `=` and at the ends of the value are ignored.
- Escapes: `\n` new line, `\t` tab, `\\` backslash.
- A key that is missing from a language falls back to English.

## For developers

- Source files live in the repository's `lang/` folder; `wails build` copies them to `build/bin/lang`
  (post-build hook in `wails.json`). `SQLBACKUP_LANG_DIR` overrides the folder (e.g. for `wails dev`).
- `go test ./internal/i18n/` checks that every key used in Go code or `frontend/src` exists in `en.lng`,
  that `en.lng` has no unused keys, and that every other language has exactly the same keys and
  placeholders as English.
- Use keys only as string literals (`i18n.T("cli.yes")`, `t('gui.menu.jobs')`), never build them
  dynamically, so the test can find them.
