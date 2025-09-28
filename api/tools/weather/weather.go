// Package weather implements tool functions to call the openweathermap API.
package weather

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/jnb666/gpt-go/api"
	"github.com/jnb666/gpt-go/api/tools"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/shared"
	log "github.com/sirupsen/logrus"
)

func Tools(apiKey string) []api.ToolFunction {
	return []api.ToolFunction{
		Current{ApiKey: apiKey},
		Forecast{ApiKey: apiKey},
	}
}

// Tool to get current weather - implements api.ToolFunction interface
type Current struct {
	ApiKey string
}

func (t Current) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "get_current_weather",
		Strict: openai.Bool(true),
		Description: openai.String("Get the current weather in a given location." +
			" Returns conditions with temperatures in Celsius and wind speed in meters/second."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": `The city name and ISO 3166 country code, e.g. "London,GB" or "New York,US".`,
				},
			},
			"required": []string{"location"},
		},
	}
}

func (t Current) Call(arg string) (req, res string, err error) {
	log.Printf("call get_current_weather(%s)", arg)
	var args struct {
		Location string
	}
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	req = fmt.Sprintf("get_current_weather%+v", args)
	w, err := currentWeather(args.Location, t.ApiKey)
	return req, w.String(), err
}

// Tool to get weather forecast data - implements api.ToolFunction interface
type Forecast struct {
	ApiKey string
}

func (t Forecast) Definition() shared.FunctionDefinitionParam {
	return shared.FunctionDefinitionParam{
		Name:   "get_weather_forecast",
		Strict: openai.Bool(true),
		Description: openai.String("Get the weather forecast in a given location." +
			" Returns a list with date and time in local timezone and predicted conditions every 3 hours.\n" +
			" Temperatures are in Celsius and wind speed in meters/second."),
		Parameters: shared.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": `The city name and ISO 3166 country code, e.g. "London,GB" or "New York,US".`,
				},
				"periods": map[string]any{
					"type":        "number",
					"description": `Number of 3 hour periods to look ahead from current time - default 24.`,
				},
			},
			"required": []string{"location"},
		},
	}
}

func (t Forecast) Call(arg string) (req, res string, err error) {
	log.Printf("call get_weather_forecast(%s)", arg)
	var args struct {
		Location string
		Periods  float64
	}
	if err := json.Unmarshal([]byte(arg), &args); err != nil {
		return arg, "", err
	}
	if args.Periods == 0 {
		args.Periods = 24
	}
	req = fmt.Sprintf("get_weather_forecast%+v", args)
	w, err := weatherForecast(args.Location, int(args.Periods), t.ApiKey)
	return req, w.String(), err
}

// Current weather API per https://openweathermap.org/current
func currentWeather(location string, apiKey string) (w currentWeatherData, err error) {
	locs, err := geocoding(location, apiKey)
	if err != nil {
		return w, err
	}
	loc := locs[0]
	uri := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?lat=%f&lon=%f&appid=%s&units=metric",
		loc.Lat, loc.Lon, apiKey)
	w.Loc = loc
	err = tools.Get(uri, &w)
	if err == nil && len(w.Weather) == 0 {
		err = fmt.Errorf("current weather for %s not found", loc)
	}
	return w, err
}

type currentWeatherData struct {
	weatherData
	Timezone int
	Loc      location
}

func (w currentWeatherData) String() string {
	return fmt.Sprintf("Current weather for %s,%s: %s", w.Loc.Name, w.Loc.Country, w.weatherData)
}

type weatherData struct {
	Dt      int
	Weather []struct {
		Description string
	}
	Main struct {
		Temp       float64
		Feels_Like float64
	}
	Wind struct {
		Speed float64
	}
}

func (w weatherData) String() string {
	s := fmt.Sprintf("%.0f°C - %s", w.Main.Temp, w.Weather[0].Description)
	if w.Main.Feels_Like != 0 && math.Abs(w.Main.Feels_Like-w.Main.Temp) > 1 {
		s += fmt.Sprintf(", feels like %.0f°C", w.Main.Feels_Like)
	}
	if w.Wind.Speed != 0 {
		s += fmt.Sprintf(", wind %.1fm/s", w.Wind.Speed)
	}
	return s
}

// 5 day weather forecast API per https://openweathermap.org/forecast5
func weatherForecast(location string, periods int, apiKey string) (w forecastWeatherData, err error) {
	locs, err := geocoding(location, apiKey)
	if err != nil {
		return w, err
	}
	loc := locs[0]
	uri := fmt.Sprintf("https://api.openweathermap.org/data/2.5/forecast?lat=%f&lon=%f&cnt=%d&appid=%s&units=metric",
		loc.Lat, loc.Lon, periods, apiKey)
	w.Loc = loc
	err = tools.Get(uri, &w)
	if err == nil && len(w.List) == 0 {
		err = fmt.Errorf("weather forecast for %s not found", loc)
	}
	return w, err
}

type forecastWeatherData struct {
	List []weatherData
	Loc  location
	City struct {
		Timezone int
	}
}

func (w forecastWeatherData) String() string {
	s := fmt.Sprintf("Weather forecast for %s,%s:\n", w.Loc.Name, w.Loc.Country)
	for _, r := range w.List {
		s += fmt.Sprintf("- %s: %s\n", localtime(r.Dt, w.City.Timezone), r)
	}
	return s
}

// Geocoding API per https://openweathermap.org/api/geocoding-api
func geocoding(location, apiKey string) (loc []location, err error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openweathermap api key is required")
	}
	uri := fmt.Sprintf("http://api.openweathermap.org/geo/1.0/direct?q=%s&&appid=%s",
		url.QueryEscape(location), apiKey)
	err = tools.Get(uri, &loc)
	if err == nil && len(loc) == 0 {
		err = fmt.Errorf("location %q not found", location)
	}
	return loc, err
}

type location struct {
	Name    string
	Country string
	State   string
	Lat     float64
	Lon     float64
}

func (l location) String() string {
	return fmt.Sprintf("%s,%s", l.Name, l.Country)
}

// Util functions
func localtime(dt, timezone int) string {
	t := time.Unix(int64(dt), 0)
	loc := time.FixedZone("", timezone)
	return t.In(loc).Format("Mon, 02 Jan 2006 15:04:05")
}
