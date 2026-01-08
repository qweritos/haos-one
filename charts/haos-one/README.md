# haos-one Helm Chart

Run HAOS in a single Kubernetes pod (StatefulSet).

## Install

```
helm install haos-one oci://registry.andrey.wtf/helm/haos-one
```

## Values

| Key | Description | Default |
| --- | ----------- | ------- |
| `replicaCount` | Number of pods | `1` |
| `image.repository` | Container image repository | `qweritos/haos-one` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `nameOverride` | Override chart name | empty |
| `fullnameOverride` | Override release name | empty |
| `hostNetwork` | Enable host networking | `false` |
| `service.enabled` | Create a Service | `true` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `8123` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | empty |
| `ingress.annotations` | Ingress annotations | `{}` |
| `ingress.hosts` | Ingress hosts and paths | `[{host: haos.local, paths: [{path: /, pathType: Prefix}]}]` |
| `ingress.tls` | Ingress TLS config | `[]` |
| `serviceMonitor.enabled` | Enable ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |
| `serviceMonitor.path` | Metrics path | `/api/prometheus` |
| `serviceMonitor.scheme` | Metrics scheme | `http` |
| `serviceMonitor.honorLabels` | Honor labels from target | `false` |
| `serviceMonitor.labels` | ServiceMonitor labels | `{release: kube-prometheus-stack}` |
| `persistence.enabled` | Mount `/mnt/data` | `false` |
| `persistence.mountPath` | Data mount path | `/mnt/data` |
| `persistence.accessMode` | PVC access mode | `ReadWriteOnce` |
| `persistence.hostPath` | Use hostPath instead of PVC | empty |
| `persistence.existingClaim` | Use an existing PVC | empty |
| `persistence.size` | PVC size | `10Gi` |
| `persistence.storageClass` | PVC storage class | empty |
| `securityContext.privileged` | Run privileged container | `true` |
| `apparmor.unconfined` | Apply unconfined AppArmor profile | `true` |
| `podAnnotations` | Pod annotations | `{}` |
| `resources` | Resource requests/limits | `{}` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Pod affinity rules | `{}` |

## Example

```
helm install haos-one oci://registry.andrey.wtf/helm/haos-one \
  --set hostNetwork=true \
  --set persistence.hostPath=/var/lib/haos-one
```
