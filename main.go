package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"

	"dockdiver/client"
	"dockdiver/registry"
	"dockdiver/useragents"
)

func printASCIIArt() {
	art := `
       __           __       ___                
  ____/ /___  _____/ /______/ (_)   _____  _____
 / __  / __ \/ ___/ //_/ __  / / | / / _ \/ ___/
/ /_/ / /_/ / /__/ ,< / /_/ / /| |/ /  __/ /   @MachIaVellill
\__,_/\____/\___/_/|_|\__,_/_/ |___/\___/_/     
`
	fmt.Println(art)
}

// socks5ConnManager manages the SOCKS5 connection with thread-safe access
type socks5ConnManager struct {
	conn *net.Conn
	mu   sync.Mutex
}

// connectSOCKS5 establishes a single SOCKS5 connection with retries and detailed error logging
func connectSOCKS5(proxyHost, proxyUsername, proxyPassword, targetAddr string, maxRetries int, warning, success func(...interface{}) string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   60 * time.Second,
		KeepAlive: 60 * time.Second,
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			fmt.Printf("%s Retrying SOCKS5 proxy connection: %s (attempt %d/%d)\n", warning("[!]"), proxyHost, attempt, maxRetries)
		}
		conn, err := dialer.Dial("tcp", proxyHost)
		if err != nil {
			if attempt == maxRetries {
				return nil, fmt.Errorf("failed to connect to SOCKS5 proxy %s: %v", proxyHost, err)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}

		// Set read timeout to avoid hanging on response
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// SOCKS5 handshake
		authMethods := []byte{0x00} // No authentication
		if proxyUsername != "" && proxyPassword != "" {
			authMethods = append(authMethods, 0x02) // Username/password
		}
		_, err = conn.Write(append([]byte{0x05, byte(len(authMethods))}, authMethods...))
		if err != nil {
			conn.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("SOCKS5 handshake failed (write auth methods): %v", err)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}
		resp := make([]byte, 2)
		_, err = io.ReadFull(conn, resp)
		if err != nil || resp[0] != 0x05 {
			conn.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("SOCKS5 handshake failed (read auth response): %v, response: %x", err, resp)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}
		if resp[1] == 0x02 { // Username/password auth
			user := []byte(proxyUsername)
			pass := []byte(proxyPassword)
			authReq := []byte{0x01, byte(len(user))}
			authReq = append(authReq, user...)
			authReq = append(authReq, byte(len(pass)))
			authReq = append(authReq, pass...)
			_, err = conn.Write(authReq)
			if err != nil {
				conn.Close()
				if attempt == maxRetries {
					return nil, fmt.Errorf("SOCKS5 auth failed (write credentials): %v", err)
				}
				time.Sleep(time.Second * time.Duration(attempt))
				continue
			}
			authResp := make([]byte, 2)
			_, err = io.ReadFull(conn, authResp)
			if err != nil || authResp[1] != 0x00 {
				conn.Close()
				if attempt == maxRetries {
					return nil, fmt.Errorf("SOCKS5 authentication failed (read auth response): %v, response: %x", err, authResp)
				}
				time.Sleep(time.Second * time.Duration(attempt))
				continue
			}
		} else if resp[1] != 0x00 {
			conn.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("SOCKS5 no acceptable auth methods, response: %x", resp)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}

		// SOCKS5 connect request with hostname resolution (socks5h)
		host, port, err := net.SplitHostPort(targetAddr)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("invalid target address %s: %v", targetAddr, err)
		}
		req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))} // 0x03 for domain name
		req = append(req, []byte(host)...)
		portNum, _ := strconv.Atoi(port)
		req = append(req, byte(portNum>>8), byte(portNum))
		_, err = conn.Write(req)
		if err != nil {
			conn.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("failed to send SOCKS5 connect request for %s: %v", targetAddr, err)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}
		resp = make([]byte, 10) // Larger buffer for connect response
		_, err = io.ReadFull(conn, resp)
		if err != nil || resp[1] != 0x00 {
			conn.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("SOCKS5 connect request for %s failed: %v, response: %x", targetAddr, err, resp)
			}
			time.Sleep(time.Second * time.Duration(attempt))
			continue
		}

		// Reset read deadline after successful handshake
		conn.SetReadDeadline(time.Time{})
		return conn, nil // Connection successful
	}
	return nil, fmt.Errorf("failed to establish SOCKS5 connection after %d attempts", maxRetries)
}

