# local-pvc-cleaner

Idea is to clean up persistent volumes and there claims after the node is deleted
on a persistent cluster when using the Rancher local-path-provisioner. In the 
future this may grow to expand to targeting any pod with those volumes bound,
adding a finalizer, deleting the persistent volumes and persistent volume claims
then removing the finalizer.

This is currently broken because the labels and way of getting resources is wrong.
