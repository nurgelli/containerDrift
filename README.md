# ContainerDrift

A small, **read-only** command-line tool that tells you when your running Docker containers no longer match your Docker Compose file.

```
$ containerdrift scan -f compose.yaml

ContainerDrift v0.1.0

Project: demo

[PASS] web
  Image matches
  Port mapping matches
  Volume mounts match
  Restart policy matches
  Privileged mode matches

[DRIFT] postgres
  Port mapping mismatch (container demo-postgres-1)

  Expected:
    0.0.0.0:5433 -> 5432/tcp

  Actual:
    0.0.0.0:5432 -> 5432/tcp

Summary:
  Services: 2
  Passed: 1
  Drift detected: 1
  Warnings: 0

Scan completed.
```

## Why

You edit `compose.yaml`, forget to run `docker compose up -d` again, and the running container keeps the old settings. Or someone changes a container by hand. Docker Compose can recreate what changed, but it does not tell you **what** differs in a form you can read or check in CI. ContainerDrift compares the two and explains each difference, field by field.

It is deliberately small and honest about its scope: it is a diagnostic tool, not a deployment or orchestration tool, and it never changes anything.

## What it checks

For every service in the Compose project:

| Check | What is compared |
|---|---|
| Missing container | A service has no container at all |
| Not running | A container exists but is not in the `running` state |
| Unexpected container | A container carries the project label but its service is not in the Compose configuration |
| Image | Image reference (Docker Hub prefixes and the implicit `:latest` tag are normalised) |
| Ports | Published ports: container port, protocol, host port, host IP |
| Volumes | Bind mounts and named volumes: type, source, target, read-only flag |
| Restart policy | Policy name and maximum retries |
| Privileged | Privileged mode |

Container IDs, IP addresses, timestamps and other runtime-generated values are never compared.

## How it works

```
                 containerdrift scan
                        |
        +---------------+----------------+
        |                                |
  desired state                     actual state
  `docker compose config            Docker Engine API
   --format json`                   (official Go SDK)
        |                                |
   internal/compose                 internal/docker
        |                                |
        +---------------+----------------+
                        |
                  internal/drift      (pure comparison, no I/O)
                        |
                  internal/report     (table or JSON)
                        |
                    exit code
```

- **Desired state** comes from `docker compose config --format json`, so Compose itself resolves multiple files, `extends`, environment interpolation and profiles. ContainerDrift does not parse Compose YAML.
- **Actual state** comes from the Docker Engine API. Containers are found through the labels Compose puts on them (`com.docker.compose.project`, `com.docker.compose.service`).
- Both sides are converted into the application's own types (`internal/model`) before being compared, so the comparison code knows nothing about Docker or Compose.

## Requirements

- Docker Engine reachable from where you run the tool (it honours `DOCKER_HOST`)
- The `docker` CLI with the Compose plugin (`docker compose`), version 2 or newer
- Go 1.25 or newer to build from source

## Install

From source:

```bash
git clone https://github.com/nurgelli/containerdrift.git
cd containerdrift
go build -o containerdrift ./cmd/containerdrift    # containerdrift.exe on Windows
```

Or, once the repository is public:

```bash
go install github.com/nurgelli/containerdrift/cmd/containerdrift@latest
```

## Usage

```bash
# Use the Compose file in the current directory
containerdrift scan

# Use the same flags you use with "docker compose up"
containerdrift scan -f compose.yaml -f compose.prod.yaml -p myapp
containerdrift scan --env-file .env.prod --profile debug

# Machine-readable output
containerdrift scan --format json

# Treat warnings as failures too
containerdrift scan --strict
```

| Flag | Meaning |
|---|---|
| `-f, --file` | Compose file, repeatable |
| `-p, --project-name` | Compose project name |
| `--env-file` | Environment file, repeatable |
| `--profile` | Compose profile to enable, repeatable |
| `--format` | `table` (default) or `json` |
| `--strict` | Exit with code 1 on warnings as well as on drift |

**Pass the same `-f`, `-p`, `--env-file` and `--profile` you used for `up`.** Otherwise the desired state ContainerDrift computes can differ from what you deployed, and the report will say so.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | No drift |
| `1` | Drift detected (or warnings, with `--strict`) |
| `2` | The scan could not be completed: Docker or Compose unavailable, invalid configuration, bad flags |

### JSON output

```json
{
  "schemaVersion": "1",
  "tool": "containerdrift",
  "version": "0.1.0",
  "project": "demo",
  "summary": { "services": 2, "passed": 1, "drift": 1, "warnings": 0 },
  "services": [
    { "name": "web", "status": "pass", "findings": [] },
    {
      "name": "postgres",
      "status": "drift",
      "findings": [
        {
          "kind": "port_mismatch",
          "severity": "drift",
          "container": "demo-postgres-1",
          "message": "Port mapping mismatch (container demo-postgres-1)",
          "expected": ["0.0.0.0:5433 -> 5432/tcp"],
          "actual": ["0.0.0.0:5432 -> 5432/tcp"]
        }
      ]
    }
  ],
  "warnings": []
}
```

`schemaVersion` changes only when an existing field is removed or changes meaning. Finding kinds: `missing_container`, `container_not_running`, `unexpected_container`, `image_mismatch`, `port_mismatch`, `volume_mismatch`, `restart_policy_mismatch`, `privileged_mismatch`.

