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
	"runtime"
	"time"

	"github.com/joho/godotenv"
)

const (
	ShutdownDelay            = 5 * time.Second
	StatusUpdateInterval     = 3 * time.Minute
	ShutdownPollInterval     = 10 * time.Second
	MinUptimeBeforeShutdown  = 1 * time.Minute
	MaxRetries                = 3
	RetryDelay                = 5 * time.Second
)

type AuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        struct {
		ID string `json:"id"`
	} `json:"user"`
}

// Update the Device struct to properly handle ISO timestamps
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
	// Set up logging to include timestamps
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	for {
		if err := run(); err != nil {
			log.Printf("Application error: %v", err)
			log.Printf("Waiting 30 seconds before retrying...")
			time.Sleep(30 * time.Second)
			continue
		}
	}
}

func run() error {
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	supabaseURL = os.Getenv("SUPABASE_URL")
	supabaseKey = os.Getenv("SUPABASE_KEY")
	email := os.Getenv("USER_EMAIL")
	password := os.Getenv("USER_PASSWORD")

	if supabaseURL == "" || supabaseKey == "" || email == "" || password == "" {
		return fmt.Errorf("required environment variables are missing")
	}

	user, err := signIn(email, password)
	if err != nil {
		return fmt.Errorf("auth failed: %v", err)
	}
	authToken = user.AccessToken
	log.Printf("Authenticated as user %s", user.User.ID)

	deviceName := getHostname()

	// ensure a row exists for this device
	if err := createDevice(user.User.ID, deviceName); err != nil {
		return fmt.Errorf("failed to create device row: %v", err)
	}

	// Create error channel for goroutines
	errChan := make(chan error, 2)

	// Start status updater
	go func() {
		errChan <- updateDeviceStatus(user.User.ID, deviceName)
	}()

	// Start shutdown listener
	go func() {
		errChan <- listenForShutdownRequests(user.User.ID, deviceName)
	}()

	// Wait for any error
	return <-errChan
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
func updateDeviceStatus(userID, name string) error {
	ticker := time.NewTicker(StatusUpdateInterval)
	defer ticker.Stop()
	for {
		now := time.Now().UTC().Format(time.RFC3339)
		if err := patchDevice(userID, name, map[string]interface{}{
			"status":          "on",
			"last_seen":       now,
			"first_online_at": now,
		}); err != nil {
			return fmt.Errorf("failed to update device status: %v", err)
		}
		<-ticker.C
	}
}

// listenForShutdownRequests polls for pending shutdown requests
func listenForShutdownRequests(userID, name string) error {
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
	// First check if device exists
	devices, err := fetchDevices(userID, name)
	if err != nil {
		return fmt.Errorf("failed to check existing device: %v", err)
	}
	if len(devices) > 0 {
		// Device exists, no need to create
		return nil
	}

	url := supabaseURL + "/rest/v1/devices"
	payload := map[string]interface{}{
		"user_id":         userID,
		"name":            name,
		"status":          "unknown",
		"first_online_at": time.Now().UTC().Format(time.RFC3339),
		"last_seen":       time.Now().UTC().Format(time.RFC3339),
	}
	// Send as single object, not array
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("apikey", supabaseKey)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")

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
func patchDevice(userID, name string, data map[string]interface{}) error {
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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// if not found, try to create the device
		devices, _ := fetchDevices(userID, name)
		if len(devices) == 0 {
			if err := createDevice(userID, name); err != nil {
				log.Println("createDevice:", err)
				return err
			}
		}
		// now you know a row exists, so just patch:
		patchDevice(userID, name, data)
		return nil
	}

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PATCH status %d: %s", resp.StatusCode, b)
	}
	log.Printf("PATCH succeeded for %q: %v", name, data)
	return nil
}

// handleShutdown applies logic, marks status, then shuts down
func handleShutdown(userID, name string, dev *Device, req map[string]interface{}) {
    // Parse expires_at from ISO string instead of float64
    if expiresStr, ok := req["expires_at"].(string); ok {
        expiresAt, err := time.Parse(time.RFC3339, expiresStr)
        if err != nil {
            log.Printf("Failed to parse expires_at: %v", err)
            return
        }
        if time.Now().UTC().After(expiresAt) {
            patchDevice(userID, name, map[string]interface{}{
                "shutdown_requested": map[string]string{"status": "expired"},
            })
            return
        }
    }

    // Rest of the function remains same
    if dev.FirstOnlineAt != nil && time.Since(*dev.FirstOnlineAt) < MinUptimeBeforeShutdown {
        log.Printf("Device %s too recently started, skipping shutdown", name)
        return
    }

    log.Println("Shutting down…")
    patchDevice(userID, name, map[string]interface{}{
        "shutdown_requested": map[string]string{"status": "shutting_down"},
        "status":            "off",
        "last_seen":        time.Now().UTC().Format(time.RFC3339),
    })
    time.Sleep(ShutdownDelay)

    // Execute shutdown command based on the OS
    var cmd *exec.Cmd
    switch os := runtime.GOOS; os {
    case "windows":
        cmd = exec.Command("shutdown", "/s", "/t", "0")
    case "linux":
        // Try systemd's loginctl first, then regular shutdown
	
		cmd = exec.Command("systemctl", "poweroff")
	
    case "darwin":
        cmd = exec.Command("osascript", "-e", 
			"tell application \"System Events\" to shut down")
    default:
        log.Printf("Unsupported OS: %s. Shutdown command not executed.", os)
        return
    }

    // Execute with error logging
    if out, err := cmd.CombinedOutput(); err != nil {
        log.Printf("Shutdown command failed: %v\nOutput: %s", err, out)
        patchDevice(userID, name, map[string]interface{}{
            "shutdown_requested": map[string]string{
                "status": "failed",
                "error": err.Error(),
            },
        })
        return
    }

    // Final status update before shutdown
    patchDevice(userID, name, map[string]interface{}{
        "shutdown_requested": map[string]string{"status": "done"},
        "status": "off",
    })
    log.Println("Shutdown command executed successfully")
}

// getHostname wraps os.Hostname
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	return h
}