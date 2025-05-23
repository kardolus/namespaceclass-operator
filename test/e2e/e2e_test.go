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
	"github.com/kardolus/namespaceclass-operator/internal/controller"
	"os/exec"
	"path/filepath"
	"strings"
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

		By("verifying the operator emits a MissingNamespaceClass warning event")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "events", "--all-namespaces").CombinedOutput()
			return string(out)
		}, 20*time.Second, 2*time.Second).Should(ContainSubstring("OrphanedNamespaceClass"))

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

		By("cleaning up the MissingNamespaceClass event")
		cmd = exec.Command("kubectl", "get", "events", "-n", "default", "--field-selector", fmt.Sprintf("involvedObject.name=%s", lateNamespace), "-o", "jsonpath={.items[*].metadata.name}")
		out, err := cmd.CombinedOutput()
		if err == nil && len(out) > 0 {
			for _, name := range strings.Fields(string(out)) {
				_ = exec.Command("kubectl", "delete", "event", name, "-n", "default").Run()
			}
		}

		_ = utils.DeleteResource("namespace", lateNamespace)
		_ = utils.DeleteResource("namespaceclass", lateClass)
		_ = utils.DeleteEventsForInvolvedObject("late-bound-ns")
	})

	It("should clean up resources when NamespaceClass is deleted and cleanup annotation is present", func() {
		const ns = "cleanup-ns"
		const class = "cleanup-class"
		const cm = "cleanup-config"

		By("creating a namespace with cleanup annotation and class label")
		Expect(utils.ApplyNamespaceWithLabel(ns, class)).To(Succeed())
		Expect(utils.PatchNamespace(ns, map[string]string{
			controller.NamespaceClassCleanupKey: "true",
		})).To(Succeed())

		By("applying the NamespaceClass")
		Expect(utils.ApplyNamespaceClass(class, cm, "to-be-cleaned")).To(Succeed())

		By("waiting for the ConfigMap to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: to-be-cleaned"))

		By("deleting the NamespaceClass")
		Expect(utils.DeleteResource("namespaceclass", class)).To(Succeed())

		By("verifying the injected resource is deleted")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns).CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("Error from server (NotFound)"))

		_ = utils.DeleteResource("namespace", ns)
		_ = utils.DeleteEventsForInvolvedObject("cleanup-ns")

	})

	It("should not clean up resources when NamespaceClass is deleted and cleanup annotation is missing", func() {
		const ns = "orphan-ns"
		const class = "orphan-class"
		const cm = "orphan-config"

		By("creating a namespace with class label but no cleanup annotation")
		Expect(utils.ApplyNamespaceWithLabel(ns, class)).To(Succeed())

		By("applying the NamespaceClass")
		Expect(utils.ApplyNamespaceClass(class, cm, "should-stay")).To(Succeed())

		By("waiting for the ConfigMap to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: should-stay"))

		By("deleting the NamespaceClass")
		Expect(utils.DeleteResource("namespaceclass", class)).To(Succeed())

		By("verifying the injected resource is still present")
		Consistently(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, 10*time.Second, 2*time.Second).Should(ContainSubstring("foo: should-stay"))

		_ = utils.DeleteResource("configmap", cm+" -n "+ns)
		_ = utils.DeleteResource("namespace", ns)
		_ = utils.DeleteEventsForInvolvedObject("orphan-ns")
	})

	It("should update injected resources when NamespaceClass is modified", func() {
		const ns = "update-ns"
		const class = "update-class"
		const cm = "update-config"

		By("creating a namespace with the class label")
		Expect(utils.ApplyNamespaceWithLabel(ns, class)).To(Succeed())

		By("applying an initial NamespaceClass")
		Expect(utils.ApplyNamespaceClass(class, cm, "v1")).To(Succeed())

		By("waiting for the initial ConfigMap value to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: v1"))

		By("updating the NamespaceClass with new ConfigMap contents")
		Expect(utils.ApplyNamespaceClass(class, cm, "v2")).To(Succeed())

		By("waiting for the updated ConfigMap value to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cm, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: v2"))

		_ = utils.DeleteResource("namespace", ns)
		_ = utils.DeleteResource("namespaceclass", class)
		_ = utils.DeleteEventsForInvolvedObject(ns)
	})

	It("should delete obsolete resources when cleanup-obsolete annotation is present", func() {
		const ns = "obsolete-ns"
		const class = "obsolete-class"
		const initialCM = "keep-this"
		const obsoleteCM = "delete-this"

		By("creating a namespace with cleanup-obsolete annotation and class label")
		Expect(utils.ApplyNamespaceWithLabel(ns, class)).To(Succeed())
		Expect(utils.PatchNamespace(ns, map[string]string{
			"namespaceclass.akuity.io/cleanup-obsolete": "true",
		})).To(Succeed())

		By("applying the NamespaceClass with two ConfigMaps")
		Expect(utils.ApplyNamespaceClassMulti(class, map[string]string{
			initialCM:  "initial",
			obsoleteCM: "temporary",
		})).To(Succeed())

		By("waiting for both ConfigMaps to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", obsoleteCM, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: temporary"))

		By("updating the NamespaceClass to remove one ConfigMap")
		Expect(utils.ApplyNamespaceClassMulti(class, map[string]string{
			initialCM: "initial", // keep this
		})).To(Succeed())

		By("verifying the obsolete ConfigMap is removed")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", obsoleteCM, "-n", ns).CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("NotFound"))

		By("verifying the remaining ConfigMap still exists")
		cmd := exec.Command("kubectl", "get", "configmap", initialCM, "-n", ns, "-o", "yaml")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("foo: initial"))

		_ = utils.DeleteResource("namespace", ns)
		_ = utils.DeleteResource("namespaceclass", class)
		_ = utils.DeleteEventsForInvolvedObject(ns)
	})

	It("should not delete user-created resources during cleanup-obsolete", func() {
		const ns = "user-owned-ns"
		const class = "user-owned-class"
		const cmInjected = "injected-by-class"
		const cmUser = "user-created"

		By("creating a namespace with cleanup-obsolete annotation and class label")
		Expect(utils.ApplyNamespaceWithLabel(ns, class)).To(Succeed())
		Expect(utils.PatchNamespace(ns, map[string]string{
			controller.NamespaceClassCleanupObsoleteKey: "true",
		})).To(Succeed())

		By("applying the NamespaceClass with one ConfigMap")
		Expect(utils.ApplyNamespaceClass(class, cmInjected, "from-class")).To(Succeed())

		By("waiting for the injected ConfigMap to appear")
		Eventually(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cmInjected, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, time.Minute, 5*time.Second).Should(ContainSubstring("foo: from-class"))

		By("creating an unrelated user-owned ConfigMap")
		Expect(utils.ApplyRawYAML(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  foo: user-owned
`, cmUser, ns))).To(Succeed())

		By("updating the NamespaceClass to trigger cleanup")
		Expect(utils.ApplyNamespaceClass(class, cmInjected, "updated-from-class")).To(Succeed())

		By("verifying the user-owned ConfigMap is still present (i.e., NOT deleted)")
		Consistently(func() string {
			out, _ := exec.Command("kubectl", "get", "configmap", cmUser, "-n", ns, "-o", "yaml").CombinedOutput()
			return string(out)
		}, 10*time.Second, 2*time.Second).Should(ContainSubstring("foo: user-owned"))

		By("cleaning up")
		_ = utils.DeleteResource("namespace", ns)
		_ = utils.DeleteResource("namespaceclass", class)
		_ = utils.DeleteEventsForInvolvedObject(ns)
	})
})
