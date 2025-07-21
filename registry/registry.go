package registry

import (
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "io"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/fatih/color"

        "dockdiver/client"
        "dockdiver/utils"
)

// progressReader wraps an io.Reader to track and report download progress
type progressReader struct {
        reader     io.Reader
        total      int64
        read       int64
        url        string
        lastUpdate int64 // Tracks last reported progress to avoid spamming
        warning    func(...interface{}) string
}

func (pr *progressReader) Read(p []byte) (int, error) {
        n, err := pr.reader.Read(p)
        pr.read += int64(n)
        if pr.total > 0 {
                // Update progress every 10% or 100KB, whichever is larger
                threshold := pr.total / 10
                if threshold < 100*1024 {
                        threshold = 100 * 1024
                }
                if pr.read-pr.lastUpdate >= threshold || err == io.EOF {
                        percentage := float64(pr.read) / float64(pr.total) * 100
                        fmt.Printf("%s Downloading %s: %.2f%% (%d/%d bytes)\n", pr.warning("[!]"), pr.url, percentage, pr.read, pr.total)
                        pr.lastUpdate = pr.read
                }
        }
        return n, err
}

func ListRepositories(url string, port int, auth client.AuthConfig, cli *client.Client) ([]string, error) {
        var allRepos []string
        const pageSize = 100 // Limit to 100 repositories per request
        nextURL := fmt.Sprintf("%s:%d/v2/_catalog?n=%d", url, port, pageSize)

        for nextURL != "" {
                fmt.Printf("%s Fetching catalog: %s\n", color.New(color.FgYellow).SprintFunc()("[!]"), nextURL)
                resp, err := cli.MakeRequest(nextURL, auth)
                if err != nil {
                        return nil, fmt.Errorf("failed to fetch catalog: %v", err)
                }

                var catalog struct {
                        Repositories []string `json:"repositories"`
                }
                if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
                        resp.Body.Close()
                        return nil, fmt.Errorf("failed to decode catalog: %v", err)
                }
                allRepos = append(allRepos, catalog.Repositories...)
                // Check for pagination Link header
                nextURL = ""
                if link := resp.Header.Get("Link"); link != "" {
                        // Parse Link header: e.g., <url>; rel="next"
                        parts := strings.Split(link, ";")
                        if len(parts) > 1 && strings.Contains(parts[1], `rel="next"`) {
                                // Extract URL from <url>
                                linkURL := strings.Trim(parts[0], "<> ")
                                if linkURL != "" {
                                        // Ensure absolute URL
                                        if !strings.HasPrefix(linkURL, "http") {
                                                linkURL = fmt.Sprintf("%s:%d%s", url, port, linkURL)
                                        }
                                        nextURL = linkURL
                                }
                        }
                }
                resp.Body.Close()
        }

        if len(allRepos) == 0 {
                return nil, fmt.Errorf("no repositories found")
        }
        return allRepos, nil
}

func DumpAllRepositories(url string, port int, auth client.AuthConfig, outputDir string, cli *client.Client) error {
        repos, err := ListRepositories(url, port, auth, cli)
        if err != nil {
                return err
        }

        var wg sync.WaitGroup
        semaphore := make(chan struct{}, 5)

        for _, repo := range repos {
                wg.Add(1)
                semaphore <- struct{}{}
                go func(r string) {
                        defer wg.Done()
                        defer func() { <-semaphore }()
                        if err := DumpRepository(url, port, r, auth, outputDir, cli); err != nil {
                                fmt.Printf("%s Error dumping %s: %v\n", color.New(color.FgRed).SprintFunc()("[-]"), r, err)
                        }
                }(repo)
        }

        wg.Wait()
        return nil
}

