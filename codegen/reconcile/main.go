/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// The following directive is necessary to make the package coherent:

package main

import (
	"bytes"
	"fmt"
	"go/format"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

func main() {
	data := struct {
		Resources []reconcileFunctionData
	}{
		Resources: []reconcileFunctionData{
			{
				ResourceName:       "Namespace",
				ImportAlias:        "corev1",
				ResourceImportPath: "k8s.io/api/core/v1",
			},
			{
				ResourceName:       "Service",
				ImportAlias:        "corev1",
				ResourceImportPath: "k8s.io/api/core/v1",
			},
			{
				ResourceName: "Secret",
				ImportAlias:  "corev1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName: "ConfigMap",
				ImportAlias:  "corev1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName: "ServiceAccount",
				ImportAlias:  "corev1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName:       "Endpoints",
				ResourceNamePlural: "Endpoints",
				ImportAlias:        "corev1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName:       "EndpointSlice",
				ImportAlias:        "discovery",
				ResourceImportPath: "k8s.io/api/discovery/v1",
			},
			{
				ResourceName:       "StatefulSet",
				ImportAlias:        "appsv1",
				ResourceImportPath: "k8s.io/api/apps/v1",
				DefaultingFunc:     "DefaultStatefulSet",
			},
			{
				ResourceName: "Deployment",
				ImportAlias:  "appsv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
				DefaultingFunc: "DefaultDeployment",
			},
			{
				ResourceName: "DaemonSet",
				ImportAlias:  "appsv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
				DefaultingFunc: "DefaultDaemonSet",
			},
			{
				ResourceName:       "PodDisruptionBudget",
				ImportAlias:        "policyv1beta1",
				ResourceImportPath: "k8s.io/api/policy/v1beta1",
				RequiresRecreate:   true,
			},
			{
				ResourceName:       "VerticalPodAutoscaler",
				ImportAlias:        "autoscalingv1",
				ResourceImportPath: "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1",
			},
			{
				ResourceName:       "ClusterRoleBinding",
				ImportAlias:        "rbacv1",
				ResourceImportPath: "k8s.io/api/rbac/v1",
			},
			{
				ResourceName: "ClusterRole",
				ImportAlias:  "rbacv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName: "Role",
				ImportAlias:  "rbacv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName: "RoleBinding",
				ImportAlias:  "rbacv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName:       "CustomResourceDefinition",
				ImportAlias:        "apiextensionsv1",
				ResourceImportPath: "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1",
			},
			{
				ResourceName:       "CronJob",
				ImportAlias:        "batchv1beta1",
				ResourceImportPath: "k8s.io/api/batch/v1beta1",
				DefaultingFunc:     "DefaultCronJob",
			},
			{
				ResourceName:       "MutatingWebhookConfiguration",
				ImportAlias:        "admissionregistrationv1",
				ResourceImportPath: "k8s.io/api/admissionregistration/v1",
			},
			{
				ResourceName: "ValidatingWebhookConfiguration",
				ImportAlias:  "admissionregistrationv1",
				// Don't specify ResourceImportPath so this block does not create a new import line in the generated code
			},
			{
				ResourceName:       "APIService",
				ImportAlias:        "apiregistrationv1",
				ResourceImportPath: "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1",
			},
			{
				ResourceName:       "Ingress",
				ResourceNamePlural: "Ingresses",
				ImportAlias:        "networkingv1",
				ResourceImportPath: "k8s.io/api/networking/v1",
			},
			{
				ResourceName:       "KubermaticConfiguration",
				ImportAlias:        "kubermaticv1",
				ResourceImportPath: "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1",
			},
			{
				ResourceName:       "Seed",
				ImportAlias:        "kubermaticv1",
				ResourceImportPath: "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1",
			},
			{
				ResourceName:       "EtcdBackupConfig",
				ImportAlias:        "kubermaticv1",
				ResourceImportPath: "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1",
			},
			{
				ResourceName:       "ConstraintTemplate",
				ImportAlias:        "gatekeeperv1",
				ResourceImportPath: "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1",
			},
			{
				ResourceName:     "ConstraintTemplate",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:     "Project",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:     "UserProjectBinding",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:     "Constraint",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:     "User",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:     "ClusterTemplate",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:       "NetworkPolicy",
				ResourceNamePlural: "NetworkPolicies",
				ImportAlias:        "networkingv1",
				ResourceImportPath: "k8s.io/api/networking/v1",
			},
			{
				ResourceName:     "RuleGroup",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:       "ApplicationDefinition",
				ImportAlias:        "appskubermaticv1",
				ResourceImportPath: "k8c.io/kubermatic/v2/pkg/apis/apps.kubermatic/v1",
				APIVersionPrefix:   "AppsKubermaticV1",
			},
			{
				ResourceName:       "VirtualMachineInstancePreset",
				ImportAlias:        "kubevirtv1",
				ResourceImportPath: "kubevirt.io/api/core/v1",
				APIVersionPrefix:   "KubeVirtV1",
			},
			{
				ResourceName:     "Preset",
				ImportAlias:      "kubermaticv1",
				APIVersionPrefix: "KubermaticV1",
			},
			{
				ResourceName:       "DataVolume",
				ImportAlias:        "cdiv1beta1",
				ResourceImportPath: "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1",
				APIVersionPrefix:   "CDIv1beta1",
			},
		},
	}

	buf := &bytes.Buffer{}
	if err := reconcileAllTemplate.Execute(buf, data); err != nil {
		log.Fatal(err)
	}

	fmtB, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("zz_generated_reconcile.go", fmtB, 0644); err != nil {
		log.Fatal(err)
	}
}

func lowercaseFirst(str string) string {
	return strings.ToLower(string(str[0])) + str[1:]
}

var (
	reconcileAllTplFuncs = map[string]interface{}{
		"namedReconcileFunc": namedReconcileFunc,
	}
	reconcileAllTemplate = template.Must(template.New("").Funcs(reconcileAllTplFuncs).Funcs(sprig.TxtFuncMap()).Parse(`// This file is generated. DO NOT EDIT.
package reconciling

import (
	"fmt"
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
{{ range .Resources }}
{{- if .ResourceImportPath }}
	{{ .ImportAlias }} "{{ .ResourceImportPath }}"
{{- end }}
{{- end }}
)

{{ range .Resources }}
{{ namedReconcileFunc .ResourceName .ImportAlias .DefaultingFunc .RequiresRecreate .ResourceNamePlural .APIVersionPrefix}}
{{- end }}

`))
)

type reconcileFunctionData struct {
	ResourceName       string
	ResourceNamePlural string
	ResourceImportPath string
	ImportAlias        string
	// Optional: A defaulting func for the given object type
	// Must be defined inside the resources package
	DefaultingFunc string
	// Whether the resource must be recreated instead of updated. Required
	// e.G. for PDBs
	RequiresRecreate bool
	// Optional: adds an api version prefix to the generated functions to avoid duplication when different resources
	// have the same ResourceName
	APIVersionPrefix string
}

func namedReconcileFunc(resourceName, importAlias, defaultingFunc string, requiresRecreate bool, plural, apiVersionPrefix string) (string, error) {
	if len(plural) == 0 {
		plural = fmt.Sprintf("%ss", resourceName)
	}

	b := &bytes.Buffer{}
	err := namedReconcileFunctionTemplate.Execute(b, struct {
		ResourceName       string
		ResourceNamePlural string
		ImportAlias        string
		DefaultingFunc     string
		RequiresRecreate   bool
		APIVersionPrefix   string
	}{
		ResourceName:       resourceName,
		ResourceNamePlural: plural,
		ImportAlias:        importAlias,
		DefaultingFunc:     defaultingFunc,
		RequiresRecreate:   requiresRecreate,
		APIVersionPrefix:   apiVersionPrefix,
	})

	if err != nil {
		return "", err
	}

	return b.String(), nil
}

var (
	reconcileFunctionTplFuncs = map[string]interface{}{
		"lowercaseFirst": lowercaseFirst,
	}
)

var namedReconcileFunctionTemplate = template.Must(template.New("").Funcs(reconcileFunctionTplFuncs).Parse(`// {{ .APIVersionPrefix }}{{ .ResourceName }}Creator defines an interface to create/update {{ .ResourceName }}s
type {{ .APIVersionPrefix }}{{ .ResourceName }}Creator = func(existing *{{ .ImportAlias }}.{{ .ResourceName }}) (*{{ .ImportAlias }}.{{ .ResourceName }}, error)

// Named{{ .APIVersionPrefix }}{{ .ResourceName }}CreatorGetter returns the name of the resource and the corresponding creator function
type Named{{ .APIVersionPrefix }}{{ .ResourceName }}CreatorGetter = func() (name string, create {{ .APIVersionPrefix }}{{ .ResourceName }}Creator)

// {{ .APIVersionPrefix }}{{ .ResourceName }}ObjectWrapper adds a wrapper so the {{ .APIVersionPrefix }}{{ .ResourceName }}Creator matches ObjectCreator.
// This is needed as Go does not support function interface matching.
func {{ .APIVersionPrefix }}{{ .ResourceName }}ObjectWrapper(create {{ .APIVersionPrefix }}{{ .ResourceName }}Creator) ObjectCreator {
	return func(existing ctrlruntimeclient.Object) (ctrlruntimeclient.Object, error) {
		if existing != nil {
			return create(existing.(*{{ .ImportAlias }}.{{ .ResourceName }}))
		}
		return create(&{{ .ImportAlias }}.{{ .ResourceName }}{})
	}
}

// Reconcile{{ .APIVersionPrefix }}{{ .ResourceNamePlural }} will create and update the {{ .APIVersionPrefix }}{{ .ResourceNamePlural }} coming from the passed {{ .APIVersionPrefix }}{{ .ResourceName }}Creator slice
func Reconcile{{ .APIVersionPrefix }}{{ .ResourceNamePlural }}(ctx context.Context, namedGetters []Named{{ .APIVersionPrefix }}{{ .ResourceName }}CreatorGetter, namespace string, client ctrlruntimeclient.Client, objectModifiers ...ObjectModifier) error {
	for _, get := range namedGetters {
		name, create := get()
{{- if .DefaultingFunc }}
		create = {{ .DefaultingFunc }}(create)
{{- end }}
		createObject := {{ .APIVersionPrefix }}{{ .ResourceName }}ObjectWrapper(create)
		createObject = createWithNamespace(createObject, namespace)
		createObject = createWithName(createObject, name)

		for _, objectModifier := range objectModifiers {
			createObject = objectModifier(createObject)
		}

		if err := EnsureNamedObject(ctx, types.NamespacedName{Namespace: namespace, Name: name}, createObject, client, &{{ .ImportAlias }}.{{ .ResourceName }}{}, {{ .RequiresRecreate}}); err != nil {
			return fmt.Errorf("failed to ensure {{ .ResourceName }} %s/%s: %w", namespace, name, err)
		}
	}

	return nil
}

`))
