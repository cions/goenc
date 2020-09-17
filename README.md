# goenc

[![GitHub Releases](https://img.shields.io/github/downloads/cions/goenc/latest/total?logo=github)](https://github.com/cions/goenc/releases)
[![CI](https://github.com/cions/goenc/workflows/CI/badge.svg)](https://github.com/cions/goenc/actions)

A simple file encryption tool

## Usage

```sh
$ goenc < input > output    # encrypt
$ goenc -d < input > output # decrypt
```

Password can be passed by the environment variable *PASSWORD*.

```sh
$ PASSWORD=<password> goenc < input > output
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
