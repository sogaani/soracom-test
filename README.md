# SORACOM demos (Go)

Demo programs for SORACOM services, using a Raspberry Pi 5 with a SORACOM Air
(cellular) SIM.

| Program | Runs on | Status |
|---|---|---|
| `cmd/client` | Raspberry Pi | Sends data to the Unified Endpoint over every Air for Cellular entry point |
| `cmd/server` | Kubernetes ([Helm chart](charts/soracom-beam/README.md)) | Receives SORACOM Beam data, stores it in MongoDB, Grafana dashboard |

## Server

`cmd/server` receives Beam requests on `POST /beam/{http,tcp,udp,sms,ussd}`,
verifies the Beam signature, and stores each message in MongoDB. It also
serves a read-only JSON API for Grafana's Infinity data source on a separate,
cluster-internal port. GitHub Actions builds the image
(`ghcr.io/<owner>/<repo>/server`, amd64 + arm64). Deployment and the SORACOM
console settings are described in [charts/soracom-beam/README.md](charts/soracom-beam/README.md).

## Client

The [Unified Endpoint](https://users.soracom.io/ja-jp/docs/unified-endpoint/)
receives data and forwards it to whatever the SIM group enables (Harvest Data,
Beam, Funnel, Funk). The client covers each
[entry point](https://users.soracom.io/ja-jp/docs/unified-endpoint/entry-points/):

| Method | Entry point | Notes |
|---|---|---|
| `http` | `http://uni.soracom.io` port 80 / 8888 | Processed by `Content-Type`. Supports `x-soracom-timestamp` and keep-alive |
| `tcp`  | `uni.soracom.io:23080` | Binary. `-binary-format` wraps it in SORACOM Binary Format v1 |
| `udp`  | `uni.soracom.io:23080` | Binary |
| `sms`  | number `901001` | Text, max 140 bytes. Needs an SMS capable plan |
| `ussd` | `*901001*<text>#` | GSM 7-bit text only, max 182 chars total. Needs a USSD capable plan (e.g. plan01s) |

`uni.soracom.io` only resolves on the SORACOM network, so run the client on the
Pi over the Air connection (or use `-host` to point at a local test server).

### Build and deploy

```sh
make build-pi                          # bin/client-linux-arm64
make deploy PI_HOST=pi@raspberrypi.local
```

### SORACOM setup

1. Put the SIM in a group and enable **SORACOM Harvest Data** (simplest way to
   see the data) and/or Beam/Funnel/Funk.
2. Optional: set the Unified Endpoint response format (group settings) to
   **Unified** to see every destination's result.
3. For `tcp -binary-format`, enable Binary Format v1 for the Unified Endpoint TCP
   entry point. For `-binary`, set the Binary Parser format to
   `cpuTemp:0:int:16:/10 load1:2:uint:16:/100 uptime:4:uint:32`.

### Usage

With no `-data`/`-hex`, the client sends a sample of the Pi's CPU temperature,
load average and uptime.

```sh
./client http                                   # JSON sample
./client http -port 8888 -content-type text/plain -data 'hello'
./client http -timestamp -count 5 -interval 10s # keep-alive, 5 requests
./client tcp                                    # JSON sample over raw TCP
./client tcp -binary -binary-format             # 8 byte sample in a Binary Format v1 frame
./client udp -hex 0102030405
./client all                                    # http + tcp + udp once each

sudo ./client sms                               # via ModemManager (mmcli)
sudo ./client ussd -data 'cpuTemp=21.5'
sudo ./client sms -backend at -device /dev/ttyUSB2   # raw AT commands
```

The `at` backend needs exclusive access to the AT port, so stop ModemManager
first (`sudo systemctl stop ModemManager`), which also drops the data
connection.
