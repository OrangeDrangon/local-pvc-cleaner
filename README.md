# local-pvc-cleaner

moved to [https://sr.ht/~drangon/local-pvc-cleaner/](https://sr.ht/~drangon/local-pvc-cleaner/)

Clean up persistent volumes, persistent volume claims, and any pod using those 
claims when the node is deleted. This is desireable when running with 
premptible nodes and operators that do not understand the idea of local storage
based persistent volumes on removable nodes.
