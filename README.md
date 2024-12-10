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

## Development

To build the Telejob source code, clone this repository and run
`. ./bin/activate-hermit` in the terminal. Then, build the source code with:

    just

To execute a full CI run locally run:

    just ci

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
