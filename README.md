# CRD vs APIService — Code samples

> Code created for the blog article **"CRD ou APIService : comment étendre l'API Kubernetes ?"**
> published on [une-tasse-de.cafe](https://une-tasse-de.cafe)

This repository contains the example code used to illustrate the difference between **CRDs** and **APIServices** as two ways to extend the Kubernetes API.

---

## Structure

```
.
├── cafe-apiservice/   # Custom API server exposed via an APIService
└── kubectl-brew/      # kubectl plugin that calls the brew sub-resource
```

### `cafe-apiservice`

A minimal Go HTTP server that implements a Kubernetes-compatible API for a custom `Cafe` resource under the group `une-tasse-de.cafe/v1alpha1`.

It exposes:
- `GET/POST /apis/une-tasse-de.cafe/v1alpha1/namespaces/{ns}/cafes` — list & create cafes
- `GET/PUT/DELETE /apis/une-tasse-de.cafe/v1alpha1/namespaces/{ns}/cafes/{name}` — get, update, delete
- `POST .../cafes/{name}/brew` — custom sub-resource action

The server generates a self-signed TLS certificate at startup and is registered with Kubernetes via an `APIService` object (see `deploy.yaml`).

**Deploy:**

```bash
# Build & push to ttl.sh (ephemeral registry, no account needed)
docker build -t ttl.sh/cafe-apiservice:1h ./cafe-apiservice
docker push ttl.sh/cafe-apiservice:1h

# Deploy to the cluster
kubectl apply -f cafe-apiservice/deploy.yaml
```

**Create a Cafe resource:**

```yaml
# espresso.yaml
apiVersion: une-tasse-de.cafe/v1alpha1
kind: Cafe
metadata:
  name: espresso-du-matin
  namespace: default
spec:
  type: espresso
  temperature: 90
```

```bash
kubectl apply -f espresso.yaml
```

**Example output:**

```console
$ kubectl get cafes
NAME                      TYPE         TEMPERATURE   READY   LAST BREWED            AGE
cappuccino-du-dimanche    cappuccino   70            true    2026-03-26T08:01:00Z   5m
espresso-du-matin         espresso     90            true    2026-03-26T08:00:00Z   5m

$ kubectl get cafe espresso-du-matin -o yaml
apiVersion: une-tasse-de.cafe/v1alpha1
kind: Cafe
metadata:
  creationTimestamp: "2026-03-26T08:00:00Z"
  name: espresso-du-matin
  namespace: default
spec:
  temperature: 90
  type: espresso
status:
  lastBrewed: "2026-03-26T08:00:00Z"
  ready: true
```

### `kubectl-brew`

A kubectl plugin written in Go that triggers the `brew` sub-resource on a `Cafe` object.

**Install:**

```bash
go build -o kubectl-brew ./kubectl-brew
mv kubectl-brew /usr/local/bin/
```

**Usage:**

```console
$ kubectl brew espresso-du-matin
☕  Brewing espresso-du-matin (namespace: default)...
✓  espresso-du-matin est prêt ! (espresso, 90°C) — préparé à 08:05:42 UTC

$ kubectl brew cappuccino-du-dimanche -n production
☕  Brewing cappuccino-du-dimanche (namespace: production)...
✓  cappuccino-du-dimanche est prêt ! (cappuccino, 70°C) — préparé à 08:06:01 UTC
```

---

## Requirements

- Go 1.22+
- A running Kubernetes cluster (local with `kind` or `minikube` works fine)
- `kubectl` configured to talk to the cluster
- Docker (to build and push the image)

---

## Related article

The full article explaining how APIServices work, how they differ from CRDs, and when to choose one over the other is available at:

**[une-tasse-de.cafe](https://une-tasse-de.cafe)**
