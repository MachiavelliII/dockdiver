package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/fatih/color"

	"dockdiver/client"
	"dockdiver/registry"
)

func main() {
	urlFlag := flag.String("url", "http://localhost", "Base URL of the Docker registry (e.g., http://example.com or example.com)")
	port := flag.Int("port", 5000, "Port of the registry (default: 5000)")
	username := flag.String("username", "", "Username for authentication")
	password := flag.String("password", "", "Password for authentication")
	bearer := flag.String("bearer", "", "Bearer token for Authorization")
	headers := flag.String("headers", "", "Custom headers as JSON (e.g., '{\"X-Custom\": \"Value\"}')")
	rate := flag.Int("rate", 10, "Requests per second (default: 10)")
	outputDir := flag.String("dir", "docker_dump", "Output directory for dumped files")
	list := flag.Bool("list", false, "List all repositories")
	dumpAll := flag.Bool("dump-all", false, "Dump all repositories")
	dump := flag.String("dump", "", "Specific repository to dump")

	flag.Parse()

	success := color.New(color.FgGreen).SprintFunc()
	errorColor := color.New(color.FgRed).SprintFunc()
	warning := color.New(color.FgYellow).SprintFunc()

	// Validate and normalize URL
	validatedURL, err := validateAndNormalizeURL(*urlFlag, *port)
	if err != nil {
		fmt.Printf("%s URL validation failed: %v\n", errorColor("[-]"), err)
		return
	}
	fmt.Printf("%s Using validated URL: %s\n", success("[+]"), validatedURL)

	cli := client.NewClient(*rate)
	auth := client.AuthConfig{
		Username: *username,
		Password: *password,
		Bearer:   *bearer,
		Headers:  *headers,
	}

	// Check if no authentication is provided
	if auth.Username == "" && auth.Password == "" && auth.Bearer == "" {
		fmt.Printf("%s No authentication provided (no username/password or bearer token). Proceeding without auth...\n", warning("[!]"))
	}

	if *list {
		repos, err := registry.ListRepositories(validatedURL, *port, auth, cli)
		if err != nil {
			fmt.Printf("%s Error listing repositories: %v\n", errorColor("[-]"), err)
			return
		}
		for _, repo := range repos {
			fmt.Printf("%s %s\n", success("[+]"), repo)
		}
	} else if *dumpAll {
		if err := registry.DumpAllRepositories(validatedURL, *port, auth, *outputDir, cli); err != nil {
			fmt.Printf("%s Error dumping all repositories: %v\n", errorColor("[-]"), err)
			return
		}
		fmt.Printf("%s Dump completed successfully\n", success("[+]"))
	} else if *dump != "" {
		if err := registry.DumpRepository(validatedURL, *port, *dump, auth, *outputDir, cli); err != nil {
			fmt.Printf("%s Error dumping repository %s: %v\n", errorColor("[-]"), *dump, err)
			return
		}
		fmt.Printf("%s Dumped %s successfully\n", success("[+]"), *dump)
	} else {
		flag.Usage()
	}
}

// validateAndNormalizeURL ensures the URL is valid and tests HTTP/HTTPS if no scheme is provided
func validateAndNormalizeURL(inputURL string, port int) (string, error) {
	// Parse the input URL
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %v", err)
	}

	// If no scheme is provided (e.g., "example.com"), test both HTTP and HTTPS
	if parsedURL.Scheme == "" {
		domain := parsedURL.String()
		if domain == "" {
			domain = inputURL // Fallback if parsing failed to extract host
		}

		// Test HTTP
		httpURL := fmt.Sprintf("http://%s:%d/v2/", domain, port)
		if testURL(httpURL) {
			return fmt.Sprintf("http://%s", domain), nil
		}

		// Test HTTPS
		httpsURL := fmt.Sprintf("https://%s:%d/v2/", domain, port)
		if testURL(httpsURL) {
			return fmt.Sprintf("https://%s", domain), nil
		}

		return "", fmt.Errorf("domain '%s' is not reachable on HTTP or HTTPS with port %d", domain, port)
	}

	// If scheme is provided, ensure it’s valid and reachable
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme '%s'; use 'http' or 'https'", parsedURL.Scheme)
	}

	// Test the provided URL with the /v2/ endpoint (Docker registry check)
	testURLStr := fmt.Sprintf("%s:%d/v2/", parsedURL.String(), port)
	if !testURL(testURLStr) {
		return "", fmt.Errorf("URL '%s' is not a reachable Docker registry on port %d", parsedURL.String(), port)
	}

	return parsedURL.String(), nil
}

// testURL checks if a URL is reachable by sending a GET request to the /v2/ endpoint
func testURL(testURL string) bool {
	client := &http.Client{
		Timeout: 10 * time.Second, // Short timeout for testing
	}
	resp, err := client.Get(testURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Accept 200 OK or 401 Unauthorized (common for registries without auth)
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
}