# Zstandard (zstd) guide for this repo

This document explains Zstandard (zstd), the Go library used here, and how the tools in this repo map to zstd options. It focuses on practical usage for compression, decompression, and dictionary training.

## What is Zstandard

Zstandard (zstd) is a fast lossless compression algorithm with a reference implementation and CLI maintained by Meta (Facebook). The format is stable and widely used in storage and transport systems.

- Official repository: <https://github.com/facebook/zstd>
- Official site: <https://facebook.github.io/zstd/>

### Recent upstream notes (as of early 2026)

The GitHub releases page lists Zstandard v1.5.7 as the latest release (dated Feb 19, 2025). Release notes call out default threading that scales with the system (up to 4 threads by default) and a new `--max` command for maximum compression beyond `--ultra -22`.

Release notes: <https://github.com/facebook/zstd/releases>

### Standards and content encoding

Zstandard is registered as an HTTP content-coding and media type in RFC 8878, and RFC 9659 updates RFC 8878 by requiring encoders/decoders to limit the Window_Size to 8 MB for HTTP content coding.

- RFC 8878: <https://www.rfc-editor.org/rfc/rfc8878>
- RFC 9659: <https://www.rfc-editor.org/rfc/rfc9659>

## Compression levels and trade-offs

Zstandard offers a range of compression levels that trade speed for ratio. The CLI also provides negative levels (via `--fast=#`) for faster compression with lower ratios. Decompression speed is designed to remain largely stable across levels.

Key points from the upstream README:

- Negative levels are enabled with `--fast=#` for faster compression.
- Decompression speed is generally stable across levels.

Source: <https://github.com/facebook/zstd>

## Dictionary compression

Dictionary compression is useful for small inputs or collections of similar samples. The upstream README recommends training a dictionary per data type and using it when compressing and decompressing.

Typical flow (CLI):

1. Train: `zstd --train *.txt -o dict.zstd`
2. Compress: `zstd -D dict.zstd file.txt`
3. Decompress: `zstd -D dict.zstd --decompress file.txt.zst`

Source: <https://github.com/facebook/zstd>

## Go library used in this repo

This repo uses the Go implementation `github.com/klauspost/compress/zstd`.

Documentation: <https://pkg.go.dev/github.com/klauspost/compress/zstd>

### Encoder levels

The Go library exposes encoder levels that roughly map to Zstandard levels:

- SpeedFastest (roughly zstd level 1)
- SpeedDefault (roughly zstd level 3)
- SpeedBetterCompression (roughly zstd level 7)
- SpeedBestCompression (roughly zstd level 11)

Source: <https://pkg.go.dev/github.com/klauspost/compress/zstd>

### Encoder options (EOption)

All encoder options and their purpose:

- WithAllLitEntropyCompression: enable literal entropy compression for all blocks.
- WithEncoderCRC: add a checksum to the frame (adds 4 bytes).
- WithEncoderConcurrency: set number of encoder goroutines (0 uses GOMAXPROCS).
- WithEncoderDict: use a prepared zstd dictionary for encoding.
- WithEncoderDictRaw: register a raw dictionary (ID + content).
- WithEncoderLevel: choose compression level (SpeedFastest..SpeedBestCompression).
- WithEncoderPadding: add padding to the output.
- WithLowerEncoderMem: reduce encoder memory usage (may affect speed/ratio).
- WithNoEntropyCompression: disable entropy compression.
- WithSingleSegment: force single-segment output when possible.
- WithWindowSize: set the encoder window size.
- WithZeroFrames: allow empty output for empty input.

Source: <https://pkg.go.dev/github.com/klauspost/compress/zstd>

### Decoder options (DOption)

All decoder options and their purpose:

- IgnoreChecksum: skip checksum verification if present.
- WithDecodeAllCapLimit: cap DecodeAll buffer capacity to a maximum size.
- WithDecodeBuffersBelow: avoid allocations for small buffers when possible.
- WithDecoderConcurrency: set number of decoder goroutines.
- WithDecoderDictRaw: register a raw dictionary (ID + content).
- WithDecoderDicts: register one or more dictionaries for decoding.
- WithDecoderLowmem: reduce decoder memory usage.
- WithDecoderMaxMemory: cap maximum decoder memory usage.
- WithDecoderMaxWindow: cap maximum window size accepted.

