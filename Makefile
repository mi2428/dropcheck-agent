SHELL                   := /bin/bash
.SHELLFLAGS             := -eu -o pipefail -c
.DEFAULT_GOAL           := help

TARGET                  ?= all
GO                      ?= go
GRADLE                  ?= ./gradlew
ADB                     ?= adb
SERIAL                  ?= $(ADB_SERIAL)
STATICCHECK             ?= staticcheck
TAGS                    ?=
ADB_INSTALL_FLAGS       ?= -r -t
E2E_PACKAGE             ?= ./integration/e2e
E2E_TIMEOUT             ?= 3h
E2E_AGENT_PACKAGE       ?= io.dropcheck.agent
INTEGRATION_PACKAGE     ?= ./integration/ingester
INTEGRATION_TIMEOUT     ?= 10m
GIT_DESCRIBE            := $(shell git describe --tags --dirty --always 2>/dev/null)
VERSION                 ?= $(or $(GIT_DESCRIBE),0.0.0-dev)
GO_LDFLAGS              ?= -X dropcheck/controller/internal/version.Version=$(VERSION)

SSID                    ?= $(DROPCHECK_E2E_WIFI_SSID)
PSK                     ?= $(DROPCHECK_E2E_WIFI_PSK)
PSK_ENV                 ?= DROPCHECK_E2E_WIFI_PSK

AGENT_BUILD_TASK        ?= :agent:assembleDebug
AGENT_TEST_TASK         ?= :agent:testDebugUnitTest
AGENT_LINT_TASK         ?= :agent:lintDebug
APK                     ?= agent/build/outputs/apk/debug/agent-debug.apk
CONTROLLER_BIN          ?= dist/dropcheck
CONTROLLER_INGESTER_BIN ?= dist/dropcheck-ingester

HELP_NAME_WIDTH    := 25
HELP_EXAMPLE_WIDTH := 41

##@ Development

