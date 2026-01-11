package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

type Movie struct {
	ID        int     `json:"id"`
	Title     string  `json:"title"`
	Genre     string  `json:"genre"`
	Year      int     `json:"year"`
	Director  string  `json:"director"`
	Rating    float64 `json:"rating"`
	Runtime   int     `json:"runtime_minutes"`
	CreatedAt string  `json:"created_at"`
}

type Book struct {
	ID        int     `json:"id"`
	Title     string  `json:"title"`
	Author    string  `json:"author"`
	Genre     string  `json:"genre"`
	Year      int     `json:"year"`
	Pages     int     `json:"pages"`
	Rating    float64 `json:"rating"`
	CreatedAt string  `json:"created_at"`
}

type Person struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Age       int    `json:"age"`
	CreatedAt string `json:"created_at"`
}

var (
	movieTitles = []string{"Silent Horizon", "Crimson Valley", "Echoes of Tomorrow", "Northbound", "Astra Drift", "Blue Lantern", "Midnight Harbor", "Glass River"}
	movieGenres = []string{"Drama", "Sci-Fi", "Thriller", "Comedy", "Adventure", "Mystery"}
	directors   = []string{"Avery Quinn", "Morgan Ellis", "Riley Chen", "Harper Singh", "Jordan Blake", "Taylor Reyes"}

	bookTitles = []string{"The Last Orchard", "Paper Cities", "Sparks in Winter", "The River and the Road", "Atlas of Dust", "The Ninth Signal"}
	bookGenres = []string{"Fantasy", "Historical", "Non-Fiction", "Mystery", "Romance", "Sci-Fi"}
	authors    = []string{"Samira Holt", "Eli Navarro", "Priya Kapoor", "Luca Moretti", "Noah Sterling", "Yuna Park"}

	firstNames = []string{"Ava", "Liam", "Maya", "Ethan", "Isla", "Noah", "Zoe", "Amir", "Nora", "Leo"}
	lastNames  = []string{"Johnson", "Khan", "Patel", "Garcia", "Nguyen", "Smith", "Rossi", "Wright"}
	cities     = []string{"Austin", "Seattle", "Denver", "Toronto", "Dublin", "Oslo", "Berlin", "Lisbon"}
	countries  = []string{"USA", "Canada", "Ireland", "Norway", "Germany", "Portugal"}
)

func main() {
	dataType := flag.String("type", "", "data type to generate: movies, books, people")
	count := flag.Int("n", 0, "number of items to generate")
	outDir := flag.String("out", "output", "output directory")
	pushURL := flag.String("pushgateway", "http://localhost:9091", "Pushgateway base URL")
	flag.Parse()

	if *dataType == "" {
		*dataType = promptString("Select type (movies, books, people): ")
	}

	if *count <= 0 {
		*count = promptInt("How many items do you want to generate? ")
	}

	dataTypeVal := strings.ToLower(strings.TrimSpace(*dataType))
	if dataTypeVal != "movies" && dataTypeVal != "books" && dataTypeVal != "people" {
		fmt.Fprintf(os.Stderr, "unknown type: %s (expected movies, books, people)\n", dataTypeVal)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	start := time.Now()

	outputFile := filepath.Join(*outDir, fmt.Sprintf("%s_%s.json", dataTypeVal, time.Now().Format("20060102_150405")))

	var err error
	switch dataTypeVal {
	case "movies":
		err = writeJSONArray(outputFile, *count, func(i int) any {
			return makeMovie(rng, i+1)
		})
	case "books":
		err = writeJSONArray(outputFile, *count, func(i int) any {
			return makeBook(rng, i+1)
		})
	case "people":
		err = writeJSONArray(outputFile, *count, func(i int) any {
			return makePerson(rng, i+1)
		})
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	if err := pushMetrics(*pushURL, dataTypeVal, *count, duration); err != nil {
		fmt.Fprintf(os.Stderr, "metrics push failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("generated %d %s into %s\n", *count, dataTypeVal, outputFile)
}

func promptString(message string) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(message)
		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "input error: %v\n", err)
			os.Exit(1)
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
}

func promptInt(message string) int {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(message)
		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "input error: %v\n", err)
			os.Exit(1)
		}
		text = strings.TrimSpace(text)
		value, err := strconv.Atoi(text)
		if err != nil || value <= 0 {
			fmt.Println("please enter a positive number")
			continue
		}
		return value
	}
}

func makeMovie(rng *rand.Rand, id int) Movie {
	return Movie{
		ID:        id,
		Title:     pick(rng, movieTitles),
		Genre:     pick(rng, movieGenres),
		Year:      rng.Intn(45) + 1980,
		Director:  pick(rng, directors),
		Rating:    randFloat(rng, 5.5, 9.8),
		Runtime:   rng.Intn(81) + 80,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
}

func makeBook(rng *rand.Rand, id int) Book {
	return Book{
		ID:        id,
		Title:     pick(rng, bookTitles),
		Author:    pick(rng, authors),
		Genre:     pick(rng, bookGenres),
		Year:      rng.Intn(60) + 1965,
		Pages:     rng.Intn(450) + 150,
		Rating:    randFloat(rng, 3.5, 5.0),
		CreatedAt: time.Now().Format(time.RFC3339),
	}
}

func makePerson(rng *rand.Rand, id int) Person {
	first := pick(rng, firstNames)
	last := pick(rng, lastNames)
	return Person{
		ID:        id,
		FirstName: first,
		LastName:  last,
		Email:     strings.ToLower(fmt.Sprintf("%s.%s@example.com", first, last)),
		City:      pick(rng, cities),
		Country:   pick(rng, countries),
		Age:       rng.Intn(52) + 18,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
}

func writeJSONArray(path string, count int, makeItem func(i int) any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	if _, err := writer.WriteString("[\n"); err != nil {
		return err
	}

	for i := 0; i < count; i++ {
		if i > 0 {
			if _, err := writer.WriteString(",\n"); err != nil {
				return err
			}
		}

		item := makeItem(i)
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}

	if _, err := writer.WriteString("\n]\n"); err != nil {
		return err
	}

	return nil
}

func pushMetrics(pushURL, dataType string, count int, duration time.Duration) error {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "generated_items_total",
		Help: "Total number of generated items by type.",
	})
	durationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "generate_duration_seconds",
		Help: "Duration of the last generation run in seconds by type.",
	})
	timestampGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_run_timestamp_seconds",
		Help: "Unix timestamp of the last generation run by type.",
	})

	if err := registry.Register(counter); err != nil {
		return err
	}
	if err := registry.Register(durationGauge); err != nil {
		return err
	}
	if err := registry.Register(timestampGauge); err != nil {
		return err
	}

	counter.Add(float64(count))
	durationGauge.Set(duration.Seconds())
	timestampGauge.Set(float64(time.Now().Unix()))

	pusher := push.New(pushURL, "generate-data").Gatherer(registry).Grouping("type", dataType)
	return pusher.Push()
}

func pick(rng *rand.Rand, items []string) string {
	return items[rng.Intn(len(items))]
}

func randFloat(rng *rand.Rand, min, max float64) float64 {
	return min + rng.Float64()*(max-min)
}
