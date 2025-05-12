package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	selectedNodeAnnotation   = "volume.kubernetes.io/selected-node"
	provisionerAnnotation    = "volume.kubernetes.io/storage-provisioner"
	expectedProvisionerValue = "rancher.io/local-path"
)

func cleanupPVCForNode(ctx context.Context, clientset *kubernetes.Clientset, nodeName string, pv corev1.PersistentVolume) {
	ann := pv.Annotations
	if ann[selectedNodeAnnotation] != nodeName ||
		ann[provisionerAnnotation] != expectedProvisionerValue {
		return
	}

	ref := pv.Spec.ClaimRef
	if ref == nil {
		return
	}

	err := clientset.CoreV1().
		PersistentVolumeClaims(ref.Namespace).
		Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil {
		fmt.Printf("failed to delete PVC %s/%s: %v\n", ref.Namespace, ref.Name, err)
		return
	}

	fmt.Printf("deleted PVC %s/%s bound to PV %s\n", ref.Namespace, ref.Name, pv.Name)
}

func cleanupPVCsForNode(ctx context.Context, clientset *kubernetes.Clientset, nodeName string) {
	pvs, err := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("error listing PVs: %v\n", err)
		return
	}
	for _, pv := range pvs.Items {
		go cleanupPVCForNode(ctx, clientset, nodeName, pv)
	}
}

func main() {
	// kubeconfig or in-cluster
	var config *rest.Config
	var err error
	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kc)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	nodeInformer := factory.Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			node := obj.(*corev1.Node)
			fmt.Printf("node deleted: %s\n", node.Name)
			cleanupPVCsForNode(context.Background(), clientset, node.Name)
		},
	})

	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(stopCh)
}
