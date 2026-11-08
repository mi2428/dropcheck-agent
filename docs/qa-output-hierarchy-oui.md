# Output hierarchy and OUI QA (2026-11-08)

The subsequent approved semantic-spacing/AP-marker redesign is recorded in
[semantic output and scan marker QA](qa-semantic-scan.md). Counts below record
their respective earlier revisions, not that redesign's final acceptance.

## Scope and implementation

The reported defect was real: AP-owned Security/Fields sections were written
at the same heading depth as their AP. Both renderers flattened child output
into their parent's destination without carrying its ownership depth.

The fix scopes indentation at the caller. Android Field/Table/Scan parts carry
display indentation separately from their labels and values, including native
tab stops and width budgets. PC child builders preserve owned field markers,
and nested table selection reserves the parent's indentation columns. A later
sibling property does not accidentally become a child of the last heading.

| Surface inspected | Correction or retained boundary |
| --- | --- |
| Scan APs and scan-detail results | AP siblings remain separate; Security, capabilities, HE/EHT, IE and Fields stay under the owning AP. |
| Connected Wi-Fi detail | Capability/security sections own their rows; later connection properties retain their own depth. |
| MLO links | Per-link evidence remains beneath its owning section/AP rather than sharing the outer AP heading depth. |
| Networks and IP | Networks have separate numbered headings; address-family children retain availability and complete raw values. |
| Monitor events | Each event owns its time/type/metadata/status; inner builders cannot append those fields to the outer list. |
| Compound diagnostics | Summary/Wi-Fi/IP/Scan/Device/Network/interpretation sections own their content, including tables. |
| Permission/probe/assertion/cycle tables | Existing record/table roles retained; no acquisition or evaluation behavior changed. |
| Filtering, safe JSON and full values | Literal identity, typed field roles, raw availability-qualified values and table records remain available. |

Android command chips now insert `sh wi st`, `sh wi st ip` and `req wi scan br`.
They reuse unique-prefix parsing. Single taps still insert; completed double
taps still execute once. `br` selects the brief view, not a different scan.

BSSID vendor fields and full-scan VENDOR cells use the same bundled IEEE
database in both languages. Connected/link identity availability still gates
derived attribution; unavailable identities do not acquire a hidden vendor in
safe values or JSON. See [OUI behavior and update commands](oui-vendors.md).

## Adversarial checks

- AP ordering, child headings/fields, later siblings after Security/IE,
  multiple Networks/events and nested tables are checked independently.
- IP availability displays retain complete multi-address raw values; display
  indentation does not replace their typed labels or full values.
- Exact-width and one-column-over-budget nested tables select the correct
  table/record layout. Existing grapheme, alignment and narrow-width checks
  remain active.
- Shared MAC fixtures cover longest-prefix 24/28/36-bit lookup, supported MAC
  spellings, local/group/unspecified/invalid/unregistered addresses and safe
  JSON/filter access. Updates cover malformed data, duplicate snapshot keys,
  ambiguous historical IEEE registrations, failed downloads, cancellation,
  atomic replacement, corrupt-cache errors and explicit repair.
- A real Android run exposed an unsupported JVM-only regex flag. The shared
  parser now uses supported Unicode separator classes, with JVM and native
  checks for NBSP/NEL normalization. No catch-and-continue or alternate
  database/source was added.
- Brief scans retain the original SSID/BSSID/radio/security/AGE(s) columns,
  without a vendor column or repeated AP-detail blocks. Full scans retain
  vendor details and exactly one blank line between AP blocks.

## Verified checks

- Fresh affected Go packages and the complete serial package suite passed;
  `go vet`, formatting and CI-pinned Staticcheck 0.7.0 with cached Go 1.26.8
  passed. Race checks passed for OUI, render and app packages.
- The versioned local-proxy consumer passed, including building the Controller
  with its embedded database. The fixture includes that non-Go asset. The
  existing public-API dependency-isolation check and 50 real-Readline cases
  also passed without device acquisition.
- JVM **337 tests / 57 suites**, zero failures/errors/skips, zero Lint issues
  and both APK builds passed.
- The opt-in Go updater and an explicit Android updater run downloaded and
  validated the official MA-L/MA-M/MA-S/IAB lists. Focused native geometry,
  hierarchy/vendor, insertion and chip-double-tap checks passed **4/4**.
- Final native coverage passed **133/133 logical case/configuration
  combinations**: all 29 cases in normal portrait/landscape and 15 affected
  cases in 320dp, 200dp, local RTL and 200% portrait/landscape. Five supplemental
  presentation/input/lifecycle/passive runners passed. Native screenshots
  were inspected in portrait, landscape, narrow and largest-text conditions.
- The live Android update's **58,971 assignments exactly match** the shared
  bundled snapshot. Installed APK hashes, exact display/font/rotation/IME
  baselines, original app-data inode, empty ADB reverse rules and protected
  wrapper/index were verified. Owned screenshot caches were removed; the
  app-private feature database was deliberately retained.

The matrix used the same product APK throughout. A test-only replay word
search initially covered too little vertical space at 200% landscape text;
its final 15%-49% upper-half band also avoids top-edge interception. All seven
affected replay groups were rerun after this fixture correction. Temporary
touch tracing was removed. The passive runner's old root-only `Network:`
matcher also needed to accept the newly explicit child indentation; exact ID,
stale-ID, failure, pipeline and recreation assertions were retained and passed.
Its dual-family observation still explicitly reported missing IPv6 default
routes/DNS, not a network-readiness pass.

Evidence is under `agent/build/qa/vendor-hierarchy/`. The native matrix uses
synthetic AP evidence and passive/local commands. The fresh-scan chip is
inserted/parsed and tested while busy; QA never starts a real active scan.
Only the separate opt-in OUI check downloads public registries. No Wi-Fi
configuration, permissions, IME, app data reset or device replacement is used.

Two package-parallel runs hit the unchanged 500ms fake-process deadline fixture
in adbdiag. Its isolated run and the complete serial suite passed without
changing deadlines or assertions. This remains a host-fixture scheduling
limit, not evidence of a device or OUI failure.

## Brief table and AP spacing follow-up

Restored the original brief-table columns in both languages; vendor details
remain in the full view. Android now explicitly separates adjacent AP blocks
with one blank line, matching the existing Controller boundary. Regression
checks assert the exact columns, native table use when it fits, and exactly
one blank line before AP 2. Existing narrow-width layout behavior is retained.

The complete Go suite, affected-package vet, formatting, generated Shell
contract, JVM337/57 suites, Lint0 and both APK builds passed again. Native
checks passed in all seven display conditions, followed by 18 passive
command-tail submissions and both IME states of the presentation runner.
Portrait screenshots confirm the restored table and AP separator. Follow-up
evidence is under `agent/build/qa/scan-table-spacing/`; no fresh scan, OUI
download or network mutation was needed.

This is not certification of other IMEs/OEMs, every possible input sequence,
or hardware manufacturer identity. IEEE assignees are not device-model proof;
randomized/local addresses remain unresolved. User-owned wrapper/README
changes and the preceding uncommitted replay work are retained.