Source: <https://pkg.go.dev/github.com/klauspost/compress/zstd>

## How this repo uses zstd

### Dictionary training

The `cmd/train-dict` tool trains a dictionary using `github.com/klauspost/compress/dict` and writes it to `dict-out/` by default. It reads samples from `output/`, chunking files to create multiple samples.

### Compression

The `cmd/compress` tool compresses every file in a folder. Relevant flags:

- `-level` maps to zstd encoder levels via `EncoderLevelFromZstd`.
- `-use-dict` and `-dict` enable dictionary compression.

Output goes to `compressed/` by default.

### Decompression

The `cmd/decompress` tool decompresses every `.zst` file in a folder. Relevant flags:

- `-use-dict` and `-dict` enable dictionary decoding.

Output goes to `decompressed/` by default.

## Dictionary selection guide

If you want a practical, step‑by‑step checklist for picking and validating dictionaries, see:

- `docs/dictionary-selection.md`

## Glossary (quick)

- Frame: the full zstd-encoded unit with a header and one or more blocks.
- Block: a chunk inside a frame that may be compressed or raw.
- Window size: how much history the encoder/decoder can use for matches; larger windows can improve ratio but use more memory.
- Dictionary: a trained byte sequence that seeds compression for similar/small inputs; both encoder and decoder must use the same dict.
- Dictionary ID: identifier embedded in the frame; decoder must have a dict with a matching ID.
- Literals: bytes encoded as-is (not back-references).
- Sequences / matches: back-references to previously seen data within the window.
- Entropy coding: Huffman/FSE coding for literals and sequences; can be tuned in the Go library.
- Ratio: output bytes divided by input bytes (lower is better).
- Throughput: bytes per second for compression/decompression.

## Option mapping cheat sheet

This table maps common concepts across the zstd CLI, the Go library, and the tools in this repo. Some CLI flags don’t have a 1:1 match in the Go library; where that’s the case, use the closest concept and verify with `zstd --help` in your environment.

| Concept | zstd CLI | Go library (klauspost/compress/zstd) | This repo |
|---|---|---|---|
| Compression level | Numeric levels; `--fast=#` for negative levels; `--ultra` for higher levels | `WithEncoderLevel(EncoderLevelFromZstd(level))` | `cmd/compress -level` |
| Fast mode (very low ratio, max speed) | `--fast=#` | Closest: `WithEncoderLevel(SpeedFastest)` | `cmd/compress -level 1` |
| Maximum compression | `--max` (slower than `--ultra -22`) | not exposed | not exposed |
| Threads | CLI supports setting thread count (flag/env) | `WithEncoderConcurrency(n)` / `WithDecoderConcurrency(n)` | not exposed yet |
| Dictionary | `-D dict.zstd` | `WithEncoderDict(dictBytes)` / `WithDecoderDicts(dictBytes)` | `-use-dict -dict` |
| Window size | Window size limits apply to HTTP content encoding | `WithWindowSize(bytes)`; decoder needs `WithDecoderMaxWindow` | not exposed yet |
| Checksums | Optional content checksum in the format | `WithEncoderCRC(true)`; `IgnoreChecksum(false)` | not exposed yet |

### Mapping for repo flags

| Repo flag | Meaning | Go option(s) |
|---|---|---|
| `-level` | compression level | `EncoderLevelFromZstd` + `WithEncoderLevel` |
| `-use-dict` / `-dict` | dictionary compression/decompression | `WithEncoderDict` / `WithDecoderDicts` |
| `-run-id` | metrics grouping key | Pushgateway grouping label |
| `-in` / `-out` | input/output folders | filesystem paths |
| `-pushgateway` | metrics endpoint | Pushgateway base URL |

## Additional resources

- Zstandard repo and README: <https://github.com/facebook/zstd>
- Release notes: <https://github.com/facebook/zstd/releases>
- Zstandard website: <https://facebook.github.io/zstd/>
- Go zstd library docs: <https://pkg.go.dev/github.com/klauspost/compress/zstd>
- Zstd content encoding RFCs: <https://www.rfc-editor.org/rfc/rfc8878> and <https://www.rfc-editor.org/rfc/rfc9659>
