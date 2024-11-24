# goenc

[![GitHub Releases](https://img.shields.io/github/v/release/cions/goenc?sort=semver)](https://github.com/cions/goenc/releases)
[![LICENSE](https://img.shields.io/github/license/cions/goenc)](https://github.com/cions/goenc/blob/master/LICENSE)
[![CI](https://github.com/cions/goenc/actions/workflows/ci.yml/badge.svg)](https://github.com/cions/goenc/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cions/goenc.svg)](https://pkg.go.dev/github.com/cions/goenc)
[![Go Report Card](https://goreportcard.com/badge/github.com/cions/goenc)](https://goreportcard.com/report/github.com/cions/goenc)

A simple file encryption tool

## Usage

```sh
$ goenc --help
Usage: goenc [OPTIONS] [INPUT] [OUTPUT]

A simple file encryption tool

Options:
  -e, --encrypt         Encrypt
  -d, --decrypt         Decrypt
  -n, --no-clobber      Do not overwrite an existing file
  -t, --time=N          Argon2 time parameter (default: 8)
  -m, --memory=N[KMG]   Argon2 memory parameter (default: 256M)
  -p, --parallelism=N   Argon2 parallelism parameter (default: 4)
  -r, --retries=N       Maximum number of attempts to enter password
                        (default: 3)
      --password-from=FILE
                        Read password from FILE
  -h, --help            Show this help message and exit
      --version         Show version information and exit

Environment Variables:
  PASSWORD              Encryption password
```

## Installation

[Download from GitHub Releases](https://github.com/cions/goenc/releases)

### Build from source

```sh
$ go install github.com/cions/goenc/cmd/goenc@latest
```

## Algorithm

- XChaCha20-Poly1305 for authenticated encryption
- Argon2id for key derivation

## License

MIT
