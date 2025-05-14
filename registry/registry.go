package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fatih/color"

	"dockdiver/client"
	"dockdiver/utils"
)

func ListRepositories(url string, port int, auth client.AuthConfig, cli *client.Client) ([]string, error) {
        catalogURL := fmt.Sprintf("%s:%d/v2/_catalog", url, port)
        resp, err := cli.MakeRequest(catalogURL, auth)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()

        var catalog struct {
                Repositories []string `json:"repositories"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
                return nil, fmt.Errorf("failed to decode catalog: %v", err)
        }

        return catalog.Repositories, nil
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

        if err := utils.CreateDir(outputDir); err != nil {
                return fmt.Errorf("failed to create output directory: %v", err)
        }

        tagsURL := fmt.Sprintf("%s:%d/v2/%s/tags/list", url, port, repo)
        resp, err := cli.MakeRequest(tagsURL, auth)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        var tags struct {
                Tags []string `json:"tags"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
                return fmt.Errorf("failed to decode tags: %v", err)
        }
        if len(tags.Tags) == 0 {
                return fmt.Errorf("no tags found for %s", repo)
        }

        tag := tags.Tags[0] // Use the first tag
        manifestURL := fmt.Sprintf("%s:%d/v2/%s/manifests/%s", url, port, repo, tag)
        manifestFile := filepath.Join(outputDir, repo, "manifest.json")
        manifestBody, err := getAndStoreResponse(manifestURL, manifestFile, auth, cli)
        if err != nil {
                return err
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
                return fmt.Errorf("failed to parse manifest: %v", err)
        }

        // Dump config blob
        if manifest.Config.Digest != "" {
                blobURL := fmt.Sprintf("%s:%d/v2/%s/blobs/%s", url, port, repo, manifest.Config.Digest)
                ext := ".json"
                if manifest.Config.MediaType != "application/vnd.docker.container.image.v1+json" {
                        ext = ".bin"
                }
                // Replace colon in config digest with underscore
                safeDigest := strings.ReplaceAll(manifest.Config.Digest, ":", "_")
                blobFile := filepath.Join(outputDir, repo, fmt.Sprintf("config_%s%s", safeDigest, ext))
                if _, err := getAndStoreBlob(blobURL, blobFile, manifest.Config.Digest, auth, cli); err != nil {
                        fmt.Printf("%s Error downloading config %s: %v\n", errorColor("[-]"), manifest.Config.Digest, err)
                } else {
                        fmt.Printf("%s Config %s downloaded and verified\n", success("[+]"), manifest.Config.Digest)
                }
        }

        // Dump layer blobs
        for _, layer := range manifest.Layers {
                if layer.Digest != "" {
                        blobURL := fmt.Sprintf("%s:%d/v2/%s/blobs/%s", url, port, repo, layer.Digest)
                        ext := ".tar.gz"
                        if layer.MediaType != "application/vnd.docker.image.rootfs.diff.tar.gzip" {
                                ext = ".bin"
                        }
                        // Replace colon in digest with underscore
                        safeDigest := strings.ReplaceAll(layer.Digest, ":", "_")
                        blobFile := filepath.Join(outputDir, repo, fmt.Sprintf("layer_%s%s", safeDigest, ext))
                        if _, err := getAndStoreBlob(blobURL, blobFile, layer.Digest, auth, cli); err != nil {
                                fmt.Printf("%s Error downloading layer %s: %v\n", errorColor("[-]"), layer.Digest, err)
                        } else {
                                fmt.Printf("%s Layer %s downloaded and verified\n", success("[+]"), layer.Digest)
                        }
                }
        }

        return nil
}

func getAndStoreResponse(url, filename string, auth client.AuthConfig, cli *client.Client) (string, error) {
        resp, err := cli.MakeRequest(url, auth)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return "", fmt.Errorf("failed to read response: %v", err)
        }

        if err := utils.StoreResponse(filename, body); err != nil {
                return "", fmt.Errorf("failed to store response: %v", err)
        }

        return string(body), nil
}

func getAndStoreBlob(url, filename, expectedDigest string, auth client.AuthConfig, cli *client.Client) (string, error) {
        resp, err := cli.MakeRequest(url, auth)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                return "", fmt.Errorf("failed to read blob: %v", err)
        }

        hash := sha256.Sum256(body)
        calculatedDigest := "sha256:" + hex.EncodeToString(hash[:])
        if calculatedDigest != expectedDigest {
                return "", fmt.Errorf("integrity check failed: expected %s, got %s", expectedDigest, calculatedDigest)
        }

        if err := utils.StoreResponse(filename, body); err != nil {
                return "", fmt.Errorf("failed to store blob: %v", err)
        }

        return string(body), nil
}