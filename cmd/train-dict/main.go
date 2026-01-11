package main

import (
	"bufio"
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

	"github.com/klauspost/compress/dict"
	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

type sampleStats struct {
	FilesScanned int
	Samples      int
	SampleBytes  int64
}

func main() {
	inputDir := flag.String("in", "output", "input directory with sample data")
	outDir := flag.String("out", "dict-out", "output directory for dictionaries")
	outFile := flag.String("out-file", "", "optional full output file path")
	dictSize := flag.Int("dict-size", 128*1024, "dictionary size in bytes")
	maxSamples := flag.Int("max-samples", 1000, "maximum number of samples to use")
	maxSampleBytes := flag.Int("max-sample-bytes", 32*1024, "maximum bytes to read per sample")
	zstdLevel := flag.Int("zstd-level", 0, "zstd compression level for training (0=default, 1=fastest, 2=default, 3=better, 4=best)")
	pushURL := flag.String("pushgateway", "http://localhost:9091", "Pushgateway base URL")
	flag.Parse()

	if *dictSize <= 0 {
		fmt.Fprintln(os.Stderr, "dict-size must be positive")
		os.Exit(1)
	}
	if *maxSamples <= 0 {
		fmt.Fprintln(os.Stderr, "max-samples must be positive")
		os.Exit(1)
	}
	if *maxSampleBytes <= 0 {
		fmt.Fprintln(os.Stderr, "max-sample-bytes must be positive")
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	outputPath := *outFile
	if outputPath == "" {
		outputPath = filepath.Join(*outDir, fmt.Sprintf("zstd_dict_%s.zdict", time.Now().Format("20060102_150405")))
	}

	start := time.Now()
	samples, stats, err := collectSamples(*inputDir, *maxSamples, *maxSampleBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to collect samples: %v\n", err)
		os.Exit(1)
	}

	options := dict.Options{
		MaxDictSize: *dictSize,
		HashBytes:   6,
	}
	if *zstdLevel > 0 {
		options.ZstdLevel = parseZstdLevel(*zstdLevel)
	}

	trained, err := dict.BuildZstdDict(samples, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to train dictionary: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, trained, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write dictionary: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	sourceLabel := filepath.Base(*inputDir)
	if sourceLabel == "." || sourceLabel == string(filepath.Separator) {
		sourceLabel = "output"
	}
	if err := pushMetrics(*pushURL, stats, len(trained), *dictSize, duration, sourceLabel); err != nil {
		fmt.Fprintf(os.Stderr, "metrics push failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("trained dictionary %s (%d bytes) from %d samples\n", outputPath, len(trained), stats.Samples)
}

func collectSamples(dir string, maxSamples, maxSampleBytes int) ([][]byte, sampleStats, error) {
	paths, err := listFiles(dir)
	if err != nil {
		return nil, sampleStats{}, err
	}
	if len(paths) == 0 {
		return nil, sampleStats{}, fmt.Errorf("no files found in %s", dir)
	}

	samples := make([][]byte, 0, min(maxSamples, len(paths)))
	stats := sampleStats{}

	for _, path := range paths {
		if len(samples) >= maxSamples {
			break
		}

		chunks, readBytes, err := readSamplesFromFile(path, maxSampleBytes, maxSamples-len(samples))
		if err != nil {
			return nil, stats, err
		}
		if len(chunks) == 0 {
			continue
		}
		stats.FilesScanned++
		samples = append(samples, chunks...)
		stats.Samples += len(chunks)
		stats.SampleBytes += readBytes
	}

	if len(samples) < 2 {
		return nil, stats, fmt.Errorf("not enough samples to train (got %d). Increase data or lower max-sample-bytes to create more chunks", len(samples))
	}

	return samples, stats, nil
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

func readSamplesFromFile(path string, maxBytes, maxSamples int) ([][]byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	buf := make([]byte, maxBytes)
	var samples [][]byte
	var total int64

	for len(samples) < maxSamples {
		n, err := io.ReadFull(reader, buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				if n == 0 {
					break
				}
				data := bytesTrimSpace(buf[:n])
				if len(data) > 0 {
					samples = append(samples, append([]byte(nil), data...))
					total += int64(len(data))
				}
				break
			}
			return nil, total, err
		}

		data := bytesTrimSpace(buf[:n])
		if len(data) == 0 {
			continue
		}
		samples = append(samples, append([]byte(nil), data...))
		total += int64(len(data))
	}

	return samples, total, nil
}

func bytesTrimSpace(input []byte) []byte {
	start := 0
	end := len(input)
	for start < end {
		switch input[start] {
		case ' ', '\n', '\r', '\t':
			start++
		default:
			goto endLoop
		}
	}
endLoop:
	for end > start {
		switch input[end-1] {
		case ' ', '\n', '\r', '\t':
			end--
		default:
			return input[start:end]
		}
	}
	return input[start:end]
}

func parseZstdLevel(level int) zstd.EncoderLevel {
	switch level {
	case 1:
		return zstd.SpeedFastest
	case 2:
		return zstd.SpeedDefault
	case 3:
		return zstd.SpeedBetterCompression
	case 4:
		return zstd.SpeedBestCompression
	default:
		return zstd.SpeedBestCompression
	}
}

func pushMetrics(pushURL string, stats sampleStats, outputBytes, dictSize int, duration time.Duration, source string) error {
	registry := prometheus.NewRegistry()

	durationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_train_duration_seconds",
		Help: "Duration of the last dictionary training run in seconds.",
	})
	samplesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_samples_count",
		Help: "Number of samples used in the last dictionary training run.",
	})
	sampleBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_sample_bytes",
		Help: "Total bytes of samples used in the last dictionary training run.",
	})
	filesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_files_scanned",
		Help: "Number of files scanned in the last dictionary training run.",
	})
	outputBytesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_output_bytes",
		Help: "Size of the generated dictionary in bytes.",
	})
	dictSizeGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_target_size_bytes",
		Help: "Target dictionary size requested for training.",
	})
	timestampGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dict_last_run_timestamp_seconds",
		Help: "Unix timestamp of the last dictionary training run.",
	})

	metrics := []prometheus.Collector{
		durationGauge,
		samplesGauge,
		sampleBytesGauge,
		filesGauge,
		outputBytesGauge,
		dictSizeGauge,
		timestampGauge,
	}
	for _, metric := range metrics {
		if err := registry.Register(metric); err != nil {
			return err
		}
	}

	durationGauge.Set(duration.Seconds())
	samplesGauge.Set(float64(stats.Samples))
	sampleBytesGauge.Set(float64(stats.SampleBytes))
	filesGauge.Set(float64(stats.FilesScanned))
	outputBytesGauge.Set(float64(outputBytes))
	dictSizeGauge.Set(float64(dictSize))
	timestampGauge.Set(float64(time.Now().Unix()))

	source = strings.TrimSpace(source)
	if source == "" {
		source = "output"
	}

	pusher := push.New(pushURL, "train-dict").Gatherer(registry).Grouping("source", source).Grouping("dict_size", strconv.Itoa(dictSize))
	return pusher.Push()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
