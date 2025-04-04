#!/bin/bash
#
# Sleep the current shell until the pod labeled $LABEL is ready

start=$(date +%s)

echo -e "Waiting for pod with label '$LABEL'"

while [[ $(kubectl get pods -l $LABEL --all-namespaces -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}') != "True" ]];
do
  sleep 1;
done

end=$(date +%s)
runtime=$((end-start))
echo "Pod labeled '$LABEL' ready in $runtime seconds"
