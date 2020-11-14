# kube-porward

Auto reconnect Kubernetes port forward

### Usage
1. Download from [the latest release](https://github.com/scottxxx666/kube-porward/releases/download/v0.0.1-alpha/kube-porward) or build from source
1. Create your config yaml
   ```yaml
   ports:
   # - <local port>:<namespace>:<service name>:<service port>
     - 3000:default:service:8080
   ```
1. ```./kube-porward -f config.yml```