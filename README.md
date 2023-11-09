# Atlas

The next-generation master server for Northstar.

## Installation

First install Go from here: [https://go.dev/doc/install](https://go.dev/doc/install)

Then run the following commands:

```bash
go run cmd/atlas/main.go 
```

## Building

To build Atlas, run the following command:

```bash
go build -o atlas.exe cmd/atlas/main.go
```

## Usage

Run the `main.exe` or directly with `go run cmd/atlas/main.go` and update your northstar config to point to the new masterserver.

```
ns_masterserver_hostname "localhost:8080"
```