package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/portforward"
	"k8s.io/kubernetes/pkg/client/unversioned/remotecommand"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/watch"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %v [NAMESPACE]", os.Args[0])
	}
	ns := os.Args[1]
	log.Printf("Waiting in namespace: %v", ns)

	cfg, err := getConfig()
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}
	client, err := client.New(cfg)
	if err != nil {
		log.Fatalf("Failed to get connection to Kubernetes master: %v", err)
	}

	stdout := make(chan string)

	go logs(client, stdout, ns)
	go forward(client, cfg, stdout, ns)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	go func() {
		<-signals
		stdout <- "Shutting down..."
		time.Sleep(time.Second)
		os.Exit(0)
	}()

	for str := range stdout {
		fmt.Println(str)
	}
}

func logs(client *client.Client, stdout chan string, ns string) error {
	opts := api.ListOptions{LabelSelector: labels.Set{"log": "yes"}.AsSelector()}
	following := map[string]bool{}
	watcher, err := client.Pods(ns).Watch(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get watcher: %v", err)
		return err
	}

	pods, err := client.Pods(ns).List(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get pods: %v", err)
		return err
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != api.PodRunning || following[pod.Name] {
			continue
		}
		following[pod.Name] = true
		go logsForPod(client, stdout, ns, pod.Name)
	}

	for result := range watcher.ResultChan() {
		if result.Type != watch.Added && result.Type != watch.Modified {
			continue
		}
		pod, ok := result.Object.(*api.Pod)
		if !ok || following[pod.Name] {
			continue
		}

		following[pod.Name] = true
		go logsForPod(client, stdout, ns, pod.Name)
	}

	return nil
}

func logsForPod(client *client.Client, stdout chan string, ns, pod string) {
	for {
		readCloser, err := client.Pods(ns).GetLogs(pod, &api.PodLogOptions{Follow: true}).Stream()
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(readCloser)
		for scanner.Scan() {
			stdout <- fmt.Sprintf("%v: %v", pod, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read logs for %v: %v\n", pod, err)
			break
		}
		readCloser.Close()

		stdout <- fmt.Sprintf("%v terminated", pod)
		break
	}
}

func forward(client *client.Client, cfg *restclient.Config, stdout chan string, ns string) {
	selector, _ := labels.Parse("containerPort")
	opts := api.ListOptions{LabelSelector: selector}
	forwarding := map[string]chan struct{}{}
	watcher, err := client.Pods(ns).Watch(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get watcher: %v", err)
		return
	}

	pods, err := client.Pods(ns).List(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		if forwarding[pod.Name] != nil {
			if pod.Status.Phase != api.PodRunning {
				stdout <- fmt.Sprintf("Stopping forward for %v", pod.Name)
				close(forwarding[pod.Name])
				delete(forwarding, pod.Name)
			}
			continue
		}
		if pod.Status.Phase != api.PodRunning {
			continue
		}
		stopCh := make(chan struct{}, 1)
		forwarding[pod.Name] = stopCh
		go forwardForPod(client, cfg, stdout, ns, pod, stopCh)
	}

	for result := range watcher.ResultChan() {
		if result.Type == watch.Error {
			continue
		}

		pod, ok := result.Object.(*api.Pod)
		if !ok {
			continue
		}

		if result.Type == watch.Deleted && forwarding[pod.Name] != nil {
			stdout <- fmt.Sprintf("Stopping forward for %v", pod.Name)
			close(forwarding[pod.Name])
			delete(forwarding, pod.Name)
			continue
		}

		if forwarding[pod.Name] != nil {
			continue
		}

		stopCh := make(chan struct{}, 1)
		forwarding[pod.Name] = stopCh
		go forwardForPod(client, cfg, stdout, ns, *pod, stopCh)
	}
}

func forwardForPod(client *client.Client, cfg *restclient.Config, stdout chan string, ns string, pod api.Pod, stopCh chan struct{}) {
	forwardTo, ok := pod.Labels["containerPort"]
	if !ok {
		return
	}
	port := ":" + forwardTo
	forwardFrom, ok := pod.Labels["hostPort"]
	if ok {
		port = fmt.Sprintf("%v:%v", forwardFrom, forwardTo)
	}
	ports := []string{port}

	req := client.RESTClient.Post().
		Resource("pods").
		Namespace(ns).
		Name(pod.Name).
		SubResource("portforward")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	go func() {
		<-signals
		close(stopCh)
	}()

	dialer, err := remotecommand.NewExecutor(cfg, "POST", req.URL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create executor: %v", err)
		return
	}

	fw, err := portforward.New(dialer, ports, stopCh, ioutil.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't forward port: %v", err)
		return
	}

	stdout <- fmt.Sprintf("Forwarding localhost:%v to %v:%v", forwardFrom, pod.Name, forwardTo)

	fw.ForwardPorts()
}
