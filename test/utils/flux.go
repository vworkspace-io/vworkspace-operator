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
	"os"
	"os/exec"
)

const (
	// helmControllerCRDURL and sourceControllerCRDURL are pinned to flux2 v2.4.0 install.yaml.
	helmControllerCRDURL = "https://github.com/fluxcd/helm-controller/releases/download/v1.1.0/" +
		"helm-controller.crds.yaml"
	sourceControllerCRDURL = "https://github.com/fluxcd/source-controller/releases/download/v1.4.1/" +
		"source-controller.crds.yaml"
	veleroVersion = "v1.15.0"
)

var fluxCRDURLs = []string{helmControllerCRDURL, sourceControllerCRDURL}

var veleroCRDURL = "https://raw.githubusercontent.com/vmware-tanzu/velero/" + veleroVersion +
	"/config/crd/v1/bases/velero.io_backups.yaml"

// InstallFluxCRDs installs minimal Flux CRDs required for HelmRelease materialization.
func InstallFluxCRDs() error {
	for _, url := range fluxCRDURLs {
		cmd := exec.Command("kubectl", "apply", "-f", url)
		if _, err := Run(cmd); err != nil {
			return err
		}
	}
	cmd := exec.Command("kubectl", "wait",
		"--for=condition=Established", "crd/helmreleases.helm.toolkit.fluxcd.io", "--timeout=120s")
	_, err := Run(cmd)
	return err
}

// IsFluxCRDsInstalled reports whether HelmRelease CRD is present.
func IsFluxCRDsInstalled() bool {
	cmd := exec.Command("kubectl", "get", "crd", "helmreleases.helm.toolkit.fluxcd.io")
	_, err := Run(cmd)
	return err == nil
}

// InstallVeleroCRDs installs the Velero Backup CRD for optional e2e coverage.
func InstallVeleroCRDs() error {
	cmd := exec.Command("kubectl", "apply", "-f", veleroCRDURL)
	if _, err := Run(cmd); err != nil {
		return err
	}
	cmd = exec.Command("kubectl", "wait", "--for=condition=Established", "crd/backups.velero.io", "--timeout=120s")
	_, err := Run(cmd)
	return err
}

// IsVeleroCRDsInstalled reports whether Velero Backup CRD is present.
func IsVeleroCRDsInstalled() bool {
	cmd := exec.Command("kubectl", "get", "crd", "backups.velero.io")
	_, err := Run(cmd)
	return err == nil
}

// UninstallFluxCRDs removes Flux CRDs installed for e2e (best effort).
func UninstallFluxCRDs() {
	for _, url := range fluxCRDURLs {
		cmd := exec.Command("kubectl", "delete", "-f", url, "--ignore-not-found", "--wait=false")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// UninstallVeleroCRDs removes Velero Backup CRD installed for e2e (best effort).
func UninstallVeleroCRDs() {
	cmd := exec.Command("kubectl", "delete", "-f", veleroCRDURL, "--ignore-not-found", "--wait=false")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// KubectlApplyYAML applies a multi-document YAML manifest via kubectl.
func KubectlApplyYAML(manifest string) error {
	path, err := writeTempManifest(manifest)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	cmd := exec.Command("kubectl", "apply", "-f", path)
	_, err = Run(cmd)
	if err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}
	return nil
}

// KubectlDeleteYAML deletes resources from a multi-document YAML manifest (best effort).
func KubectlDeleteYAML(manifest string) {
	path, err := writeTempManifest(manifest)
	if err != nil {
		warnError(err)
		return
	}
	defer func() { _ = os.Remove(path) }()
	cmd := exec.Command("kubectl", "delete", "-f", path, "--ignore-not-found", "--wait=false")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

func writeTempManifest(manifest string) (string, error) {
	f, err := os.CreateTemp("", "vworkspace-e2e-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp manifest: %w", err)
	}
	if _, err := f.WriteString(manifest); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("write temp manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temp manifest: %w", err)
	}
	return f.Name(), nil
}