### Use in CI

```yaml
- run: docker compose up -d
- run: containerdrift scan --format json
```

The step fails when the exit code is not `0`.

## Comparison rules worth knowing

- **Ports.** An empty host port in Compose (for example `"3000"`) means "any host port". An empty host IP means "any host IP", and a second IPv4/IPv6 entry that Docker adds for the same published port is not reported. A host IP that you pin (`127.0.0.1:8080:80`) must match exactly.
- **Volumes.** Every volume declared in Compose must exist in the container with the same type, source, target and read-only flag. Extra mounts in the container are ignored, because images declare anonymous volumes that Compose never lists. Named volumes are compared by their resolved name (`<project>_<volume>`). Windows-style bind paths are compared ignoring case and separator style.
- **Restart policy.** A service without `restart:` is expected to have the policy `no`, which is what Docker reports.
- **Services built from source** (`build:` without `image:`) are not checked for their image, because the image name is generated by Compose.
- **One-off containers** created with `docker compose run` are ignored.
- **Scaled services** (several containers per service): each container is compared with the same service definition.

## Security

- ContainerDrift only lists and inspects containers. It never starts, stops, recreates or modifies anything.
- `docker compose config` prints resolved environment variables, including secrets. ContainerDrift decodes only the fields it needs, so **environment values never enter its data structures**, are not compared, and are not printed. (The raw command output exists in memory briefly while it is decoded.)
- External commands are run without a shell, with arguments passed separately, so file names and project names cannot be interpreted as shell syntax. Commands have a timeout.
- **Docker socket access is not harmless.** Anyone who can talk to the Docker socket effectively has root on the host, even if the program using it is read-only. Run ContainerDrift directly on the host where you can. If you run it in a container (see below), you are giving that container the same power: only do so with an image you built yourself.

## Running in a container

A `Dockerfile` is provided. It needs the Docker socket and your project directory:

```bash
docker build -t containerdrift .
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD":/work \
  containerdrift scan -f compose.yaml
```

Be aware of two limitations. First, see the security note above. Second, inside the container `docker compose config` resolves bind-mount paths as the container sees them (`/work/...`), while the daemon reports host paths, so **bind-mount checks can report false drift when run this way**. Running the binary on the host avoids both problems.

## Limitations

What is **not** supported in v0.1:

- Environment variables, networks, healthchecks, commands/entrypoints, resource limits and labels are not compared.
- `deploy.replicas` and `scale` are not compared, so a missing replica is not detected.
- Mount types other than `bind` and `volume` (for example `tmpfs`) are skipped, with a warning.
- Port ranges are compared as Compose expands them; unusual range forms have not been tested.
- `--project-directory` is not exposed; relative paths are resolved by Compose relative to the first `-f` file.
- Services of a Compose profile that was not enabled with `--profile` but that have running containers are reported as unexpected containers.
- A container started by a different tool, or by `docker run`, is only seen when it carries the Compose project label.

## Status and verification

Be aware of what has and has not been verified:

- The unit tests run without Docker (`go test ./...`, also with `-race`) and cover the Compose conversion, the Docker inspection (against a fake engine), the comparison rules, the reporters and the CLI.
- The Compose configuration format and the Docker inspect output were checked by hand against **Docker Desktop on Windows with Docker Compose 2.38.1**.
- The binary was also run end to end against a fake Docker daemon speaking the real Docker API wire format, using output captured from that Windows setup.
- The integration tests in `tests/integration` pass against a real daemon on **Docker Desktop for Windows with Docker Compose 2.38.1**: a freshly deployed project (no drift), a changed port, a stopped container and a removed container are all reported correctly. They have not been run on Linux or macOS, and the GitHub Actions workflow has not been observed running yet.
- Linux and macOS behaviour is expected to match but is not independently verified.
- The Docker SDK module (`github.com/moby/moby/client`) is still at a `v0.x` version, so its API may change between releases. The version is pinned in `go.mod`.

## Development

```bash
make test          # unit tests, no Docker needed
make race          # unit tests with the race detector
make integration   # tests against a real Docker daemon
make vet fmt-check
make build
```

Without `make`: `go test ./...`, `go vet ./...`, `gofmt -l .`, and for the integration tests `go test -tags integration ./tests/...`.

### Project layout

```
cmd/containerdrift/   main: signal handling, exit codes
internal/cli/         Cobra commands, wiring
internal/compose/     runs "docker compose config", converts it to the model
internal/docker/      Docker Engine API client, converts containers to the model
internal/model/       types shared by all packages (imports nothing)
internal/drift/       comparison rules (pure functions)
internal/report/      table and JSON output
tests/integration/    tests against a real Docker daemon
examples/             a demo Compose file
```

### Contributing

Issues and pull requests are welcome. Please:

1. Keep changes small and focused; one concern per pull request.
2. Add or update table-driven tests; the comparison rules in `internal/drift` must stay free of I/O.
3. Run `gofmt`, `go vet` and `go test ./...` before pushing.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
5. Do not document a behaviour in the README that is not implemented and tested.

## Roadmap

Ideas that are **not implemented**: configurable comparison rules, a watch mode, Prometheus metrics, replica-count checks, environment checks with a safe secret policy, and multi-host support.

## License

MIT, see [LICENSE](LICENSE).
