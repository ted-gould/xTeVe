<div align="center" style="background-color: #111; padding: 100;">
    <a href="https://github.com/ted-gould/xTeVe"><img width="880" height="200" src="html/img/logo_b_880x200.jpg" alt="xTeVe" /></a>
</div>
<br>

# xTeVe

## M3U Proxy and EPG aggregator for Plex DVR and Emby Live TV

### This is a fork of <https://github.com/xteve-project/xTeVe>, all credit goes to the original author

Documentation for setup and configuration is [here](https://github.com/xteve-project/xTeVe-Documentation/blob/main/en/configuration.md).

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

* See [releases page](https://github.com/ted-gould/xTeVe/releases)

---

## TLS mode

This mode can be enabled by ticking the checkbox in `Settings -> General`.

Unless the server's certificate and it's private key already exists in xTeVe config directory, xTeVe will generate a self-signed automatically.

Self-signed certificate will only allow TLS mode to start up but not to actually establish a secure connections.
For truly working HTTPS, you should [generate](https://gist.github.com/fntlnz/cf14feb5a46b2eda428e000157447309) a certificate by yourself and **also** add the CA certificate to the client-side certificate storage (where the web browser, Plex etc. is).

Certificate and it's private key should be placed in xTeVe config directory like so:

```text
/home/username/.xteve/certificates/xteve.crt
/home/username/.xteve/certificates/xteve.key
```

If the certificate is signed by a certificate authority (CA), it should be the concatenation of the server's certificate, any intermediates, and the CA's certificate.

This will also enable copy to clipboad by clicking the green links at the header. (DVR IP,M3U URL,XEPG URL)

---
<!-- 
### xTeVe Beta branch

New features and bug fixes are only available in beta branch. Only after successful testing are they are merged into the main branch.

**It is not recommended to use the beta version in a production system.**  

With the command line argument `branch` the Git Branch can be changed. xTeVe must be started via the terminal.  

#### Switch from main to beta branch

```text
xteve -branch beta

...
[xTeVe] GitHub:                https://github.com/senexcrenshaw
[xTeVe] Git Branch:            beta [senexcrenshaw]
...
```

#### Switch from beta to main branch

```text
xteve -branch main

...
[xTeVe] GitHub:                https://github.com/senexcrenshaw
[xTeVe] Git Branch:            main [senexcrenshaw]
...
```

When the branch is changed, an update is only performed if there is a new version and the update function is activated in the settings.  

--- -->

## Build from source code

### Requirements

* [Go](https://go.dev/dl/) (1.23 or newer)
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

3. **Run the build script:**
   ```sh
   sh build.sh
   ```

---

## Forks

When creating a fork, the xTeVe GitHub account must be changed from the source code or the update function disabled.

xteve.go - Line: 29

```go
var GitHub = GitHubStruct{Branch: "main", User: "senexcrenshaw", Repo: "xTeVe", Update: true}

// Branch: GitHub Branch
// User:   GitHub Username
// Repo:   GitHub Repository
// Update: Automatic updates from the GitHub repository [true|false]
```

---

## Stream Reconnection

xTeVe can be configured to automatically reconnect to a stream if the connection is interrupted. This is useful for IPTV providers that use connection breaks to disrupt service. This feature works for both HLS and TS streams, and will attempt to reconnect both on initial connection and if the stream is interrupted mid-stream.

To enable this feature, go to `Settings -> Streaming` and check the `Enable Stream Retry` box. You can also configure the maximum number of retries and the delay between retries.
