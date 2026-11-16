package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ActivateExistingInstance checks whether a TinyDrop instance is already running on the configured port.
// If running, it asks that instance to open its web UI in the browser and returns true.
func ActivateExistingInstance(port int) bool {
	if port <= 0 || port > 65535 {
		port = 20999
	}

	client := &http.Client{
		Timeout: 1200 * time.Millisecond,
		Transport: &http.Transport{
			Proxy: nil, // bypass proxy for local loopback
		},
	}

	// 1. First ping to verify if an instance is alive
	pingURL := fmt.Sprintf("http://127.0.0.1:%d/api/ping", port)
	resp, err := client.Get(pingURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return false
	}
	var pingData struct {
		Ok bool `json:"ok"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&pingData)
	_ = resp.Body.Close()
	if !pingData.Ok {
		return false
	}

	// 2. Request the running instance to open/restore its web UI dashboard
	openURL := fmt.Sprintf("http://127.0.0.1:%d/api/pc/open-dashboard", port)
	openReq, err := http.NewRequest(http.MethodPost, openURL, nil)
	if err != nil {
		return true
	}
	openResp, err := client.Do(openReq)
	if err == nil && openResp != nil {
		_ = openResp.Body.Close()
	}

	return true
}
