/*
Copyright 2026 vWorkspace Contributors.

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

package utils

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

const seaweedCRDsRelDir = "charts/vworkspace-operator/charts/seaweedfs-crds/crds"

// InstallSeaweedCRDs installs Seaweed operator CRDs required for controller watches
// (Seaweed, S3Credentials, and related types).
func InstallSeaweedCRDs() error {
	crdDir, err := repoPath(seaweedCRDsRelDir)
	if err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "apply", "-f", crdDir)
	if _, err := Run(cmd); err != nil {
		return err
	}
	for range 30 {
		if IsSeaweedCRDsInstalled() {
			return nil
		}
		cmd = exec.Command("sleep", "2")
		_, _ = Run(cmd)
	}
	return fmt.Errorf("timed out waiting for Seaweed CRDs to become available")
}

// IsSeaweedCRDsInstalled reports whether the Seaweed CRDs required for e2e are present.
func IsSeaweedCRDsInstalled() bool {
	for _, name := range []string{
		"seaweeds.seaweed.seaweedfs.com",
		"s3credentials.seaweed.seaweedfs.com",
	} {
		cmd := exec.Command("kubectl", "get", "crd", name)
		if _, err := Run(cmd); err != nil {
			return false
		}
	}
	return true
}

// UninstallSeaweedCRDs removes Seaweed CRDs installed for e2e (best effort).
func UninstallSeaweedCRDs() {
	crdDir, err := repoPath(seaweedCRDsRelDir)
	if err != nil {
		warnError(err)
		return
	}
	cmd := exec.Command("kubectl", "delete", "-f", crdDir, "--ignore-not-found", "--wait=false")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

func repoPath(rel string) (string, error) {
	root, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, rel), nil
}
