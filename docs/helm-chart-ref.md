# Elemental Lifecycle Manager Helm Chart Reference

The Elemental Lifecycle Manager (LCM) is distributed as two Helm charts:

- `elemental-lifecycle-manager-crds` - installs the CRDs managed by LCM, such as the `Release` resource.
- `elemental-lifecycle-manager` - installs the LCM controller.

## Install

This section shows how to deploy the LCM charts with their default configuration. To customize the installation, see the [chart options](#elemental-lifecycle-manager-chart-options) section.

> IMPORTANT: To ensure LCM installs and upgrades correctly, **always** install the `elemental-lifecycle-manager-crds` chart before the `elemental-lifecycle-manager` chart.

### From Source

> NOTE: Installing charts from source is intended for development use only. For reproducible installations, use the published OCI chart image.

1. Install `elemental-lifecycle-manager-crds`:
   ```sh
   helm install elemental-lifecycle-manager-crds \
     ./charts/elemental-lifecycle-manager-crds \
     --namespace elemental-system \
     --create-namespace
   ```

1. Install `elemental-lifecycle-manager`:
   > NOTE: When installing from source, **always** make sure that the chart is referring to an existing LCM image.
   
   ```sh
   helm install elemental-lifecycle-manager \
    ./charts/elemental-lifecycle-manager \
    --namespace elemental-system 
   ```

### From OCI image

1. Install `elemental-lifecycle-manager-crds`:
   ```sh
   helm install elemental-lifecycle-manager-crds \
    oci://registry.suse.com/elemental/charts/elemental-lifecycle-manager-crds \
    --namespace elemental-system \
    --create-namespace \
    --version 0.1.0
   ```

1. Install `elemental-lifecycle-manager`:
   ```sh
   helm install elemental-lifecycle-manager \
    oci://registry.suse.com/elemental/charts/elemental-lifecycle-manager \
    --version 0.1.0 \
    --namespace elemental-system
   ```

## `elemental-lifecycle-manager` Chart Options

Since the `elemental-lifecycle-manager-crds` chart does not expose any configuration options, this document focuses on the `elemental-lifecycle-manager` chart.

| Option                                       | Default Value                                                   | Description |
|----------------------------------------------|-----------------------------------------------------------------|-------------|
| `affinity`                                   | {}                                                              | `map` - Adds affinity rules for scheduling the LCM Pod. |
| `enableHTTP2`                                | false                                                           | `bool` - Overrides whether HTTP/2 is enabled for the LCM webhook and metrics servers. |
| `extraArgs`                                  | []                                                              | `list` - Adds command-line arguments to the LCM container. |
| `extraEnv`                                   | []                                                              | `list` - Adds environment variables to the LCM container. |
| `extraVolumeMounts`                          | []                                                              | `list` - Adds volume mounts to the LCM container. |
| `extraVolumes`                               | []                                                              | `list` - Adds volumes to the LCM Pod. |
| `fullnameOverride`                           | ""                                                              | `string` - Sets a custom fully qualified LCM chart release name. |
| `healthProbe.liveness.initialDelaySeconds`   | 15                                                              | `int` - Overrides the default initial delay, in seconds, before the LCM liveness probe starts. |
| `healthProbe.liveness.periodSeconds`         | 20                                                              | `int` - Overrides the default interval, in seconds, between LCM liveness probe checks. |
| `healthProbe.readiness.initialDelaySeconds`  | 5                                                               | `int` - Overrides the default initial delay, in seconds, before the LCM readiness probe starts. |
| `healthProbe.readiness.periodSeconds`        | 10                                                              | `int` - Overrides the default interval, in seconds, between LCM readiness probe checks. |
| `image.pullPolicy`                           | "IfNotPresent"                                                  | `string` - Overrides the default pull policy for the LCM image. |
| `image.repository`                           | registry.suse.com/elemental/elemental-lifecycle-manager           | `string` - Overrides the default LCM image repository. |
| `image.tag`                                  | ""                                                              | `string` - Overrides the LCM container image tag. Defaults to .Chart.appVersion. |
| `imagePullSecrets`                           | []                                                              | `list` - Adds image pull secrets to the LCM Pod. |
| `metrics.cert.createDefault`                 | true                                                            | `bool` - Overrides whether LCM uses cert-manager to generate a self-signed serving certificate for the metrics server when metrics are enabled and secure metrics are used. |
| `metrics.cert.existingSecret`                | ""                                                              | `string` - Points to an existing Secret containing `tls.crt` and `tls.key` for the LCM metrics serving certificate. Applicable only when `metrics.cert.createDefault=false`. |
| `metrics.enabled`                            | false                                                           | `bool` - Overrides whether the LCM metrics endpoint is enabled. |
| `metrics.secure`                             | false                                                           | `bool` - Overrides whether the LCM metrics endpoint is served over HTTPS when metrics are enabled. |
| `metrics.service.annotations`                | {}                                                              | `map` - Adds annotations to the LCM metrics Service. |
| `metrics.service.name`                       | ""                                                              | `string` - Sets a custom LCM metrics Service name. |
| `metrics.service.port`                       | 8080                                                            | `int` - Overrides the default port exposed for the LCM metrics Service. |
| `metrics.service.type`                       | "ClusterIP"                                                     | `string` - Overrides the default service type for the LCM metrics Service. |
| `nameOverride`                               | ""                                                              | `string` - Sets the base name used in LCM resource names. |
| `nodeSelector`                               | {}                                                              | `map` - Adds node selector constraints for scheduling the LCM Pod. |
| `podAnnotations`                             | {}                                                              | `map` - Adds annotations to the LCM Pod. |
| `podLabels`                                  | {}                                                              | `map` - Adds labels to the LCM Pod. |
| `podSecurityContext.runAsNonRoot`            | true                                                            | `bool` - Overrides whether the LCM Pod runs as a non-root user. |
| `podSecurityContext.seccompProfile.type`     | "RuntimeDefault"                                                | `string` - Overrides the seccomp profile type applied to the LCM Pod. |
| `replicaCount`                               | 1                                                               | `int` - Overrides the default number of LCM Deployment replicas. |
| `resources`                                  | {}                                                              | `map` - Sets resource requests and limits for the LCM container. |
| `securityContext.allowPrivilegeEscalation`   | false                                                           | `bool` - Overrides whether privilege escalation is allowed in the LCM container. |
| `securityContext.capabilities.drop`          | [ALL]                                                           | `list` - Overrides the Linux capabilities dropped from the LCM container. |
| `securityContext.readOnlyRootFilesystem`     | true                                                            | `bool` - Overrides whether the LCM container uses a read-only root filesystem. |
| `serviceAccount.annotations`                 | {}                                                              | `map` - Adds annotations to the LCM ServiceAccount. |
| `serviceAccount.automount`                   | true                                                            | `bool` - Overrides whether the service account token is automatically mounted into the LCM Pod. |
| `serviceAccount.create`                      | true                                                            | `bool` - Overrides whether the chart creates a ServiceAccount for LCM. |
| `serviceAccount.name`                        | ""                                                              | `string` - Sets the ServiceAccount name for LCM. When empty, LCM uses the generated chart fullname if service account creation is enabled. |
| `tolerations`                                | []                                                              | `list` - Adds tolerations for scheduling the LCM Pod. |
| `webhook.cert.caBundle`                      | ""                                                              | `string` - Sets the CA bundle used by the API server to verify the LCM webhook serving certificate. Applicable only when `webhook.cert.createDefault=false`. |
| `webhook.cert.createDefault`                 | true                                                            | `bool` - Overrides whether LCM uses cert-manager to generate a self-signed serving certificate for the webhook. |
| `webhook.cert.existingSecret`                | ""                                                              | `string` - Points to an existing Secret containing `tls.crt` and `tls.key` for the LCM webhook serving certificate. Applicable only when `webhook.cert.createDefault=false`. |
| `webhook.enabled`                            | true                                                            | `bool` - Overrides whether the LCM validation webhook and related resources are enabled. |
| `webhook.failurePolicy`                      | "Fail"                                                          | `string` - Overrides the default failure policy used by the LCM validating webhook configuration. |
| `webhook.service.annotations`                | {}                                                              | `map` - Adds annotations to the LCM webhook Service. |
| `webhook.service.name`                       | ""                                                              | `string` - Sets a custom LCM webhook Service name. |
| `webhook.service.port`                       | 443                                                             | `int` - Overrides the default port exposed by the LCM webhook Service. |
| `webhook.service.targetPort`                 | 9443                                                            | `int` - Overrides the default target port used by the LCM webhook Service. |

### Defining a custom webhook and metrics service certificate

By default, the LCM chart uses [cert-manager](https://cert-manager.io) to generate self-signed serving certificates for the `webhook` and `metrics` services.

If using `cert-manager` is not desirable, the chart offers a way to provide custom certificates through [TLS Secrets](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets). A single TLS Secret can be shared by both services, provided the certificate is valid for both Service DNS names.

The following example shows how to configure one custom certificate for both the `webhook` and `metrics` services.

#### 1. Choose the LCM release and namespace

Decide the Helm release name and namespace before creating the certificate. Kubernetes Services are reachable through these DNS names:

```text
<service-name>.<namespace>.svc
<service-name>.<namespace>.svc.cluster.local
```

By default, the webhook and metrics service names are prefixed with the generated chart fullname, which is derived from the Helm release name and chart name unless explicitly overridden.

So, for a Helm release with name `elemental-lifecycle-manager` in namespace `elemental-system`, webhook and metrics service DNS names will be:

| Service | Short DNS name                                                     | Full DNS name                                                                    |
|---------|--------------------------------------------------------------------|----------------------------------------------------------------------------------|
| Webhook | `elemental-lifecycle-manager-webhook-service.elemental-system.svc` | `elemental-lifecycle-manager-webhook-service.elemental-system.svc.cluster.local` |
| Metrics | `elemental-lifecycle-manager-metrics-service.elemental-system.svc` | `elemental-lifecycle-manager-metrics-service.elemental-system.svc.cluster.local` |

> NOTE: If you set `fullnameOverride`, `webhook.service.name`, or `metrics.service.name`, use the resulting Service names instead.

#### 2. Create a certificate for both services

Create a local CA and a serving certificate whose Subject Alternative Names include both the webhook and metrics Service DNS names:

```sh
cat > san.cnf <<'EOF'
[server_cert]
subjectAltName = @alt_names

[alt_names]
DNS.1 = elemental-lifecycle-manager-webhook-service.elemental-system.svc
DNS.2 = elemental-lifecycle-manager-webhook-service.elemental-system.svc.cluster.local
DNS.3 = elemental-lifecycle-manager-metrics-service.elemental-system.svc
DNS.4 = elemental-lifecycle-manager-metrics-service.elemental-system.svc.cluster.local
EOF

openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
  -subj "/CN=lcm-custom-ca" \
  -out ca.crt

openssl genrsa -out tls.key 2048
openssl req -new -key tls.key \
  -subj "/CN=elemental-lifecycle-manager" \
  -out tls.csr

openssl x509 -req -in tls.csr \
  -CA ca.crt \
  -CAkey ca.key \
  -CAcreateserial \
  -out tls.crt \
  -days 365 \
  -sha256 \
  -extensions server_cert \
  -extfile san.cnf
```

If you use a different release name, namespace, `fullnameOverride`, `webhook.service.name`, or `metrics.service.name`, update the DNS names in `san.cnf` before creating the certificate.

#### 3. Create the TLS Secret

Create the TLS Secret in the same namespace where LCM will be installed:

```sh
# Ensure namespace is created
kubectl create namespace elemental-system

# Actual secret creation
kubectl create secret tls lcm-serving-cert \
  --namespace elemental-system \
  --cert ./tls.crt \
  --key ./tls.key
```

#### 4. Install LCM

Install the CRDs chart first:

```sh
helm install elemental-lifecycle-manager-crds \
    oci://registry.suse.com/elemental/charts/elemental-lifecycle-manager-crds \
    --namespace elemental-system \
    --create-namespace \
    --version 0.1.0
```

Setup a custom values file that overrides the default certificate configuration for the `webhook` and `metrics` services. 

> NOTE: The `webhook.cert.caBundle` value is set from `ca.crt` so the Kubernetes API server can verify the webhook certificate.

```sh
CA_BUNDLE="$(base64 < ./ca.crt | tr -d '\n')"

cat > custom-certs-values.yaml <<EOF
webhook:
  cert:
    createDefault: false
    existingSecret: lcm-serving-cert
    caBundle: ${CA_BUNDLE}

metrics:
  enabled: true
  secure: true
  cert:
    createDefault: false
    existingSecret: lcm-serving-cert
EOF
```

Then install the LCM chart with the custom values file:

```sh
helm install elemental-lifecycle-manager \
    oci://registry.suse.com/elemental/charts/elemental-lifecycle-manager \
    --version 0.1.0 \
    --namespace elemental-system \
    --values custom-certs-values.yaml
```

With this configuration, the chart mounts `lcm-serving-cert` for both the `webhook` and secure `metrics` endpoints, while also ensuring that the webhook certificate is verified by the Kubernetes API server using `webhook.cert.caBundle`. The metrics certificate is served by LCM on the metrics endpoint; metrics clients must trust the CA that signed the certificate.
