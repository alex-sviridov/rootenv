/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultLabImagesDir = "/etc/lab-images"

// loadLabImageRef resolves the built image reference for a lab asset image
// name (e.g. "ubuntu-sshd") by reading the file Skaffold's resourceSelector
// writes into the lab-images ConfigMap, mounted as a volume on the
// controller-manager pod. If no matching file exists, the image name isn't
// one Skaffold builds for us — fall back to using it as-is, so Kubernetes
// pulls it from a public registry (e.g. "ubuntu" from Docker Hub).
func loadLabImageRef(name string) (string, error) {
	dir := os.Getenv("LAB_IMAGES_DIR")
	if dir == "" {
		dir = defaultLabImagesDir
	}

	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return name, nil
		}
		return "", fmt.Errorf("resolving lab image %q: reading %s: %w", name, path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
