# soracom-beam Helm chart

Receives data that SORACOM Beam forwards from the Unified Endpoint, stores it
in MongoDB and shows it in Grafana.

```
device ──> Unified Endpoint ──> SORACOM Beam ──HTTPS──> Cloudflare ──tunnel──> Ingress (cloudflare-tunnel)
                                                                                  │ /beam/*
                                                                                  ▼
                       Grafana ──(Infinity, :8081 /api/v1)──> server :8080 ──> MongoDB (Longhorn PVC)
```

| Component | Kind | Notes |
|---|---|---|
| server | Deployment + Service | `:8080` Beam ingest (public via Ingress), `:8081` read-only query API (cluster only) |
| MongoDB | StatefulSet (`mongo:8.0`) | Longhorn PVC. Time series collection `soracom.messages` |
| Grafana | Deployment | Infinity data source and the "SORACOM Beam" dashboard are provisioned |
| Ingress | `ingressClassName: cloudflare-tunnel` | Routes only `/beam/` to the server |
| NetworkPolicy | | MongoDB ← server only; query API ← Grafana only |

## Prerequisites

- **Longhorn**, with a StorageClass named `longhorn` (`mongodb.persistence.storageClass`).
- **Cloudflare Tunnel Ingress Controller**, with IngressClass `cloudflare-tunnel`
  (`ingress.className`). It creates the public hostname `ingress.host` on Cloudflare.
- **Node CPU.** MongoDB 5.0+ needs x86-64 with AVX, or ARMv8.2-A. A Raspberry Pi 5
  works; a Raspberry Pi 4 does not.
- **Internet egress for Grafana.** It downloads the Infinity plugin on startup.
- **The image** `ghcr.io/<owner>/<repo>/server`, built by `.github/workflows/image.yml`.
  If the GHCR package is private, create a pull secret and set
  `server.imagePullSecrets`.

## Install

1. Create a pre-shared key in the SORACOM console (Security → Credential store →
   pre-shared key). Use the same value for `server.beam.signatureKey`.
2. Install:

   ```sh
   helm upgrade --install soracom-beam charts/soracom-beam \
     --namespace soracom --create-namespace \
     --set ingress.host=beam.example.com \
     --set server.image.repository=ghcr.io/<owner>/<repo>/server \
     --set server.beam.signatureKey='<pre-shared key>'
   ```

   Passwords for MongoDB and Grafana are generated and stored in the Secret
   `soracom-beam`. That Secret is kept on `helm uninstall`, because the MongoDB
   volume was initialized with them. To manage secrets yourself, set
   `existingSecret` and provide these keys: `mongodb-root-password`,
   `mongodb-password`, `beam-signature-key`, `grafana-admin-password`.

   **GitOps (Argo CD / Flux) or `helm template`:** password generation uses Helm's
   `lookup`, which returns nothing there. Each render would then create new
   passwords that no longer match the initialized MongoDB volume, so always use
   `existingSecret` in those setups.

3. Open Grafana:

   ```sh
   kubectl -n soracom get secret soracom-beam -o jsonpath='{.data.grafana-admin-password}' | base64 -d; echo
   kubectl -n soracom port-forward svc/soracom-beam-grafana 3000:80
   # http://localhost:3000  → Dashboards → SORACOM → SORACOM Beam (user: admin)
   ```

## SORACOM settings (SIM group)

Beam does not tell the server which protocol the device used. Each Beam entry
point therefore forwards to its own path, `https://<ingress.host>/beam/<source>`:

| Beam entry point | Destination URL |
|---|---|
| HTTP エントリポイント | `https://beam.example.com/beam/http` |
| TCP → HTTP/HTTPS | `https://beam.example.com/beam/tcp` |
| UDP → HTTP/HTTPS | `https://beam.example.com/beam/udp` |
| SMS → HTTP/HTTPS | `https://beam.example.com/beam/sms` |
| USSD → HTTP/HTTPS | `https://beam.example.com/beam/ussd` |

For each entry point:

- Turn on **署名ヘッダ付与** (signature header) with the pre-shared key from step 1.
  Requests without a valid signature are rejected with 403. Beam signs only
  the `x-soracom-*` headers, not the body. The server therefore also rejects
  timestamps outside `server.beam.maxClockSkew` (2 minutes) to narrow the
  replay window.
- Turn on the **IMSI** header, plus IMEI, SIM ID or MSISDN if you want them stored.
- Optionally configure the Binary Parser for TCP/UDP. Beam then forwards the
  parsed JSON instead of `{"payload":"<base64>"}`.

Then enable Beam as a Unified Endpoint destination for the group, and send data
with `cmd/client` from the Pi.

## What gets stored

One document per message in the time series collection, with `time` taken from
`x-soracom-timestamp`:

```json
{ "time": "...", "meta": { "imsi": "...", "source": "sms" },
  "data": { "cpuTemp": 48.3, "load1": 0.12, "uptime": 3600 } }
```

- **`data`:** JSON objects, and `key=value,...` text with numeric values (the
  client's SMS/USSD format).
- **`text`:** any other text.
- **`raw`:** binary.

## Values

See [values.yaml](values.yaml). Common ones:

| Key | Default | |
|---|---|---|
| `ingress.host` | `beam.example.com` | Public hostname for Beam |
| `server.beam.signatureKey` | `""` | Required unless `server.beam.allowUnsigned=true` |
| `server.beam.maxClockSkew` | `2m` | Replay window for `x-soracom-timestamp` |
| `server.image.tag` | `main` | Pin to `sha-<short>` for reproducible deploys |
| `mongodb.persistence.size` | `10Gi` | |
| `grafana.persistence.enabled` | `false` | Keep UI-made changes on a Longhorn PVC |
| `grafana.ingress.enabled` | `false` | Put Cloudflare Access in front if you enable it |
