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
	"os/exec"
	"path/filepath"
)

const seaweedCRDRelPath = "charts/vworkspace-operator/charts/seaweedfs-crds/crds/seaweeds.seaweed.seaweedfs.com.yaml"

// InstallSeaweedCRDs installs the Seaweed CRD required for the operator watch.
func InstallSeaweedCRDs() error {
	crdPath, err := repoPath(seaweedCRDRelPath)
	if err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "apply", "-f", crdPath)
	if _, err := Run(cmd); err != nil {
		return err
	}
	cmd = exec.Command("kubectl", "wait",
		"--for=condition=Established", "crd/seaweeds.seaweed.seaweedfs.com", "--timeout=120s")
	_, err = Run(cmd)
	return err
}

// IsSeaweedCRDsInstalled reports whether the Seaweed CRD is present.
func IsSeaweedCRDsInstalled() bool {
	cmd := exec.Command("kubectl", "get", "crd", "seaweeds.seaweed.seaweedfs.com")
	_, err := Run(cmd)
	return err == nil
}

// UninstallSeaweedCRDs removes the Seaweed CRD installed for e2e (best effort).
func UninstallSeaweedCRDs() {
	crdPath, err := repoPath(seaweedCRDRelPath)
	if err != nil {
		warnError(err)
		return
	}
	cmd := exec.Command("kubectl", "delete", "-f", crdPath, "--ignore-not-found", "--wait=false")
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
