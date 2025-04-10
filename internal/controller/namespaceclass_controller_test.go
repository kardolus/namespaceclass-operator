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

package controller_test

import (
	"context"
	"encoding/json"
	"github.com/kardolus/namespaceclass-operator/api/v1alpha1"
	"github.com/kardolus/namespaceclass-operator/internal/controller"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Reconcile", func() {

	Describe("Create", func() {
		It("should skip reconciliation if the NamespaceClass label is missing", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}

			r, _, ctx := setupTestReconciler(ns)

			result, err := r.Reconcile(ctx, requestFor(ns))
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())

			cMaps := listConfigMaps(r.Client, ctx, "test-ns")
			Expect(cMaps).To(BeEmpty())
		})

		It("should return an error if the referenced NamespaceClass does not exist", func() {
			ns := newNamespace("test-ns", "public-network")

			r, _, ctx := setupTestReconciler(ns)

			_, err := r.Reconcile(ctx, requestFor(ns))
			Expect(err).To(HaveOccurred())
		})

		It("should skip embedded resources that fail to unmarshal", func() {
			ns := newNamespace("test-ns", "broken-class")

			// Invalid Kubernetes object
			invalid := runtime.RawExtension{Raw: []byte(`"not a k8s object"`)}

			class := &v1alpha1.NamespaceClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "broken-class",
				},
				Spec: v1alpha1.NamespaceClassSpec{
					Resources: []runtime.RawExtension{invalid},
				},
			}

			r, _, ctx := setupTestReconciler(ns, class)

			_, err := r.Reconcile(ctx, requestFor(ns))
			Expect(err).NotTo(HaveOccurred())

			// Verify no resource was created
			cMaps := listConfigMaps(r.Client, ctx, "test-ns")
			Expect(cMaps).To(BeEmpty())
		})

		It("should log and skip resources that already exist", func() {
			ns := newNamespace("test-ns", "dup-class")

			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "injected-config",
					Namespace: "test-ns",
				},
				Data: map[string]string{"foo": "bar"},
			}

			class := newNamespaceClass("dup-class", mustRawConfigMap("injected-config", map[string]string{"foo": "bar"}))

			// cm is already "existing" in the namespace
			r, _, ctx := setupTestReconciler(ns, class, cm)

			_, err := r.Reconcile(ctx, requestFor(ns))
			Expect(err).NotTo(HaveOccurred())

			// Still exactly one ConfigMap — not duplicated
			cMaps := listConfigMaps(r.Client, ctx, "test-ns")
			Expect(cMaps).To(HaveLen(1))
			Expect(cMaps[0].Name).To(Equal("injected-config"))
		})

		It("should apply resources from NamespaceClass into the namespace", func() {
			ns := newNamespace("test-ns", "public-network")
			class := newNamespaceClass("public-network", mustRawConfigMap("injected-config", map[string]string{"foo": "bar"}))

			r, _, ctx := setupTestReconciler(ns, class)

			_, err := r.Reconcile(ctx, requestFor(ns))
			Expect(err).NotTo(HaveOccurred())

			var cm corev1.ConfigMap
			err = r.Get(ctx, types.NamespacedName{
				Name:      "injected-config",
				Namespace: "test-ns",
			}, &cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data).To(HaveKeyWithValue("foo", "bar"))
		})
	})

	Describe("Delete", func() {
		It("should delete resources if cleanup annotation is set", func() {
			ns := newNamespace("cleanup-ns", "clean-class")
			setCleanupAnnotation(ns)

			class := newDeletedNamespaceClass("clean-class", mustRawConfigMap("to-delete", map[string]string{"foo": "bar"}))
			injected := newInjectedConfigMap("to-delete", ns.Name, map[string]string{"foo": "bar"})

			r, _, ctx := setupTestReconciler(ns, class, injected)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: class.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(listConfigMaps(r.Client, ctx, ns.Name)).To(BeEmpty())
		})

		It("should emit an event if cleanup annotation is not set", func() {
			ns := newNamespace("orphan-ns", "orphan-class")

			class := newDeletedNamespaceClass("orphan-class", mustRawConfigMap("should-stay", map[string]string{"baz": "qux"}))
			injected := newInjectedConfigMap("should-stay", ns.Name, map[string]string{"baz": "qux"})

			r, _, ctx := setupTestReconciler(ns, class, injected)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: class.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(listConfigMaps(r.Client, ctx, ns.Name)).To(HaveLen(1))
		})

		It("should emit an event if NamespaceClass is already deleted and namespace still references it", func() {
			ns := newNamespace("ghost-ns", "ghost-class")

			r, _, ctx := setupTestReconciler(ns)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "ghost-class"},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(listConfigMaps(r.Client, ctx, "ghost-ns")).To(BeEmpty())
		})
	})

	Describe("Update", func() {
		It("should update existing resource if it already exists", func() {
			ns := newNamespace("update-ns", "class")
			original := newInjectedConfigMap("to-update", ns.Name, map[string]string{"foo": "original"})
			updated := mustRawConfigMap("to-update", map[string]string{"foo": "updated"})

			class := newNamespaceClass("class", updated)
			r, _, ctx := setupTestReconciler(ns, class, original)

			_, err := r.Reconcile(ctx, requestFor(class))
			Expect(err).NotTo(HaveOccurred())

			var cm corev1.ConfigMap
			err = r.Get(ctx, types.NamespacedName{
				Name:      "to-update",
				Namespace: ns.Name,
			}, &cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data).To(HaveKeyWithValue("foo", "updated"))
		})

		It("should be idempotent and not fail when applied twice", func() {
			ns := newNamespace("same-ns", "idempotent-class")
			cm := mustRawConfigMap("my-config", map[string]string{"foo": "bar"})
			class := newNamespaceClass("idempotent-class", cm)

			r, _, ctx := setupTestReconciler(ns, class)

			// First reconcile
			_, err := r.Reconcile(ctx, requestFor(class))
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile should not fail or duplicate
			_, err = r.Reconcile(ctx, requestFor(class))
			Expect(err).NotTo(HaveOccurred())

			cms := listConfigMaps(r.Client, ctx, ns.Name)
			Expect(cms).To(HaveLen(1))
			Expect(cms[0].Data).To(HaveKeyWithValue("foo", "bar"))
		})

		It("should delete obsolete resources if cleanup-obsolete annotation is set", func() {
			ns := newNamespace("rename-ns", "rename-class")
			ns.Annotations = map[string]string{
				controller.NamespaceClassCleanupObsoleteKey: "true",
			}

			oldCM := mustRawConfigMap("old-name", map[string]string{"foo": "old"})
			injected := newInjectedConfigMap("old-name", ns.Name, map[string]string{"foo": "old"})

			class := newNamespaceClass("rename-class", oldCM)
			r, _, ctx := setupTestReconciler(ns, class, injected)

			// First reconcile to apply the old resource
			_, err := r.Reconcile(ctx, requestFor(class))
			Expect(err).NotTo(HaveOccurred())

			// Simulate controller having tracked oldCM in status
			var persisted v1alpha1.NamespaceClass
			Expect(r.Get(ctx, types.NamespacedName{Name: class.Name}, &persisted)).To(Succeed())
			persisted.Status.LastAppliedResources = []runtime.RawExtension{oldCM}
			Expect(r.Status().Update(ctx, &persisted)).To(Succeed())

			// Now update spec to use new-name instead
			newCM := mustRawConfigMap("new-name", map[string]string{"foo": "new"})
			persisted.Spec.Resources = []runtime.RawExtension{newCM}
			Expect(r.Update(ctx, &persisted)).To(Succeed())

			// Trigger another reconcile
			var trigger v1alpha1.NamespaceClass
			Expect(r.Get(ctx, types.NamespacedName{Name: persisted.Name}, &trigger)).To(Succeed())
			_, err = r.Reconcile(ctx, requestFor(&trigger))
			Expect(err).NotTo(HaveOccurred())

			// Now only new-name should exist
			cms := listConfigMaps(r.Client, ctx, ns.Name)
			Expect(cms).To(HaveLen(1))
			Expect(cms[0].Name).To(Equal("new-name"))
		})

		It("should not delete obsolete resources if cleanup-obsolete annotation is missing", func() {
			ns := newNamespace("preserve-ns", "preserve-class")
			oldCM := newInjectedConfigMap("old-name", ns.Name, map[string]string{"foo": "old"})
			newCM := mustRawConfigMap("new-name", map[string]string{"foo": "new"})

			class := newNamespaceClass("preserve-class", newCM)
			r, _, ctx := setupTestReconciler(ns, class, oldCM)

			_, err := r.Reconcile(ctx, requestFor(class))
			Expect(err).NotTo(HaveOccurred())

			cms := listConfigMaps(r.Client, ctx, ns.Name)
			Expect(cms).To(HaveLen(2))
			Expect([]string{cms[0].Name, cms[1].Name}).To(ContainElements("old-name", "new-name"))
		})
	})

	Describe("Finalizers", func() {
		It("should add a finalizer to NamespaceClass if missing", func() {
			class := newNamespaceClass("needs-finalizer", mustRawConfigMap("some", map[string]string{"x": "y"}))
			ns := newNamespace("some-ns", "needs-finalizer")

			r, _, ctx := setupTestReconciler(ns, class)

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: class.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			var updated v1alpha1.NamespaceClass
			Expect(r.Get(ctx, types.NamespacedName{Name: class.Name}, &updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(controller.NamespaceClassFinalizerKey))
		})
	})
})

