# Telejob

Telejob is a prototype remote job execution service. It provides an API for
running and managing Linux processes with resource limits.

This project is a system engineering [code challenge] for [Teleport].

**Features**

- Run commands with resource limits (CPU, memory, I/O).
- Secure communication using mutual TLS (mTLS).
- gRPC API for job management.
- Command-line interface (CLI) for easy interaction.

The [Design Doc](docs/design-doc.md) provides a detailed overview of the design,
API, security considerations, and user experience.

**Disclaimer**

This is a prototype implementation for demonstration purposes. It is not
intended for production use.

[code challenge]: https://github.com/gravitational/careers/blob/4a037eaac492f3ae94bf2bdbd6ef1685bd4842e9/challenges/systems/challenge-1.md
[Teleport]: https://goteleport.com/

## Usage

Install the Telejob binaries without cloning the repository with:

    go install github.com/juliaogris/telejob/cmd/telejob@latest
    go install github.com/juliaogris/telejob/cmd/telejob-server@latest

Prerequisites:

- [Go][go-install] 1.21 or later
- Certificate setup, see [Certificates](#certificates) or consider using [mkcert].

[go-install]: https://go.dev/doc/install

### Sample Certificate Setup with `mkcert`

You can use [`mkcert`][mkcert] to create a test setup using the system trust
store:

    mkcert -install
    mkcert -key-file server.key -cert-file server.crt -ecdsa localhost 127.0.0.1
    mkcert -key-file client.key -cert-file client.crt -client -ecdsa localhost 127.0.0.1
    ln -s "$(mkcert -CAROOT)"/rootCA.pem ca.crt

[mkcert]: https://github.com/FiloSottile/mkcert

### Serve

Start the Telejob server with:

    sudo "$GOPATH"/bin/telejob-server \
        --address localhost:8443 \
        --server-cert server.crt \
        --server-key server.key \
        --client-ca-cert ca.crt

Passing the Client CA explicitly is required.

### Client

Set up the environment so you can make multiple client calls more easily:

    export TELEJOB_ADDRESS=127.0.0.1:8443
    export TELEJOB_CLIENT_CERT=client.crt
    export TELEJOB_CLIENT_KEY=client.key
    export TELEJOB_TIME_FORMAT="15:04:05"

Then run:

    $ telejob start sleep 100
    1

    $ telejob status 1
    ID  COMMAND    STATE    STARTED   STOPPED  EXIT
    1   sleep 100  running  10:29:24

    $ telejob stop 1

The Server CA is taken from the system trust store.

Optionally use the Server CA explicitly:

    $ telejob status --server-ca-cert ca.crt 1
    ID  COMMAND    STATE    STARTED   STOPPED   EXIT
    1   sleep 100  stopped  10:29:24  10:30:10  signal

### Resource Limits

To test resource limits, run the server with additional `--limit-RESOURCE`
flags, for example:

    sudo "$GOPATH"/bin/telejob-server \
        --address localhost:8443 \
        --server-cert server.crt \
        --server-key server.key \
        --client-ca-cert ca.crt \
        --cpu-limit 0.2 \
        --memory-limit 2000

In the following example we use the [`stress`][stress] command installed on the
telejob server host, e.g. with `apt install stress`

In one terminal observe the CPU and memory usage with:

    watch -n 1 "ps -C stress -o pid,comm,%cpu,%mem,rss,vsz"

In another terminal start a job that consumes CPU:

    telejob start stress --cpu 1 --timeout 10s

Notice how the CPU usage is hovering around 20% for a single process due to
`--cpu-limit 0.2`. If you run the `stress` command with `--cpu 2`, you will see
the CPU usage of two processes at around 10%, as expected.

Test the memory limits with different values for `--vm-bytes` on stress and
`--memory-limit` on telejob-server:

    telejob start -- stress --timeout 10 --vm 2 --vm-bytes 300K

[stress]: https://github.com/resurrecting-open-source-projects/stress

## Development

To build the Telejob source code, clone this repository and run
`. ./bin/activate-hermit` in the terminal. Then, build the source code with:

    just

To execute a full CI run locally run:

    just ci

Stress test with a numeric argument specifying the number of concurrent jobs
(maximum: ~9000, limited by Go's runtime):

    just stress JOBS

All other targets can be listed with `just --list`.

### Hermit

This repository uses [Hermit] to manage its tools, ensuring consistent tool
versions across different development environments (local development, CI).
Cloning the repo is the only setup required. Hermit automatically installs
tools such as `just`, `go`, and `buf` when needed.

There are two ways to use the tools:

1.  Prefix them with `bin/` (e.g., `bin/just ci`).
2.  Activate Hermit: `. ./bin/activate-hermit` (adds tools to your PATH).

For automatic activation when entering the project directory, install
[Hermit shell hooks]: `hermit shell-hooks`.

[Hermit]: https://cashapp.github.io/hermit
[Hermit shell hooks]: https://cashapp.github.io/hermit/usage/shell/#shell-hooks

### Manual Testing

To simplify manual testing, use the provided just targets, which have test
certificates pre-configured.

Start the Telejob server with:

    just serve

In another terminal, run the client CLI to interact with the server, for
example:

    just run start sleep 100
    just run status <ID>
    just run stop <ID>
    just run status <ID>

## Build and Run

Build binaries with

    just build

This creates the following binaries in the `out/` directory:

- `telejob-server`: The Telejob gRPC server.
- `telejob`: The Telejob CLI client.

Run the server with

    ./out/telejob-server --help

and the client with

    ./out/telejob-client --help

## Certificates

Generate client and server Certificate Authorities (CAs) as well as certificates
for the server and client with:

    just certs

This command creates the following files in the `certs/` directory:

- `server-ca.crt` (expiry: 10 years, CA)
- `server-ca.key`
- `client-ca.crt` (expiry: 10 years, CA)
- `client-ca.key`
- `server.crt` (expiry: 90 days, IP: 127.0.0.1, domain: localhost)
- `server.key`
- `client1.crt` (expiry: 1 day, IP: 127.0.0.1, domain: localhost)
- `client1.key`
- `client2.crt` (expiry: 1 day, IP: 127.0.0.1, domain: localhost)
- `client2.key`

This repository also includes test certificates with extended expiry in the
`pkg/telejob/testdata/` directory. These were generated with the following
commands:

    cdir="pkg/telejob/testdata"
    CERT_DIR=$cdir CERT_EXPIRY="20 years" just certs
    CERT_DIR=$cdir CERT_IP="" just cert client-no-ip client-ca "20 years"
    CERT_DIR=$cdir CERT_IP="" just cert server-no-ip server-ca "20 years"

**Note:** The test certificates are insecure as they have their private keys
published and extended expiry. Do not use them for any purpose other than
testing.
