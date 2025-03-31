# Namespace Class

## Description

Kubernetes admins wish to define a set of namespace "classes". A NamespaceClass defines a set of
complimentary resources, policies, etc... which are additionally created and managed
when a namespace is created from a certain class.

To solve this, we will introduce a new `NamespaceClass` CRD and controller to automate the maintenance
of these resources.

## Use Case

A Kubernetes operator should be able to apply a new kind of Kubernetes resource called NamespaceClass,
implemented as CRD (custom resource definition). For example, the admin might define two classes
of namespaces relating to network access:

* `public-network` - public-network namespaces would additionally contain policies allowing the namespace to be reachable over public internet
* `internal-network`- internal-network namespaces would additionally create policies restricting network access to only corporate VPN

```yaml
apiVersion: v1
kind: NamespaceClass
metadata:
  name: public-network
spec:
  # TODO: NetworkPolicies allowing ingress from the world
```

```yaml
apiVersion: v1
kind: NamespaceClass
metadata:
  name: internal-network
spec:
  # TODO: NetworkPolicies allowing egress/ingress to specific VPN IP address
```

To use a `NamespaceClass`, a namespace will have a label indicating which NamespaceClass it would
derive from. For example, the admin could create a `web-portal` namespace which allows public
egress into the namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: web-portal
  labels:
    namespaceclass.akuity.io/name: public-network
```

When the `web-portal` namespace is created, the controller would create the associated resources
defined in the `public-network` NamespaceClass.

NOTE: NamespaceClass should allow creating any kind of resources (not only `NetworkPolicy`/`ServiceAccount`).

## Requirements

NOTE: this problem is intended to have minimal requirements in order to allow freedom in the
design and architectural decisions. It only has the following requirements:

### NamespaceClass CRD

Design a new CustomResourceDefinition, `NamespaceClass`, which allows an operator the flexibility to
define the additional Kubernetes resources which should be created when a namespace of that class
is created (e.g. NetworkPolicies, ServiceAccounts, PodSecurityPolicies etc...). There are no
requirements on what the API spec should look like.

### NamespaceClass controller

Create a kubernetes controller, which monitors Namespaces and creates the additional resources
as defined by the class annotated in the namespace.

### Switching classes

A `Namespace` may be modified to switch to a different class. When this happens the controller should
handle the creation and deletion of the associated resources between the old and the new class

### Updating classes

A `NamespaceClass` may be modified to have a different set of resources. When this happens, existing
Namepaces of that class should then be updated to create or delete resources according to the
updated class.