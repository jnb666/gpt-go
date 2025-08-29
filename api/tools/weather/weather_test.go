package weather

import (
	"log"
	"os"
	"testing"
)

var apiKey = os.Getenv("OWM_API_KEY")

func init() {
	log.SetFlags(0)
}

func TestGeocoding(t *testing.T) {
	loc, err := geocoding("New York,US", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", loc[0])
}

func TestCurrentWeather(t *testing.T) {
	w, err := currentWeather("New York,US", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(w)
}

func TestWeatherForecast(t *testing.T) {
	w, err := weatherForecast("New York,US", 8, apiKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(w)
}
