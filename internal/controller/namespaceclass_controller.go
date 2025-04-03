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
	"github.com/kardolus/namespaceclass-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	NamespaceClassNameKey    = "namespaceclass.akuity.io/name"
	NamespaceClassCleanupKey = "namespaceclass.akuity.io/cleanup"
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

	var ns corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &ns); err == nil {
		return r.reconcileNamespaceCreate(ctx, &ns)
	}

	var class v1alpha1.NamespaceClass
	if err := r.Get(ctx, req.NamespacedName, &class); err == nil {
		log.Info("Reconciling NamespaceClass", "name", class.Name)

		var nsList corev1.NamespaceList
		if err := r.List(ctx, &nsList, client.MatchingLabels{
			NamespaceClassNameKey: class.Name,
		}); err != nil {
			return ctrl.Result{}, err
		}

		for _, ns := range nsList.Items {
			log := log.WithValues("namespace", ns.Name)
			for _, res := range class.Spec.Resources {
				obj := &unstructured.Unstructured{}
				if err := obj.UnmarshalJSON(res.Raw); err != nil {
					log.Error(err, "Failed to unmarshal resource")
					continue
				}
				obj.SetNamespace(ns.Name)
				if err := r.Create(ctx, obj); err != nil {
					log.Error(err, "Failed to create resource", "gvk", obj.GroupVersionKind())
				} else {
					log.Info("Created resource", "kind", obj.GetKind(), "name", obj.GetName())
				}
			}
		}
		return ctrl.Result{}, nil
	}

	// If neither exists, maybe the class was deleted
	log.Info("Handling deleted NamespaceClass")
	return r.reconcileNamespaceClassDelete(ctx, req.Name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("namespaceclass-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NamespaceClass{}). // Primary resource
		Watches(                         // Watch namespaces to trigger reconcile on the referenced NamespaceClass
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
		return ctrl.Result{}, err
	}

	for _, ns := range nsList.Items {
		log := log.WithValues("namespace", ns.Name)

		cleanup := ns.Annotations[NamespaceClassCleanupKey] == "true"
		if cleanup {
			log.Info("Cleaning up resources for namespace referencing deleted class")

			// TODO: Update to support all types of resources from class.Spec.Resources
			var cms corev1.ConfigMapList
			if err := r.List(ctx, &cms, client.InNamespace(ns.Name)); err != nil {
				log.Error(err, "Failed to list ConfigMaps for cleanup")
				continue
			}

			for _, cm := range cms.Items {
				if err := r.Delete(ctx, &cm); err != nil {
					log.Error(err, "Failed to delete ConfigMap", "name", cm.Name)
				} else {
					log.Info("Deleted ConfigMap", "name", cm.Name)
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

// TODO implement UPDATE
// TODO re-review the generated RBAC - did we go too far with the permissions?
// TODO bonus: use Akuity to run CI/CD?
