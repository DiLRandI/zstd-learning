# Choosing and training a Zstandard dictionary

This guide focuses on **how to select training data and validate a dictionary** so it actually improves compression. It’s grounded in the upstream Zstandard guidance and the Go library behavior used in this repo.

## When a dictionary helps (and when it doesn’t)

A dictionary is most useful for **many small, similar samples**. Zstd’s own documentation frames dictionary training as a way to improve compression for small data, and notes that dictionaries are most effective in the first few KB and should be tailored to a data type (no universal dictionary). That implies a separate dictionary per record type or schema can be beneficial, while highly diverse data often sees less gain.

## Sample selection: what to feed the trainer

Use **representative samples that match real payloads**:

- Keep samples **similar in structure and vocabulary** to what you will compress.
- Split by data type or schema (e.g., `movies`, `books`, `people`) rather than mixing unrelated records.
- If your inputs are already large and not “small data,” dictionary benefits will often diminish.

The Go library we use also emphasizes that dictionaries should be built with similar data; otherwise the output may even get slightly larger, and there’s a startup cost to dictionary compression—so **test performance and ratio** before committing to a dictionary in production.

## Rule‑of‑thumb sizing

The Zstandard dictionary training docs provide a few practical guidelines:

- **Dictionary size**: ~100 KB is a common, reasonable target.
- **Number of samples**: a few thousand samples is often recommended (varies by data).
- **Minimum sample count**: training sets typically contain lots of small files (often >100).
- **Total sample bytes**: aim for roughly **100× the target dictionary size**.
- **Minimum sample size**: samples smaller than ~8 bytes are too small to be useful.
- **Training can fail** if there are too few samples or samples are too small; in that case, using no dictionary is usually best.

These are starting points, not hard rules—use them to decide whether to expand the training set or shrink the dictionary.

## Practical checklist (copy/paste)

Use this checklist when deciding whether to train a dictionary:

- [ ] **Use-case fit:** Are inputs small and similar? (If not, dictionary likely won’t help.)
- [ ] **Data split:** One dictionary per data type or schema (avoid mixing unrelated samples).
- [ ] **Sample volume:** Total sample size ≈ 100× target dictionary size.
- [ ] **Sample count:** Aim for thousands of samples if possible.
- [ ] **Sample quality:** Samples reflect real production payloads.
- [ ] **Validation:** Compare compression ratio + speed vs. no dictionary.
- [ ] **Compatibility:** Ensure decoders use the same dictionary (ID must match).

## CLI flow (for validation)

Zstd’s CLI demonstrates the dictionary workflow clearly:

1. Train a dictionary
   `zstd --train /path/to/samples/* -o dict.zstd`

2. Compress with the dictionary
   `zstd -D dict.zstd file`

3. Decompress with the dictionary
   `zstd -D dict.zstd --decompress file.zst`

## How this repo applies it

- `cmd/train-dict` defaults to **128 KB** dictionaries and chunks files into samples.
- `cmd/compress`/`cmd/decompress` expose `-use-dict` and `-dict` flags so you can A/B test quickly.
- For tracking: use `-run-id` so Grafana lets you compare runs by dictionary vs. no dictionary.

## Sources and further reading

- Zstandard README (dictionary usage and small‑data guidance): https://github.com/facebook/zstd
- Zstandard manual (CLI dictionary flow): https://github.com/facebook/zstd/blob/dev/programs/zstd.1.md
- Zstd dictionary training tips (sample sizing rules): https://docs.rs/zstd-sys/latest/zstd_sys/struct.ZDICT_fastCover_params_t.html
- Go zstd library dictionary notes: https://pkg.go.dev/github.com/klauspost/compress/zstd