func main() {
	printASCIIArt()

	urlFlag := flag.String("url", "", "Base URL or hostname of the Docker registry (e.g., http://example.com or example.com)")
	port := flag.Int("port", 5000, "Port of the registry (used if not specified in URL)")
	username := flag.String("username", "", "Username for Basic authentication")
	password := flag.String("password", "", "Password for Basic authentication")
	bearer := flag.String("bearer", "", "Bearer token for Authorization")
	headers := flag.String("headers", "", "Custom headers as JSON (e.g., '{\"X-Custom\": \"Value\"}')")
	rate := flag.Int("rate", 3, "Requests per second")
	outputDir := flag.String("dir", "docker_dump", "Output directory for dumped files")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification")
	proxy := flag.String("proxy", "", "Proxy URL (e.g., http://127.0.0.1:8080, https://proxy.com:8443, or socks5://127.0.0.1:1080)")
	proxyUsername := flag.String("proxy-username", "", "Username for SOCKS5 proxy authentication")
	proxyPassword := flag.String("proxy-password", "", "Password for SOCKS5 proxy authentication")
	list := flag.Bool("list", false, "List all repositories")
	dumpAll := flag.Bool("dump-all", false, "Dump all repositories")
	dump := flag.String("dump", "", "Specific repository to dump")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP request timeout (e.g., 10s, 500ms)")

	flag.Parse()

	success := color.New(color.FgGreen).SprintFunc()
	errorColor := color.New(color.FgRed).SprintFunc()
	warning := color.New(color.FgYellow).SprintFunc()

	// Check if no flags are provided
	if flag.NFlag() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Select a single User-Agent for the entire session
	userAgent := useragents.GetRandomUserAgent()
	fmt.Printf("%s Selected User-Agent: %s\n", success("[+]"), userAgent)

	// Create custom HTTP transport
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: *insecure},
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		MaxConnsPerHost:     1,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   false,
	}

	// Variables to hold proxy details and SOCKS5 connection manager
	var proxyHost string
	var connManager socks5ConnManager

	// Configure proxy if provided
	useProxy := *proxy != ""
	if *proxy != "" {
		proxyURL, err := url.Parse(*proxy)
		if err != nil {
			fmt.Printf("%s Invalid proxy URL: %v\n", errorColor("[-]"), err)
			os.Exit(1)
		}
		fmt.Printf("%s Connecting to proxy: %s\n", warning("[!]"), proxyURL.String())
		proxyHost = proxyURL.Host
		switch proxyURL.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
			transport.ProxyConnectHeader = http.Header{
				"User-Agent": []string{userAgent},
			}
			if proxyURL.Scheme == "https" {
				transport.TLSClientConfig = &tls.Config{
					InsecureSkipVerify: *insecure,
					MinVersion:         tls.VersionTLS12,
				}
			}
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 60 * time.Second,
				}
				return dialer.DialContext(ctx, network, addr)
			}
			fmt.Printf("%s Using HTTP/HTTPS proxy: %s\n", success("[+]"), proxyURL.String())
		case "socks5":
			// Parse the URL to extract the hostname
			parsedURL, err := url.Parse(*urlFlag)
			if err != nil {
				fmt.Printf("%s Invalid URL: %v\n", errorColor("[-]"), err)
				os.Exit(1)
			}
			host := parsedURL.Hostname()
			if host == "" {
				host = *urlFlag // Fallback to raw input if no scheme
			}
			// Use port from URL if specified, otherwise use default port
			portStr := parsedURL.Port()
			if portStr == "" {
				portStr = strconv.Itoa(*port)
			}
			targetAddr := fmt.Sprintf("%s:%s", host, portStr)
			// Establish SOCKS5 connection
			conn, err := connectSOCKS5(proxyHost, *proxyUsername, *proxyPassword, targetAddr, 3, warning, success)
			if err != nil {
				fmt.Printf("%s %v\n", errorColor("[-]"), err)
				fmt.Printf("%s Debug: Test proxy with: curl -v --proxy socks5h://%s%s %s\n", warning("[!]"), proxyURL.Host, proxyURL.Path, *urlFlag)
				os.Exit(1)
			}
			connManager.conn = &conn
			fmt.Printf("%s SOCKS5 connection established to %s\n", success("[+]"), targetAddr)

			// Set up DialContext to reuse or re-establish SOCKS5 connection silently
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				connManager.mu.Lock()
				defer connManager.mu.Unlock()

				// Check if connection is alive
				if connManager.conn != nil && *connManager.conn != nil {
					_, err := (*connManager.conn).Write([]byte{})
					if err == nil {
						return *connManager.conn, nil
					}
					// Connection is dead, close it
					(*connManager.conn).Close()
					connManager.conn = nil
				}

				// Re-establish connection silently
				conn, err := connectSOCKS5(proxyHost, *proxyUsername, *proxyPassword, targetAddr, 1, warning, success)
				if err != nil {
					return nil, fmt.Errorf("failed to re-establish SOCKS5 connection: %v", err)
				}
				connManager.conn = &conn
				return *connManager.conn, nil
			}
		default:
			fmt.Printf("%s Unsupported proxy scheme: %s\n", errorColor("[-]"), proxyURL.Scheme)
			os.Exit(1)
		}
	}

	if !useProxy {
		// Default DialContext for non-proxy cases
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 60 * time.Second,
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}

	// Create custom HTTP client with User-Agent, timeout, and keep-alive headers
	httpClient := &http.Client{
		Timeout:   *timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			req.Header.Set("User-Agent", userAgent)
			req.Header.Set("Connection", "keep-alive")
			return nil
		},
	}

	// Pass the custom HTTP client and User-Agent to the client package
	cli := client.NewClientWithHTTPClient(*rate, *insecure, httpClient, userAgent)

	// Define auth for registry requests
	auth := client.AuthConfig{
		Username: *username,
		Password: *password,
		Bearer:   *bearer,
		Headers:  *headers,
	}

	// Validate URL
	validatedURL, urlPort, err := validateAndNormalizeURL(*urlFlag, *port, *insecure, httpClient, userAgent)
	if err != nil {
		fmt.Printf("%s URL validation failed: %v\n", errorColor("[-]"), err)
		os.Exit(1)
	}
	fmt.Printf("%s Using validated URL: %s\n", success("[+]"), validatedURL)

	// Detect and display registry version
	version, err := detectRegistryVersion(validatedURL, urlPort, httpClient, userAgent)
	if err != nil {
		fmt.Printf("%s Error detecting registry version: %v\n", errorColor("[-]"), err)
		connManager.mu.Lock()
		if connManager.conn != nil {
			(*connManager.conn).Close()
			connManager.conn = nil
		}
		connManager.mu.Unlock()
		os.Exit(1)
	}
	fmt.Printf("%s Registry API Version: %s\n", success("[+]"), version)

	// Prompt for actions if no action flags are provided
	hasAction := *list || *dumpAll || *dump != ""
	if !hasAction {
		fmt.Printf("%s No action specified. Please choose one of the following:\n", warning("[!]"))
		fmt.Println("  -list : List all repositories")
		fmt.Println("  -dump <repository> : Dump a specific repository")
		fmt.Println("  -dump-all : Dump all repositories")
		os.Exit(1)
	}

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Printf("%s Failed to create output directory %s: %v\n", errorColor("[-]"), *outputDir, err)
		os.Exit(1)
	}

	// Proceed with actions
	if auth.Username == "" && auth.Password == "" && auth.Bearer == "" {
		fmt.Printf("%s No authentication provided (no username/password or bearer token). Proceeding without auth...\n", warning("[!]"))
	}

	if *insecure {
		fmt.Printf("%s TLS verification disabled (insecure mode enabled)\n", warning("[!]"))
	}

	// Handle list action
	if *list {
		repos, err := registry.ListRepositories(validatedURL, urlPort, auth, cli)
		if err != nil {
			fmt.Printf("%s Error listing repositories: %v\n", errorColor("[-]"), err)
			if strings.Contains(err.Error(), "401") {
				fmt.Printf("%s Authentication required. Please provide valid credentials using -username and -password or -bearer.\n", warning("[!]"))
				if useProxy {
					fmt.Printf("%s Debug: Check the registry response with: curl --proxy %s -u %s:%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *proxy, *username, *password, validatedURL, urlPort)
				} else {
					fmt.Printf("%s Debug: Check the registry response with: curl -u %s:%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *username, *password, validatedURL, urlPort)
				}
			} else {
				fmt.Printf("%s Try providing authentication with -username and -password or -bearer, or check server availability.\n", warning("[!]"))
				if useProxy {
					fmt.Printf("%s Debug: Check the registry response with: curl --proxy socks5h://%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *proxy, validatedURL, urlPort)
				} else {
					fmt.Printf("%s Debug: Check the registry response with: curl %s:%d/v2/_catalog?n=100\n", warning("[!]"), validatedURL, urlPort)
				}
			}
			connManager.mu.Lock()
			if connManager.conn != nil {
				(*connManager.conn).Close()
				connManager.conn = nil
			}
			connManager.mu.Unlock()
			os.Exit(1)
		}
		if len(repos) == 0 {
			fmt.Printf("%s No repositories found. The registry may be empty, requires authentication, or the proxy failed to route the request.\n", warning("[!]"))
			if useProxy {
				fmt.Printf("%s Debug: Check the registry response with: curl --proxy socks5h://%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *proxy, validatedURL, urlPort)
				if auth.Username != "" && auth.Password != "" {
					fmt.Printf("%s Debug with auth: curl --proxy socks5h://%s -u %s:%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *proxy, *username, *password, validatedURL, urlPort)
				}
			} else {
				fmt.Printf("%s Debug: Check the registry response with: curl %s:%d/v2/_catalog?n=100\n", warning("[!]"), validatedURL, urlPort)
				if auth.Username != "" && auth.Password != "" {
					fmt.Printf("%s Debug with auth: curl -u %s:%s %s:%d/v2/_catalog?n=100\n", warning("[!]"), *username, *password, validatedURL, urlPort)
				}
			}
			// Make a debug request to /v2/_catalog to log response
			endpoint := fmt.Sprintf("%s:%d/v2/_catalog?n=100", validatedURL, urlPort)
			req, err := http.NewRequest("GET", endpoint, nil)
			if err == nil {
				req.Header.Set("User-Agent", userAgent)
				req.Header.Set("Connection", "keep-alive")
				if auth.Username != "" && auth.Password != "" {
					req.SetBasicAuth(auth.Username, auth.Password)
				}
				resp, err := httpClient.Do(req)
				if err != nil {
					fmt.Printf("%s Debug: Failed to query /v2/_catalog: %v\n", errorColor("[-]"), err)
				} else {
					defer resp.Body.Close()
					fmt.Printf("%s Debug: /v2/_catalog response status: %s\n", warning("[!]"), resp.Status)
					body, _ := io.ReadAll(resp.Body)
					fmt.Printf("%s Debug: /v2/_catalog response body: %s\n", warning("[!]"), string(body))
				}
			}
		} else {
			for _, repo := range repos {
				fmt.Printf("%s %s\n", success("[+]"), repo)
			}
		}
	}

	// Handle dump-all action
	if *dumpAll {
		if err := registry.DumpAllRepositories(validatedURL, urlPort, auth, *outputDir, cli); err != nil {
			fmt.Printf("%s Error dumping all repositories: %v\n", errorColor("[-]"), err)
			connManager.mu.Lock()
			if connManager.conn != nil {
				(*connManager.conn).Close()
				connManager.conn = nil
			}
			connManager.mu.Unlock()
			os.Exit(1)
		}
		fmt.Printf("%s Dump completed successfully\n", success("[+]"))
	}

	// Handle dump specific repository
	if *dump != "" {
		if err := registry.DumpRepository(validatedURL, urlPort, *dump, auth, *outputDir, cli); err != nil {
			fmt.Printf("%s Error dumping repository %s: %v\n", errorColor("[-]"), *dump, err)
			connManager.mu.Lock()
			if connManager.conn != nil {
				(*connManager.conn).Close()
				connManager.conn = nil
			}
			connManager.mu.Unlock()
			os.Exit(1)
		}
		fmt.Printf("%s Dumped %s successfully\n", success("[+]"), *dump)
	}

	// Clean up SOCKS5 connection if it exists
	connManager.mu.Lock()
	if connManager.conn != nil {
		(*connManager.conn).Close()
		connManager.conn = nil
	}
	connManager.mu.Unlock()
}

// detectRegistryVersion queries the /v2/ endpoint to identify the API version
func detectRegistryVersion(url string, port int, client *http.Client, userAgent string) (string, error) {
	endpoint := fmt.Sprintf("%s:%d/v2/", url, port)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Connection", "keep-alive")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query /v2/: %v", err)
	}
	defer resp.Body.Close()

	// Check Docker-Distribution-API-Version header
	version := resp.Header.Get("Docker-Distribution-API-Version")
	if version == "" {
		// Infer from status: 200 or 401 indicates v2
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			version = "v2"
		} else {
			return "", fmt.Errorf("unknown API version, status: %d", resp.StatusCode)
		}
	}
	return version, nil
}

func validateAndNormalizeURL(inputURL string, defaultPort int, insecure bool, client *http.Client, userAgent string) (string, int, error) {
	inputURL = strings.TrimRight(inputURL, "/")
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", 0, fmt.Errorf("invalid URL format: %v", err)
	}

	// Determine the port: use URL port if specified, otherwise use defaultPort
	port := defaultPort
	if parsedURL.Port() != "" {
		port, err = strconv.Atoi(parsedURL.Port())
		if err != nil {
			return "", 0, fmt.Errorf("invalid port in URL: %v", err)
		}
	}

	if parsedURL.Scheme == "" {
		domain := parsedURL.String()
		if domain == "" {
			domain = inputURL
		}

		// Try HTTP
		httpURL := fmt.Sprintf("http://%s:%d/v2/", domain, port)
		fmt.Printf("[!] Testing HTTP URL: %s\n", httpURL)
		if err := testURL(httpURL, insecure, client, userAgent); err == nil {
			return fmt.Sprintf("http://%s", domain), port, nil
		} else {
			fmt.Printf("[!] HTTP test failed: %v\n", err)
		}

		// Try HTTPS
		httpsURL := fmt.Sprintf("https://%s:%d/v2/", domain, port)
		fmt.Printf("[!] Testing HTTPS URL: %s\n", httpsURL)
		if err := testURL(httpsURL, insecure, client, userAgent); err == nil {
			return fmt.Sprintf("https://%s", domain), port, nil
		} else {
			fmt.Printf("[!] HTTPS test failed: %v\n", err)
		}

		return "", 0, fmt.Errorf("domain '%s' is not reachable on HTTP or HTTPS with port %d", domain, port)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", 0, fmt.Errorf("unsupported scheme '%s'; use 'http' or 'https'", parsedURL.Scheme)
	}

	testURLStr := fmt.Sprintf("%s:%d/v2/", parsedURL.String(), port)
	fmt.Printf("[!] Testing URL: %s\n", testURLStr)
	if err := testURL(testURLStr, insecure, client, userAgent); err == nil {
		return parsedURL.String(), port, nil
	}
	return "", 0, fmt.Errorf("URL '%s' is not reachable on port %d: %v", parsedURL.String(), port, err)
}

func testURL(testURL string, insecure bool, client *http.Client, userAgent string) (err error) {
	for i := 0; i < 2; i++ {
		req, err := http.NewRequest("GET", testURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Connection", "keep-alive")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[!] Request attempt %d failed: %v\n", i+1, err)
			time.Sleep(1000 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
			return nil
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return fmt.Errorf("connection failed after retries")
}