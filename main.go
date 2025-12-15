package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// --- CONFIGURATION STRUCTS ---
type Config struct {
	API      APIConfig      `json:"api"`
	IDs      IDsConfig      `json:"ids"`
	Settings SettingsConfig `json:"settings"`
}

type APIConfig struct {
	BaseURL  string `json:"base_url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type IDsConfig struct {
	SolarManagerID string `json:"solar_manager_id"`
	SensorTemp     string `json:"sensor_temp_id"`
	DeviceWP       string `json:"device_wp_id"`     // Heat Pump ID
	DeviceHeater   string `json:"device_heater_id"` // Heating Element ID
}

type SettingsConfig struct {
	TargetTempWP           float64 `json:"target_temp_wp"`
	TargetTempLegio        float64 `json:"target_temp_legio"`
	CheckIntervalMinutes   int     `json:"check_interval_minutes"`
	LegionellaIntervalDays int     `json:"legionella_interval_days"`
	MaxDurationHoursWP     int     `json:"max_duration_hours_wp"`
	MaxDurationHoursHeater int     `json:"max_duration_hours_heater"`
}

// --- STATE STRUCTS ---
type State struct {
	LastSanitization time.Time `json:"last_sanitization"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type SensorInfo struct {
	Mode int `json:"mode"`
}

type StreamData struct {
	Data struct {
		Temperature float64 `json:"temperature"`
		WaterTemp   float64 `json:"water_temperature"`
	} `json:"data"`
}

const (
	StateFile  = "legio_state.json"
	ConfigFile = "config.json"

	// API Modes according to Solar Manager Swagger
	ModeOn        = 1 // Always On
	ModeSolarOnly = 3 // Solar Optimized
)

var (
	appConfig    Config
	bearerToken  string
	currentState State
)

func main() {
	log.Println("Starting SolarManager Legionella Protection Service...")

	// 1. Load Config
	if err := loadConfig(); err != nil {
		log.Fatalf("Could not load config: %v", err)
	}

	// 2. Load State
	loadState()

	// 3. Initial Login
	if err := login(); err != nil {
		log.Fatalf("Initial login failed: %v", err)
	}

	interval := time.Duration(appConfig.Settings.CheckIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Service running. Checking every %d minutes.", appConfig.Settings.CheckIntervalMinutes)

	for {
		select {
		case <-ticker.C:
			currentTemp, err := getTemperature(appConfig.IDs.SensorTemp)
			if err != nil {
				log.Printf("Temp sensor error: %v (Attempting re-login)", err)
				login()
				continue
			}

			legioInterval := time.Duration(appConfig.Settings.LegionellaIntervalDays) * 24 * time.Hour

			log.Printf("Current: %.1f°C (Target: %.1f°C). Last sanitization: %s",
				currentTemp, appConfig.Settings.TargetTempLegio, currentState.LastSanitization.Format("2006-01-02 15:04"))

			// Passive Reset: If water is already hot enough (solar/usage), reset timer
			if currentTemp >= appConfig.Settings.TargetTempLegio {
				if time.Since(currentState.LastSanitization) > 24*time.Hour {
					log.Println("Temperature reached passively (>65°C). Resetting timer.")
					updateState(time.Now())
				}
				continue
			}

			// Active Intervention
			if time.Since(currentState.LastSanitization) > legioInterval {
				log.Println("WARNING: Legionella protection due! Starting heating program.")
				runLegionellaProgram()
			}
		}
	}
}

func loadConfig() error {
	file, err := os.ReadFile(ConfigFile)
	if err != nil {
		return err
	}
	return json.Unmarshal(file, &appConfig)
}

func runLegionellaProgram() {
	// --- PHASE 1: HEAT PUMP (WP) ---
	log.Println("--- PHASE 1: Heat Pump ---")

	// Save original mode
	origModeWP, err := getDeviceMode(appConfig.IDs.DeviceWP)
	if err != nil {
		log.Printf("Error reading WP mode: %v", err)
		return
	}

	// Only switch ON if not already ON
	if origModeWP != ModeOn {
		setDeviceMode(appConfig.IDs.DeviceWP, ModeOn)
	}

	maxDurWP := time.Duration(appConfig.Settings.MaxDurationHoursWP) * time.Hour
	monitorTemperatureRise(appConfig.IDs.SensorTemp, appConfig.Settings.TargetTempWP, maxDurWP)

	// Restore original mode
	log.Printf("Restoring WP mode: %d", origModeWP)
	setDeviceMode(appConfig.IDs.DeviceWP, origModeWP)

	// --- PHASE 2: HEATING ELEMENT ---
	log.Println("--- PHASE 2: Heating Element ---")

	temp, _ := getTemperature(appConfig.IDs.SensorTemp)
	if temp >= appConfig.Settings.TargetTempLegio {
		log.Println("Target temperature reached by WP. Skipping Phase 2.")
		updateState(time.Now())
		return
	}

	// Save original mode
	origModeHeater, err := getDeviceMode(appConfig.IDs.DeviceHeater)
	if err != nil {
		origModeHeater = ModeSolarOnly // Default fallback
	}

	if origModeHeater != ModeOn {
		setDeviceMode(appConfig.IDs.DeviceHeater, ModeOn)
	}

	maxDurHeater := time.Duration(appConfig.Settings.MaxDurationHoursHeater) * time.Hour
	success := monitorTemperatureRise(appConfig.IDs.SensorTemp, appConfig.Settings.TargetTempLegio, maxDurHeater)

	// Restore original mode
	log.Printf("Restoring Heater mode: %d", origModeHeater)
	setDeviceMode(appConfig.IDs.DeviceHeater, origModeHeater)

	if success {
		log.Println("SUCCESS: Sanitization completed.")
		updateState(time.Now())
	} else {
		log.Println("ERROR: Target temperature not reached.")
	}
}

func monitorTemperatureRise(sensorID string, target float64, timeout time.Duration) bool {
	startTime := time.Now()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t, err := getTemperature(sensorID)
			if err != nil {
				continue
			}
			if t >= target {
				return true
			}
			if time.Since(startTime) > timeout {
				return false
			}
		}
	}
}