func DumpRepository(url string, port int, repo string, auth client.AuthConfig, outputDir string, cli *client.Client) error {
        success := color.New(color.FgGreen).SprintFunc()
        errorColor := color.New(color.FgRed).SprintFunc()
        warning := color.New(color.FgYellow).SprintFunc()

        if err := utils.CreateDir(outputDir); err != nil {
                return fmt.Errorf("failed to create output directory: %v", err)
        }

        tagsURL := fmt.Sprintf("%s:%d/v2/%s/tags/list", url, port, repo)
        fmt.Printf("%s Fetching tags for %s: %s\n", warning("[!]"), repo, tagsURL)
        resp, err := cli.MakeRequest(tagsURL, auth)
        if err != nil {
                return fmt.Errorf("failed to fetch tags for %s: %v", repo, err)
        }
        defer resp.Body.Close()

        var tags struct {
                Tags []string `json:"tags"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
                return fmt.Errorf("failed to decode tags for %s: %v", repo, err)
        }
        if len(tags.Tags) == 0 {
                return fmt.Errorf("no tags found for %s", repo)
        }

        tag := tags.Tags[0]
        fmt.Printf("%s Selected tag: %s\n", warning("[!]"), tag)
        manifestURL := fmt.Sprintf("%s:%d/v2/%s/manifests/%s", url, port, repo, tag)
        manifestFile := filepath.Join(outputDir, repo, "manifest.json")
        fmt.Printf("%s Fetching manifest: %s\n", warning("[!]"), manifestURL)
        manifestBody, err := getAndStoreResponse(manifestURL, manifestFile, auth, cli)
        if err != nil {
                return fmt.Errorf("failed to fetch manifest for %s:%s: %v", repo, tag, err)
        }

        var manifest struct {
                Config struct {
                        Digest    string `json:"digest"`
                        MediaType string `json:"mediaType"`
                } `json:"config"`
                Layers []struct {
                        Digest    string `json:"digest"`
                        MediaType string `json:"mediaType"`
                } `json:"layers"`
        }
        if err := json.Unmarshal([]byte(manifestBody), &manifest); err != nil {
                return fmt.Errorf("failed to parse manifest for %s:%s: %v", repo, tag, err)
        }

        // Dump config blob
        if manifest.Config.Digest != "" {
                blobURL := fmt.Sprintf("%s:%d/v2/%s/blobs/%s", url, port, repo, manifest.Config.Digest)
                ext := ".json"
                if manifest.Config.MediaType != "application/vnd.docker.container.image.v1+json" {
                        ext = ".bin"
                }
                safeDigest := strings.ReplaceAll(manifest.Config.Digest, ":", "_")
                blobFile := filepath.Join(outputDir, repo, fmt.Sprintf("config_%s%s", safeDigest, ext))
                fmt.Printf("%s Fetching config blob: %s\n", warning("[!]"), blobURL)
                if _, err := getAndStoreBlob(blobURL, blobFile, manifest.Config.Digest, auth, cli, warning); err != nil {
                        fmt.Printf("%s Error downloading config %s: %v\n", errorColor("[-]"), manifest.Config.Digest, err)
                } else {
                        fmt.Printf("%s Config %s downloaded and verified\n", success("[+]"), manifest.Config.Digest)
                }
        }

        // Dump layer blobs
        for i, layer := range manifest.Layers {
                if layer.Digest != "" {
                        blobURL := fmt.Sprintf("%s:%d/v2/%s/blobs/%s", url, port, repo, layer.Digest)
                        ext := ".tar.gz"
                        if layer.MediaType != "application/vnd.docker.image.rootfs.diff.tar.gzip" {
                                ext = ".bin"
                        }
                        safeDigest := strings.ReplaceAll(layer.Digest, ":", "_")
                        blobFile := filepath.Join(outputDir, repo, fmt.Sprintf("layer_%s%s", safeDigest, ext))
                        fmt.Printf("%s Fetching layer %d blob: %s\n", warning("[!]"), i+1, blobURL)
                        if _, err := getAndStoreBlob(blobURL, blobFile, layer.Digest, auth, cli, warning); err != nil {
                                fmt.Printf("%s Error downloading layer %s: %v\n", errorColor("[-]"), layer.Digest, err)
                        } else {
                                fmt.Printf("%s Layer %s downloaded and verified\n", success("[+]"), layer.Digest)
                        }
                }
        }

        fmt.Printf("%s Dumped %s successfully\n", success("[+]"), repo)
        return nil
}

func getAndStoreResponse(url, filename string, auth client.AuthConfig, cli *client.Client) (string, error) {
        resp, err := cli.MakeRequest(url, auth)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()

        // Add timeout for reading response body
        bodyReader := &timeoutReader{
                reader:  resp.Body,
                timeout: 30 * time.Second,
        }
        body, err := io.ReadAll(bodyReader)
        if err != nil {
                return "", fmt.Errorf("failed to read response for %s: %v", url, err)
        }

        if err := utils.StoreResponse(filename, body); err != nil {
                return "", fmt.Errorf("failed to store response for %s: %v", filename, err)
        }

        return string(body), nil
}

// timeoutReader wraps an io.Reader with a timeout
type timeoutReader struct {
        reader  io.Reader
        timeout time.Duration
}

func (tr *timeoutReader) Read(p []byte) (int, error) {
        n := 0
        errChan := make(chan error, 1)
        go func() {
                var err error
                n, err = tr.reader.Read(p)
                errChan <- err
        }()

        select {
        case err := <-errChan:
                return n, err
        case <-time.After(tr.timeout):
                return 0, fmt.Errorf("read timeout after %v", tr.timeout)
        }
}

func getAndStoreBlob(url, filename, expectedDigest string, auth client.AuthConfig, cli *client.Client, warning func(...interface{}) string) (string, error) {
        resp, err := cli.MakeRequest(url, auth)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()

        // Get total size from Content-Length header
        totalSize := resp.ContentLength
        if totalSize <= 0 {
                totalSize = -1
        }

        // Create a temporary file for streaming
        tmpFile, err := os.CreateTemp(filepath.Dir(filename), "blob_*.tmp")
        if err != nil {
                return "", fmt.Errorf("failed to create temp file: %v", err)
        }
        defer os.Remove(tmpFile.Name())

        // Create progress reader
        pr := &progressReader{
                reader:  resp.Body,
                total:   totalSize,
                url:     url,
                warning: warning,
        }

        // Compute SHA256 while writing to temp file
        hash := sha256.New()
        writer := io.MultiWriter(tmpFile, hash)
        _, err = io.Copy(writer, pr)
        if err != nil {
                tmpFile.Close()
                return "", fmt.Errorf("failed to read blob for %s: %v", url, err)
        }
        tmpFile.Close()

        // Verify integrity
        calculatedDigest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
        if calculatedDigest != expectedDigest {
                return "", fmt.Errorf("integrity check failed: expected %s, got %s", expectedDigest, calculatedDigest)
        }

        // Move temp file to final destination
        if err := os.Rename(tmpFile.Name(), filename); err != nil {
                return "", fmt.Errorf("failed to move temp file to %s: %v", filename, err)
        }

        return "", nil
}