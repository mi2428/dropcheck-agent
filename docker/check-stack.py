"""Exercise a running disposable Compose stack with synthetic archive data only."""
import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--project", required=True)
parser.add_argument("--file", default="docker-compose.yml")
args = parser.parse_args()
fixture = "image-check-" + uuid.uuid4().hex
root = Path(__file__).resolve().parent.parent
compose = ["docker", "compose", "-p", args.project, "-f", args.file]
config = json.loads(subprocess.check_output(compose + ["config", "--format", "json"], cwd=root))["services"]
assert config["ingester"]["environment"]["DROPCHECK_INGESTER_POLL_INTERVAL"] == "1h", "use 1h to distinguish notifications from polling"
ids = subprocess.check_output(compose + ["ps", "--all", "--quiet"], cwd=root, text=True).split()
containers = json.loads(subprocess.check_output(["docker", "inspect", *ids]))
for container in containers:
    name = container["Config"]["Labels"]["com.docker.compose.service"]
    bindings = container["HostConfig"].get("PortBindings")
    for ports in (bindings or {}).values():
        assert all(port["HostIp"] == "127.0.0.1" for port in ports), (name, "non-loopback binding")
    if name in ("ingester", "pushgateway"):
        assert not container["HostConfig"].get("PortBindings"), (name, "host writer exposed")
    if name == "minio-init":
        assert container["State"]["Status"] == "exited" and container["State"]["ExitCode"] == 0, "MinIO init failed or still running"
    else:
        assert container["State"]["Running"], (name, "container is not running")


def address(service, target):
    port = next(port for port in config[service]["ports"] if port["target"] == target)
    assert port["host_ip"] == "127.0.0.1"
    return "http://127.0.0.1:" + str(port["published"])


def http(url, method="GET", data=None, status=200, headers=None):
    request = urllib.request.Request(url, data=data, method=method, headers=headers or {})
    try:
        response = urllib.request.urlopen(request, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        assert response.status == status, (method, response.status, "unexpected HTTP status")
        return response.read(1 << 20)


def private_get(url):
    return subprocess.check_output([
        "docker", "run", "--rm", "--network", args.project + "_default",
        "--entrypoint", "/usr/bin/wget", config["minio-init"]["image"], "-q", "-O", "-", url,
    ], timeout=15, text=True)


def until(check, description):
    for attempt in range(30):
        if check():
            return
        time.sleep(1)
    raise AssertionError(description)


# Minimal protobuf wire fixture matching proto/dropcheck/v1/control.proto.
# Only nonnegative varints and length-delimited fields are needed here.
def varint(value):
    assert value >= 0
    result = bytearray()
    while value > 127:
        result.append((value & 127) | 128)
        value >>= 7
    return bytes(result) + bytes([value])


def message(*fields):
    result = b""
    for number, value in fields:
        if isinstance(value, int):
            result += varint(number << 3) + varint(value)
        else:
            value = value.encode() if isinstance(value, str) else value
            result += varint(number << 3 | 2) + varint(len(value)) + value
    return result


def archive(finished, ping):
    status = 1 if ping else 2
    summary = message((1, "image-check-" + str(finished)), (2, fixture), (4, finished - 10), (5, finished))
    device = message((2, "ImageFixture"), (3, "image-fixture"))
    festa = message((1, fixture), (4, message((1, "image-check"), (2, "FixtureNetwork"))))
    result = message((1, status), (3, 10))
    command = b""
    if ping:
        command = message((13, message((1, "192.0.2.1"))))
        result += message((13, message((1, "192.0.2.1"), (9, 10))))
    step = message((1, 1), (2, "image-check"), (3, 1), (6, finished - 10), (7, finished), (8, command), (9, result))
    return message((1, summary), (2, device), (3, festa), (4, step))


api = address("minio", 9000)
prometheus = address("prometheus", 9090)
bucket = config["minio-init"]["environment"]["MINIO_BUCKET"]
object_url = api + "/" + urllib.parse.quote(bucket) + "/" + fixture + "/current.pb"
http(api + "/minio/health/ready")
http(address("minio", 9001))
assert private_get("http://ingester:8082/healthz").strip() == "ok"
http(api + "/" + bucket + "?list-type=2", status=403)
http(api + "/" + bucket + "/image-check/denied.txt", method="PUT", data=b"denied", status=403)
http(object_url, method="PUT", data=archive(1700000001000, True))
http(object_url, status=403)
http(object_url, method="DELETE", status=403)


def metrics():
    return [line for line in private_get("http://pushgateway:9091/metrics").splitlines()
            if not line.startswith("#") and f'festa="{fixture}"' in line]


def current(value, ping):
    lines = metrics()
    success = [line for line in lines if line.startswith("dropcheck_success{")]
    assert len(success) <= 1, "run/object identities accumulated"
    for line in success:
        assert not any(name + "=" in line for name in ("run_id", "object_key", "step", "command"))
    return (len(success) == 1 and float(success[0].rsplit(" ", 1)[1]) == value
            and any(line.startswith("dropcheck_ping_success{") for line in lines) == ping)


until(lambda: current(1, True), "initial notification/ingestion failed")
http(object_url, method="PUT", data=archive(1700000002000, False))
until(lambda: current(0, False), "same-group replacement kept stale metrics")
http(api + "/" + bucket + "/" + fixture + "/older.pb", method="PUT", data=archive(1700000001500, True))
time.sleep(2)
assert current(0, False), "older notification replaced the newest group"


def query(expression):
    body = json.loads(http(prometheus + "/api/v1/query?" + urllib.parse.urlencode({"query": expression})))
    assert body["status"] == "success"
    return body["data"]["result"]


def scraped():
    values = query(f'dropcheck_success{{festa="{fixture}"}}')
    return (len(values) == 1 and values[0]["metric"]["job"] == "dropcheck_results"
            and float(values[0]["value"][1]) == 0 and not query(f'dropcheck_ping_success{{festa="{fixture}"}}'))


until(scraped, "Prometheus did not scrape replacement metrics")
targets = json.loads(http(prometheus + "/api/v1/targets"))["data"]["activeTargets"]
assert any(target["labels"]["job"] == "pushgateway" and target["health"] == "up" for target in targets)
if "grafana" in config:
    grafana = address("grafana", 3000)
    assert json.loads(http(grafana + "/api/health"))["database"] == "ok"
    http(grafana + "/api/datasources", status=401)
    credentials = os.environ["GRAFANA_ADMIN_USER"] + ":" + os.environ["GRAFANA_ADMIN_PASSWORD"]
    auth = {"Authorization": "Basic " + base64.b64encode(credentials.encode()).decode()}
    health = json.loads(http(grafana + "/api/datasources/uid/prometheus/health", headers=auth))
    assert health["status"] == "OK", "Grafana datasource health failed"
print(args.project + ": PASS bindings, upload permissions, real notification/ingestion, replacement, ordering, scrape"
      + (", Grafana auth/datasource" if "grafana" in config else ""))
