package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func inContainerImage(job *batchv1.Job) string {
	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == "in-container-dummy" {
			return container.Image
		}
	}
	return ""
}

func testPod(owner *batchv1.Job, target *apiv1.Pod) (*apiv1.Pod, error) {
	inContainerImage := inContainerImage(owner)
	if inContainerImage == "" {
		return nil, fmt.Errorf("Unable to obtain in-container image.")
	}
	pod := apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       owner.ObjectMeta.Name,
					UID:        owner.ObjectMeta.UID,
				},
			},
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:  "test-container",
					Image: inContainerImage,
					Env: []apiv1.EnvVar{
						{
							Name:  "TARGET_HOST",
							Value: target.Status.PodIP,
						},
					},
				},
			},
		},
	}
	return &pod, nil
}

type httpSrv struct {
	stopCh chan struct{}
}

func (srv httpSrv) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Hello!")
	close(srv.stopCh)
}

func runTest(podsClient corev1.PodInterface, myJob *batchv1.Job, myPod *apiv1.Pod) time.Duration {
	log.Printf("Running test")

	stopCh := make(chan struct{})
	srv := http.Server{
		Handler: httpSrv{stopCh},
	}
	go func() {
		srv.ListenAndServe()
	}()

	podDef, err := testPod(myJob, myPod)
	if err != nil {
		log.Fatalf("Error creating pod definition: %v", err)
	}

	start := time.Now()
	_, err = podsClient.Create(podDef)
	if err != nil {
		log.Fatalf("Error creating test pod: %v", err)
	}

	log.Printf("Waiting for request")
	<-stopCh
	stop := time.Now()

	elapsed := stop.Sub(start)
	log.Printf("Elapsed time: %v", elapsed)

	srv.Shutdown(context.TODO())

	return elapsed
}

func main() {
	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Error building kubernetes clientset: %v", err)
	}

	jobsClient := kubeClient.Batch().Jobs("default")
	myJob, err := jobsClient.Get("startup-test", metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Error getting jobs client: %v", err)
	}

	podsClient := kubeClient.Core().Pods("default")

	var myPod *apiv1.Pod
	for {
		myPods, err := podsClient.List(metav1.ListOptions{
			LabelSelector: "app=startup-test",
		})
		if err != nil {
			log.Fatalf("Error getting list of pods for our job: %v", err)
		}
		if len(myPods.Items) != 1 {
			log.Fatalf("Got multiple pods for our job: %v", myPods)
		}

		myPod = &myPods.Items[0]
		if myPod.Status.PodIP == "" {
			log.Printf("No PodIP in status, waiting.")
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	runTest(podsClient, myJob, myPod)
}
