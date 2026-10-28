package main

// The demo project's files: a tiny weather service in Go.

var seedFiles = map[string]string{
	"README.md": "# aurora\n\nA tiny weather forecast service.\n\n## Usage\n\n    aurora -city Berlin\n",
	"go.mod":    "module example.com/aurora\n\ngo 1.27\n",
	"main.go": `package main

import (
	"flag"
	"fmt"
	"os"

	"example.com/aurora/internal/forecast"
)

func main() {
	city := flag.String("city", "Berlin", "city to forecast")
	flag.Parse()

	f, err := forecast.Fetch(*city)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aurora:", err)
		os.Exit(1)
	}
	fmt.Println(f)
}
`,
	"internal/forecast/forecast.go": forecastOld,
	"docs/notes.txt":                "- try open-meteo instead\n",
}

const forecastOld = `package forecast

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Forecast is the weather for one city.
type Forecast struct {
	City    string
	TempC   float64
	Summary string
}

func (f Forecast) String() string {
	return fmt.Sprintf("%s: %.1f°C, %s", f.City, f.TempC, f.Summary)
}

var client = &http.Client{Timeout: 10 * time.Second}

// Fetch returns the current forecast for city.
func Fetch(city string) (Forecast, error) {
	resp, err := client.Get("https://api.example.com/v1/forecast?city=" + city)
	if err != nil {
		return Forecast{}, err
	}
	defer resp.Body.Close()

	var f Forecast
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return Forecast{}, fmt.Errorf("decode forecast: %w", err)
	}
	return f, nil
}
`

const forecastNew = `package forecast

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Forecast is the weather for one city.
type Forecast struct {
	City    string
	TempC   float64
	Summary string
	Hourly  []Hour
}

// Hour is the forecast for a single hour.
type Hour struct {
	At    time.Time
	TempC float64
}

func (f Forecast) String() string {
	return fmt.Sprintf("%s: %.1f°C, %s", f.City, f.TempC, f.Summary)
}

var client = &http.Client{Timeout: 5 * time.Second}

// Fetch returns the forecast for city, served from cache when fresh.
func Fetch(city string) (Forecast, error) {
	if f, ok := cache.get(city); ok {
		return f, nil
	}
	resp, err := client.Get("https://api.example.com/v1/forecast?city=" + url.QueryEscape(city))
	if err != nil {
		return Forecast{}, err
	}
	defer resp.Body.Close()

	var f Forecast
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return Forecast{}, fmt.Errorf("decode forecast: %w", err)
	}
	cache.put(city, f)
	return f, nil
}
`

const cacheGo = `package forecast

import (
	"sync"
	"time"
)

const ttl = 10 * time.Minute

type entry struct {
	f       Forecast
	fetched time.Time
}

var cache = &store{m: map[string]entry{}}

type store struct {
	mu sync.Mutex
	m  map[string]entry
}
`
