# kube-porward

Kube-porward is a tool for port forwarding to Kubernetes services.
- Auto reconnect
- Port forward to multiple services at the same time

### Usage
#### Build from source
1. ```git clone https://github.com/scottxxx666/kube-porward.git```
1. ```cd kube-porward```
1. Create your config yaml
   ```yaml
   ports:
   # - <local port>:<namespace>:<service name>:<service port>
     - 3000:default:service:8080
   ```
1. ```go run main.go -f config.yml```

#### Direct download
1. Download from [the latest release](https://github.com/scottxxx666/kube-porward/releases/latest)
1. ```chmod +x kube-porward```
1. ```sudo xattr -dr com.apple.quarantine ./kube-porward```
1. Create your config yaml
   ```yaml
   ports:
   # - <local port>:<namespace>:<service name>:<service port>
     - 3000:default:service:8080
   ```
1. ```./kube-porward -f config.yml```

### Todo
- websocket