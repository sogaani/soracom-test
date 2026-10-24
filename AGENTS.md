# AGENTS.md

Guidance for coding agents working in this repository.

## Project

Go demo programs for SORACOM services. The device is a Raspberry Pi 5
(64-bit Raspberry Pi OS, linux/arm64) with a SORACOM Air for Cellular SIM,
usually managed by NetworkManager/ModemManager. The server that receives data
through SORACOM Beam runs on Kubernetes (Helm chart), stores it in MongoDB
and is visualized with Grafana.

## Layout

```
cmd/client/          CLI run on the Pi: one subcommand per Unified Endpoint entry point
cmd/server/          Beam receiver: :8080 POST /beam/{source} (public), :8081 GET /api/v1 (internal)
internal/unified/    HTTP / TCP / UDP senders, Binary Format v1, response parsing
internal/modem/      SMS / USSD via mmcli (ModemManager) or raw AT commands
internal/payload/    Sample data from the Pi (/sys, /proc) as JSON, text and binary
internal/beam/       Beam signature verification and body parsing per source
internal/store/      MongoDB time series collection (mongo-driver v2)
internal/api/        Read-only query API for Grafana (Infinity data source)
charts/soracom-beam/ Helm chart: server, MongoDB (Longhorn PVC), Grafana, Ingress (cloudflare-tunnel)
.github/workflows/   ci.yml (lint, test, chart checks), image.yml (multi-arch image to GHCR)
```

New programs go under `cmd/<name>/`; shared code under `internal/`.

## Commands

```sh
go test ./...        # unit tests; use local listeners, never the real endpoint
make lint            # golangci-lint run + fmt --diff; must report 0 issues
make fmt             # apply gofumpt + goimports
make build-pi        # GOOS=linux GOARCH=arm64 CGO_ENABLED=0 -> bin/client-linux-arm64
make chart-lint      # helm lint + kubeconform (needs helm, kubeconform)
make image           # local server image
```

End-to-end check without a cluster: run `mongo:8.0` and the server image in
Docker, POST to `/beam/<source>` with a signature computed as described below,
then GET `/api/v1/messages` on :8081.

The development machine is macOS and cannot reach `uni.soracom.io` (it only
resolves on the SORACOM network) and has no modem. Real sends are verified by
the user on the Pi; say so instead of claiming they work.

## Conventions

- Lint config is `.golangci.yml` (golangci-lint v2), adapted from
  https://zenn.dev/23prime/articles/99ee4447cfe688: default linters plus gosec,
  bidichk, errorlint, bodyclose, unconvert, usestdlibvars, modernize,
  exhaustive; formatters gofumpt and goimports. `errcheck` has
  `check-blank: true`, so `_ = f()` is reported too: handle the error (e.g.
  `defer func() { err = errors.Join(err, c.Close()) }()`), or for genuine
  best-effort calls add `//nolint:<linter> // <reason>`. Never use `//nolint`
  without a reason, and fix the code rather than relaxing the config.
- Standard library only, except `go.mongodb.org/mongo-driver/v2` in the
  server. Ask before adding another dependency.
- Stay `CGO_ENABLED=0`-compatible. Shell out to system tools (`mmcli`, `stty`)
  rather than using cgo.
- Hard-coded SORACOM values (hosts, ports, service codes, limits) live as
  named constants with a link to the doc they come from.
- Tests use `httptest` / local TCP and UDP listeners. Code that needs the modem
  or the SORACOM network is not unit tested; keep its parsing/validation in
  pure functions that are.

## SORACOM facts this code relies on

Source of truth: https://users.soracom.io/ja-jp/docs/ (check it before changing
protocol behavior; the docs are in Japanese).

- Unified Endpoint entry points for Air for Cellular
  ([entry-points](https://users.soracom.io/ja-jp/docs/unified-endpoint/entry-points/)):
  - HTTP: `uni.soracom.io` (alias `unified.soracom.io`) ports 80, 8888. POST to
    `/`; the body is processed by `Content-Type`.
  - TCP / UDP: same host, port 23080, processed as binary.
  - SMS to `901001`, USSD `*901001*<text>#`, processed as text.
  - Other service codes: Beam 901011, Funnel 901021, Harvest Data 901031, Funk 901041.
- `x-soracom-timestamp` header (UNIX ms) sets the Harvest Data record time
  (accepted from 40 days in the past to 1 day in the future).
- HTTP keep-alive timeout on the endpoint is 65s.
- TCP/UDP responses are `"{status} {body}"`, e.g. `201`, `200 Hi`. With a
  Custom response format the status may be omitted. With the TCP entry point,
  wait for the response before closing or the data may be discarded.
- Unified response format: `{"result":"ok|ng","detail":{"SoracomHarvest":{"statusCode":201},...}}`,
  status 200 or 207. Funk bodies are base64 with `"encoding":"base64"`.
- SORACOM Binary Format v1 (TCP only, must be enabled per entry point):
  `len(2B, BE) | body | CRC-16/CCITT-FALSE over len+body (2B, BE)`, body
  1–65535 bytes. Doc example: body `010203040506` -> `00060102030405064917`.
- SMS: payload up to 140 bytes; not every plan can send SMS to SORACOM
  (e.g. planP1 cannot).
- USSD: GSM 7-bit basic charset only (no `{}[]~\^|`), DCS 15, max 182 chars
  including `*901001*` and `#`, uplink only, plan dependent (e.g. plan01s yes,
  planX1/planP1 no). A reply arrives only if the destination returns
  `text/plain` GSM 7-bit text.
- Beam forwards to the destination URL configured per entry point. The server
  cannot tell the source protocol from the request, so each entry point uses
  its own path `/beam/{http,tcp,udp,sms,ussd}`. Bodies
  ([tcp-http](https://users.soracom.io/ja-jp/docs/beam/tcp-http/),
  [sms-http](https://users.soracom.io/ja-jp/docs/beam/sms-http/),
  [ussd-http](https://users.soracom.io/ja-jp/docs/beam/ussd-http/)):
  http = the device's body as-is; tcp/udp = `{"payload":"<base64>"}` or the
  Binary Parser/Orbit JSON; sms = `{"payload":"<text>"}`; ussd =
  `{"imsi","ussdDataCodingScheme","ussdString","value"}`.
- Beam signature ([verify-signature](https://users.soracom.io/ja-jp/docs/beam/verify-signature/)):
  `sha256hex(psk + "x-soracom-imei=…x-soracom-imsi=…x-soracom-msisdn=…x-soracom-sim-id=…x-soracom-timestamp=…")`,
  only headers that were sent, in that order; `x-soracom-signature-version: 20151001`.
  Doc example: psk `topsecret`, imei 867612345678901, imsi 295012345678901,
  timestamp 1640962800000 → `83341a7b…24aecba` (tested in `internal/beam`).
- The ingest port is public. Never add unauthenticated endpoints to it, and
  never route the :8081 query API through the Ingress.
- Grafana has no free MongoDB data source (the official one is Enterprise, the
  community one is unsigned), so it reads the server's JSON API through the
  signed Infinity plugin. Dashboards live in `charts/soracom-beam/dashboards/`.
- The Binary Parser format for `payload.Sample.Binary()` is
  `cpuTemp:0:int:16:/10 load1:2:uint:16:/100 uptime:4:uint:32`. Update this
  line and README.md if the encoding changes.
