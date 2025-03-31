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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	if err := r.Get(ctx, req.NamespacedName, &ns); err == nil {
		return r.reconcileNamespace(ctx, &ns)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

func (r *NamespaceClassReconciler) reconcileNamespace(ctx context.Context, ns *corev1.Namespace) (ctrl.Result, error) {
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

// TODO implement DELETE
// TODO implement UPDATE
// TODO Add predicate to filter namespaces that have the namespaceclass.akuity.io/name label
// TODO re-review the generated RBAC - did we go too far with the permissions?
