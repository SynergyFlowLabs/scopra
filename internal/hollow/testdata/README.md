# Hollow test data

`podman-info-v4.0.0-docs.json` is copied verbatim from the `podman info --format json` example in Podman's documentation. Podman is licensed under the Apache License 2.0.

| | |
|---|---|
| Repository | `containers/podman` |
| File | `docs/source/markdown/podman-info.1.md` |
| Tag | `v6.1.4` |
| Lines | 175 to 325 |
| SHA-256 | `947038d38dfc0a0853b215ab959d1f40e611214547b240d239dbbb775e96c351` |

It records Podman 4.0.0 running rootless on cgroups v2 with **no** delegated cgroup controllers (`"cgroupControllers": []`). ADR-010's preflight therefore refuses it.

The tests derive other cases from it by changing single fields.
