# Atlas

The next-generation master server for Northstar.

## Installation

First install GO from here: [https://go.dev/doc/install](https://go.dev/doc/install)

Then run the following commands:

```bash
go build cmd/atlas/main.go 
```

## Usage

Run the `main.exe` and update your northstar config to point to the new masterserver.

```
ns_masterserver_hostname "localhost:8080"
```