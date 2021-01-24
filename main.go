package main

import (
	"flag"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/util/homedir"
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
	var fileName string
	flag.StringVar(&fileName, "f", "", "YAML file to parse.")
	var shouldMonitor bool
	flag.BoolVar(&shouldMonitor, "m", false, "Should monitor memory and goroutine or not.")
	flag.Parse()

	if shouldMonitor {
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
	}

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