// --- API CLIENT FUNCTIONS ---

func login() error {
	url := appConfig.API.BaseURL + "/oauth/login"
	payload := map[string]string{
		"email":    appConfig.API.Username,
		"password": appConfig.API.Password,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %d", resp.StatusCode)
	}
	var res TokenResponse
	json.NewDecoder(resp.Body).Decode(&res)
	bearerToken = res.Token
	return nil
}

func getTemperature(sensorID string) (float64, error) {
	url := fmt.Sprintf("%s/stream/sensor/%s/%s", appConfig.API.BaseURL, appConfig.IDs.SolarManagerID, sensorID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var stream StreamData
	if err := json.NewDecoder(resp.Body).Decode(&stream); err != nil {
		return 0, err
	}
	// Use water_temperature if available, otherwise fallback to temperature
	if stream.Data.WaterTemp > 0 {
		return stream.Data.WaterTemp, nil
	}
	return stream.Data.Temperature, nil
}

func getDeviceMode(deviceID string) (int, error) {
	url := fmt.Sprintf("%s/info/sensor/%s", appConfig.API.BaseURL, deviceID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var info SensorInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return 0, err
	}
	return info.Mode, nil
}

func setDeviceMode(deviceID string, mode int) {
	// Try smart-plug first, change to 'switch' or 'car-charger' if API demands it
	url := fmt.Sprintf("%s/control/smart-plug/%s", appConfig.API.BaseURL, deviceID)
	payload := map[string]int{"mode": mode}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, _ := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
}

func loadState() {
	file, err := os.ReadFile(StateFile)
	if err == nil {
		json.Unmarshal(file, &currentState)
	} else {
		// Default: Assume last sanitization was 8 days ago to trigger immediate check
		currentState = State{LastSanitization: time.Now().Add(-8 * 24 * time.Hour)}
	}
}

func updateState(t time.Time) {
	currentState.LastSanitization = t
	data, _ := json.MarshalIndent(currentState, "", " ")
	os.WriteFile(StateFile, data, 0644)
}
