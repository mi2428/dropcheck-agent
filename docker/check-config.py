"""Check both local-stack trust boundaries without starting containers."""
import json
import os
from pathlib import Path
import subprocess


root = Path(__file__).resolve().parent.parent
env = dict(os.environ, MINIO_ROOT_USER="config-check", MINIO_ROOT_PASSWORD="config-check-only",
           GRAFANA_ADMIN_USER="config-check", GRAFANA_ADMIN_PASSWORD="config-check-only",
           DROPCHECK_INGESTER_WEBHOOK_TOKEN="config-check-token")
bindings = {"minio": ["MINIO_API_BIND", "MINIO_CONSOLE_BIND"],
            "prometheus": ["PROMETHEUS_HTTP_BIND"], "grafana": ["GRAFANA_HTTP_BIND"]}
for names in bindings.values():
    for name in names:
        env.pop(name, None)
for file in ("docker-compose.yml", "docker-compose.test.yml"):
    command = ["docker", "compose", "--env-file", os.devnull, "-f", file, "config", "--format", "json"]
    config = json.loads(subprocess.check_output(command, cwd=root, env=env))
    services = config["services"]
    for name in ("ingester", "pushgateway", "minio-init"):
        assert not services[name].get("ports"), (file, name, "must not publish host ports")
    for name, variables in bindings.items():
        if name not in services:
            continue
        ports = services[name]["ports"]
        assert len(ports) == len(variables), (file, name, ports)
        assert all(port["host_ip"] == "127.0.0.1" for port in ports), (file, name, ports)
    init = services["minio-init"]["entrypoint"]
    assert init[:2] == ["/bin/sh", "-ec"], (file, "init must fail fast")
    assert "s3:PutObject" in init[2] and "/*.pb" in init[2], (file, "archive-only upload policy")
    assert "mc anonymous set upload" not in init[2], (file, "unscoped upload policy")
    override = dict(env, MINIO_API_BIND="192.0.2.1")
    overridden = json.loads(subprocess.check_output(command, cwd=root, env=override))["services"]
    assert overridden["minio"]["ports"][0]["host_ip"] == "192.0.2.1"
    assert overridden["minio"]["ports"][1]["host_ip"] == "127.0.0.1"
    missing = dict(env)
    missing.pop("MINIO_ROOT_PASSWORD")
    result = subprocess.run(command, cwd=root, env=missing, capture_output=True)
    assert result.returncode != 0 and b"MINIO_ROOT_PASSWORD" in result.stderr
    print(file + ": loopback, private writers, explicit upload override, required credentials OK")
    for name, service in services.items():
        image = service.get("image", "")
        if name in ("minio", "minio-init"):
            assert service["pull_policy"] == "build" and service["build"]["target"] in ("minio", "mc")
            assert len(image.rsplit(":", 1)[1]) == 40, (file, name, "source commit pin")
            source = (root / "docker/minio/Dockerfile").read_text()
            assert "/tar.gz/" + image.rsplit(":", 1)[1] in source, (file, name, "source/image commit mismatch")
        elif image:
            assert "@sha256:" in image and len(image.rsplit("@sha256:", 1)[1]) == 64, (file, name, image)
    helper = (root / "controller/integration/ingester/ingester_test.go").read_text()
    assert services["minio"]["image"] in helper, "Compose/helper must use the same source build"
    print(file + ": immutable registry refs and matching source build OK")

for file in ("docker/minio/Dockerfile", "docker/ingester/Dockerfile"):
    aliases = set()
    for line in (root / file).read_text().splitlines():
        if not line.startswith("FROM "):
            continue
        parts = line.split()
        assert parts[1] in aliases or "@sha256:" in parts[1], (file, "mutable base image", parts[1])
        if len(parts) == 4:
            aliases.add(parts[3])
print("Dockerfiles: immutable base images OK")
