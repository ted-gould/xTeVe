<div align="center" style="background-color: #111; padding: 100;">
    <a href="https://github.com/ted-gould/xTeVe"><img width="880" height="200" src="src/html/img/logo_b_880x200.jpg" alt="xTeVe" /></a>
</div>
<br>

# xTeVe

## M3U Proxy and EPG aggregator for Plex DVR and Emby Live TV

### This is a fork of <https://github.com/xteve-project/xTeVe>, all credit goes to the original author

Documentation for setup and configuration is [here](https://github.com/ted-gould/xTeVe/blob/main/docs/configuration.md).

---

## Features

### Files

* Merge external M3U files
* Merge external XMLTV files (EPG aggregation)
* Automatic M3U and XMLTV update
* M3U and XMLTV export

#### Channel management

* Filtering streams
* Teleguide timeshift
* Channel mapping
* Channel order
* Channel logos
* Channel categories

#### Streaming

* Buffer with HLS / M3U8 support
* Re-streaming
* Stream Reconnection
* Number of tuners adjustable
* Compatible with Plex / Emby EPG

---

## WebDAV

xTeVe includes a read-only WebDAV server accessible at `/dav/`. This feature allows you to mount your M3U playlists as a file system, which is particularly useful for integrating VOD (Video On Demand) content into media centers like Plex or Emby.

### Structure

The WebDAV file system is organized as follows:

```text
/dav/<M3U Hash>/On Demand/<Group Title>/
```

Inside each group, content is further organized into:

*   **Series:** `Series/<Series Name>/Season <N>/<Episode>`
*   **Individual:** `Individual/<Movie Name>`

Note: WebDAV support is read-only.

---

## Downloads

[![Get it from the Snap Store](https://snapcraft.io/en/dark/install.svg)](https://snapcraft.io/xteve)

## Snap Configuration

When installed as a snap, xTeVe runs as a service on port `8080` and stores its configuration in `/var/snap/xteve/common/config`. The following settings can be changed with `snap set`:

| Key | Description | Default |
| --- | --- | --- |
| `otel-exporter-type` | OpenTelemetry exporter to use: `stdout`, `otlp` (gRPC), or `otlp-http` (HTTP). Leave unset to disable telemetry export. | unset (disabled) |
| `otel-exporter-otlp-endpoint` | Endpoint to send telemetry to when using the `otlp` or `otlp-http` exporter. | |
| `otel-exporter-otlp-headers` | Comma-separated `key=value` headers sent with each OTLP export (e.g. authentication tokens). | |
| `webdav-cache-size` | Maximum number of items kept in the WebDAV file cache. Values above `100000` are capped. | `100` |

Example:

```sh
sudo snap set xteve webdav-cache-size=1000
```

Settings are read when the service starts, so restart it after changing them:

```sh
sudo snap restart xteve
```

To view the current settings, or clear one back to its default:

```sh
sudo snap get xteve
sudo snap unset xteve webdav-cache-size
```

The snap also provides two helper commands:

* `xteve.status` — prints the status of the running xTeVe service.
* `xteve.inactive` — exits successfully if xTeVe has no active streams or other open connections. This is also used by the snap's pre-refresh hook to postpone automatic refreshes while streams are playing.

## Build from source code

### Requirements

* [Go](https://go.dev/dl/) (1.25 or newer)
* [Node.js](https://nodejs.org/en/download/) (which includes `npm`)

### Dependencies

This project uses Go modules and NPM for dependency management.

* Go dependencies are listed in the `go.mod` file and can be downloaded by running `go mod tidy`.
* Node.js dependencies are listed in the `package.json` file and can be installed by running `npm install`.

### Build

The following steps will create the `xteve`, `xteve-inactive`, and `xteve-status` binaries in a new `bin/` directory.

1. **Clone the repository:**
   ```sh
   git clone https://github.com/ted-gould/xTeVe.git
   cd xTeVe
   ```

2. **Install JavaScript dependencies:**
   ```sh
   npm install
   ```

3. **Run the build command:**
   ```sh
   make build
   ```

To enable this feature, go to `Settings -> Streaming` and check the `Enable Stream Retry` box. You can also configure the maximum number of retries and the delay between retries.

---

## OpenTelemetry Tracing

xTeVe supports OpenTelemetry tracing, which allows you to send traces to an observability platform of your choice.

### Configuration

The OpenTelemetry exporter can be configured using `snap set`:

| Key | Description | Default |
| --- | --- | --- |
| `otel-exporter-type` | The type of exporter to use. Supported values are `stdout`, `otlp` (gRPC), and `otlp-http` (HTTP). Leave unset to disable. | unset (disabled) |
| `otel-exporter-otlp-endpoint` | The endpoint to send traces to when using the `otlp` or `otlp-http` exporter. | |
| `otel-exporter-otlp-headers` | Comma-separated `key=value` headers to send with each export when using the `otlp` or `otlp-http` exporter. | |

See [Snap Configuration](#snap-configuration) for all snap settings. Restart the service (`sudo snap restart xteve`) after changing them.

### Example: Exporting to Axiom

To export traces to [Axiom](https://axiom.co), you can use the following configuration:

```sh
sudo snap set xteve otel-exporter-type="otlp"
sudo snap set xteve otel-exporter-otlp-endpoint="https://api.axiom.co"
sudo snap set xteve otel-exporter-otlp-headers="Authorization=Bearer <YOUR_AXIOM_API_TOKEN>,x-axiom-dataset=<YOUR_DATASET_NAME>"
```
