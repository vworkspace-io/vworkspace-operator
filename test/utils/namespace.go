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
	"time"
)

const defaultNamespaceWait = 2 * time.Minute

// WaitForNamespaceDeleted polls until the namespace no longer exists.
func WaitForNamespaceDeleted(name string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultNamespaceWait
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", "get", "ns", name)
		if _, err := Run(cmd); err != nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for namespace %q to be deleted", name)
}

// EnsureNamespace waits for any in-progress deletion, then creates the namespace.
func EnsureNamespace(name string) error {
	if err := WaitForNamespaceDeleted(name, defaultNamespaceWait); err != nil {
		return err
	}
	cmd := exec.Command("kubectl", "create", "ns", name)
	_, err := Run(cmd)
	return err
}

// DeleteNamespace deletes a namespace without blocking, then waits until it is gone.
func DeleteNamespace(name string) error {
	cmd := exec.Command("kubectl", "delete", "ns", name, "--ignore-not-found", "--wait=false")
	if _, err := Run(cmd); err != nil {
		return err
	}
	return WaitForNamespaceDeleted(name, defaultNamespaceWait)
}
