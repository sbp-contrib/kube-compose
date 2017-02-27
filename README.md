## kube-compose
kube-wait is a small utility that provides docker-compose-like functionality for Kubernetes

### Usage

```
kube-compose NAMESPACE
```

Given a namespace, kube-compose will watch all pods in that namespace.
It will stream the log of pods with the label `log: "yes"` set.
If the label `containerPort` and `hostPort` are set, it will forward `hostPort`
on the host to `containerPort` in the container.

Example:

```
spec:
  template:
    metadata:
      labels:
        log: "yes"
        hostPort: "8061"
        containerPort: "8000"
```
