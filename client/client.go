package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// ProgressReader wraps an io.Reader to track download progress
type ProgressReader struct {
	reader     io.Reader
	total      int64
	read       int64
	url        string
	lastUpdate time.Time
}

func NewProgressReader(reader io.Reader, total int64, url string) *ProgressReader {
	return &ProgressReader{
		reader:     reader,
		total:      total,
		url:        url,
		lastUpdate: time.Now(),
	}
}

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.read += int64(n)

	// Update progress every second
	if time.Since(pr.lastUpdate) >= time.Second {
		percent := float64(pr.read) / float64(pr.total) * 100
		fmt.Printf("[!] Downloading %s: %.2f%% (%d/%d bytes)\n", pr.url, percent, pr.read, pr.total)
		pr.lastUpdate = time.Now()
	}

	return n, err
}

type Client struct {
	HTTPClient *http.Client
	Limiter    *rate.Limiter
	UserAgent  string
}

type AuthConfig struct {
	Username string
	Password string
	Bearer   string
	Headers  string
}

func NewClient(rateLimit int, insecure bool) *Client {
	transport := &http.Transport{
		MaxIdleConns:        1,                // Single connection to align with main.go
		MaxIdleConnsPerHost: 1,                // Single connection per host
		MaxConnsPerHost:     1,                // Limit to one connection to avoid concurrency
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   false,            // Disable HTTP/2 to ensure keep-alive
	}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		HTTPClient: &http.Client{
			Timeout:   600 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		Limiter: rate.NewLimiter(rate.Limit(rateLimit), 1),
	}
}

func NewClientWithHTTPClient(rateLimit int, insecure bool, httpClient *http.Client, userAgent string) *Client {
	// Respect the provided httpClient's transport settings, only set TLSClientConfig if insecure is true and not already set
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = &http.Transport{
			MaxIdleConns:        1,
			MaxIdleConnsPerHost: 1,
			MaxConnsPerHost:     1,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   false,
		}
	}
	if insecure && (transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify) {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient.Transport = transport
	// Override timeout for downloads
	httpClient.Timeout = 600 * time.Second
	return &Client{
		HTTPClient: httpClient,
		Limiter:    rate.NewLimiter(rate.Limit(rateLimit), 1),
		UserAgent:  userAgent,
	}
}

func (c *Client) MakeRequest(url string, auth AuthConfig) (*http.Response, error) {
	if err := c.Limiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("rate limiter error: %v", err)
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %v", err)
		}

		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
		req.Header.Set("Connection", "keep-alive") // Ensure keep-alive
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
			// Only retry on timeout or specific SOCKS5 connection errors
			if netErr, ok := err.(net.Error); ok && (netErr.Timeout() || isConnectionClosedError(err)) && attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt-1)) * time.Second
				fmt.Printf("[!] Request to %s failed: %v, retrying in %v (attempt %d/%d)\n", url, err, backoff, attempt, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, fmt.Errorf("request failed: %v", err)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			if authHeader := resp.Header.Get("Www-Authenticate"); authHeader != "" {
				resp.Body.Close()
				return nil, fmt.Errorf("unauthorized: %s", authHeader)
			}
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status: %s", resp.Status)
		}

		// Wrap response body with progress reader if Content-Length is available
		if contentLength := resp.ContentLength; contentLength > 0 {
			resp.Body = io.NopCloser(NewProgressReader(resp.Body, contentLength, url))
		}

		return resp, nil
	}
	return nil, fmt.Errorf("request failed after %d retries", maxRetries)
}

// isConnectionClosedError checks if the error is related to a closed connection
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "connection reset by peer") ||
		strings.Contains(err.Error(), "broken pipe")
}