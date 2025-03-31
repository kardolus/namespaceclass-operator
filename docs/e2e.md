Steps to test manually on a clean kind cluster:

1. Build the operator image locally (skip if already built):

```shell
make docker-build IMG=namespaceclass-operator:test
```

2. Load the image into the Kind cluster:

```shell
kind load docker-image namespaceclass-operator:test --name apache
```

3. Install CRDs (Custom Resource Definitions) in the cluster:

```shell
make install
```

This will install the necessary CRDs for the NamespaceClass resource.
4. Deploy the operator in the cluster:

```shell
make deploy IMG=namespaceclass-operator:test
```

Ensure that the operator is successfully deployed. You can check the deployment status with:

```shell
kubectl get deployments -n namespaceclass-operator-system
```

Make sure the namespaceclass-operator-controller-manager is running.

5. Apply a NamespaceClass resource:

Create the NamespaceClass with the ConfigMap resource definition:

```shell
kubectl apply -f - <<EOF
apiVersion: namespace.kardolus.dev/v1alpha1
kind: NamespaceClass
metadata:
  name: public-network
spec:
  resources:
  - apiVersion: v1
    kind: ConfigMap
    metadata:
      name: injected-config
    data:
      foo: bar
EOF
```

This will create a NamespaceClass called public-network that specifies a ConfigMap with the name injected-config.

6. Create a namespace and label it with namespaceclass.akuity.io/name:

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: web-portal
  labels:
    namespaceclass.akuity.io/name: public-network
EOF
```

This will create the web-portal namespace and label it with the NamespaceClass you just created.

7. Verify the injected ConfigMap:

Now, you need to check if the ConfigMap was created in the web-portal namespace. Run the following command:

```shell
kubectl get configmap injected-config -n web-portal -o yaml
```

If everything is correct, you should see the ConfigMap definition with the foo: bar data.