# goenc

[![GitHub Releases](https://img.shields.io/github/v/release/cions/goenc?sort=semver)](https://github.com/cions/goenc/releases)
[![LICENSE](https://img.shields.io/github/license/cions/goenc)](https://github.com/cions/goenc/blob/master/LICENSE)
[![CI](https://github.com/cions/goenc/workflows/CI/badge.svg)](https://github.com/cions/goenc/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/cions/goenc.svg)](https://pkg.go.dev/github.com/cions/goenc)
[![Go Report Card](https://goreportcard.com/badge/github.com/cions/goenc)](https://goreportcard.com/report/github.com/cions/goenc)

A simple file encryption tool

## Usage

```sh
$ goenc [-d] [<input>] [<output>]
```

Password can be passed by the environment variable *PASSWORD*.

```sh
$ PASSWORD=<password> goenc <input> <output>
```

## Installation

[Download from GitHub Releases](https://github.com/cions/goenc/releases)

### Build from source

```sh
$ go install github.com/cions/goenc@latest
```

## Algorithm

XChaCha20-Poly1305 with Argon2id for key-derivation

## License

MIT
