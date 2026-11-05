# Request command naming follow-up

Date: 2026-11-05. Scope: PC/Android interactive Shell naming, help/completion, fixtures and documentation. No device operation, installation, network probe, commit or push is part of this change.

## Contract and implementation

- Replace the five interactive paths with `request pmtu`, `request ip external`, `request dns resolve`, `request http check` and `request http download`. Do not retain executable aliases for the replaced paths.
- Keep Wi-Fi operations and technical option keys unchanged. Parent-only IP/DNS/HTTP input is local help, not an acquisition. The shared generated grammar still drives both Shell inventories.
- Reuse exactly the existing operations/RPCs, defaults, validation, physical Wi-Fi Network selectors, protocol data and renderers. The external-IP command is a service-observed source-address query, not an address allocation or local interface inspection. The HTTP download still discards content after measuring it; it is not a file-save command.
- Preserve independent one-shot command spellings and all wire identifiers. No compatibility layer, new dependency, collection fallback or new execution path is introduced.

## Checks

- Before the rename, the new Go regression failed for all five new paths and confirmed that all five obsolete paths were still executable. The unchanged regression now compares each new request's complete RunCommand against its independently supported CLI operation, including explicit values and SSID/BSSID/Network ID selectors.
- Go and Android tests cover group-only help, child/argument completion, unique prefixes, keyword-looking host/URL values and secret-safe contextual help. Shared invalid fixtures reject the obsolete spellings and missing leaf arguments.
- Existing endpoint/port/enum/timeout/empty-value regressions were migrated, including dynamically assembled command paths, rather than left to pass merely because an obsolete path is rejected. The E2E matrix migrates only Shell/parser rows; one-shot rows remain unchanged.
- A device-free tagged regression preserves the PMTU E2E timeout for both `request pmtu` and independent `path-mtu`. This does not execute the live matrix. Native invalid-input instrumentation and real-readline probe recall use the new spellings.

## Verification and limits

Final verification passed:

- All Go packages and **50 real-readline PTY cases** through `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make test TARGET=controller`.
- Race tests for command/shell/app/linuxcli/render/pipeline; vet, staticcheck 2026.1, modernize, generated-contract drift, formatting and whitespace checks; Controller build.
- The device-free tagged `TestPMTUCommandNamesKeepE2ETimeout` regression, with live E2E disabled. The built one-shot CLI's local help still exposes its independent spellings.
- Android **266 unit tests / 48 suites, zero failures/errors/skips**; lint with no issues; application and instrumentation APK builds. Instrumentation was built, not installed or executed.
- Inventory audit: only the five agreed paths/descriptions changed; all option/value/effect/enum contracts remain identical. E2E audit: **49 Shell/parser command rows migrated**, all **46 independent CLI rows unchanged**, case IDs/count stable. Documentation links/fences/whitespace and PTY script syntax checks passed.

No live E2E, device UI, radio or external-service behavior is certified. Existing request effects and typed evaluators are intentionally unchanged, including the separately noted reconnect-outcome concern. This follow-up does not implement an unrelated reconnect fix or alter raw/debug separation.
