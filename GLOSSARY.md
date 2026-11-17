# gcs-connector-evcc

Bridges a local evcc instance to the GCS platform: reads finished charging sessions from evcc and reports them to GCS as charges, so a host doesn't have to enter charged kWh manually.

## Language

**Session**:
A finished charging event as evcc reports it (vehicle, loadpoint, start/end time, charged energy, solar percentage). The connector's input; never sent to GCS as-is.
_Avoid_: Charge (that's the GCS-side representation), transaction

**Charge**:
The GCS-side record of one Session, posted to the GCS Connector API. Carries only energy, timing, loadpoint/vehicle names and green percentage, never evcc's price or CO2 data, since GCS is explicitly not a payment platform.
_Avoid_: Session, payload (payload is the wire format, not the concept)

**Watermark**:
The timestamp of the most recently sent Session, persisted locally in `state.json`. A sync cycle only considers Sessions finished after the watermark, so nothing is sent twice by the connector itself; GCS also deduplicates server-side as a second line of defense.
_Avoid_: Cursor, checkpoint

**Sync cycle**:
One full pass: fetch Sessions from evcc, filter to eligible ones, map each to a Charge, send it to GCS, advance the watermark. Runs on a timer (`sync_interval_minutes`) or on demand via the webhook.
_Avoid_: Sync run, poll

**Loadpoint**:
An evcc charging point (e.g. a wallbox). Sessions belong to exactly one Loadpoint; a Loadpoint can be excluded from syncing via `ignore_loadpoints`.

**Vehicle**:
The evcc vehicle associated with a Session. Can be excluded from syncing via `ignore_vehicles` (e.g. a private second car).

**Green percentage**:
The share of a Session's charged energy sourced from solar, taken from evcc's `solarPercentage` and reported to GCS unchanged. The only "quality" signal GCS receives about a Charge.
_Avoid_: Solar percentage (that's evcc's field name, not the GCS concept)

**Connector**:
This application. Runs as a standalone daemon next to evcc; optional, since GCS also accepts charges entered manually.
_Avoid_: Bridge, sync service
