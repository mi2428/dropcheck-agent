#!/usr/bin/env python3
"""Consume the controller as a versioned module, without replace or network."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import zipfile

SOURCE = Path(__file__).resolve().parent.parent / "controller"
GO = os.environ.get("GO", "go")
MODULE = (SOURCE / "go.mod").read_text().splitlines()[0].removeprefix("module ")
VERSION = "v0.0.0"


def main():
    cache = subprocess.check_output([GO, "env", "GOMODCACHE"], text=True).strip()
    with tempfile.TemporaryDirectory(prefix="dropcheck-distribution-") as temporary:
        work = Path(temporary)
        proxy = work / "proxy"
        versions = proxy / MODULE / "@v"
        versions.mkdir(parents=True)
        (versions / "list").write_text(VERSION + "\n")
        (versions / (VERSION + ".mod")).write_bytes((SOURCE / "go.mod").read_bytes())
        (versions / (VERSION + ".info")).write_text(json.dumps({"Version": VERSION, "Time": "2026-11-05T00:00:00Z"}))
        with zipfile.ZipFile(versions / (VERSION + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
            for source in sorted(SOURCE.rglob("*.go")) + [SOURCE / "go.mod", SOURCE / "go.sum", SOURCE / "internal/oui/data/registry.tsv.gz"]:
                archive.write(source, f"{MODULE}@{VERSION}/{source.relative_to(SOURCE).as_posix()}")
        consumer = work / "consumer"
        consumer.mkdir()
        go_version = next(line for line in (SOURCE / "go.mod").read_text().splitlines() if line.startswith("go "))
        (consumer / "go.mod").write_text(f"module distribution.test/consumer\n\n{go_version}\n\nrequire {MODULE} {VERSION}\n")
        (consumer / "consumer_test.go").write_text('''package consumer
import (
    "testing"
    "time"
    dc "''' + MODULE + '''/pkg/dropcheck"
)
func TestPublicModule(t *testing.T) {
    op, err := dc.Ping(dc.PingOptions{Host: "example.invalid", Count: 1, Timeout: time.Second})
    if err != nil { t.Fatal(err) }
    op, err = op.WithNetwork(dc.NetworkSelector{SSID: "123"})
    if err != nil || op.Name() != "ping" { t.Fatal(op, err) }
    zero := uint32(0)
    raw := &dc.CommandResult{Status: dc.StatusOK, Payload: &dc.PingPayload{Ping: &dc.PingResult{Received: &zero}}}
    if raw.GetPing().Received == nil || raw.GetPing().GetReceived() != 0 { t.Fatal("typed zero was lost") }
    var client dc.Client
    if err := client.Close(); err != nil { t.Fatal(err) }
}
''')
        env = dict(os.environ, GOENV="off", GOWORK="off", GOTOOLCHAIN="local",
                   GOPROXY=proxy.as_uri() + "," + (Path(cache) / "cache/download").as_uri(),
                   GONOPROXY="none", GOPRIVATE="", GOSUMDB="off", GOMODCACHE=str(work / "modcache"))
        subprocess.run([GO, "test", "-mod=mod", "-count=1", "./..."], cwd=consumer, env=env, check=True, timeout=120)
        subprocess.run([GO, "build", "-mod=mod", "-o", str(work / "dropcheck"), MODULE + "/cmd/dropcheck"], cwd=consumer, env=env, check=True, timeout=120)
        dependencies = set(subprocess.check_output([GO, "list", "-deps", MODULE + "/pkg/dropcheck"], cwd=consumer, env=env, text=True, timeout=30).splitlines())
        for private_surface in ["tuirun", "tuiconfig", "tuistate", "tui", "render"]:
            assert MODULE + "/internal/" + private_surface not in dependencies, private_surface
        assert "replace" not in (consumer / "go.mod").read_text()
    print("Versioned public module consumption: PASS (local proxy; no replacement or network)")


if __name__ == "__main__":
    main()
