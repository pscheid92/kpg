[![CI](https://github.com/pscheid92/kpg/actions/workflows/ci.yml/badge.svg)](https://github.com/pscheid92/kpg/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/pscheid92/kpg.svg)](https://pkg.go.dev/github.com/pscheid92/kpg)
[![Go Report Card](https://goreportcard.com/badge/github.com/pscheid92/kpg)](https://goreportcard.com/report/github.com/pscheid92/kpg)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

# kpg

`kpg` is a narrow, command-first CLI for connecting to Kubernetes-hosted Postgres databases.

It discovers supported Postgres operator resources, reads the generated application credentials, opens a local port-forward to the read-write service, and exposes `PG*` environment variables for `psql`, `pgcli`, or any Postgres-compatible client.

## Usage

```sh
kpg list
kpg list --output json
kpg connect
kpg connect <cluster|namespace/cluster|substring>
kpg connect <provider>:<namespace>/<cluster>
kpg connect <cluster|namespace/cluster|substring> -- psql
kpg connect <cluster|namespace/cluster|substring> --output shell
kpg connect <cluster|namespace/cluster|substring> --user <user> --database <db>
kpg last
kpg last --user <user> --database <db>
kpg last -- psql
kpg version
kpg completion <bash|zsh|fish|powershell>
```

Shared flags:

```sh
-c, --context <name>            kube context override
-n, --namespace <name>          restrict discovery/lookup to one namespace
-p, --local-port <port>         use a fixed local port
-o, --output shell|dotenv|json
    --request-timeout <duration> timeout for Kubernetes API calls, for example 30s; off by
                                default so interactive credential plugins are not cut short;
                                the port-forward itself is never limited
-h, --help
```

## Install

Homebrew on macOS or Linux:

```sh
brew install pscheid92/tap/kpg
```

With Go 1.27 or newer:

```sh
go install github.com/pscheid92/kpg@latest
```

Prebuilt binaries for Linux, macOS, and Windows are attached to GitHub Releases.

Requirements:

- A kubeconfig with access to the target cluster
- CloudNativePG or Zalando Postgres Operator resources in the active context
- `psql`, `pgcli`, or another Postgres client when using command mode

On Windows, `--output` and command mode work as on other platforms. The subshell
started by a plain `kpg connect` is `SHELL` if set, otherwise `pwsh`, Windows
PowerShell, or `COMSPEC` (`cmd.exe`), whichever is found first.

### Releases

Release builds set `kpg version` from the tag, commit, and commit date, and are
reproducible from the tag. `go install` builds report the module version and
the commit recorded by the Go toolchain. Releases are created from tags named
`v*`, for example:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Each release ships, per archive, a [`pqsign`](https://github.com/pscheid92/pqsign)
signature (`.pqsig`) and an SPDX SBOM (`.sbom.json`), plus a signed
`checksums.txt` and a GitHub build provenance attestation. Verify an archive with
the committed release public key:

```sh
pqsign verify kpg_0.1.0_linux_amd64.tar.gz -p release.key.pub
```

and its provenance with:

```sh
gh attestation verify kpg_0.1.0_linux_amd64.tar.gz --owner pscheid92
```

The release workflow expects these GitHub repository secrets:

- `PQSIGN_SECRET_KEY`: base64-encoded encrypted pqsign secret key file matching `release.key.pub`
- `PQSIGN_PASSWORD`: password for that secret key
- `HOMEBREW_TAP_GITHUB_TOKEN` (optional): token with write access to `pscheid92/homebrew-tap`; without it the Homebrew cask is only rendered into `dist/`

## Connecting

In an interactive terminal, `kpg connect` opens a subshell with `PG*` environment values already exported. Exiting that shell closes the tunnel:

```sh
kpg connect app-db
psql
exit
```

When run in an interactive terminal with no target, `kpg connect` opens a searchable target picker before starting the subshell. Type to filter, use the arrow keys or `ctrl+j`/`ctrl+k` to move, press Enter to connect, or Esc to cancel. In non-interactive use, pass the target explicitly.

If a command is provided after `--`, `kpg` injects the same `PG*` environment values into that command and stops the tunnel when the command exits. `kpg` exits with the command's exit code:

```sh
kpg connect app-db -- psql
kpg connect app-db -- pgcli
```

Ctrl-C inside the subshell or the command is handled by that program, for
example `psql` cancels the running query and a shell prints a new prompt. `kpg`
keeps the tunnel open until the program exits. Sending `kpg` a `SIGTERM`
forwards it to the program.

The program also receives `KPG_TARGET` (`namespace/cluster`) and
`KPG_PROVIDER`, for example to show the connection in a shell prompt. `kpg`
removes `PGSERVICE`, `PGSERVICEFILE`, and `PGHOSTADDR` from the program's
environment because libpq would let them override the tunnel.

If the tunnel drops, for example because the primary moved to another pod
during a failover, `kpg` re-establishes it on the same local port: once
immediately, then with pauses of 1, 2, 4, and 8 seconds. Sessions that were
open before the loss end with it. `psql` usually tries to reset before the
tunnel is back and then gives up, so `kpg` prints the command that reconnects
it, for example:

```text
port-forward to secretli/secretli-db re-established on 127.0.0.1:51664
sessions opened before the loss must reconnect; in psql run: \c "host=127.0.0.1 port=51664 dbname=secretli user=secretli"
```

An open session also delays a CloudNativePG failover by up to the cluster's
`smartShutdownTimeout`, because the old primary waits for existing
connections before it stops.

`kpg` suppresses client-go's internal log lines so they do not interrupt an
interactive session. Set `KPG_DEBUG=1` to see them.

Use `--output` to print connection values instead of entering a subshell or running a command. The values are printed once the tunnel accepts connections, and `kpg connect` then keeps the tunnel alive in the foreground until Ctrl-C:

```sh
export PGHOST=127.0.0.1
export PGPORT=15432
export PGUSER=app
export PGPASSWORD=secret
export PGDATABASE=app
export PGSSLMODE=disable
```

```sh
kpg connect app-db --output shell
kpg connect app-db --output dotenv
kpg connect app-db --output json
```

Use `--user` and `--database` to override the defaults discovered from the
operator resource and generated credentials:

```sh
kpg connect app-db --user reporting_user --database reports
kpg last --user reporting_user --database reports
```

If no credentials secret exists for the selected user, `kpg` warns on stderr
and leaves `PGPASSWORD` unset so the client prompts for a password.

Targets can be written as a cluster name, `namespace/cluster`, or a unique substring match. If multiple providers expose the same namespace and cluster, use a provider-qualified target:

```sh
kpg connect cnpg:app/app-db
kpg connect zalando:postgres/acid-main
```

Ambiguous matches fail with candidate suggestions. `kpg list` prints provider information when it is needed to distinguish targets, and says so on stderr when nothing was found. `kpg list --output json` emits script-friendly target metadata without secrets.

The last successful target is stored as soon as the tunnel is ready, at the XDG state path, normally:

```text
~/.local/state/kpg/last.json
```

Only the provider, namespace, and cluster are stored. Discovery data and secrets are not cached.

## Discovery

CloudNativePG and Zalando Postgres Operator are supported.

`kpg` uses Kubernetes API discovery to detect which supported provider
resources are registered, then lists only those resources. It does not list
`CustomResourceDefinition` objects. By default, discovery tries to list
supported resources across namespaces. If Kubernetes denies an all-namespace
provider list, `kpg` retries that provider in the current kube context
namespace. Use `--namespace <name>` to make discovery strictly namespaced, or
pass a namespace-qualified target such as `app/app-db`.

CloudNativePG:

```text
postgresql.cnpg.io/v1 Cluster
```

For each cluster:

```text
RW service: status.writeService, normally <cluster>-rw
App secret: spec.bootstrap.<method>.secret.name, normally <cluster>-app
```

The application database and its owner come from the bootstrap section the
cluster uses, `initdb`, `recovery`, or `pg_basebackup`, with the operator's
defaults (`app` owned by `app`) when they are not set. The generated app secret
overrides both when it is readable. Additional users for `--user`:

- `postgres` when `spec.enableSuperuserAccess` is true, read from
  `spec.superuserSecret.name` or `<cluster>-superuser`
- declarative roles under `spec.managed.roles` that can log in and have a
  `passwordSecret`; these also appear in the interactive user picker

Any other `--user` is looked up in `<cluster>-<user>`.

Zalando Postgres Operator:

```text
acid.zalan.do/v1 postgresql
```

For each Zalando cluster:

```text
RW service: <cluster>
User secret: <username>.<cluster>.credentials.postgresql.acid.zalan.do
```

The first database in `spec.databases`, sorted by name, is used as the default database. Its owner is used as the default user. If `spec.databases` is empty, the first prepared database in `spec.preparedDatabases`, sorted by name, is used. If no database is available, the first user in `spec.users`, sorted by name, is used. Cross-namespace user notation like `appspace.db_user` is supported for the documented default secret naming convention. When `--database` selects a database with a known owner and `--user` is not set, that owner is used as the default user.

## Permissions

`kpg` needs these Kubernetes permissions in the namespaces it works with:

| Resource | Verbs | Used for |
|---|---|---|
| `clusters.postgresql.cnpg.io` | list | discovery |
| `postgresqls.acid.zalan.do` | list | discovery |
| `secrets` | get | credentials; `kpg list` shows spec values when denied |
| `secrets` | list | Zalando user discovery; skipped when denied |
| `services` | get | locating the read-write service |
| `pods` | list, get | choosing the pod to forward to |
| `endpointslices.discovery.k8s.io` | list | services without a selector |
| `pods/portforward` | create | the tunnel |
| `namespaces` | list | `-n` shell completion only |

API discovery (`GET /apis/<group>/<version>`) is available to every
authenticated user by default. A provider whose list is forbidden is skipped
when another provider works; when every provider is forbidden, `kpg` says so
and suggests `-n`.

## Development

```sh
just check
just coverage
just coverage-html
just fix
just vuln
just cross
just acceptance app/app-db
just release-snapshot
just release-snapshot-signed
```

The CLI is built with Cobra. Kubernetes access uses `client-go`: dynamic clients for provider CRDs, typed core clients for Secrets/Services/Pods/Namespaces, and client-go port-forwarding (WebSocket with SPDY fallback, like `kubectl`) against the selected pod behind each provider's read-write service. Provider rules live in separate files under `internal/kube`.

`just check` runs tests, `go vet`, `golangci-lint` (v2.14.0, the release CI
pins; building v2.13.x from source with `go run` fails to read Go 1.27
packages), and `go fix -diff`.
The interactive pickers can be exercised on a real terminal with the
`manual` build tag: `go test -c -tags manual -o picker.test ./internal/kpg/`
and then `./picker.test -test.run TestManualTargetPicker`.
`just acceptance` requires a working kube context, access to the target cluster, and `psql` on PATH.
`just release-snapshot` requires GoReleaser and writes unsigned local release artifacts to `dist/`.
`just release-snapshot-signed` also requires pqsign and expects `PQSIGN_SECRET_KEY_PATH` plus `PQSIGN_PASSWORD`.

CI runs the same checks on Linux, the test suite with the race detector on
Linux and macOS, a cross-compile of every release target, and `govulncheck`.
Release notes are grouped from commit prefixes: `feat:` and `fix:` get their
own sections, `docs:`, `test:`, `chore:`, and `ci:` are left out.

## Completion

Shell completion is provided by Cobra:

```sh
kpg completion zsh
kpg completion bash
kpg completion fish
```

Completions include:

- `kpg -c <TAB>` for kubeconfig contexts
- `kpg -n <TAB>` for namespaces in the selected context
- `kpg connect <TAB>` for current Postgres targets

Namespace filtering is honored, for example `kpg connect -n app <TAB>`.

For a one-off zsh session:

```sh
source <(kpg completion zsh)
```

For persistent zsh completion:

```sh
just install-completion-zsh
```

Then ensure your `~/.zshrc` has the completion directory in `fpath` before `compinit`:

```sh
fpath=("$HOME/.zsh/completions" $fpath)
autoload -Uz compinit
compinit
```

Open a new shell, or run:

```sh
exec zsh
```

## License

MIT
