# gc-connector-evcc

Der **evcc-Connector** für [Green Charging Society (GCS)](https://github.com/larknafets/gc-platform) — ein Peer2Peer-Netzwerk zum kostenfreien Tausch von Ladestrom zwischen Wallbox-Besitzern.

Wer bereits [evcc](https://evcc.io) zur Steuerung seiner Wallbox einsetzt, muss die geladenen kWh nicht manuell in GCS eintragen: Dieser Connector läuft lokal beim Gastgeber, liest abgeschlossene Ladesessions automatisch aus evcc aus und synchronisiert sie mit der GCS-Plattform.

Der Connector ist **optional** — ohne ihn funktioniert GCS weiterhin über manuelle Eingabe der geladenen kWh.

## Funktionsweise

1. Der Connector läuft als eigenständiger Prozess (Daemon) neben evcc und pollt in einem konfigurierbaren Takt (`sync_interval_minutes`) die evcc-API nach neuen, abgeschlossenen Ladesessions.
2. Jede neue Session wird auf das GCS-Payload-Format gemappt und einzeln per `POST` an die GCS-Connector-API gesendet (authentifiziert über API-Key/Secret).
3. evcc's Feld `solarPercentage` wird als `clean_percentage` übertragen. **Preis- und CO₂-Daten aus evcc werden nie an GCS weitergegeben** — sie verlassen den Connector nicht (GCS ist explizit keine Bezahlplattform).
4. Bereits gesendete Sessions werden lokal in einer `state.json` vermerkt (Watermark), damit nichts doppelt gesendet wird — GCS erkennt Duplikate serverseitig zusätzlich ab.
5. Bei Verbindungsproblemen (evcc oder GCS nicht erreichbar, Rate-Limit) wird nur gewarnt und beim nächsten Takt automatisch erneut versucht — kein Absturz, keine manuelle Eingriffe nötig.
6. Fahrzeuge oder Ladepunkte lassen sich über `ignore_vehicles`/`ignore_loadpoints` von der Synchronisation ausschließen (z. B. ein privater Zweitwagen oder ein Nicht-EV-Ladepunkt).

## Installation

### Fertiges Binary (empfohlen)

Binaries für Windows, macOS und Linux (jeweils amd64) sowie Raspberry Pi (ARMv6/ARMv7/aarch64) werden bei jedem Release automatisch gebaut und stehen unter [Releases](https://github.com/larknafets/gc-connector-evcc/releases) zum Download bereit. Einfach das passende Archiv herunterladen, entpacken und die `gcs-connector`-Binary an einen Ort deiner Wahl legen.

### Docker

Alternativ steht ein Multi-Arch-Image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) über die GitHub Container Registry bereit — praktisch, wenn der Connector auf demselben Host wie evcc per Docker Compose mitlaufen soll:

```bash
docker pull ghcr.io/larknafets/gc-connector-evcc:latest
```

Der Container erwartet `.env` und `state.json` unter `/config` — dieses Verzeichnis solltest du auf ein persistentes Host-Verzeichnis mounten:

```bash
docker run -d \
  --name gcs-connector \
  -v $(pwd)/gcs-connector-data:/config \
  ghcr.io/larknafets/gc-connector-evcc:latest
```

Vor dem ersten Start muss unter `gcs-connector-data/.env` eine Config vorhanden sein (siehe [Konfiguration](#konfiguration)) — entweder manuell angelegt oder über den Setup-Wizard erzeugt:

```bash
docker run -it --rm \
  -v $(pwd)/gcs-connector-data:/config \
  ghcr.io/larknafets/gc-connector-evcc:latest init
```

### Aus dem Quellcode bauen

```bash
git clone https://github.com/larknafets/gc-connector-evcc.git
cd gc-connector-evcc
go build -o gcs-connector ./cmd/gcs-connector
```

Benötigt Go 1.23 oder neuer.

## Erststart / Konfiguration

Der einfachste Weg ist der interaktive Setup-Wizard:

```bash
gcs-connector init
```

Er fragt alle nötigen Werte der Reihe nach ab, testet die Erreichbarkeit von GCS-Server und evcc-Instanz, und schreibt am Ende eine `.env`-Datei ins aktuelle Verzeichnis. Wird `gcs-connector init` erneut aufgerufen, erkennt er eine bestehende Config und fragt vor dem Überschreiben nach — die aktuellen Werte sind dabei bereits vorausgefüllt, der Wizard eignet sich also auch zum nachträglichen Ändern einzelner Werte.

Wird der Connector ohne vorhandene Config gestartet, weist er auf `gcs-connector init` hin, statt stillschweigend zu scheitern.

### `.env`-Variablen

| Variable | Pflicht | Beschreibung |
|---|---|---|
| `api_base_url` | ja | Adresse der GCS-Instanz (zentral oder selbst gehostet) |
| `evcc_base_url` | ja | Adresse der lokalen evcc-Instanz, z. B. `http://192.168.1.50:7070` |
| `api_key` | ja | Connector-API-Key (aus dem GCS-Profil, „Connector/API-Zugang“) |
| `api_secret` | ja | Zugehöriges Secret |
| `site_name` | ja | Bezeichnung dieser Connector-Instanz, wird bei jeder Ladung mitgeschickt |
| `sync_interval_minutes` | ja | Sync-Takt in Minuten |
| `ignore_vehicles` | nein | Kommagetrennte Liste von evcc-Fahrzeugnamen, die nicht synchronisiert werden sollen |
| `ignore_loadpoints` | nein | Kommagetrennte Liste von evcc-Ladepunktnamen, die nicht synchronisiert werden sollen |
| `debug` | nein | `true`/`false` — ausführliches Logging inkl. HTTP-Request-Details |
| `log_file` | nein | Pfad zur Log-Datei; leer = Ausgabe auf stdout |

Der Pfad zur `.env`-Datei lässt sich statt des Default-Arbeitsverzeichnisses auch über `--config <pfad>` oder die Umgebungsvariable `GCS_CONNECTOR_CONFIG` festlegen.

## Nutzung

**Im Vordergrund starten** (Daemon-Betrieb, läuft dauerhaft und synct im konfigurierten Takt — der erste Sync passiert sofort beim Start):

```bash
gcs-connector
```

Zum Beenden reicht Strg+C bzw. ein `SIGTERM` — ein gerade laufender Sync-Takt wird dabei noch zu Ende gebracht statt hart abgebrochen.

**Einmal-Test ohne zu senden** (zeigt, welche Sessions beim nächsten echten Sync übertragen würden, inklusive welche davon serverseitig schon als vorhanden gelten, ohne dass dabei etwas an GCS gesendet oder lokaler Zustand verändert wird):

```bash
gcs-connector --dry-run
```

**Andere Config-Datei verwenden:**

```bash
gcs-connector --config /pfad/zu/anderer.env
```

### Als Hintergrunddienst

Der Connector bringt seine eigene Ablauf-Schleife mit (kein externer Cron-Job nötig) — für den Dauerbetrieb empfiehlt sich trotzdem eine Prozess-Überwachung, die ihn bei einem Absturz oder Neustart des Rechners automatisch wieder hochfährt (z. B. ein systemd-Service unter Linux oder die Docker-Variante mit `--restart unless-stopped`).

## Entwicklung

```bash
go build ./...
go vet ./...
go test ./... -race
```

Die Tests laufen vollständig gegen `httptest`-Doppel für evcc und die GCS-API — kein Netzwerkzugriff, keine laufende evcc-/GCS-Instanz nötig.

## Lizenz

[MIT](LICENSE)
