# goenc

[![GitHub Releases](https://img.shields.io/github/downloads/cions/goenc/latest/total?logo=github)](https://github.com/cions/goenc/releases)
[![CI](https://github.com/cions/goenc/workflows/CI/badge.svg)](https://github.com/cions/goenc/actions)
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
$ go get github.com/cions/goenc
```

## Algorithm

XChaCha20-Poly1305 with Argon2id for key-derivation

## License

MIT