.PHONY: build
build: ## Build targets; use TARGET=agent,controller or TARGET=all
	@die(){ printf 'make build: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent controller"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) run "$(GRADLE)" "$(AGENT_BUILD_TASK)" "-PdropcheckVersion=$(VERSION)" ;; \
			controller) (cd controller; bin="$(CONTROLLER_BIN)"; [[ "$$bin" != */* ]] || run mkdir -p "$${bin%/*}"; run "$(GO)" build -ldflags "$(GO_LDFLAGS)" -o "$$bin" ./cmd/dropcheck; bin="$(CONTROLLER_INGESTER_BIN)"; [[ "$$bin" != */* ]] || run mkdir -p "$${bin%/*}"; run "$(GO)" build -ldflags "$(GO_LDFLAGS)" -o "$$bin" ./cmd/dropcheck-ingester) ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

.PHONY: test
test: ## Test targets; use TARGET=agent,controller or TAGS=... for Go tags
	@die(){ printf 'make test: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent controller"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) run "$(GRADLE)" "$(AGENT_TEST_TASK)" "-PdropcheckVersion=$(VERSION)" ;; \
			controller) go_test=("$(GO)" test); [[ -z "$(TAGS)" ]] || go_test+=(-tags "$(TAGS)"); go_test+=(./...); (cd controller && run "$${go_test[@]}") ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

.PHONY: fmt
fmt: ## Format targets where a formatter is configured
	@die(){ printf 'make fmt: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent controller"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) printf 'agent formatter is not configured; skipping\n' ;; \
			controller) run find controller -path controller/internal/controlpb -prune -o -name '*.go' -exec gofmt -w {} + ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

.PHONY: lint
lint: ## Lint targets; controller runs go vet and staticcheck
	@die(){ printf 'make lint: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent controller"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) run "$(GRADLE)" "$(AGENT_LINT_TASK)" "-PdropcheckVersion=$(VERSION)" ;; \
			controller) vet=("$(GO)" vet); static=("$(STATICCHECK)"); [[ -z "$(TAGS)" ]] || { vet+=(-tags "$(TAGS)"); static+=(-tags "$(TAGS)"); }; vet+=(./...); static+=(./...); (cd controller && run "$${vet[@]}" && run "$${static[@]}") ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

.PHONY: install
install: ## Build and install the debug APK; use SERIAL=<adb serial> when needed
	@die(){ printf 'make install: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) run "$(GRADLE)" "$(AGENT_BUILD_TASK)" "-PdropcheckVersion=$(VERSION)"; apk="$(APK)"; [[ -f "$$apk" ]] || die "missing APK $$apk"; adb=("$(ADB)"); [[ -z "$(SERIAL)" ]] || adb+=(-s "$(SERIAL)"); run "$${adb[@]}" install $(ADB_INSTALL_FLAGS) "$$apk" ;; \
			controller) die "install supports TARGET=agent only" ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

.PHONY: e2e
e2e: ## Run real-device shell/CLI e2e matrix; use SERIAL=... SSID=... PSK=...
	@die(){ printf 'make e2e: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	serial="$(SERIAL)"; ssid="$(SSID)"; psk="$(PSK)"; psk_env="$(PSK_ENV)"; \
	[[ -n "$$serial" ]] || die "SERIAL is required"; \
	[[ -n "$$ssid" ]] || die "SSID is required"; \
	if [[ -n "$$psk" ]]; then export DROPCHECK_E2E_WIFI_PSK="$$psk"; psk_env="DROPCHECK_E2E_WIFI_PSK"; \
	else [[ -n "$${!psk_env:-}" ]] || die "PSK or $$psk_env is required"; fi; \
	export DROPCHECK_E2E_LIVE=1 DROPCHECK_E2E_SERIAL="$$serial" DROPCHECK_E2E_WIFI_SSID="$$ssid" DROPCHECK_E2E_WIFI_PSK_ENV="$$psk_env" DROPCHECK_E2E_ADB="$(ADB)" DROPCHECK_E2E_PACKAGE="$(E2E_AGENT_PACKAGE)"; \
	(cd controller && run "$(GO)" test -v -count=1 -tags e2e -timeout "$(E2E_TIMEOUT)" "$(E2E_PACKAGE)")

.PHONY: integration
integration: ## Run Docker-backed integration tests
	@run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	(cd controller && run "$(GO)" test -v -count=1 -tags integration -timeout "$(INTEGRATION_TIMEOUT)" "$(INTEGRATION_PACKAGE)")

.PHONY: version
version: ## Print the version resolved from git describe
	@printf '%s\n' "$(VERSION)"

.PHONY: quality
quality: fmt lint test ## Format, lint, and test selected targets

.PHONY: clean
clean: ## Remove build artifacts for selected targets
	@die(){ printf 'make clean: %s\n' "$$*" >&2; exit 1; }; \
	run(){ printf '+'; printf ' %q' "$$@"; printf '\n'; "$$@"; }; \
	targets="$(TARGET)"; targets="$${targets//,/ }"; [[ -n "$$targets" ]] || die "TARGET is empty"; \
	[[ " $$targets " == *" all "* ]] && targets="agent controller"; \
	for target in $$targets; do \
		case "$$target" in \
			agent) run "$(GRADLE)" clean ;; \
			controller) run rm -rf controller/dist; (cd controller && run "$(GO)" clean -cache -testcache) ;; \
			*) die "unknown TARGET=$$target" ;; \
		esac; \
	done

##@ Help

.PHONY: help
help: ## Show this help message
	@awk -v width="$(HELP_NAME_WIDTH)" 'BEGIN {FS = ":.*##"} \
		{ lines[NR] = $$0 } \
		END { \
			section = ""; \
			for (i = 1; i <= NR; i++) { \
				$$0 = lines[i]; \
				if ($$0 ~ /^##@/) { \
					section = substr($$0, 5); \
				} else if ($$0 ~ /^[a-zA-Z0-9_.-]+:.*##/) { \
					split($$0, parts, ":.*##"); \
					sub(/^[[:space:]]+/, "", parts[2]); \
					if (section != "") printf "\n\033[1m%s\033[0m\n", section; \
					section = ""; \
					printf "  \033[36m%-*s\033[0m%s\n", width, parts[1], parts[2]; \
				} \
			} \
		}' $(MAKEFILE_LIST)
	@printf "\n\033[1mVariables:\033[0m\n"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "TARGET" "agent, controller, or all; comma and space lists are accepted"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "VERSION" "Version from git describe; override with VERSION=x.y.z"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "TAGS" "Go build/test tags for controller targets"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "SSID" "Test Wi-Fi SSID for make e2e"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "PSK" "Test Wi-Fi PSK for make e2e; PSK_ENV can be used instead"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "PSK_ENV" "Environment variable containing the Wi-Fi PSK, defaults to $(PSK_ENV)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "SERIAL" "ADB serial for make install, defaults to ADB_SERIAL"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "ADB_INSTALL_FLAGS" "adb install flags, defaults to $(ADB_INSTALL_FLAGS)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "APK" "Debug APK path, defaults to $(APK)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "CONTROLLER_BIN" "Controller binary path under controller/, defaults to $(CONTROLLER_BIN)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "CONTROLLER_INGESTER_BIN" "Controller ingester binary path under controller/, defaults to $(CONTROLLER_INGESTER_BIN)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "E2E_TIMEOUT" "Go test timeout for make e2e, defaults to $(E2E_TIMEOUT)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "INTEGRATION_PACKAGE" "Docker-backed integration package, defaults to $(INTEGRATION_PACKAGE)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "INTEGRATION_TIMEOUT" "Go test timeout for make integration, defaults to $(INTEGRATION_TIMEOUT)"
	@printf "\n\033[1mExamples:\033[0m\n"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make build TARGET=agent,controller" "# Build both Android agent and Go controller"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make test TARGET=controller" "# Run controller tests"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make integration" "# Run Docker-backed ingester integration tests"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make e2e SERIAL=DEVICE SSID=Lab PSK=..." "# Run real-device shell/CLI e2e matrix"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make install SERIAL=DEVICE" "# Build and adb install the debug APK"
