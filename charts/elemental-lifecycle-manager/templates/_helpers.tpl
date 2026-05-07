{{/*
Expands the name of the chart.
*/}}
{{- define "elemental-lifecycle-manager.name" -}}
{{- default "elemental-lifecycle-manager" .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Expands the fully qualified application name.
*/}}
{{- define "elemental-lifecycle-manager.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "elemental-lifecycle-manager.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Expands the name of the controller's service account.
*/}}
{{- define "elemental-lifecycle-manager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "elemental-lifecycle-manager.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Expands the name of the controller's webhook service.
*/}}
{{- define "elemental-lifecycle-manager.webhookServiceName" -}}
{{- default (printf "%s-webhook-service" (include "elemental-lifecycle-manager.fullname" .)) .Values.webhook.service.name -}}
{{- end -}}

{{/*
Expands the name of the controller's metrics service.
*/}}
{{- define "elemental-lifecycle-manager.metricsServiceName" -}}
{{- default (printf "%s-metrics-service" (include "elemental-lifecycle-manager.fullname" .)) .Values.metrics.service.name -}}
{{- end -}}

{{/*
Expands the name of the ClusterRole that allows clients to read secure metrics.
*/}}
{{- define "elemental-lifecycle-manager.metricsReaderRoleName" -}}
{{- printf "%s-metrics-reader" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the controller's manager ClusterRole.
*/}}
{{- define "elemental-lifecycle-manager.managerRoleName" -}}
{{- printf "%s-manager-role" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the controller's manager ClusterRoleBinding.
*/}}
{{- define "elemental-lifecycle-manager.managerRoleBindingName" -}}
{{- printf "%s-manager-rolebinding" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Returns true when leader election should be enabled for the controller.
*/}}
{{- define "elemental-lifecycle-manager.leaderElectionEnabled" -}}
{{- if gt (int .Values.replicaCount) 1 -}}
true
{{- end -}}
{{- end -}}

{{/*
Expands the name of the RBAC role related to leader election.
*/}}
{{- define "elemental-lifecycle-manager.leaderRoleName" -}}
{{- printf "%s-leader-role" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the RBAC role binding related to leader election.
*/}}
{{- define "elemental-lifecycle-manager.leaderRoleBindingName" -}}
{{- printf "%s-leader-rolebinding" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Returns true when the chart should create and use the default cert-manager
webhook certificate instead of an existing user-provided secret.
*/}}
{{- define "elemental-lifecycle-manager.useDefaultWebhookCert" -}}
{{- if and .Values.webhook.enabled .Values.webhook.cert.createDefault -}}
{{- if .Values.webhook.cert.existingSecret -}}
{{- fail "webhook.cert.existingSecret must be empty when webhook.cert.createDefault=true" -}}
{{- end -}}
{{- if .Values.webhook.cert.caBundle -}}
{{- fail "webhook.cert.caBundle must be empty when webhook.cert.createDefault=true" -}}
{{- end -}}
true
{{- end -}}
{{- end -}}

{{/*
Returns true when secure metrics should use a chart-created cert-manager
certificate instead of an existing user-provided secret.
*/}}
{{- define "elemental-lifecycle-manager.useDefaultMetricsCert" -}}
{{- if and .Values.metrics.enabled .Values.metrics.secure .Values.metrics.cert.createDefault -}}
{{- if .Values.metrics.cert.existingSecret -}}
{{- fail "metrics.cert.existingSecret must be empty when metrics.cert.createDefault=true" -}}
{{- end -}}
true
{{- end -}}
{{- end -}}

{{/*
Expands the name of the cert-manager Certificate for the webhook server.
*/}}
{{- define "elemental-lifecycle-manager.webhookCertificateName" -}}
{{- printf "%s-serving-cert" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the Secret containing the webhook serving certificate.
*/}}
{{- define "elemental-lifecycle-manager.webhookCertificateSecretName" -}}
{{- printf "%s-webhook-server-cert" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the self-signed cert-manager Issuer.
*/}}
{{- define "elemental-lifecycle-manager.certificateIssuerName" -}}
{{- printf "%s-selfsigned-issuer" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the cert-manager Certificate for the metrics server.
*/}}
{{- define "elemental-lifecycle-manager.metricsCertificateName" -}}
{{- printf "%s-metrics-certs" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the name of the Secret containing the metrics serving certificate.
*/}}
{{- define "elemental-lifecycle-manager.metricsCertificateSecretName" -}}
{{- printf "%s-metrics-server-cert" (include "elemental-lifecycle-manager.fullname" .) -}}
{{- end -}}

{{/*
Expands the metrics certificate Secret name, requires an existing secret when
metrics.cert.createDefault is false.
*/}}
{{- define "elemental-lifecycle-manager.metricsSecretName" -}}
{{- if include "elemental-lifecycle-manager.useDefaultMetricsCert" . -}}
{{- include "elemental-lifecycle-manager.metricsCertificateSecretName" . -}}
{{- else -}}
{{- required "metrics.cert.existingSecret is required when metrics.cert.createDefault=false" .Values.metrics.cert.existingSecret -}}
{{- end -}}
{{- end -}}

{{/*
Expands the webhook certificate Secret name, requires an existing secret when
webhook.cert.createDefault is false.
*/}}
{{- define "elemental-lifecycle-manager.webhookSecretName" -}}
{{- if include "elemental-lifecycle-manager.useDefaultWebhookCert" . -}}
{{- include "elemental-lifecycle-manager.webhookCertificateSecretName" . -}}
{{- else -}}
{{- required "webhook.cert.existingSecret is required when webhook.cert.createDefault=false" .Values.webhook.cert.existingSecret -}}
{{- end -}}
{{- end -}}

{{/*
Renders common labels shared by all chart resources.
*/}}
{{- define "elemental-lifecycle-manager.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
app.kubernetes.io/name: {{ include "elemental-lifecycle-manager.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
app.kubernetes.io/part-of: elemental-lifecycle-manager
{{- end -}}

{{/*
Renders selector labels shared by the Deployment pod template and Services.
*/}}
{{- define "elemental-lifecycle-manager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "elemental-lifecycle-manager.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end -}}
