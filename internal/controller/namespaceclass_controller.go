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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const NamespaceClassLabelKey = "namespaceclass.akuity.io/name"

// NamespaceClassReconciler reconciles a NamespaceClass object
type NamespaceClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=namespace.kardolus.dev,resources=namespaceclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles namespace events and applies resources from the associated
// NamespaceClass, if present.
//
// This method is triggered whenever a NamespaceClass or Namespace object changes.
// For Namespace objects, if the "namespaceclass.akuity.io/name" label is present,
// the controller will look up the referenced NamespaceClass and create the defined
// resources within the Namespace.
func (r *NamespaceClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ns corev1.Namespace

	if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// Namespace was deleted — handle cleanup
		return r.reconcileNamespaceDelete(ctx, req.NamespacedName.Name)
	}

	return r.reconcileNamespaceCreate(ctx, &ns)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Watches(
			&v1alpha1.NamespaceClass{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceClassToNamespaces),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				// Cast to your specific type if needed
				_, ok := obj.(*v1alpha1.NamespaceClass)
				return ok // TODO look into filter logic
			})),
		).
		Complete(r)
}

func (r *NamespaceClassReconciler) mapNamespaceClassToNamespaces(ctx context.Context, obj client.Object) []ctrl.Request {
	className := obj.GetName()

	var namespaces corev1.NamespaceList
	if err := r.List(ctx, &namespaces, client.MatchingLabels{
		NamespaceClassLabelKey: className,
	}); err != nil {
		return nil
	}

	var requests []ctrl.Request
	for _, ns := range namespaces.Items {
		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: ns.Name},
		})
	}
	return requests
}

func (r *NamespaceClassReconciler) reconcileNamespaceCreate(ctx context.Context, ns *corev1.Namespace) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", ns.Name)

	log.Info("Reconciling namespace")

	className, ok := ns.Labels[NamespaceClassLabelKey]
	if !ok {
		log.Info("Skipping namespace without NamespaceClass label")
		return ctrl.Result{}, nil
	}

	var class v1alpha1.NamespaceClass
	if err := r.Get(ctx, types.NamespacedName{Name: className}, &class); err != nil {
		log.Error(err, "Failed to get NamespaceClass", "className", className)
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

func (r *NamespaceClassReconciler) reconcileNamespaceDelete(ctx context.Context, name string) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", name)
	log.Info("Cleaning up resources for deleted namespace")

	// Get all NamespaceClasses (or index later for optimization)
	var classes v1alpha1.NamespaceClassList
	if err := r.List(ctx, &classes); err != nil {
		log.Error(err, "Failed to list NamespaceClasses")
		return ctrl.Result{}, err
	}

	for _, class := range classes.Items {
		for _, res := range class.Spec.Resources {
			obj := &unstructured.Unstructured{}
			if err := obj.UnmarshalJSON(res.Raw); err != nil {
				log.Error(err, "Failed to unmarshal embedded resource")
				continue
			}

			obj.SetNamespace(name)
			if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
				log.Error(err, "Failed to delete resource from namespace", "kind", obj.GetKind(), "name", obj.GetName())
			} else {
				log.Info("Deleted resource", "kind", obj.GetKind(), "name", obj.GetName())
			}
		}
	}

	return ctrl.Result{}, nil
}

// TODO implement UPDATE
// TODO re-review the generated RBAC - did we go too far with the permissions?
// TODO bonus: use Akuity to run CI/CD?
