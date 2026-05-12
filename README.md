[![CI](https://github.com/pscheid92/kpg/actions/workflows/ci.yml/badge.svg)](https://github.com/pscheid92/kpg/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/pscheid92/kpg.svg)](https://pkg.go.dev/github.com/pscheid92/kpg)
[![Go Report Card](https://goreportcard.com/badge/github.com/pscheid92/kpg)](https://goreportcard.com/report/github.com/pscheid92/kpg)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

# kpg

`kpg` is a narrow, command-first CLI for connecting to Kubernetes-hosted Postgres databases.

V1 discovers supported Postgres operator resources, reads generated application credentials, opens a foreground port-forward to the read-write service, and exposes `PG*` environment variables for `psql`, `pgcli`, or any Postgres-compatible client.

## Usage

```sh
kpg list
kpg list --output json
kpg connect
kpg connect <cluster|namespace/cluster|substring>
kpg connect <provider>:<namespace>/<cluster>
kpg connect <cluster|namespace/cluster|substring> -- psql
kpg connect <cluster|namespace/cluster|substring> --output shell
kpg last
kpg last -- psql
kpg version
kpg completion <bash|zsh|fish|powershell>
```

Shared flags:

```sh
-c, --context <name>      kube context override
-n, --namespace <name>    restrict discovery/lookup to one namespace
-p, --local-port <port>   use a fixed local port
-o, --output shell|dotenv|json
-h, --help
```

## Install

Requirements:

- Go 1.26 or newer
- A kubeconfig with access to the target cluster
- CloudNativePG or Zalando Postgres Operator resources in the active context
- `psql`, `pgcli`, or another Postgres client when using command mode

```sh
go install github.com/pscheid92/kpg@latest
```

Prebuilt binaries for Linux, macOS, and Windows are attached to GitHub Releases.
Release builds set `kpg version` from the tag, commit, and build date. Releases
are created from tags named `v*`, for example:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Release archives and `checksums.txt` are signed with
[`pqsign`](https://github.com/pscheid92/pqsign). Each signed file has a matching
`.pqsig` asset in the release. Verify an archive with the committed release
public key:

```sh
pqsign verify kpg_0.1.0_linux_amd64.tar.gz -p release.key.pub
```

The release workflow expects these GitHub repository secrets:

- `PQSIGN_SECRET_KEY`: base64-encoded encrypted pqsign secret key file matching `release.key.pub`
- `PQSIGN_PASSWORD`: password for that secret key

In an interactive terminal, `kpg connect` opens a subshell with `PG*` environment values already exported. Exiting that shell closes the tunnel:

```sh
kpg connect app-db
psql
exit
```

When run in an interactive terminal with no target, `kpg connect` opens a searchable target picker before starting the subshell. Type to filter, use arrow keys or `j`/`k` to move, press Enter to connect, or Esc to cancel. In non-interactive use, pass the target explicitly.

If a command is provided after `--`, `kpg` injects the same `PG*` environment values into that command and stops the tunnel when the command exits:

```sh
kpg connect app-db -- psql
kpg connect app-db -- pgcli
```

Use `--output` to print connection values instead of entering a subshell or running a command. In this mode, `kpg connect` keeps the tunnel alive in the foreground until Ctrl-C:

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

Targets can be written as a cluster name, `namespace/cluster`, or a unique substring match. If multiple providers expose the same namespace and cluster, use a provider-qualified target:

```sh
kpg connect cnpg:app/app-db
kpg connect zalando:postgres/acid-main
```

Ambiguous matches fail with candidate suggestions. `kpg list` prints provider information when it is needed to distinguish targets. `kpg list --output json` emits script-friendly target metadata without secrets.

The last successful target is stored at the XDG state path, normally:

```text
~/.local/state/kpg/last.json
```

Only the namespace and cluster are stored. Discovery data and secrets are not cached.

## Discovery

V1 supports CloudNativePG and Zalando Postgres Operator.

By default, discovery tries to list supported resources across namespaces. If
Kubernetes denies an all-namespace provider list, `kpg` retries that provider in
the current kube context namespace. Use `--namespace <name>` to make discovery
strictly namespaced, or pass a namespace-qualified target such as `app/app-db`.

CloudNativePG:

```text
postgresql.cnpg.io/v1 Cluster
```

For each cluster:

```text
RW service: <cluster>-rw
App secret: <cluster>-app
```

Database and user are read from the generated app secret when present. Missing values fall back to `spec.bootstrap.initdb.database` and `spec.bootstrap.initdb.owner`.

Zalando Postgres Operator:

```text
acid.zalan.do/v1 postgresql
```

For each Zalando cluster:

```text
RW service: <cluster>
User secret: <username>.<cluster>.credentials.postgresql.acid.zalan.do
```

The first database in `spec.databases`, sorted by name, is used as the default database. Its owner is used as the default user. If there are no databases, the first user in `spec.users`, sorted by name, is used. Cross-namespace user notation like `appspace.db_user` is supported for the documented default secret naming convention.

## Development

```sh
just check
just coverage
just coverage-html
just acceptance app/app-db
just release-snapshot
just release-snapshot-signed
```

The CLI is built with Cobra. Kubernetes access uses `client-go`: dynamic clients for provider CRDs, typed core clients for Secrets/Services/Pods/Namespaces, and client-go port-forwarding against the selected pod behind each provider's read-write service. Provider rules live in separate files under `internal/kube`.

`just acceptance` requires a working kube context, access to the target cluster, and `psql` on PATH.
`just release-snapshot` requires GoReleaser and writes unsigned local release artifacts to `dist/`.
`just release-snapshot-signed` also requires pqsign and expects `PQSIGN_SECRET_KEY_PATH` plus `PQSIGN_PASSWORD`.

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
