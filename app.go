package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/joho/godotenv"
)

const (
	ShutdownDelay            = 5 * time.Second
	StatusUpdateInterval     = 3 * time.Minute
	ShutdownPollInterval     = 10 * time.Second
	MinUptimeBeforeShutdown  = 1 * time.Minute
)

type AuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        struct {
		ID string `json:"id"`
	} `json:"user"`
}

type Device struct {
	ID              string          `json:"id"`
	UserID          string          `json:"user_id"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	LastSeen        *time.Time      `json:"last_seen"`
	FirstOnlineAt   *time.Time      `json:"first_online_at"`
	ShutdownRequest json.RawMessage `json:"shutdown_requested"`
}

var (
	supabaseURL string
	supabaseKey string
	httpClient  = &http.Client{Timeout: 10 * time.Second}
	authToken   string
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file:", err)
	}
	supabaseURL = os.Getenv("SUPABASE_URL")
	supabaseKey = os.Getenv("SUPABASE_KEY")

	user, err := signIn(os.Getenv("USER_EMAIL"), os.Getenv("USER_PASSWORD"))
	if err != nil {
		log.Fatal("Auth failed:", err)
	}
	authToken = user.AccessToken
	log.Printf("Authenticated as user %s\n", user.User.ID)

	deviceName := getHostname()

	// ensure a row exists for this device
	if err := createDevice(user.User.ID, deviceName); err != nil {
		log.Fatalf("failed to create device row: %v", err)
	}

	go updateDeviceStatus(user.User.ID, deviceName)
	listenForShutdownRequests(user.User.ID, deviceName)
}

// signIn calls Supabase Auth REST API to get an access token
func signIn(email, password string) (*AuthResponse, error) {
	url := supabaseURL + "/auth/v1/token?grant_type=password"
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth error %d: %s", resp.StatusCode, b)
	}

	var ar AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, err
	}
	return &ar, nil
}

// updateDeviceStatus periodically PATCHes status="on"
func updateDeviceStatus(userID, name string) {
	ticker := time.NewTicker(StatusUpdateInterval)
	defer ticker.Stop()
	for {
		patchDevice(userID, name, map[string]interface{}{
			"status":          "on",
			"last_seen":       time.Now().UTC(),
			"first_online_at": time.Now().UTC(),
		})
		<-ticker.C
	}
}

// listenForShutdownRequests polls for pending shutdown requests
func listenForShutdownRequests(userID, name string) {
	for {
		devices, err := fetchDevices(userID, name)
		if err != nil {
			log.Println("Fetch devices error:", err)
			time.Sleep(ShutdownPollInterval)
			continue
		}

		// if no device row yet, create it and retry
		if len(devices) == 0 {
			log.Printf("No row found for device %q, creating one…", name)
			if err := createDevice(userID, name); err != nil {
				log.Println("createDevice failed:", err)
			} else {
				log.Printf("Created device row for %q", name)
			}
			time.Sleep(ShutdownPollInterval)
			continue
		}

		device := devices[0]
		if device.ShutdownRequest != nil {
			var req map[string]interface{}
			if err := json.Unmarshal(device.ShutdownRequest, &req); err != nil {
				log.Println("Failed to parse shutdown request:", err)
			} else if status, _ := req["status"].(string); status == "pending" {
				handleShutdown(userID, name, &device, req)
			}
		}

		time.Sleep(ShutdownPollInterval)
	}
}

// fetchDevices GETs /rest/v1/devices?user_id=eq.&name=eq.
func fetchDevices(userID, name string) ([]Device, error) {
	url := supabaseURL + "/rest/v1/devices?user_id=eq." + userID +
		"&name=eq." + name + "&select=*"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch error %d: %s", resp.StatusCode, b)
	}
	var dl []Device
	if err := json.NewDecoder(resp.Body).Decode(&dl); err != nil {
		return nil, err
	}
	return dl, nil
}

// createDevice does a POST /rest/v1/devices
func createDevice(userID, name string) error {
	url := supabaseURL + "/rest/v1/devices"
	payload := map[string]interface{}{
		"user_id":         userID,
		"name":            name,
		"status":          "unknown",
		"first_online_at": time.Now().UTC(),
		"last_seen":       time.Now().UTC(),
	}
	body, _ := json.Marshal([]interface{}{payload})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	// merge-duplicates on unique constraint (user_id,name)
	req.Header.Set("Prefer", "resolution=merge-duplicates")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("createDevice status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// patchDevice PATCHes; on 404 retry createDevice once
func patchDevice(userID, name string, data map[string]interface{}) {
	url := supabaseURL + "/rest/v1/devices?user_id=eq." + userID +
		"&name=eq." + name
	body, _ := json.Marshal(data)
	req, _ := http.NewRequest("PATCH", url, bytes.NewReader(body))
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Println("PATCH error:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// if not found, try to create the device
		devices, _ := fetchDevices(userID, name)
		if len(devices) == 0 {
			if err := createDevice(userID, name); err != nil {
				log.Println("createDevice:", err)
				return
			}
		}
		// now you know a row exists, so just patch:
		patchDevice(userID, name, data)
		return
	}

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("PATCH status %d: %s", resp.StatusCode, b)
	} else {
		log.Printf("PATCH succeeded for %q: %v", name, data)
	}
}

// handleShutdown applies logic, marks status, then shuts down
func handleShutdown(userID, name string, dev *Device, req map[string]interface{}) {
	if exp, ok := req["expires_at"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			patchDevice(userID, name, map[string]interface{}{"shutdown_requested": map[string]string{"status": "expired"}})
			return
		}
	}
	if dev.FirstOnlineAt != nil && time.Since(*dev.FirstOnlineAt) < MinUptimeBeforeShutdown {
		return
	}
	patchDevice(userID, name, map[string]interface{}{"shutdown_requested": map[string]string{"status": "done"}})
	log.Println("Shutting down…")
	time.Sleep(ShutdownDelay)
	exec.Command("shutdown", "now").Run()
}

// getHostname wraps os.Hostname
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	return h
}