# Xylon compose worker

Sibling Go service that remuxes Xylon room recording stems (`.mjr`) into MP4,
splices restart segments with a closed wall-clock gap, and sweeps retention.
Live layout artifacts (`kind: "layout"`, `.mp4`) are copied beside the segment
output and are never passed to `janus-pp-rec`.

Xylon posts signed `recording.completed` / `recording.failed` webhooks to this
service (configure `XYLON_CONTROL_WEBHOOK_URL`). Remux never runs inside the
Xylon OTP media path.

## What problem this solves

After an admin stops recording, stems already sit on disk. Customers need an
MP4 without SSH to the SFU node, plus kept stems until TTL, and a continuous
session file when recording restarts (`preceded_by`).

## Run (lab)

```bash
cd xylon-compose-worker
go run ./cmd/compose -config config.example.json
```

Default listen: `127.0.0.1:8094`.

Point Xylon Control at this worker:

```text
webhook_url    = http://127.0.0.1:8094/v1/webhooks/xylon
webhook_secret = lab-webhook-secret   # same as compose + interface config
```

Optional: set `interface_webhook_url` so compose forwards the identical signed
body to the customer interface (lab stand-in for deploy dual-subscribe).

Requires `janus-pp-rec` and `ffmpeg` on `PATH` (or set paths in config) for
real remux. Unit tests always include splice/remux cases and **skip only when**
those binaries are missing. Gap close uses ffmpeg `tpad` + `apad`.

## HTTP API

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/healthz` | `{"ok":true}` |
| POST | `/v1/webhooks/xylon` | Verify `x-xylon-signature`; enqueue compose; `202` |

On remux failure stems are left untouched and `compose_failed.json` is written
under the segment output dir.

## Retention

Env `RECORD_RETENTION_HOURS` overrides config `retention_hours` (default **120**).
Sweeper deletes files under `stem_root` and `out_root` by mtime.

## Config

See `config.example.json`: `listen_addr`, `webhook_secret`, `stem_root`,
`out_root`, `index_path`, `retention_hours`, tool paths, `interface_webhook_url`.

## Lab curl path

1. Start secret store, Xylon (with `XylonRecord.Handler` + webhook_url),
   interface, admin app, and this compose worker.
2. Admin `POST .../record/start` then `.../record/stop`.
3. Check `out_root/<tenant>/<session>/<recording_id>/segment.mp4` and
   `.../session.mp4`.
4. Second start/stop with gap: session MP4 grows with closed silence/black gap.
5. Interface `GET /v1/webhooks/xylon/latest` shows the forwarded event.

Related: [GOLANG_APPS.md](../GOLANG_APPS.md), [RECORD.md](../RECORD.md).
