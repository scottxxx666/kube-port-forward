package main

import (
	"flag"
	"fmt"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/homedir"
	"k8s.io/kubectl/pkg/util"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Yaml struct {
	Ports []string `yaml:ports`
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func main() {
	go func() {
		for true {
			fmt.Println("Goroutine num: ", runtime.NumGoroutine())
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Printf("Alloc = %v MiB  ", bToMb(m.Alloc))
			fmt.Printf("TotalAlloc = %v MiB  ", bToMb(m.TotalAlloc))
			fmt.Printf("Sys = %v MiB  ", bToMb(m.Sys))
			fmt.Printf("NumGC = %v \n", m.NumGC)

			time.Sleep(1 * time.Minute)
		}
	}()

	var fileName string
	flag.StringVar(&fileName, "f", "", "YAML file to parse.")
	flag.Parse()

	if fileName == "" {
		fmt.Println("Please provide yaml file by using -f option")
		return
	}
	yamlFile, err := ioutil.ReadFile(fileName)
	if err != nil {
		fmt.Printf("Error reading YAML file: %s\n", err)
		return
	}

	y := Yaml{}
	err = yaml.Unmarshal(yamlFile, &y)
	if err != nil {
		panic(err)
	}

	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	var wg sync.WaitGroup
	num := len(y.Ports)
	inputCh := make(chan Forwarder, num)
	wg.Add(num)
	for _, i := range y.Ports {
		r := strings.Split(i, ":")
		localPort, err := strconv.Atoi(r[0])
		if err != nil {
			panic(err)
		}
		namespace := r[1]
		serviceName := r[2]
		remotePort, err := strconv.ParseInt(r[3], 10, 32)
		if err != nil {
			panic(err)
		}
		inputCh <- Forwarder{
			Namespace:   namespace,
			ServiceName: serviceName,
			LocalPort:   localPort,
			ServicePort: int32(remotePort),
		}

		go func() {
			for input := range inputCh {
				input.forward(*kubeconfig)
				inputCh <- input
			}
		}()
	}

	wg.Wait()
	fmt.Println("***ALL END***")
}

type Forwarder struct {
	Namespace   string
	ServiceName string
	LocalPort   int
	ServicePort int32
}

func (f Forwarder) forward(kubeconfig string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recover from: ", r)
			return
		}
		println("Reconnect")
	}()

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err)
	}

	roundTripper, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	ctx := context.TODO()

	service, err := clientset.CoreV1().Services(f.Namespace).Get(ctx, f.ServiceName, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	set := labels.Set(service.Spec.Selector)

	pods, err := clientset.CoreV1().Pods(f.Namespace).List(ctx, metav1.ListOptions{LabelSelector: set.AsSelector().String()})
	if err != nil {
		panic(err.Error())
	}
	if len(pods.Items) == 0 {
		panic("no pods")
	}
	podName := pods.Items[0].Name
	remotePort, _ := util.LookupContainerPortNumberByServicePort(*service, pods.Items[0], f.ServicePort)
	fmt.Println(pods.Items[0].Name)

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", f.Namespace, podName)
	hostIP := strings.TrimLeft(config.Host, "htps:/")
	serverURL := url.URL{Scheme: "https", Path: path, Host: hostIP}

	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.LocalPort)))
	defer listener.Close()
	if err != nil {
		panic(err)
	}
	fmt.Printf("Forwarding from %s -> %d\n", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.LocalPort)), remotePort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// TODO consider using something like https://github.com/hydrogen18/stoppableListener?
			// if !strings.Contains(strings.ToLower(err.Error()), "use of closed network connection") {
			fmt.Errorf("error accepting connection on port %d: %v", f.LocalPort, err)
			return
		}

		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, &serverURL)
		const PortForwardProtocolV1Name = "portforward.k8s.io"
		streamConn, _, err := dialer.Dial(PortForwardProtocolV1Name)
		if err != nil {
			conn.Close()
			f.logError(podName, "create stream connection failed", err)
			panic("create stream connection failed")
			return
		}

		go f.handleConnection(streamConn, remotePort, podName, conn)
	}
}

func (f Forwarder) logError(podName string, msg string, err error) {
	fmt.Println(podName, msg, err)
}

func (f Forwarder) handleConnection(streamConn httpstream.Connection, remotePort int32, podName string, conn net.Conn) {
	defer conn.Close()
	defer streamConn.Close()

	fmt.Printf("Handling connection for %d\n", f.LocalPort)

	headers := http.Header{}
	headers.Set(v1.StreamType, v1.StreamTypeError)
	headers.Set(v1.PortHeader, fmt.Sprintf("%d", remotePort))
	headers.Set(v1.PortForwardRequestIDHeader, strconv.Itoa(1234))
	errorStream, err := streamConn.CreateStream(headers)
	if err != nil {
		f.logError(podName, "error creating error stream for port %d -> %d: %v", err)
		return
	}
	// we're not writing to this stream
	errorStream.Close()

	errorChan := make(chan error)
	go func() {
		message, err := ioutil.ReadAll(errorStream)
		switch {
		case err != nil:
			errorChan <- fmt.Errorf("error reading from error stream for port %d -> %d: %v", f.LocalPort, remotePort, err)
		case len(message) > 0:
			errorChan <- fmt.Errorf("an error occurred forwarding %d -> %d: %v", f.LocalPort, remotePort, string(message))
		}
		close(errorChan)
	}()

	headers.Set(v1.StreamType, v1.StreamTypeData)
	dataStream, err := streamConn.CreateStream(headers)
	if err != nil {
		f.logError(podName, "create data stream fail", err)
		return
	}
	localError := make(chan struct{})
	remoteDone := make(chan struct{})

	// Copy from the remote side to the local port.
	go func() {
		// if _, err := io.Copy(conn, dataStream); err != nil && !strings.Contains(err.Error(), "use of closed network streamConn") {
		if _, err := io.Copy(conn, dataStream); err != nil {
			f.logError(podName, "error copying from remote stream to local streamConn: %v", err)
			return
		}

		fmt.Println(podName, "Close remote to local")
		// inform the select below that the remote copy is done
		close(remoteDone)
	}()

	go func() {
		// inform server we're not sending any more data after copy unblocks
		defer dataStream.Close()

		// Copy from the local port to the remote side.
		// if _, err := io.Copy(dataStream, conn); err != nil && !strings.Contains(err.Error(), "use of closed network streamConn") {
		if _, err := io.Copy(dataStream, conn); err != nil {
			fmt.Printf("error copying from local streamConn to remote stream: %v", err)
			// break out of the select below without waiting for the other copy to finish
			fmt.Println(podName + " Local error")
			close(localError)
		}
		fmt.Println(podName, "Close local to remote")
	}()

	// wait for either a local->remote error or for copying from remote->local to finish
	select {
	case <-remoteDone:
	case <-localError:
	}

	err = <-errorChan
	if err != nil {
		f.logError(podName, "error channel: ", err)
	}
	fmt.Println(podName + " END")
}
