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

package e2e

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kardolus/namespaceclass-operator/test/utils"
)

const (
	operatorNamespace  = "namespaceclass-operator-system"
	testNamespace      = "web-portal"
	namespaceClassName = "public-network"
	injectedConfigMap  = "injected-config"
	controllerImage    = "namespaceclass-operator:test" // local image tag for Kind
)

var _ = Describe("namespaceclass operator e2e", Ordered, func() {
	BeforeAll(func() {
		By("building the manager image")
		cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", controllerImage))
		out, err := utils.Run(cmd)
		fmt.Fprintf(GinkgoWriter, "\nmake docker-build output:\n%s\n", out)
		Expect(err).NotTo(HaveOccurred())

		By("loading the image into kind")
		Expect(utils.LoadImageToKindClusterWithName(controllerImage, "apache")).To(Succeed())

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		out, err = utils.Run(cmd)
		fmt.Fprintf(GinkgoWriter, "\nmake install output:\n%s\n", out)
		Expect(err).NotTo(HaveOccurred())

		By("patching the image in config/manager/kustomization.yaml")
		cmd = exec.Command("bash", "-c",
			fmt.Sprintf("cd config/manager && %s edit set image controller=%s",
				filepath.Join(utils.MustProjectDir(), "bin", "kustomize"), controllerImage))
		out, err = utils.Run(cmd)
		fmt.Fprintf(GinkgoWriter, "\nkustomize edit output:\n%s\n", out)
		Expect(err).NotTo(HaveOccurred())

		By("deploying the controller with patched image")
		cmd = exec.Command(filepath.Join(utils.MustProjectDir(), "bin", "kustomize"), "build", filepath.Join(utils.MustProjectDir(), "config/default"))
		manifests, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = bytes.NewReader(manifests)
		cmd.Stdout = GinkgoWriter
		cmd.Stderr = GinkgoWriter
		Expect(cmd.Run()).To(Succeed())
	})

	AfterAll(func() {
		By("cleaning up test namespace and resources")

		if err := utils.DeleteResource("namespace", testNamespace); err != nil {
			fmt.Fprintf(GinkgoWriter, "failed to delete namespace %s: %v\n", testNamespace, err)
		}
		if err := utils.DeleteResource("namespaceclass", namespaceClassName); err != nil {
			fmt.Fprintf(GinkgoWriter, "failed to delete namespaceclass %s: %v\n", namespaceClassName, err)
		}
		if err := utils.DeleteResource("namespace", operatorNamespace); err != nil {
			fmt.Fprintf(GinkgoWriter, "failed to delete namespace %s: %v\n", operatorNamespace, err)
		}
	})

	It("should create resources defined in a NamespaceClass when a namespace is labeled", func() {
		By("applying NamespaceClass resource")
		Expect(utils.ApplyNamespaceClass(namespaceClassName, injectedConfigMap, "bar")).To(Succeed())

		By("creating a namespace with the class label")
		Expect(utils.ApplyNamespaceWithLabel(testNamespace, namespaceClassName)).To(Succeed())

		By("waiting for the operator deployment to be ready")
		cmd := exec.Command("kubectl", "rollout", "status", "deployment/namespaceclass-operator-controller-manager", "-n", operatorNamespace, "--timeout=60s")
		out, err := utils.Run(cmd)
		fmt.Fprintf(GinkgoWriter, "\nrollout status output:\n%s\n", out)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the operator to reconcile resources")
		Eventually(func() string {
			out, err := exec.Command("kubectl", "get", "configmap", injectedConfigMap, "-n", testNamespace, "-o", "yaml").CombinedOutput()
			fmt.Fprintf(GinkgoWriter, "\nkubectl get configmap output:\n%s\n", out)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "\nerror retrieving configmap: %v\n", err)
			}
			return string(out)
		}, time.Minute, time.Second*10).Should(ContainSubstring("foo: bar"))
	})

	It("should not duplicate resources if reconciled multiple times", func() {
		By("applying NamespaceClass resource")
		Expect(utils.ApplyNamespaceClass(namespaceClassName, injectedConfigMap, "bar")).To(Succeed())

		By("creating a namespace with the class label")
		Expect(utils.ApplyNamespaceWithLabel(testNamespace, namespaceClassName)).To(Succeed())

		By("waiting for the operator to inject the ConfigMap")
		Eventually(func() string {
			out, err := exec.Command("kubectl", "get", "configmap", injectedConfigMap, "-n", testNamespace, "-o", "yaml").CombinedOutput()
			fmt.Fprintf(GinkgoWriter, "\nconfigmap after initial create:\n%s\n", out)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "\nerror retrieving configmap: %v\n", err)
			}
			return string(out)
		}, time.Minute, time.Second*5).Should(ContainSubstring("foo: bar"))

		By("re-applying the same NamespaceClass")
		Expect(utils.ApplyNamespaceClass(namespaceClassName, injectedConfigMap, "bar")).To(Succeed())

		By("checking there is still only one injected ConfigMap")
		cmd := exec.Command("kubectl", "get", "configmap", "-n", testNamespace, "-o", "name")
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		fmt.Fprintf(GinkgoWriter, "\nconfigmaps after re-applying NamespaceClass:\n%s\n", out)

		var count int
		for _, line := range bytes.Split(out, []byte("\n")) {
			if bytes.Contains(line, []byte(injectedConfigMap)) {
				count++
			}
		}
		Expect(count).To(Equal(1), "expected only one ConfigMap named %s, found %d", injectedConfigMap, count)
	})

	It("should reconcile a namespace when its NamespaceClass is created later", func() {
		const lateNamespace = "late-bound-ns"
		const lateClass = "delayed-class"
		const lateConfigMap = "late-injected"

		By("creating a namespace first with a class label pointing to a non-existent class")
		Expect(utils.ApplyNamespaceWithLabel(lateNamespace, lateClass)).To(Succeed())

		By("verifying the ConfigMap is not there yet")
		cmd := exec.Command("kubectl", "get", "configmap", lateConfigMap, "-n", lateNamespace, "-o", "yaml")
		_, err := cmd.CombinedOutput()
		Expect(err).To(HaveOccurred()) // should not exist yet

		By("now applying the NamespaceClass with that name")
		Expect(utils.ApplyNamespaceClass(lateClass, lateConfigMap, "late-bar")).To(Succeed())

		By("waiting for the operator to reconcile and inject the ConfigMap")
		Eventually(func() string {
			out, err := exec.Command("kubectl", "get", "configmap", lateConfigMap, "-n", lateNamespace, "-o", "yaml").CombinedOutput()
			fmt.Fprintf(GinkgoWriter, "\nkubectl get configmap output:\n%s\n", out)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "\nerror retrieving configmap: %v\n", err)
			}
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: late-bar"))

		// Cleanup just this test
		_ = utils.DeleteResource("namespace", lateNamespace)
		_ = utils.DeleteResource("namespaceclass", lateClass)
	})

	It("should delete resources when a namespace is deleted", func() {
		By("applying NamespaceClass resource")
		Expect(utils.ApplyNamespaceClass(namespaceClassName, injectedConfigMap, "bar")).To(Succeed())

		By("creating a namespace with the class label")
		Expect(utils.ApplyNamespaceWithLabel(testNamespace, namespaceClassName)).To(Succeed())

		By("waiting for the ConfigMap to be injected")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", injectedConfigMap, "-n", testNamespace, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, time.Second*5).Should(ContainSubstring("foo: bar"))

		By("deleting the namespace")
		Expect(utils.DeleteResource("namespace", testNamespace)).To(Succeed())

		By("verifying the injected ConfigMap is gone")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", injectedConfigMap, "-n", testNamespace).CombinedOutput()
			return string(out)
		}, time.Minute, time.Second*5).Should(ContainSubstring("Error from server (NotFound)"))
	})
})
