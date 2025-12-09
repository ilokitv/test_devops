package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	temperatureGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "current_temperature_celsius",
			Help: "Current temperature in Celsius",
		},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(temperatureGauge)
}

type WeatherResponse struct {
	Temperature float64 `json:"temperature"`
	Unit        string  `json:"unit"`
	Timestamp   string  `json:"timestamp"`
	Source      string  `json:"source"`
}

type OpenWeatherResponse struct {
	Main struct {
		Temp float64 `json:"temp"`
	} `json:"main"`
}

func getTemperature() (float64, error) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	city := os.Getenv("WEATHER_CITY")
	if city == "" {
		city = "Moscow"
	}

	if apiKey == "" {

		return 15.0, nil
	}

	url := fmt.Sprintf("http://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric", city, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var weather OpenWeatherResponse
	if err := json.Unmarshal(body, &weather); err != nil {
		return 0, err
	}

	return weather.Main.Temp, nil
}

func temperatureHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	temp, err := getTemperature()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching temperature: %v", err), http.StatusInternalServerError)
		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "500").Inc()
		return
	}

	temperatureGauge.Set(temp)

	response := WeatherResponse{
		Temperature: temp,
		Unit:        "celsius",
		Timestamp:   time.Now().Format(time.RFC3339),
		Source:      "weather-api",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	duration := time.Since(start).Seconds()
	httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r := mux.NewRouter()
	r.Use(loggingMiddleware)

	// API endpoints
	r.HandleFunc("/api/temperature", temperatureHandler).Methods("GET")
	r.HandleFunc("/health", healthHandler).Methods("GET")

	// Prometheus metrics
	r.Handle("/metrics", promhttp.Handler())

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Weather App</title>
    <style>
        body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
        .temperature { font-size: 48px; color: #2196F3; margin: 20px; }
        .info { color: #666; }
    </style>
</head>
<body>
    <h1>Weather Application</h1>
    <div class="temperature" id="temp">Loading...</div>
    <div class="info">Temperature updates every 5 seconds</div>
    <script>
        function updateTemperature() {
            fetch('/api/temperature')
                .then(response => response.json())
                .then(data => {
                    document.getElementById('temp').textContent = data.temperature.toFixed(1) + '°C';
                })
                .catch(err => console.error('Error:', err));
        }
        updateTemperature();
        setInterval(updateTemperature, 5000);
    </script>
</body>
</html>
		`)
		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
	}).Methods("GET")

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
