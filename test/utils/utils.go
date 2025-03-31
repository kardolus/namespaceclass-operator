/*
Copyright 2025.

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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
)

// ApplyNamespaceClass applies a NamespaceClass manifest with a single ConfigMap resource.
func ApplyNamespaceClass(className, configMapName, value string) error {
	manifest := fmt.Sprintf(`
apiVersion: namespace.kardolus.dev/v1alpha1
kind: NamespaceClass
metadata:
  name: %s
spec:
  resources:
  - apiVersion: v1
    kind: ConfigMap
    metadata:
      name: %s
    data:
      foo: %s
`, className, configMapName, value)
	return KubectlApply([]byte(manifest))
}

// ApplyNamespaceWithLabel applies a Namespace manifest with a namespaceclass label.
func ApplyNamespaceWithLabel(namespace, classLabel string) error {
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    namespaceclass.akuity.io/name: %s
`, namespace, classLabel)
	return KubectlApply([]byte(manifest))
}

// DeleteResource deletes a Kubernetes resource by kind and name (and namespace, if provided).
func DeleteResource(kind, name string, namespace ...string) error {
	args := []string{"delete", kind, name}
	if len(namespace) > 0 {
		args = append(args, "-n", namespace[0])
	}
	cmd := exec.Command("kubectl", args...)
	return cmd.Run()
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

// KubectlApply applies the given YAML manifest using `kubectl apply -f -`
func KubectlApply(manifest []byte) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(manifest)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// LoadImageToKindClusterWithName loads a Docker image into the current Kind cluster
func LoadImageToKindClusterWithName(image, clusterName string) error {
	cmd := exec.Command("kind", "load", "docker-image", image, "--name", clusterName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "\nkind load output:\n%s\n", out)
		return fmt.Errorf("failed to load image into kind cluster: %v\nOutput: %s", err, string(out))
	}
	return nil
}

func MustProjectDir() string {
	dir, err := GetProjectDir()
	if err != nil {
		panic(err)
	}
	return dir
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) ([]byte, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}