func listConfigMaps(t client.Client, ctx context.Context, ns string) []corev1.ConfigMap {
	var list corev1.ConfigMapList
	err := t.List(ctx, &list, client.InNamespace(ns))
	Expect(err).NotTo(HaveOccurred())
	return list.Items
}

func mustRawConfigMap(name string, data map[string]string) runtime.RawExtension {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: data,
	}

	raw, err := json.Marshal(cm)
	Expect(err).NotTo(HaveOccurred())
	return runtime.RawExtension{Raw: raw}
}

func newNamespace(name, classLabel string) *corev1.Namespace {
	labels := map[string]string{}
	if classLabel != "" {
		labels[controller.NamespaceClassNameKey] = classLabel
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newDeletedNamespaceClass(name string, resources ...runtime.RawExtension) *v1alpha1.NamespaceClass {
	now := metav1.Now()
	return &v1alpha1.NamespaceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Finalizers:        []string{controller.NamespaceClassFinalizerKey},
			DeletionTimestamp: &now,
		},
		Spec: v1alpha1.NamespaceClassSpec{
			Resources: resources,
		},
	}
}

func newInjectedConfigMap(name, ns string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Data: data,
	}
}

func newNamespaceClass(name string, resources ...runtime.RawExtension) *v1alpha1.NamespaceClass {
	return &v1alpha1.NamespaceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.NamespaceClassSpec{
			Resources: resources,
		},
	}
}

func requestFor(obj client.Object) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(), // optional if cluster-scoped
		},
	}
}

func setCleanupAnnotation(ns *corev1.Namespace) {
	if ns.Annotations == nil {
		ns.Annotations = map[string]string{}
	}
	ns.Annotations[controller.NamespaceClassCleanupKey] = "true"
}

func setupTestReconciler(objs ...client.Object) (*controller.NamespaceClassReconciler, runtime.Scheme, context.Context) {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.NamespaceClass{}).
		Build()

	r := &controller.NamespaceClassReconciler{
		Client:   client,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	ctx := context.Background()
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter)))
	ctx = log.IntoContext(ctx, log.Log)

	return r, *scheme, ctx
}
