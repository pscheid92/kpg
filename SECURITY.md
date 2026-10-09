# Security policy

## Supported versions

Only the latest release receives fixes. Upgrade before reporting an issue
that may already be addressed.

## Reporting a vulnerability

Please do not open a public issue for security problems. Use GitHub's private
vulnerability reporting for this repository instead:
<https://github.com/pscheid92/kpg/security/advisories/new>.

Include the kpg version, the operator (CloudNativePG or Zalando Postgres
Operator), and steps to reproduce. You will get an acknowledgement within a
few days and a fix or a workaround as soon as one is available.

## What kpg touches

kpg reads operator resources, services, pods, endpoint slices, and the
credentials secrets of the clusters you connect to, and opens a local
port-forward. Passwords are only ever placed in the environment of the child
process or printed when you ask for `--output`. The state file stores the last
target's namespace and cluster, never credentials.

## Verifying releases

Release archives and `checksums.txt` are signed with
[pqsign](https://github.com/pscheid92/pqsign); each has a `.pqsig` file and
the public key is committed as `release.key.pub`. Releases also carry a GitHub
build provenance attestation that you can check with:

```sh
gh attestation verify kpg_<version>_<os>_<arch>.tar.gz --owner pscheid92
```
