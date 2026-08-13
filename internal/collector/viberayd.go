package collector

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

// ViberaydClient polls the Viberayd management API (JSON endpoints).
type ViberaydClient struct {
	client  *Client
	apiURL  string
	subURL  string
}

func NewViberaydClient(apiURL, subURL string, timeout time.Duration) *ViberaydClient {
	return &ViberaydClient{
		client: NewClient(timeout),
		apiURL: apiURL,
		subURL: subURL,
	}
}

// ViberaydStats mirrors GET /api/stats.
type ViberaydStats struct {
	Total       int    `json:"total"`
	Working     int    `json:"working"`
	Failed      int    `json:"failed"`
	Unreachable int    `json:"unreachable"`
	UpdatedAt   string `json:"updated_at"`
}

// ViberaydConfig mirrors one ConfigEntry from GET /api/configs.
type ViberaydConfig struct {
	Raw          string `json:"raw"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"`
	SourceURL    string `json:"source_url"`
	FirstSeen    string `json:"first_seen"`
	LastTested   string `json:"last_tested"`
	LastSuccess  string `json:"last_success"`
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
	State        string `json:"state"`
	LatencyMs    int    `json:"latency_ms"`
}

// ViberaydConfigPage mirrors GET /api/configs.
type ViberaydConfigPage struct {
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
	Total   int              `json:"total"`
	Configs []ViberaydConfig `json:"configs"`
}

// FetchStats returns the current stats snapshot. On error it returns the
// error; the caller marks the daemon unreachable.
func (v *ViberaydClient) FetchStats(ctx context.Context) (ViberaydStats, error) {
	body, err := v.client.Get(ctx, v.apiURL+"/api/stats")
	if err != nil {
		return ViberaydStats{}, err
	}
	var s ViberaydStats
	if err := json.Unmarshal(body, &s); err != nil {
		return ViberaydStats{}, err
	}
	return s, nil
}

// FetchConfigs fetches the config table. stateFilter (optional) is applied
// backend-side; empty string returns all.
func (v *ViberaydClient) FetchConfigs(ctx context.Context, page, perPage int, stateFilter string) (ViberaydConfigPage, error) {
	url := v.apiURL + "/api/configs?page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
	body, err := v.client.Get(ctx, url)
	if err != nil {
		return ViberaydConfigPage{}, err
	}
	var p ViberaydConfigPage
	if err := json.Unmarshal(body, &p); err != nil {
		return ViberaydConfigPage{}, err
	}
	if stateFilter != "" && len(p.Configs) > 0 {
		filtered := p.Configs[:0]
		for _, c := range p.Configs {
			if c.State == stateFilter {
				filtered = append(filtered, c)
			}
		}
		p.Configs = filtered
	}
	return p, nil
}

// FetchURLs returns the subscription URL list.
func (v *ViberaydClient) FetchURLs(ctx context.Context) ([]string, error) {
	body, err := v.client.Get(ctx, v.apiURL+"/api/urls")
	if err != nil {
		return nil, err
	}
	var urls []string
	if err := json.Unmarshal(body, &urls); err != nil {
		return nil, err
	}
	return urls, nil
}

// SubURL returns the subscription endpoint for clients (informational).
func (v *ViberaydClient) SubURL() string { return v.subURL }
