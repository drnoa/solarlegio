package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Test loading the configuration from a temporary file
func TestLoadConfig(t *testing.T) {
	// 1. Create a temporary config file
	tmpFile, err := os.CreateTemp("", "config_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up after test

	// 2. Write dummy JSON content
	dummyConfig := `{
		"api": { "base_url": "http://test-url", "username": "user", "password": "pass" },
		"settings": { "target_temp_legio": 65.0 }
	}`
	if _, err := tmpFile.Write([]byte(dummyConfig)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// 3. Point the application to this temp file
	originalConfigFile := ConfigFile
	ConfigFile = tmpFile.Name()
	defer func() { ConfigFile = originalConfigFile }() // Restore after test

	// 4. Run the function
	if err := loadConfig(); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// 5. Assert results
	if appConfig.Settings.TargetTempLegio != 65.0 {
		t.Errorf("Expected 65.0, got %f", appConfig.Settings.TargetTempLegio)
	}
}

// Test saving and loading the state
func TestStateReadWrite(t *testing.T) {
	// 1. Create temp file
	tmpFile, err := os.CreateTemp("", "state_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// 2. Override global file path
	originalStateFile := StateFile
	StateFile = tmpFile.Name()
	defer func() { StateFile = originalStateFile }()

	// 3. Test Update (Write)
	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	updateState(testTime)

	// 4. Test Load (Read)
	loadState()

	if !currentState.LastSanitization.Equal(testTime) {
		t.Errorf("Expected %v, got %v", testTime, currentState.LastSanitization)
	}
}

// Test API interaction using a Mock Server (no real internet needed)
func TestGetTemperature(t *testing.T) {
	// 1. Create a Mock Server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the URL is correct
		expectedPath := "/stream/sensor/GatewayID/SensorID"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Return fake JSON response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "data": { "temperature": 42.5, "water_temperature": 0 } }`))
	}))
	defer mockServer.Close()

	// 2. Configure app to use Mock Server
	appConfig.API.BaseURL = mockServer.URL
	appConfig.IDs.SolarManagerID = "GatewayID"

	// Manually set a token to bypass login check in getTemperature
	bearerToken = "test-token"

	// 3. Call the function
	temp, err := getTemperature("SensorID")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// 4. Assert
	if temp != 42.5 {
		t.Errorf("Expected 42.5, got %f", temp)
	}
}
