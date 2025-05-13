package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	pvcByNodeIndex           = "pvcByNode"
	podByPvcIndex            = "podByPvc"
)

func deleteVolumes(ctx context.Context, clientset *kubernetes.Clientset, factory informers.SharedInformerFactory, pvc *corev1.PersistentVolumeClaim) {
	err := clientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
	if err != nil {
		fmt.Printf("failed to delete pvc(%s): %v\n", pvc.Name, err)
		return
	}
	fmt.Printf("deleted pvc(%s)\n", pvc.Name)

	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		fmt.Printf("pvc(%s) is not bound to a volume\n", pvc.Name)
		return
	}

	err = clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
	if err != nil {
		fmt.Printf("failed to delete pv(%s): %v\n", pvName, err)
		return
	}

	fmt.Printf("deleted pv(%s)\n", pvName)

	pods, err := factory.Core().V1().Pods().Informer().GetIndexer().ByIndex(podByPvcIndex, pvc.Name)
	if err != nil {
		fmt.Printf("error getting pods from index: %v\n", err)
		return
	}

	for _, podAny := range pods {
		pod := podAny.(*corev1.Pod)
		err = clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil {
			fmt.Printf("failed to delete pod(%s): %v\n", pod.Name, err)
			continue
		}

		fmt.Printf("deleted pod(%s)\n", pod.Name)
	}
}

func cleanupVolumesByNode(ctx context.Context, clientset *kubernetes.Clientset, nodeName string, factory informers.SharedInformerFactory) {
	persistentVolumeClaims, err := factory.Core().V1().PersistentVolumeClaims().Informer().GetIndexer().ByIndex(pvcByNodeIndex, nodeName)
	if err != nil {
		fmt.Printf("error getting pvc from index: %v\n", err)
		return
	}
	for _, pvcAny := range persistentVolumeClaims {
		pvc := pvcAny.(*corev1.PersistentVolumeClaim)
		deleteVolumes(ctx, clientset, factory, pvc)
	}
}

func main() {
	// kubeconfig or in-cluster
	var config *rest.Config
	var err error
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
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

	podInformer := factory.Core().V1().Pods().Informer()
	podInformer.AddIndexers(cache.Indexers{
		podByPvcIndex: func(obj any) ([]string, error) {
			pod := obj.(*corev1.Pod)
			pvcs := make([]string, 0, len(pod.Spec.Volumes))
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim == nil {
					continue
				}

				claimName := volume.PersistentVolumeClaim.ClaimName
				if claimName == "" {
					continue
				}
				pvcs = append(pvcs, claimName)
			}

			return pvcs, nil
		},
	})

	pvcInformer := factory.Core().V1().PersistentVolumeClaims().Informer()
	pvcInformer.AddIndexers(cache.Indexers{
		pvcByNodeIndex: func(obj any) ([]string, error) {
			pvc := obj.(*corev1.PersistentVolumeClaim)
			if pvc.Annotations[provisionerAnnotation] != expectedProvisionerValue {
				return nil, nil
			}

			return []string{pvc.Annotations[selectedNodeAnnotation]}, nil
		},
	})

	nodeInformer := factory.Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			node := obj.(*corev1.Node)
			fmt.Printf("node deleted: %s\n", node.Name)
			cleanupVolumesByNode(context.TODO(), clientset, node.Name, factory)
		},
	})

	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	pvcs, err := factory.Core().V1().PersistentVolumeClaims().Lister().List(labels.Everything())
	for _, pvc := range pvcs {
		if pvc.Annotations[provisionerAnnotation] != expectedProvisionerValue {
			continue
		}

		nodeName := pvc.Annotations[selectedNodeAnnotation]
		_, exists, err := factory.Core().V1().Nodes().Informer().GetStore().GetByKey(nodeName)
		if err != nil {
			fmt.Printf("failed to get node(%s) from pvc(%s): %v\n", nodeName, pvc.Name, err)
			continue
		}

		if exists {
			fmt.Printf("node(%s) does exist in store from pvc(%s)\n", nodeName, pvc.Name)
			continue
		}

		fmt.Printf("node(%s) does not exist in store from pvc(%s)\n", nodeName, pvc.Name)
		deleteVolumes(context.TODO(), clientset, factory, pvc)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(stopCh)
}
