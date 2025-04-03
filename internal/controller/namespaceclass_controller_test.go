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
		labels[controller.NamespaceClassLabelKey] = classLabel
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
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

func setupTestReconciler(objs ...client.Object) (*controller.NamespaceClassReconciler, runtime.Scheme, context.Context) {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

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
