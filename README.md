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

## Downloads

[![Get it from the Snap Store](https://snapcraft.io/en/dark/install.svg)](https://snapcraft.io/xteve)

## Build from source code

### Requirements

* [Go](https://go.dev/dl/) (1.24 or newer)
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
| `otel-exporter-type` | The type of exporter to use. Supported values are `stdout` and `otlp`. | `stdout` |
| `otel-exporter-otlp-endpoint` | The endpoint to send traces to when using the `otlp` exporter. | |
| `otel-exporter-otlp-headers` | Headers to send with each trace when using the `otlp` exporter. | |

### Example: Exporting to Axiom

To export traces to [Axiom](https://axiom.co), you can use the following configuration:

```sh
sudo snap set xteve otel-exporter-type="otlp"
sudo snap set xteve otel-exporter-otlp-endpoint="https://api.axiom.co"
sudo snap set xteve otel-exporter-otlp-headers="Authorization=Bearer <YOUR_AXIOM_API_TOKEN>,x-axiom-dataset=<YOUR_DATASET_NAME>"
```
