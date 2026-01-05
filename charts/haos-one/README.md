# haos-one Helm Chart

Run HAOS in a single Kubernetes pod (StatefulSet).

## Install

```
helm install haos-one ./charts/haos-one
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
| `persistence.enabled` | Mount `/mnt/data` | `true` |
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
helm install haos-one ./charts/haos-one \
  --set hostNetwork=true \
  --set persistence.hostPath=/var/lib/haos-one
```
