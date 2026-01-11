package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

type runStats struct {
	FilesProcessed int
	InputBytes     int64
	OutputBytes    int64
}

func main() {
	inputDir := flag.String("in", "compressed", "input directory with .zst files to decompress")
	outDir := flag.String("out", "decompressed", "output directory for decompressed files")
	useDict := flag.Bool("use-dict", false, "enable dictionary decompression")
	dictPath := flag.String("dict", "", "path to zstd dictionary file")
	runID := flag.String("run-id", "", "run identifier for metrics grouping")
	pushURL := flag.String("pushgateway", "http://localhost:9091", "Pushgateway base URL")
	flag.Parse()

	if *useDict && strings.TrimSpace(*dictPath) == "" {
		fmt.Fprintln(os.Stderr, "-dict is required when -use-dict is set")
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	paths, err := listFiles(*inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list input files: %v\n", err)
		os.Exit(1)
	}
	if len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "no files found in %s\n", *inputDir)
		os.Exit(1)
	}

	var dictBytes []byte
	if *useDict {
		dictBytes, err = os.ReadFile(*dictPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read dict: %v\n", err)
			os.Exit(1)
		}
	}

	start := time.Now()
	stats, err := decompressFiles(paths, *inputDir, *outDir, dictBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decompression failed: %v\n", err)
		os.Exit(1)
	}
	duration := time.Since(start)

	sourceLabel := filepath.Base(*inputDir)
	if sourceLabel == "." || sourceLabel == string(filepath.Separator) {
		sourceLabel = "compressed"
	}
	if strings.TrimSpace(*runID) == "" {
		*runID = time.Now().Format("20060102_150405")
	}

	if err := pushMetrics(*pushURL, stats, duration, sourceLabel, *useDict, *runID); err != nil {
		fmt.Fprintf(os.Stderr, "metrics push failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("decompressed %d files (%d bytes -> %d bytes) into %s\n", stats.FilesProcessed, stats.InputBytes, stats.OutputBytes, *outDir)
}

func decompressFiles(paths []string, baseDir, outDir string, dictBytes []byte) (runStats, error) {
	stats := runStats{}

	options := []zstd.DOption{}
	if len(dictBytes) > 0 {
		options = append(options, zstd.WithDecoderDicts(dictBytes))
	}

	decoder, err := zstd.NewReader(nil, options...)
	if err != nil {
		return stats, err
	}
	defer decoder.Close()

	for _, path := range paths {
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return stats, err
		}

		outRel := strings.TrimSuffix(rel, ".zst")
		if outRel == rel {
			outRel = rel + ".out"
		}
		outPath := filepath.Join(outDir, outRel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return stats, err
		}

		inFile, err := os.Open(path)
		if err != nil {
			return stats, err
		}
		outFile, err := os.Create(outPath)
		if err != nil {
			inFile.Close()
			return stats, err
		}

		decoder.Reset(inFile)
		written, err := io.Copy(outFile, decoder)
		if err != nil {
			outFile.Close()
			inFile.Close()
			return stats, err
		}

		if err := outFile.Close(); err != nil {
			inFile.Close()
			return stats, err
		}
		if err := inFile.Close(); err != nil {
			return stats, err
		}

		info, err := os.Stat(path)
		if err != nil {
			return stats, err
		}

		stats.FilesProcessed++
		stats.InputBytes += info.Size()
		stats.OutputBytes += written
	}

	return stats, nil
}

func listFiles(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipDir) {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func pushMetrics(pushURL string, stats runStats, duration time.Duration, source string, useDict bool, runID string) error {
	registry := prometheus.NewRegistry()

	durationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_duration_seconds",
		Help: "Duration of the last decompression run in seconds.",
	})
	filesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_files_processed",
		Help: "Number of files processed in the last decompression run.",
	})
	inputBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_input_bytes",
		Help: "Total input bytes decompressed in the last run.",
	})
	outputBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_output_bytes",
		Help: "Total output bytes produced in the last run.",
	})
	ratioGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_ratio",
		Help: "Output/input size ratio for the last decompression run.",
	})
	timestampGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "decompress_last_run_timestamp_seconds",
		Help: "Unix timestamp of the last decompression run.",
	})

	metrics := []prometheus.Collector{
		durationGauge,
		filesGauge,
		inputBytesGauge,
		outputBytesGauge,
		ratioGauge,
		timestampGauge,
	}
	for _, metric := range metrics {
		if err := registry.Register(metric); err != nil {
			return err
		}
	}

	durationGauge.Set(duration.Seconds())
	filesGauge.Set(float64(stats.FilesProcessed))
	inputBytesGauge.Set(float64(stats.InputBytes))
	outputBytesGauge.Set(float64(stats.OutputBytes))
	if stats.InputBytes > 0 {
		ratioGauge.Set(float64(stats.OutputBytes) / float64(stats.InputBytes))
	}
	timestampGauge.Set(float64(time.Now().Unix()))

	source = strings.TrimSpace(source)
	if source == "" {
		source = "compressed"
	}

	pusher := push.New(pushURL, "decompress").Gatherer(registry)
	pusher = pusher.Grouping("source", source).Grouping("use_dict", strconv.FormatBool(useDict)).Grouping("run_id", runID)
	return pusher.Push()
}
