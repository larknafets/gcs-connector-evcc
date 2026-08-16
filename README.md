# gcs-connector-evcc

Der **evcc-Connector** für [Green Charging Society (GCS)](https://github.com/larknafets/gcs-platform) — ein Peer2Peer-Netzwerk zum kostenfreien Tausch von Ladestrom zwischen Wallbox-Besitzern.

Wer bereits [evcc](https://evcc.io) zur Steuerung seiner Wallbox einsetzt, muss die geladenen kWh nicht manuell in GCS eintragen: Dieser Connector läuft lokal beim Gastgeber, liest abgeschlossene Ladesessions automatisch aus evcc aus und synchronisiert sie mit der GCS-Plattform.

Der Connector ist **optional** — ohne ihn funktioniert GCS weiterhin über manuelle Eingabe der geladenen kWh.

## Funktionsweise

1. Der Connector läuft als eigenständiger Prozess (Daemon) neben evcc und pollt in einem konfigurierbaren Takt (`sync_interval_minutes`, Default 60 Minuten) die evcc-API nach neuen, abgeschlossenen Ladesessions. Optional lässt sich zusätzlich ein Webhook aktivieren, über den evcc einen Sync-Durchlauf sofort nach Ladeende anstößt, statt auf den nächsten Takt zu warten (siehe [Sofort-Sync per evcc-Webhook](#sofort-sync-per-evcc-webhook)).
2. Jede neue Session wird auf das GCS-Payload-Format gemappt und einzeln per `POST` an die GCS-Connector-API gesendet (authentifiziert über API-Key/Secret).
3. evcc's Feld `solarPercentage` wird als `clean_percentage` übertragen. **Preis- und CO₂-Daten aus evcc werden nie an GCS weitergegeben** — sie verlassen den Connector nicht (GCS ist explizit keine Bezahlplattform).
4. Bereits gesendete Sessions werden lokal in einer `state.json` vermerkt (Watermark), damit nichts doppelt gesendet wird — GCS erkennt Duplikate serverseitig zusätzlich ab.
5. Bei Verbindungsproblemen (evcc oder GCS nicht erreichbar, Rate-Limit) wird nur gewarnt und beim nächsten Takt automatisch erneut versucht — kein Absturz, keine manuelle Eingriffe nötig.
6. Fahrzeuge oder Ladepunkte lassen sich über `ignore_vehicles`/`ignore_loadpoints` von der Synchronisation ausschließen (z. B. ein privater Zweitwagen oder ein Nicht-EV-Ladepunkt).

## Installation

### Fertiges Binary (empfohlen)

Binaries für Windows (amd64), macOS (amd64 und Apple Silicon/arm64) und Linux (amd64) sowie Raspberry Pi (ARMv6/ARMv7/aarch64) werden bei jedem Release automatisch gebaut und stehen unter [Releases](https://github.com/larknafets/gcs-connector-evcc/releases) zum Download bereit. Einfach das passende Archiv herunterladen, entpacken und die `gcs-connector`-Binary an einen Ort deiner Wahl legen.

### Docker

Alternativ steht ein Multi-Arch-Image (`linux/amd64`, `linux/arm64`, `linux/arm/v7`) über die GitHub Container Registry bereit — praktisch, wenn der Connector auf demselben Host wie evcc per Docker Compose mitlaufen soll:

```bash
docker pull ghcr.io/larknafets/gcs-connector-evcc:latest
```

Der Container erwartet `.env` und `state.json` unter `/config` — dieses Verzeichnis solltest du auf ein persistentes Host-Verzeichnis mounten:

```bash
docker run -d \
  --name gcs-connector \
  -v $(pwd)/gcs-connector-data:/config \
  ghcr.io/larknafets/gcs-connector-evcc:latest
```

Vor dem ersten Start muss unter `gcs-connector-data/.env` eine Config vorhanden sein (siehe [Konfiguration](#konfiguration)) — entweder manuell angelegt oder über den Setup-Wizard erzeugt:

```bash
docker run -it --rm \
  -v $(pwd)/gcs-connector-data:/config \
  ghcr.io/larknafets/gcs-connector-evcc:latest init
```

#### Docker Compose

Für den Dauerbetrieb (automatischer Neustart bei Absturz/Reboot) eignet sich Docker Compose besser als ein einzelner `docker run`-Aufruf:

```yaml
services:
  gcs-connector:
    image: ghcr.io/larknafets/gcs-connector-evcc:latest
    container_name: gcs-connector
    restart: unless-stopped
    volumes:
      - ./gcs-connector-data:/config
```

`docker compose up -d` startet den Connector im Hintergrund; `.env` muss vorher unter `gcs-connector-data/.env` existieren (siehe oben, `init`-Aufruf). Läuft evcc selbst ebenfalls per Docker Compose auf demselben Host, am einfachsten beide Services im selben Compose-Projekt (oder im selben externen Netzwerk) betreiben und `evcc_base_url` auf den evcc-Servicenamen statt eine IP-Adresse setzen, z. B.:

```yaml
services:
  evcc:
    image: evcc/evcc:latest
    container_name: evcc
    restart: unless-stopped
    # ... evcc-eigene Konfiguration ...

  gcs-connector:
    image: ghcr.io/larknafets/gcs-connector-evcc:latest
    container_name: gcs-connector
    restart: unless-stopped
    depends_on:
      - evcc
    volumes:
      - ./gcs-connector-data:/config
```

`evcc_base_url` wäre dann `http://evcc:7070`, da beide Services im selben Compose-Netzwerk automatisch per Servicename erreichbar sind.

### Aus dem Quellcode bauen

```bash
git clone https://github.com/larknafets/gcs-connector-evcc.git
cd gcs-connector-evcc
go build -o gcs-connector ./cmd/gcs-connector
```

Benötigt Go 1.25 oder neuer.

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
| `sync_interval_minutes` | nein | Sync-Takt in Minuten. Default: `60` |
| `ignore_vehicles` | nein | Kommagetrennte Liste von evcc-Fahrzeugnamen, die nicht synchronisiert werden sollen |
| `ignore_loadpoints` | nein | Kommagetrennte Liste von evcc-Ladepunktnamen, die nicht synchronisiert werden sollen |
| `debug` | nein | `true`/`false` — ausführliches Logging inkl. HTTP-Request-Details |
| `log_file` | nein | Pfad zur Log-Datei; leer = Ausgabe auf stdout |
| `webhook_port` | nein | Aktiviert den Webhook-Listener (siehe [Sofort-Sync per evcc-Webhook](#sofort-sync-per-evcc-webhook)) auf diesem Port; leer = deaktiviert |
| `webhook_secret` | nur mit `webhook_port` | Bearer-Token, das der Webhook-Aufruf mitschicken muss |

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

### Sofort-Sync per evcc-Webhook

Ohne weitere Konfiguration wartet eine gerade abgeschlossene Ladesession bis zu `sync_interval_minutes` (Default 60), bevor sie zu GCS übertragen wird. Wer es eiliger hat, kann evcc per [Messaging](https://docs.evcc.io/docs/reference/configuration/messaging/) so einrichten, dass es beim `stop`-Event (Ladevorgang beendet) einen Webhook auf dem Connector aufruft, der sofort einen Sync-Durchlauf anstößt — der reguläre `sync_interval_minutes`-Takt läuft parallel als Fallback weiter, falls der Webhook mal nicht ankommt.

Der Webhook triggert dabei nur einen normalen Sync-Durchlauf (der Connector fragt wie gewohnt die evcc-Session-API ab); er überträgt selbst keine Ladedaten. Damit bleiben Watermark (`state.json`), Duplikat-Erkennung und Retry-Verhalten unverändert erhalten.

**1. Webhook im Connector aktivieren** — in der `.env`:

```
webhook_port="8080"
webhook_secret="ein-langes-zufälliges-secret"
```

Der Connector öffnet dann `POST http://<connector-host>:8080/sync`, das mit einem Bearer-Token (`webhook_secret`) abgesichert ist. Bei Docker/Docker Compose muss der Port zusätzlich veröffentlicht werden, z. B. `-p 8080:8080` bzw. im Compose-Service:

```yaml
services:
  gcs-connector:
    # ...
    ports:
      - "8080:8080"
```

**2. evcc für den `stop`-Event einen HTTP-Aufruf konfigurieren** — in der `evcc.yaml`:

```yaml
messaging:
  events:
    stop:
      title: ""
      msg: ""
  services:
    - type: custom
      encoding: none
      send:
        source: http
        uri: http://<connector-host>:8080/sync
        method: POST
        headers:
          Authorization: "Bearer ein-langes-zufälliges-secret"
        body: "{{.send}}"
```

`title`/`msg` können leer bleiben, da der Connector den Request-Body nicht auswertet — er reicht als reines "jetzt syncen"-Signal. Läuft evcc selbst per Docker Compose, `<connector-host>` entsprechend auf den Servicenamen des Connector-Containers setzen (analog zu `evcc_base_url` oben).

## Entwicklung

```bash
go build ./...
go vet ./...
go test ./... -race
```

Die Tests laufen vollständig gegen `httptest`-Doppel für evcc und die GCS-API — kein Netzwerkzugriff, keine laufende evcc-/GCS-Instanz nötig.

## Lizenz

[MIT](LICENSE)
