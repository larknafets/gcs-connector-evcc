---
status: accepted
---

# Under Supervisor, `state.json` lives in the `addon_config` mount, not `/data` — no migration for existing installs

`/data/options.json` is the Supervisor's only way of delivering an add-on's config, and its directory (`/data`) is a private, host-hidden folder swept into Supervisor's own backup/restore handling — not something a user can browse or reason about. Before this change, `internal/state.Store` derived its directory from whatever config source was actually used (`internal/cli/config_load.go`'s `loadConfig`), so under Supervisor `state.json` landed in `/data` purely as a side effect of `options.json` living there too.

We split those two concerns: `options.json`'s location is fixed by Supervisor and can't move, but `state.json` (the sync watermark) is ours to place deliberately. It now lives under the add-on's `addon_config` mount (`map: - addon_config:rw` in `gcs-ha-addons/gcs-connector-evcc/config.yaml`, container path `/config`) — the Supervisor host exposes this per-add-on folder under `app_configs/<slug>`, visible via Samba/SSH/File Editor, matching the same `/config` convention the Docker/Compose install already documents in the connector's README. `loadConfig` takes the Supervisor state directory as an explicit parameter rather than inferring it from `optionsPath`'s directory, so the two config sources (`.env` file vs. Supervisor options) can each pick their own state directory independently — `.env` mode keeps colocating `state.json` next to the `.env` file, unchanged.

Existing Supervisor installs have a `state.json` under `/data` already. We deliberately do **not** ship migration code to copy it into the new `addon_config` mount: the watermark reset causes at most one extra sync cycle re-sending already-synced sessions, and GCS already deduplicates these server-side (see the connector README's `state.json` note) — the existing add-on is early-stage with few installs, so the one-time duplicate-send cost is lower than the complexity of a migration path.

## Reconsider if

The add-on gets wide adoption before this ships (duplicate-send cost at scale might justify a migration step), or GCS's server-side dedup gets removed/weakened.
