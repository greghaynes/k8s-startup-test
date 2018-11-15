package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL   = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig  = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	iterations  = flag.Int("iterations", 100, "Number of testing iterations to perform.")
	parallelism = flag.Int("parallelism", 1, "Number of tests to run in parallel.")
)

func inContainerImage(job *batchv1.Job) string {
	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == "in-container-dummy" {
			return container.Image
		}
	}
	return ""
}

func ownerReference(owner *batchv1.Job) *metav1.OwnerReference {
	ownerRef := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       owner.ObjectMeta.Name,
		UID:        owner.ObjectMeta.UID,
	}
	return &ownerRef
}

func testPod(owner *batchv1.Job, target *apiv1.Pod, secret string) (*apiv1.Pod, error) {
	inContainerImage := inContainerImage(owner)
	if inContainerImage == "" {
		return nil, fmt.Errorf("Unable to obtain in-container image.")
	}
	pod := apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-pod",
			Namespace:    "default",
			OwnerReferences: []metav1.OwnerReference{
				*ownerReference(owner),
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
						{
							Name:  "SECRET",
							Value: secret,
						},
					},
				},
			},
		},
	}
	return &pod, nil
}

type httpSrv struct {
	mutex    sync.Mutex
	reqCond  *sync.Cond
	reqTimes map[string]time.Time
}

func (srv *httpSrv) WaitForSecret(secret string) time.Time {
	srv.mutex.Lock()
	defer srv.mutex.Unlock()
	waitKey := fmt.Sprintf("secret=%s", secret)
	log.Printf("Waiting for secret %s", secret)
	for {
		reqTime, ok := srv.reqTimes[waitKey]
		if ok {
			return reqTime
		}
		srv.reqCond.Wait()
	}
}

func (srv httpSrv) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	srv.mutex.Lock()
	defer srv.mutex.Unlock()
	srv.reqTimes[req.URL.RawQuery] = time.Now()
	srv.reqCond.Broadcast()
	fmt.Fprintf(w, "Got request!")
}

func runSingleTest(srv *httpSrv, podsClient corev1.PodInterface, myJob *batchv1.Job, myPod *apiv1.Pod, secret string) time.Duration {
	log.Printf("Running test")

	podDef, err := testPod(myJob, myPod, secret)
	if err != nil {
		log.Fatalf("Error creating pod definition: %v", err)
	}

	start := time.Now()
	createdPod, err := podsClient.Create(podDef)
	if err != nil {
		log.Fatalf("Error creating test pod: %v", err)
	}

	log.Printf("Waiting for request")
	stop := srv.WaitForSecret(secret)

	elapsed := stop.Sub(start)
	log.Printf("Elapsed time for %s: %v", secret, elapsed)

	err = podsClient.Delete(createdPod.ObjectMeta.Name, nil)
	if err != nil {
		log.Fatalf("Failed to delete test pod: %v", err)
	}

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

	handler := httpSrv{
		reqTimes: make(map[string]time.Time),
	}
	handler.reqCond = sync.NewCond(&handler.mutex)
	// Start http server
	srv := http.Server{
		Handler: handler,
	}
	go func() {
		srv.ListenAndServe()
	}()

	stopCh := make(chan interface{})
	workQueue := make(chan int)
	resultsQueue := make(chan time.Duration)

	// Dispatch work
	go func() {
		for i := 0; i < *iterations; i++ {
			workQueue <- i
		}
	}()

	results := make([]time.Duration, *iterations)
	go func() {
		for i := 0; i < *iterations; i++ {
			results[i] = <-resultsQueue
		}
		close(stopCh)
	}()

	for i := 0; i < *parallelism; i++ {
		go func() {
			for {
				select {
				case testNum := <-workQueue:
					resultsQueue <- runSingleTest(&handler, podsClient, myJob, myPod, fmt.Sprintf("%d", testNum))
				case <-stopCh:
					return
				}
			}
		}()
	}

	<-stopCh
	srv.Shutdown(context.TODO())
	fmt.Printf("Results:\n")
	for _, res := range results {
		fmt.Printf("%d,", res/time.Millisecond)
	}
	fmt.Printf("\n")
}
