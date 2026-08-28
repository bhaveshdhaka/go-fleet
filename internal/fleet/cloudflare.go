package fleet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Cloudflare read client (WO-7): the three GET calls doctor needs. Token
// comes from the site's secrets/cloudflare.env and is used only as a
// request header — never logged, journaled, or returned.

// cloudflareAPIBase is a var only so tests can point it at a local
// httptest server; production binaries use the pinned CF endpoint.
var cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

func cloudflareAPI(token, method, path string) (json.RawMessage, error) {
	return cloudflareAPIRaw(token, method, path, nil)
}

func cloudflareAPIRaw(token, method, path string, body []byte) (json.RawMessage, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, cloudflareAPIBase+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, cloudflareAPIBase+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("CF API %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API %s %s unreachable: %v", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("CF API %s %s: read failed: %v", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CF API %s %s -> HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	var envelope struct {
		Success bool            `json:"success"`
		Errors  json.RawMessage `json:"errors"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("CF API %s %s: bad envelope: %v", method, path, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("CF API %s %s failed: %s", method, path, string(envelope.Errors))
	}
	return envelope.Result, nil
}

func TunnelStatus(token, accountID, tunnelID string) (string, error) {
	res, err := cloudflareAPI(token, "GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID)
	if err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

func TunnelIngress(token, accountID, tunnelID string) ([]string, error) {
	res, err := cloudflareAPI(token, "GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations")
	if err != nil {
		return nil, err
	}
	var out struct {
		Config struct {
			Ingress []struct {
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"config"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	var hosts []string
	for _, i := range out.Config.Ingress {
		if i.Hostname != "" {
			hosts = append(hosts, i.Hostname)
		}
	}
	return hosts, nil
}

func GetCnames(token, zoneID, host string) ([]map[string]any, error) {
	res, err := cloudflareAPI(token, "GET",
		"/zones/"+zoneID+"/dns_records?type=CNAME&name="+host+"&per_page=100")
	if err != nil {
		return nil, err
	}
	var recs []map[string]any
	if err := json.Unmarshal(res, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}
