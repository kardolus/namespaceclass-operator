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

package controller

import (
	"context"
	"fmt"
	"github.com/kardolus/namespaceclass-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	NamespaceClassNameKey      = "namespaceclass.akuity.io/name"
	NamespaceClassCleanupKey   = "namespaceclass.akuity.io/cleanup"
	NamespaceClassFinalizerKey = "namespaceclass.kardolus.dev/finalizer"
)

// NamespaceClassReconciler reconciles a NamespaceClass object
type NamespaceClassReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles both Namespace and NamespaceClass events.
//
// For Namespace events:
//   - If the "namespaceclass.akuity.io/name" label is present on the Namespace,
//     the controller looks up the referenced NamespaceClass and injects its
//     defined resources into the Namespace.
//
// For NamespaceClass deletion events:
//   - The controller identifies all Namespaces that reference the deleted class.
//   - If a referencing Namespace has the annotation
//     "namespaceclass.akuity.io/cleanup: true", injected resources are cleaned up.
//   - Otherwise, a warning Event is emitted to indicate that the Namespace is now orphaned.
func (r *NamespaceClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("reconcile", req.NamespacedName)

	// If this is a Namespace, handle it
	var ns corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &ns); err == nil {
		return r.reconcileNamespaceCreate(ctx, &ns)
	}

	// Otherwise assume it's a NamespaceClass
	var class v1alpha1.NamespaceClass
	if err := r.Get(ctx, req.NamespacedName, &class); err != nil {
		// If not found, look for orphaned namespaces and emit warning events
		if client.IgnoreNotFound(err) == nil {
			var nsList corev1.NamespaceList
			if listErr := r.List(ctx, &nsList, client.MatchingLabels{
				NamespaceClassNameKey: req.Name,
			}); listErr != nil {
				return ctrl.Result{}, listErr
			}
			for _, ns := range nsList.Items {
				r.Recorder.Eventf(&ns, corev1.EventTypeWarning, "OrphanedNamespaceClass",
					"Namespace references missing NamespaceClass '%s'", req.Name)
			}
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling NamespaceClass", "name", class.Name)

	// Handle deletion via finalizer
	if !class.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&class, NamespaceClassFinalizerKey) {
			log.Info("🧹 Finalizing NamespaceClass deletion")
			if res, err := r.reconcileNamespaceClassDelete(ctx, class.Name); err != nil {
				return res, err
			}

			// Remove finalizer after successful cleanup
			controllerutil.RemoveFinalizer(&class, NamespaceClassFinalizerKey)
			if err := r.Update(ctx, &class); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("✅ Finalizer removed, deletion can proceed")
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present for cleanup later
	if !controllerutil.ContainsFinalizer(&class, NamespaceClassFinalizerKey) {
		controllerutil.AddFinalizer(&class, NamespaceClassFinalizerKey)
		if err := r.Update(ctx, &class); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("🔖 Finalizer added to NamespaceClass")
	}

	// Inject or update resources in all matching namespaces
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList, client.MatchingLabels{
		NamespaceClassNameKey: class.Name,
	}); err != nil {
		return ctrl.Result{}, err
	}

	for _, ns := range nsList.Items {
		nsLog := log.WithValues("namespace", ns.Name)
		for _, res := range class.Spec.Resources {
			if err := r.upsert(ctx, res, ns.Name); err != nil {
				nsLog.Error(err, "Failed to upsert resource")
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("namespaceclass-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NamespaceClass{}). // Primary resource
		Watches( // Watch namespaces to trigger reconcile on the referenced NamespaceClass
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToNamespaceClass),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *NamespaceClassReconciler) mapNamespaceToNamespaceClass(ctx context.Context, obj client.Object) []reconcile.Request {
	className := obj.GetLabels()[NamespaceClassNameKey]
	if className == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: className},
	}}
}

func (r *NamespaceClassReconciler) reconcileNamespaceClassDelete(ctx context.Context, className string) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("deletedNamespaceClass", className)

	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList, client.MatchingLabels{
		NamespaceClassNameKey: className,
	}); err != nil {
		log.Error(err, "Failed to list namespaces for cleanup")
		return ctrl.Result{}, err
	}

	var class v1alpha1.NamespaceClass
	if err := r.Get(ctx, types.NamespacedName{Name: className}, &class); err != nil {
		log.Error(err, "Class not found — skipping resource cleanup")
		return ctrl.Result{}, nil // Don't fail reconciliation; just skip
	}

	for _, ns := range nsList.Items {
		log := log.WithValues("namespace", ns.Name)

		cleanup := ns.Annotations[NamespaceClassCleanupKey] == "true"
		if cleanup {
			for i, res := range class.Spec.Resources {
				fmt.Printf("    🔧 [%d] Unmarshaling resource...\n", i)
				obj := &unstructured.Unstructured{}
				if err := obj.UnmarshalJSON(res.Raw); err != nil {
					log.Error(err, "🚫 Failed to unmarshal embedded resource")
					continue
				}

				gvk := obj.GroupVersionKind()
				name := obj.GetName()

				obj.SetNamespace(ns.Name)

				if err := r.Delete(ctx, obj); err != nil {
					log.Error(err, "Failed to delete resource", "kind", gvk.Kind, "name", name)
				} else {
					log.Info("Deleted resource", "kind", gvk.Kind, "name", name)
				}
			}
		} else {
			log.Info("Skipping cleanup; annotation not set")

			r.Recorder.Eventf(&ns, corev1.EventTypeWarning, "OrphanedNamespaceClass",
				"Namespace references deleted NamespaceClass '%s' but does not have cleanup enabled", className)
		}
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceClassReconciler) reconcileNamespaceCreate(ctx context.Context, ns *corev1.Namespace) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", ns.Name)

	log.Info("Reconciling namespace")

	className, ok := ns.Labels[NamespaceClassNameKey]
	if !ok {
		log.Info("Skipping namespace without NamespaceClass label")
		return ctrl.Result{}, nil
	}

	var class v1alpha1.NamespaceClass
	if err := r.Get(ctx, types.NamespacedName{Name: className}, &class); err != nil {
		log.Error(err, "Failed to get NamespaceClass", "className", className)
		r.Recorder.Eventf(ns, corev1.EventTypeWarning, "MissingNamespaceClass",
			"Namespace references missing NamespaceClass '%s'", className)
		return ctrl.Result{}, err
	}

	log.Info("Applying NamespaceClass", "class", className)

	for _, res := range class.Spec.Resources {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(res.Raw); err != nil {
			log.Error(err, "Failed to unmarshal embedded resource")
			continue
		}

		// Force the resource into the namespace
		obj.SetNamespace(ns.Name)

		if err := r.Create(ctx, obj); err != nil {
			log.Error(err, "Failed to create resource in namespace", "gvk", obj.GroupVersionKind())
			continue
		}

		log.Info("Created resource", "kind", obj.GetKind(), "name", obj.GetName())
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceClassReconciler) upsert(ctx context.Context, raw runtime.RawExtension, namespace string) error {
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", namespace)

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(raw.Raw); err != nil {
		log.Error(err, "Failed to unmarshal embedded resource")
		return err
	}

	obj.SetNamespace(namespace)

	// Check if the resource already exists
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: namespace,
	}
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	if err := r.Get(ctx, key, existing); err == nil {
		// Already exists: perform update
		obj.SetResourceVersion(existing.GetResourceVersion())
		if err := r.Update(ctx, obj); err != nil {
			log.Error(err, "Failed to update existing resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
			return err
		}
		log.Info("Updated existing resource", "kind", obj.GetKind(), "name", obj.GetName())
		return nil
	}

	// Otherwise, create it
	if err := r.Create(ctx, obj); err != nil {
		log.Error(err, "Failed to create resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
		return err
	}

	log.Info("Created resource", "kind", obj.GetKind(), "name", obj.GetName())
	return nil
}

// TODO Upsert: handle resource deletions
// TODO Upsert: handle renamed resources
// TODO Bonus: use Akuity or Argo to run CI/CD
