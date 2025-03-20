package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"

	"dockdiver/client"
	"dockdiver/registry"
)

func printASCIIArt() {
	art := `
       __           __       ___                
  ____/ /___  _____/ /______/ (_)   _____  _____
 / __  / __ \/ ___/ //_/ __  / / | / / _ \/ ___/
/ /_/ / /_/ / /__/ ,< / /_/ / /| |/ /  __/ /    
\__,_/\____/\___/_/|_|\__,_/_/ |___/\___/_/     
`
	fmt.Println(art)
}

func main() {
	printASCIIArt()

	urlFlag := flag.String("url", "http://localhost", "Base URL of the Docker registry (e.g., http://example.com or example.com)")
	port := flag.Int("port", 5000, "Port of the registry (default: 5000)")
	username := flag.String("username", "", "Username for authentication")
	password := flag.String("password", "", "Password for authentication")
	bearer := flag.String("bearer", "", "Bearer token for Authorization")
	headers := flag.String("headers", "", "Custom headers as JSON (e.g., '{\"X-Custom\": \"Value\"}')")
	rate := flag.Int("rate", 10, "Requests per second (default: 10)")
	outputDir := flag.String("dir", "docker_dump", "Output directory for dumped files")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification (use with caution)")
	list := flag.Bool("list", false, "List all repositories")
	dumpAll := flag.Bool("dump-all", false, "Dump all repositories")
	dump := flag.String("dump", "", "Specific repository to dump")

	flag.Parse()

	success := color.New(color.FgGreen).SprintFunc()
	errorColor := color.New(color.FgRed).SprintFunc()
	warning := color.New(color.FgYellow).SprintFunc()

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	validatedURL, err := validateAndNormalizeURL(*urlFlag, *port, *insecure)
	if err != nil {
		fmt.Printf("%s URL validation failed: %v\n", errorColor("[-]"), err)
		return
	}
	fmt.Printf("%s Using validated URL: %s\n", success("[+]"), validatedURL)

	cli := client.NewClient(*rate, *insecure)
	auth := client.AuthConfig{
		Username: *username,
		Password: *password,
		Bearer:   *bearer,
		Headers:  *headers,
	}

	if auth.Username == "" && auth.Password == "" && auth.Bearer == "" {
		fmt.Printf("%s No authentication provided (no username/password or bearer token). Proceeding without auth...\n", warning("[!]"))
	}

	if *insecure {
		fmt.Printf("%s TLS verification disabled (insecure mode enabled)\n", warning("[!]"))
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

func validateAndNormalizeURL(inputURL string, port int, insecure bool) (string, error) {
	parsedURL, err := url.Parse(inputURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %v", err)
	}

	if parsedURL.Scheme == "" {
		domain := parsedURL.String()
		if domain == "" {
			domain = inputURL
		}

		httpURL := fmt.Sprintf("http://%s:%d/v2/", domain, port)
		if testURL(httpURL, insecure) {
			return fmt.Sprintf("http://%s", domain), nil
		}

		httpsURL := fmt.Sprintf("https://%s:%d/v2/", domain, port)
		if testURL(httpsURL, insecure) {
			return fmt.Sprintf("https://%s", domain), nil
		}

		return "", fmt.Errorf("domain '%s' is not reachable on HTTP or HTTPS with port %d", domain, port)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme '%s'; use 'http' or 'https'", parsedURL.Scheme)
	}

	testURLStr := fmt.Sprintf("%s:%d/v2/", parsedURL.String(), port)
	if !testURL(testURLStr, insecure) {
		return "", fmt.Errorf("URL '%s' is not a reachable Docker registry on port %d", parsedURL.String(), port)
	}

	return parsedURL.String(), nil
}

func testURL(testURL string, insecure bool) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	if insecure && strings.HasPrefix(testURL, "https://") {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	resp, err := client.Get(testURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
}