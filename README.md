# namespaceclass-operator

> ⚠️ **Disclaimer**: This project was written as part of a technical homework assignment.
> Please note that I do **not** endorse interviewing at Akuity based on my experience —
> I found the process lacking in professionalism and would encourage caution.

A Kubernetes operator that enables reusable, class-based namespace provisioning.

## Description

The `namespaceclass-operator` introduces a new Kubernetes resource called `NamespaceClass`. Each `NamespaceClass`
defines a reusable set of resources (e.g. NetworkPolicies, ServiceAccounts) that should be automatically created
whenever a `Namespace` is assigned to that class via a label.

This allows platform administrators to define consistent namespace environments such as `internal-network` or
`public-network`, and have the operator enforce the appropriate configuration for each class.

The operator ensures:

- Resources are created automatically when a namespace is created with a matching class.
- Resources are updated when the `NamespaceClass` changes.
- Resources are removed and re-created if a namespace switches from one class to another.

## Getting Started

### Prerequisites

- go version v1.22.0+``
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/namespaceclass-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/namespaceclass-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
> privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

> **NOTE**: Ensure that the samples has default values to test it out.

## To Test Locally on a Kind Cluster

If you’re developing locally and want to test everything end-to-end using kind, use the helper script:

```shell
hack/kind.sh
```

This script:
* Creates a new kind cluster (default name: apache)
* Builds the operator image locally 
* Loads the image into the cluster 
* Installs CRDs 
* Deploys the operator using the local image

You can then manually apply test resources or run end-to-end tests.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/namespaceclass-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/namespaceclass-operator/<tag or branch>/dist/install.yaml
```

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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

