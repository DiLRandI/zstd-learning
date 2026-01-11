# zstd-learning

A small learning playground for Zstandard (zstd) with Go tools for data generation, dictionary training, compression, and decompression. It also includes Prometheus + Pushgateway + Grafana dashboards for observability.

## Quick start

Start the monitoring stack:

```shell
make monitor
```

Generate sample data:

```shell
go run ./cmd/generate-data -type movies -n 100
```

Train a dictionary (writes to `dict-out/` by default):

```shell
go run ./cmd/train-dict -in output -dict-size 131072
```

Compress a folder:

```shell
go run ./cmd/compress -in output -out compressed -level 0
```

Decompress a folder:

```shell
go run ./cmd/decompress -in compressed -out decompressed
```

## Dashboards

Grafana is provisioned with dashboards for:

- Generator
- Dictionary trainer
- Compression / decompression

Once the stack is up, open Grafana at `http://localhost:3000`.

## Documentation

See the full Zstandard guide and library option reference here:

- `docs/zstd-guide.md`
  - Includes a glossary and a CLI ↔ Go option mapping cheat sheet.

## External resources

- Zstandard repo and README: <https://github.com/facebook/zstd>
- Zstandard website: <https://facebook.github.io/zstd/>
- Zstandard releases: <https://github.com/facebook/zstd/releases>
- Go zstd library docs: <https://pkg.go.dev/github.com/klauspost/compress/zstd>
- Zstd content encoding RFCs: <https://www.rfc-editor.org/rfc/rfc8878> and <https://www.rfc-editor.org/rfc/rfc9659>
