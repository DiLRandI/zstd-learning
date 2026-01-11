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
	inputDir := flag.String("in", "output", "input directory with files to compress")
	outDir := flag.String("out", "compressed", "output directory for compressed files")
	level := flag.Int("level", 0, "zstd compression level (0=default, 1..22 supported)")
	useDict := flag.Bool("use-dict", false, "enable dictionary compression")
	dictPath := flag.String("dict", "", "path to zstd dictionary file")
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
	stats, err := compressFiles(paths, *inputDir, *outDir, *level, dictBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compression failed: %v\n", err)
		os.Exit(1)
	}
	duration := time.Since(start)

	sourceLabel := filepath.Base(*inputDir)
	if sourceLabel == "." || sourceLabel == string(filepath.Separator) {
		sourceLabel = "output"
	}

	if err := pushMetrics(*pushURL, stats, duration, sourceLabel, *level, *useDict); err != nil {
		fmt.Fprintf(os.Stderr, "metrics push failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("compressed %d files (%d bytes -> %d bytes) into %s\n", stats.FilesProcessed, stats.InputBytes, stats.OutputBytes, *outDir)
}

func compressFiles(paths []string, baseDir, outDir string, level int, dictBytes []byte) (runStats, error) {
	stats := runStats{}

	options := []zstd.EOption{}
	if level != 0 {
		options = append(options, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	}
	if len(dictBytes) > 0 {
		options = append(options, zstd.WithEncoderDict(dictBytes))
	}

	encoder, err := zstd.NewWriter(nil, options...)
	if err != nil {
		return stats, err
	}
	defer encoder.Close()

	for _, path := range paths {
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return stats, err
		}

		outPath := filepath.Join(outDir, rel) + ".zst"
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

		encoder.Reset(outFile)
		written, err := io.Copy(encoder, inFile)
		if closeErr := encoder.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
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

		info, err := os.Stat(outPath)
		if err != nil {
			return stats, err
		}

		stats.FilesProcessed++
		stats.InputBytes += written
		stats.OutputBytes += info.Size()
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

func pushMetrics(pushURL string, stats runStats, duration time.Duration, source string, level int, useDict bool) error {
	registry := prometheus.NewRegistry()

	durationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_duration_seconds",
		Help: "Duration of the last compression run in seconds.",
	})
	filesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_files_processed",
		Help: "Number of files processed in the last compression run.",
	})
	inputBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_input_bytes",
		Help: "Total input bytes compressed in the last run.",
	})
	outputBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_output_bytes",
		Help: "Total output bytes produced in the last run.",
	})
	ratioGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_ratio",
		Help: "Output/input size ratio for the last compression run.",
	})
	timestampGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "compress_last_run_timestamp_seconds",
		Help: "Unix timestamp of the last compression run.",
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
		source = "output"
	}
	levelLabel := "default"
	if level != 0 {
		levelLabel = strconv.Itoa(level)
	}

	pusher := push.New(pushURL, "compress").Gatherer(registry)
	pusher = pusher.Grouping("source", source).Grouping("use_dict", strconv.FormatBool(useDict)).Grouping("level", levelLabel)
	return pusher.Push()
}
