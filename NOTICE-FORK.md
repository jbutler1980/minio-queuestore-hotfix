# Modification notice

This repository is a **modified version of MinIO**. It is **not** an official
MinIO release and is **not affiliated with, sponsored by, or endorsed by
MinIO, Inc.**

MinIO is a trademark of MinIO, Inc. The name is used here only to identify the
upstream software this fork is derived from (nominative use). No trademark
rights are claimed or granted.

## Provenance

| | |
|---|---|
| Upstream project | https://github.com/minio/minio (archived read-only, last push 2026-04-24) |
| Forked from tag | `RELEASE.2025-06-13T11-33-47Z` |
| Upstream commit | `a6c538c5a113a588d49b4f3af36ae3046cfa5ac6` |
| Corresponds to image | `quay.io/minio/minio:RELEASE.2025-06-13T11-33-47Z-cpuv1` (`sha256:ad8a8318847c0bb7dcfe59caf407f12fad6f387bb66386ee772c4704f0a48d50`) |
| Modification date | 2026-08-02 |

## What was changed

Exactly one behavioural change, in `internal/store/queuestore.go`:

```go
if len(items) == 0 {
    return item, os.ErrNotExist
}
```

`QueueStore.Get()` indexed `items[0]` without checking that `GetMultiple()`
returned anything. `GetMultiple()` returns `(nil, nil)` — an empty slice and no
error — whenever jsoniter's `decoder.More()` is false on the first call.
`GetRaw()`'s only content guard is `len(raw) == 0`, so a file of NUL bytes has a
non-zero length yet decodes to zero items, because jsoniter's `nextToken()`
returns byte `0x00`, which it cannot distinguish from its EOF sentinel. `Get()`
then panicked with `index out of range [0] with length 0`, killing the whole
MinIO process during event/audit store replay.

This restores the guard removed upstream in
`cefc43e4daa4cbb490ef6726ea374e26a93eb85e` ("simplify the Get()/GetMultiple()
re-use GetRaw() for both", 2024-11-07).

A regression test was added at `internal/store/queuestore_corrupt_test.go`.
Upstream had no coverage for empty or zero-item payloads.

Because upstream is archived, this change cannot be submitted upstream.

## Binary distribution

A container image built from this source is published at:

```
docker.io/jbutler1980/minio-queuestore-hotfix:RELEASE.2025-06-13T11-33-47Z-cpuv1-hotfix.98870401
```

That image is the upstream `-cpuv1` image with only `/usr/bin/minio` replaced;
the UBI 8.10 micro base, `mc`, static `curl`, CA bundle, entrypoint and
`/licenses` are inherited unchanged from the pinned upstream digest.

Build parameters (matching upstream's release build):

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
go build -tags kqueue -trimpath --ldflags "<gen-ldflags.go output>"
Go toolchain: go1.24.4 (same as the upstream release binary)
```

Note: the `-cpuv1` suffix on upstream's images denotes the **UBI8** base image,
not a `GOAMD64` setting — upstream's `Dockerfile.release.old_cpu` downloads the
same `dl.min.io` binary as `Dockerfile.release`. Go's default `GOAMD64` is `v1`
regardless; it is set explicitly above only for reproducibility.

## License

MinIO is licensed under the **GNU Affero General Public License v3.0**.
This fork is distributed under the same licence. All upstream copyright
notices, `LICENSE` and `CREDITS` are retained unmodified.

Copyright (c) 2015-2025 MinIO, Inc.
