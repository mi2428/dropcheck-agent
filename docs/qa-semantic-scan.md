# Semantic output and scan marker QA (2026-11-08)

## Contract and root cause

The earlier AP-to-AP fix did not separate the AP display footer/legend from
AP 1. Android represented headings and ordinary text identically, while its
native scan renderer generated a footer independently. Hand-inserted empty
Text parts covered individual examples but did not encode paragraph intent.

Text parts now carry an explicit section-boundary role, preserved through
sanitization and nesting. Logical/native output renders those boundaries as
one blank line without adding a leading blank. Plain text and command echoes
keep their original content; the implementation does not detect headings from
text patterns. Table rows and same-group fields remain adjacent. Native scan
footers and following AP sections obey the same paragraph rules. Existing Go
section/blank/table helpers are reused at missing ownership transitions.

| Surface inspected | Result |
| --- | --- |
| Scan summary/table/footer/legend/APs | Explicit boundaries, including the first AP; original brief columns retained. |
| Scan AP child information/IE/fields | Explicit section roles and sibling boundaries; direct AP link ID moved before child groups. |
| Full scan AP identity | Number and marker precede a self-identifying body; SSID/BSSID remain readable without scrolling back to the summary table. |
| Wi-Fi summary and connection detail | Child section boundaries, connection metadata and permissions separated; availability/raw values retained. |
| IP | Address families, routing/DNS and capability/link-property groups separated. |
| Multiple Networks and events | Numbered sibling headings use the common boundary; metadata stays with its owner. |
| MLO | Related-MLD `+` renamed to `~`; legends and sibling/affiliated/HE6GHz blocks separated. |
| Compound diagnostics | Summary/Wi-Fi/IP/Scan/Device/Network/interpretation boundaries retained through nesting. |
| Device capabilities | Tables retain compact rows; fields/errors have explicit boundaries. |
| Probes and command metadata | Header/body and native-reference boundaries explicit; single-result fields stay compact. |
| Permission and local version/CLI/OUI status | Permission heading separated; single-object local records remain compact. |
| Test reports and help | Existing table/paragraph boundaries retained; no generic text reformatter added. |
| Raw snapshots and literal input | Wrapper headings separated, but reference bodies and command echoes are not paragraph-normalized. |

## AP evidence and marker priority

Full scan headers use `[*] AP n`, `[+] AP n` or `[ ] AP n`, in that priority.
The star means a valid BSSID matches a recorded physical Wi-Fi association.
Plus means framework `802.11be` or non-truncated decoded EHT evidence; a
connected EHT AP keeps the star and its EHT evidence in the body. Empty,
truncated, MLD-only and wide-channel-only evidence does not create a plus.
Blank is not a negative capability verdict.

The shared protobuf adds `WifiScanConnections` to cached/fresh scan and scan
detail results. The collector records BSSIDs, Unix observation time,
completeness and errors after reading scan results, using existing per-Network
identity acquisition and excluding VPNs. There is no second rendering-time
query, default-Network substitution or SSID/MLD guess. Complete empty means
observed disconnected. Partial, missing and inconsistent observations are
explicit; only valid known partial BSSIDs can match. Interruption propagates
after callback cleanup. These are separate point-in-time observations, not an
atomic or continuously monitored connection guarantee.

## Checks

- Both languages run the same 19 marker cases from
  `shell/scan_mark_cases.tsv`: priority, case normalization, multiple connected
  identities, positive EHT sources, incomplete evidence and invalid MAC/time.
- Tests retain the recorded snapshot through filtering, safe JSON and scan
  detail without modifying the acquisition or dispatching another command.
- Paragraph checks cover section/body adjacency, exact sibling boundaries,
  address/routing transitions, nested/affiliated links, scan appendices,
  literal reference text and unchanged command echoes. Old root-only heading
  and single-identity-copy expectations were updated for the approved format,
  not removed without replacement.
- Complete Go package suite, vet, race checks for render/app/command/acquire,
  CI-pinned Staticcheck 0.7.0 with cached Go1.26.8, formatting, generated Shell
  contract, local-proxy public module consumption/CLI build and 50 real
  Readline cases passed. Pinned protobuf generation is byte-reproducible.
- JVM340/58 suites passed with no failures/errors/skips; focused native scan
  marker/spacing/brief assertions, Lint and both APK builds passed.

The final native matrix uses the same approved device and synthetic AP
evidence, not a fresh scan. A passive scan command additionally checks that
the real collector supplies the typed connection observation. No network
settings, permissions, IME or app-data reset is needed. Local evidence and
native screenshots are under `agent/build/qa/semantic-scan/`.

## Final native acceptance and limits

- **133/133 logical case/configuration combinations** passed across portrait,
  landscape, 320dp, 200dp, local RTL and 200% portrait/landscape.
- Six supplemental runner modes passed: presentation with and without IME,
  input, lifecycle, the existing nine-case passive/session runner and the
  separate passive scan-connection case. The latter also verifies a subsequent
  session command, its typed verbatim echo and clearing the session credential.
- Portrait, landscape, narrow, RTL and 200%-landscape native images were
  inspected. They show the AP display-to-first-AP gap, fixed-width marks,
  indented child sections and the original compact brief table.
- Installed product/test APK hashes, exact font/display/rotation/IME baselines,
  original app-data inode, empty ADB reverse rules and protected wrapper/index
  were verified. Owned screenshot caches were removed; the OUI database was
  retained without downloading an update.

The new passive command was initially rejected by the instrumentation's
allowlist, before execution. Only the exact `show wifi scan | display json`
command was added; fresh scan/probe commands remain forbidden. A combined
passive-scan-plus-session invocation reached its later session stage but hit
the 45-second host limit and was not accepted as a pass. The required checks
were split into bounded nine-case and scan-connection modes without relaxing
deadlines or outcome assertions. Command-echo checks now inspect command-role
blocks rather than materializing every result block's text; the scan mode
retains an explicit post-scan session/echo check. Stage timing is emitted to
make future slow runs diagnosable. The exact cause of the earlier combined
run's duration was not established; its failure logs are retained.

The passive dual-family test still explicitly reports missing IPv6 default
routes/DNS; no network-readiness pass is claimed. Not every possible device,
IME, network transition or output combination is certified. No commit or push
was made, and unrelated user changes remain intact.
