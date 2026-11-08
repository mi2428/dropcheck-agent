# Offline BSSID vendor lookup

Android Shell and Controller Shell resolve BSSIDs against the IEEE public MA-L
(24-bit), MA-M (28-bit), MA-S (36-bit) and historical IAB (36-bit) assignments.
The longest matching prefix wins. The displayed organization is the IEEE
assignee, not proof of a radio's actual manufacturer, retail brand or model.
Brief scans retain their original radio/security/age table columns without a
vendor column; use the full scan or connection views for vendor details.
Local/randomized, multicast, unspecified, invalid and unknown addresses are
explicitly unresolved; the local bit is never cleared to guess a vendor.
Compact cells use `? (local MAC)`, `? (group MAC)`, `? (unspecified)`,
`? (invalid MAC)` or `? (unregistered)` rather than inventing a vendor.
Historical prefixes with multiple listed organizations retain all names and
are explicitly marked as multiple IEEE assignees.

```text
show oui
request oui update
```

Both commands act on the machine running that Shell: Controller updates its
own host cache, while Android updates the app-private database. Selecting an
Android target in Controller Shell does not change that ownership. `show oui`
reports the active snapshot's fetch time, assignment count and source.
Controller stores updates beneath `os.UserCacheDir()/dropcheck/oui/`; Android
uses the app-private `files/oui/` directory. Both use `registry.tsv.gz`.

Lookup is entirely offline. Only the explicit update command accesses the
network, downloading the complete public lists over HTTPS. BSSIDs, SSIDs and
credentials are never sent to a vendor-lookup service. An update validates all
four lists before atomically replacing the local snapshot. Failed downloads,
invalid data, cancellation and write failures keep the previous snapshot;
corrupt existing snapshots produce a visible database error rather than
silently reverting to the bundled data. No automatic update, network fallback,
extra dependency or remote per-address query is used.

The shared compressed snapshot is
`controller/internal/oui/data/registry.tsv.gz`, also packaged as an Android
Java resource. It contains 58,971 unique prefixes fetched on 2026-11-08.
Its UTF-8 payload starts with `# dropcheck-oui-v1<TAB><RFC3339 fetch time>`;
subsequent lines are `<uppercase hexadecimal prefix><TAB><organization>`.
The data is factual public IEEE registration data; it is not covered by a
claim that IEEE's materials were authored by this project's contributors.
To refresh the checked-in snapshot, run the update command and copy its
normalized gzip file to the shared path, then run both languages' parity tests.

Sources checked on 2026-11-08:

- [IEEE Registration Authority public listings](https://standards.ieee.org/products-programs/regauth/)
- `https://standards-oui.ieee.org/oui/oui.txt`
- `https://standards-oui.ieee.org/oui28/mam.txt`
- `https://standards-oui.ieee.org/oui36/oui36.txt`
- `https://standards-oui.ieee.org/iab/iab.txt`

Offline parity and failure tests use `shell/oui_lookup_cases.tsv` in both
languages. The opt-in live check downloads public lists into a disposable test
directory without changing the user's active database:

```sh
cd controller
DROPCHECK_OUI_LIVE=1 go test -count=1 -run TestOfficialIEEEUpdate -v ./internal/oui
```
