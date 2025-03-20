package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"dockdiver/useragents"
)

type Client struct {
	HTTPClient *http.Client
	Limiter    *rate.Limiter
	UserAgents []string
}

type AuthConfig struct {
	Username string
	Password string
	Bearer   string
	Headers  string
}

func NewClient(rateLimit int) *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		Limiter:    rate.NewLimiter(rate.Limit(rateLimit), 1),
		UserAgents: useragents.UserAgents,
	}
}

func (c *Client) MakeRequest(url string, auth AuthConfig) (*http.Response, error) {
	if err := c.Limiter.Wait(context.Background()); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", c.UserAgents[rand.Intn(len(c.UserAgents))])
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	if auth.Username != "" && auth.Password != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
	if auth.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+auth.Bearer)
	}
	if auth.Headers != "" {
		var customHeaders map[string]string
		if err := json.Unmarshal([]byte(auth.Headers), &customHeaders); err != nil {
			return nil, fmt.Errorf("invalid headers JSON: %v", err)
		}
		for k, v := range customHeaders {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		if authHeader := resp.Header.Get("Www-Authenticate"); authHeader != "" {
			return nil, fmt.Errorf("unauthorized: %s", authHeader)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	return resp, nil
}