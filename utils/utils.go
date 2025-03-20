package utils

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var mutex = &sync.Mutex{}

func CreateDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

func StoreResponse(filename string, data []byte) error {
	mutex.Lock()
	defer mutex.Unlock()

	fullPath := filepath.Clean(filename)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(fullPath, data, 0644)
}

func URLToFilename(url string) string {
	hash := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x.json", hash)
}