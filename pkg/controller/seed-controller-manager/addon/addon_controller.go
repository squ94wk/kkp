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

package addon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strings"
	"time"

	"go.uber.org/zap"

	"k8c.io/kubermatic/v2/pkg/addon"
	addonutils "k8c.io/kubermatic/v2/pkg/addon"
	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	kubermaticv1helper "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1/helper"
	clusterclient "k8c.io/kubermatic/v2/pkg/cluster/client"
	kuberneteshelper "k8c.io/kubermatic/v2/pkg/kubernetes"
	"k8c.io/kubermatic/v2/pkg/resources"
	"k8c.io/kubermatic/v2/pkg/semver"
	"k8c.io/kubermatic/v2/pkg/util/kubectl"
	"k8c.io/kubermatic/v2/pkg/version/kubermatic"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/tools/record"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"
)

const (
	ControllerName = "kkp-addon-controller"

	addonLabelKey        = "kubermatic-addon"
	cleanupFinalizerName = "cleanup-manifests"
	addonEnsureLabelKey  = "addons.kubermatic.io/ensure"
)

// KubeconfigProvider provides functionality to get a clusters admin kubeconfig.
type KubeconfigProvider interface {
	GetAdminKubeconfig(ctx context.Context, c *kubermaticv1.Cluster) ([]byte, error)
	GetClient(ctx context.Context, c *kubermaticv1.Cluster, options ...clusterclient.ConfigOption) (ctrlruntimeclient.Client, error)
}

// Reconciler stores necessary components that are required to manage in-cluster Add-On's.
type Reconciler struct {
	ctrlruntimeclient.Client

	log                  *zap.SugaredLogger
	workerName           string
	addonEnforceInterval int
	addonVariables       map[string]interface{}
	kubernetesAddonDir   string
	overwriteRegistry    string
	recorder             record.EventRecorder
	KubeconfigProvider   KubeconfigProvider
	versions             kubermatic.Versions
}

// Add creates a new Addon controller that is responsible for
// managing in-cluster addons.
func Add(
	mgr manager.Manager,
	log *zap.SugaredLogger,
	numWorkers int,
	workerName string,
	addonEnforceInterval int,
	addonCtxVariables map[string]interface{},
	kubernetesAddonDir,
	overwriteRegistry string,
	kubeconfigProvider KubeconfigProvider,
	versions kubermatic.Versions,
) error {
	log = log.Named(ControllerName)
	client := mgr.GetClient()

	reconciler := &Reconciler{
		Client: client,

		log:                  log,
		addonVariables:       addonCtxVariables,
		addonEnforceInterval: addonEnforceInterval,
		kubernetesAddonDir:   kubernetesAddonDir,
		KubeconfigProvider:   kubeconfigProvider,
		workerName:           workerName,
		recorder:             mgr.GetEventRecorderFor(ControllerName),
		overwriteRegistry:    overwriteRegistry,
		versions:             versions,
	}

	ctrlOptions := controller.Options{
		Reconciler:              reconciler,
		MaxConcurrentReconciles: numWorkers,
	}
	c, err := controller.New(ControllerName, mgr, ctrlOptions)
	if err != nil {
		return err
	}

	enqueueClusterAddons := handler.EnqueueRequestsFromMapFunc(func(a ctrlruntimeclient.Object) []reconcile.Request {
		cluster := a.(*kubermaticv1.Cluster)
		if cluster.Status.NamespaceName == "" {
			return nil
		}

		addonList := &kubermaticv1.AddonList{}
		if err := client.List(context.Background(), addonList, ctrlruntimeclient.InNamespace(cluster.Status.NamespaceName)); err != nil {
			log.Errorw("Failed to get addons for cluster", zap.Error(err), "cluster", cluster.Name)
			return nil
		}
		var requests []reconcile.Request
		for _, addon := range addonList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: addon.Namespace, Name: addon.Name},
			})
		}
		return requests
	})

	// Only react to cluster update events when our condition changed, or when CNI config changed
	clusterPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj := e.ObjectOld.(*kubermaticv1.Cluster)
			newObj := e.ObjectNew.(*kubermaticv1.Cluster)
			oldCondition := oldObj.Status.Conditions[kubermaticv1.ClusterConditionAddonControllerReconcilingSuccess]
			newCondition := newObj.Status.Conditions[kubermaticv1.ClusterConditionAddonControllerReconcilingSuccess]
			if !reflect.DeepEqual(oldCondition, newCondition) {
				return true
			}
			if !reflect.DeepEqual(oldObj.Spec.CNIPlugin, newObj.Spec.CNIPlugin) {
				return true
			}
			return false
		},
	}
	if err := c.Watch(&source.Kind{Type: &kubermaticv1.Cluster{}}, enqueueClusterAddons, clusterPredicate); err != nil {
		return err
	}

	return c.Watch(&source.Kind{Type: &kubermaticv1.Addon{}}, &handler.EnqueueRequestForObject{})
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.log.With("request", request)
	log.Debug("Processing")

	addon := &kubermaticv1.Addon{}
	if err := r.Get(ctx, request.NamespacedName, addon); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	cluster := &kubermaticv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: addon.Spec.Cluster.Name}, cluster); err != nil {
		// If it's not a NotFound err, return it
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		// Remove the cleanup finalizer if the cluster is gone, as we can not delete the addons manifests
		// from the cluster anymore
		if err := r.removeCleanupFinalizer(ctx, log, addon); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to remove addon cleanup finalizer: %w", err)
		}

		return reconcile.Result{}, nil
	}

	if cluster.Status.Versions.ControlPlane == "" {
		log.Debug("Skipping because the cluster has no version status yet, skipping")
		return reconcile.Result{}, nil
	}

	log = r.log.With("cluster", cluster.Name, "addon", addon.Name)

	// Add a wrapping here so we can emit an event on error
	result, err := kubermaticv1helper.ClusterReconcileWrapper(
		ctx,
		r.Client,
		r.workerName,
		cluster,
		r.versions,
		kubermaticv1.ClusterConditionAddonControllerReconcilingSuccess,
		func() (*reconcile.Result, error) {
			return r.reconcile(ctx, log, addon, cluster)
		},
	)
	if err != nil {
		log.Errorw("Reconciling failed", zap.Error(err))
		r.recorder.Event(addon, corev1.EventTypeWarning, "ReconcilingError", err.Error())
		r.recorder.Eventf(cluster, corev1.EventTypeWarning, "ReconcilingError",
			"failed to reconcile Addon %q: %v", addon.Name, err)
	}
	if result == nil {
		// we check for this after the ClusterReconcileWrapper() call because otherwise the cluster would never reconcile since we always requeue
		result = &reconcile.Result{}
		if r.addonEnforceInterval != 0 { // addon enforce is enabled
			// All is well, requeue in addonEnforceInterval minutes. We do this to enforce default addons and prevent cluster admins from disabling them.
			result.RequeueAfter = time.Duration(r.addonEnforceInterval) * time.Minute
		}
	}
	return *result, err
}

func (r *Reconciler) reconcile(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) (*reconcile.Result, error) {
	if cluster.Status.ExtendedHealth.Apiserver != kubermaticv1.HealthStatusUp {
		log.Debug("API server is not running, trying again in 10 seconds")
		return &reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	reqeueAfter, err := r.ensureRequiredResourceTypesExist(ctx, log, addon, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to check if all required resources exist: %w", err)
	}
	if reqeueAfter != nil {
		return reqeueAfter, nil
	}

	if addon.DeletionTimestamp != nil {
		if err := r.cleanupManifests(ctx, log, addon, cluster); err != nil {
			return nil, fmt.Errorf("failed to delete manifests from cluster: %w", err)
		}
		if err := r.removeCleanupFinalizer(ctx, log, addon); err != nil {
			return nil, fmt.Errorf("failed to ensure that the cleanup finalizer got removed from the addon: %w", err)
		}
		return nil, nil
	}
	// This is true when the addon: 1) is fully deployed, 2) doesn't have a `addonEnsureLabelKey` set to true.
	// we do this to allow users to "edit/delete" resources deployed by unlabeled addons,
	// while we enfornce the labeled ones
	if addonResourcesCreated(addon) && !hasEnsureResourcesLabel(addon) {
		return nil, nil
	}

	// Reconciling
	if err := r.ensureIsInstalled(ctx, log, addon, cluster); err != nil {
		return nil, fmt.Errorf("failed to deploy the addon manifests into the cluster: %w", err)
	}
	if err := r.ensureFinalizerIsSet(ctx, addon); err != nil {
		return nil, fmt.Errorf("failed to ensure that the cleanup finalizer exists on the addon: %w", err)
	}
	if err := r.ensureResourcesCreatedConditionIsSet(ctx, addon); err != nil {
		return nil, fmt.Errorf("failed to set add ResourcesCreated Condition: %w", err)
	}
	return nil, nil
}

func (r *Reconciler) removeCleanupFinalizer(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon) error {
	return kuberneteshelper.TryRemoveFinalizer(ctx, r, addon, cleanupFinalizerName)
}

func (r *Reconciler) getAddonManifests(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) ([]addon.Manifest, error) {
	addonDir := r.kubernetesAddonDir
	clusterIP, err := resources.UserClusterDNSResolverIP(cluster)
	if err != nil {
		return nil, err
	}
	dnsResolverIP := clusterIP
	if cluster.Spec.ClusterNetwork.NodeLocalDNSCacheEnabled == nil || *cluster.Spec.ClusterNetwork.NodeLocalDNSCacheEnabled {
		// NOTE: even if NodeLocalDNSCacheEnabled is nil, we assume it is enabled (backward compatibility for already existing clusters)
		dnsResolverIP = resources.NodeLocalDNSCacheAddress
	}

	kubeconfig, err := r.KubeconfigProvider.GetAdminKubeconfig(ctx, cluster)
	if err != nil {
		return nil, err
	}

	credentials, err := resources.GetCredentials(resources.NewCredentialsData(ctx, cluster, r.Client))
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Add addon variables if available.
	variables := make(map[string]interface{})

	if sub := r.addonVariables[addon.Spec.Name]; sub != nil {
		variables = sub.(map[string]interface{})
	}

	if addon.Spec.Variables != nil && len(addon.Spec.Variables.Raw) > 0 {
		if err = json.Unmarshal(addon.Spec.Variables.Raw, &variables); err != nil {
			return nil, err
		}
	}

	data, err := addonutils.NewTemplateData(
		cluster,
		credentials,
		string(kubeconfig),
		clusterIP,
		dnsResolverIP,
		variables,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create template data for addon manifests: %w", err)
	}

	manifestPath := path.Join(addonDir, addon.Spec.Name)
	allManifests, err := addonutils.ParseFromFolder(log, r.overwriteRegistry, manifestPath, data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse addon templates in %s: %w", manifestPath, err)
	}

	return allManifests, nil
}

// combineManifests returns all manifests combined into a multi document yaml.
func (r *Reconciler) combineManifests(manifests []*bytes.Buffer) *bytes.Buffer {
	parts := make([]string, len(manifests))
	for i, m := range manifests {
		s := m.String()
		s = strings.TrimSuffix(s, "\n")
		s = strings.TrimSpace(s)
		parts[i] = s
	}

	return bytes.NewBufferString(strings.Join(parts, "\n---\n") + "\n")
}

// ensureAddonLabelOnManifests adds the addonLabelKey label to all manifests.
// For this to happen we need to decode all yaml files to json, parse them, add the label and finally encode to yaml again.
func (r *Reconciler) ensureAddonLabelOnManifests(addon *kubermaticv1.Addon, manifests []addon.Manifest) ([]*bytes.Buffer, error) {
	var rawManifests []*bytes.Buffer

	wantLabels := r.getAddonLabel(addon)
	for _, m := range manifests {
		parsedUnstructuredObj := &metav1unstructured.Unstructured{}
		if _, _, err := metav1unstructured.UnstructuredJSONScheme.Decode(m.Content.Raw, nil, parsedUnstructuredObj); err != nil {
			return nil, fmt.Errorf("parsing unstructured failed: %w", err)
		}

		existingLabels := parsedUnstructuredObj.GetLabels()
		if existingLabels == nil {
			existingLabels = map[string]string{}
		}

		// Apply the wanted labels
		for k, v := range wantLabels {
			existingLabels[k] = v
		}
		parsedUnstructuredObj.SetLabels(existingLabels)

		jsonBuffer := &bytes.Buffer{}
		if err := metav1unstructured.UnstructuredJSONScheme.Encode(parsedUnstructuredObj, jsonBuffer); err != nil {
			return nil, fmt.Errorf("encoding json failed: %w", err)
		}

		// Must be encoding back to yaml, otherwise kubectl fails to apply because it tries to parse the whole
		// thing as json
		yamlBytes, err := yaml.JSONToYAML(jsonBuffer.Bytes())
		if err != nil {
			return nil, err
		}

		rawManifests = append(rawManifests, bytes.NewBuffer(yamlBytes))
	}

	return rawManifests, nil
}

func (r *Reconciler) getAddonLabel(addon *kubermaticv1.Addon) map[string]string {
	return map[string]string{
		addonLabelKey: addon.Spec.Name,
	}
}

type fileHandlingDone func()

func getFileDeleteFinalizer(log *zap.SugaredLogger, filename string) fileHandlingDone {
	return func() {
		if err := os.RemoveAll(filename); err != nil {
			log.Errorw("Failed to delete file", zap.Error(err), "file", filename)
		}
	}
}

func (r *Reconciler) writeCombinedManifest(log *zap.SugaredLogger, manifest *bytes.Buffer, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) (string, fileHandlingDone, error) {
	// Write combined Manifest to disk
	manifestFilename := path.Join("/tmp", fmt.Sprintf("cluster-%s-%s.yaml", cluster.Name, addon.Name))
	if err := os.WriteFile(manifestFilename, manifest.Bytes(), 0644); err != nil {
		return "", nil, fmt.Errorf("failed to write combined manifest to %s: %w", manifestFilename, err)
	}
	log.Debugw("Wrote combined manifest", "file", manifestFilename)

	return manifestFilename, getFileDeleteFinalizer(log, manifestFilename), nil
}

func (r *Reconciler) writeAdminKubeconfig(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) (string, fileHandlingDone, error) {
	// Write kubeconfig to disk
	kubeconfig, err := r.KubeconfigProvider.GetAdminKubeconfig(ctx, cluster)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get admin kubeconfig for cluster %s: %w", cluster.Name, err)
	}
	kubeconfigFilename := path.Join("/tmp", fmt.Sprintf("cluster-%s-addon-%s-kubeconfig", cluster.Name, addon.Name))
	if err := os.WriteFile(kubeconfigFilename, kubeconfig, 0644); err != nil {
		return "", nil, fmt.Errorf("failed to write admin kubeconfig for cluster %s: %w", cluster.Name, err)
	}
	log.Debugw("Wrote admin kubeconfig", "file", kubeconfigFilename)

	return kubeconfigFilename, getFileDeleteFinalizer(log, kubeconfigFilename), nil
}

func (r *Reconciler) setupManifestInteraction(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) (string, string, fileHandlingDone, error) {
	manifests, err := r.getAddonManifests(ctx, log, addon, cluster)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get addon manifests: %w", err)
	}

	rawManifests, err := r.ensureAddonLabelOnManifests(addon, manifests)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to add the addon specific label to all addon resources: %w", err)
	}

	rawManifest := r.combineManifests(rawManifests)
	manifestFilename, manifestDone, err := r.writeCombinedManifest(log, rawManifest, addon, cluster)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to write all addon resources into a combined manifest file: %w", err)
	}

	kubeconfigFilename, kubeconfigDone, err := r.writeAdminKubeconfig(ctx, log, addon, cluster)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to write the admin kubeconfig to the local filesystem: %w", err)
	}

	done := func() {
		kubeconfigDone()
		manifestDone()
	}
	return kubeconfigFilename, manifestFilename, done, nil
}

func (r *Reconciler) getApplyCommand(ctx context.Context, kubeconfigFilename, manifestFilename string, selector fmt.Stringer, clusterVersion semver.Semver) (*exec.Cmd, error) {
	binary, err := kubectl.BinaryForClusterVersion(&clusterVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to determine kubectl binary to use: %w", err)
	}

	cmd := exec.CommandContext(
		ctx,
		binary,
		"--kubeconfig", kubeconfigFilename,
		"apply",
		"--prune",
		"--filename", manifestFilename,
		"--selector", selector.String(),
	)
	return cmd, nil
}

func (r *Reconciler) ensureIsInstalled(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) error {
	kubeconfigFilename, manifestFilename, done, err := r.setupManifestInteraction(ctx, log, addon, cluster)
	if err != nil {
		return err
	}
	defer done()

	d, err := os.ReadFile(manifestFilename)
	if err != nil {
		return err
	}
	sd := strings.TrimSpace(string(d))
	if len(sd) == 0 {
		log.Debug("Skipping addon installation as the manifest is empty after parsing")
		return nil
	}

	// We delete all resources with this label which are not in the combined manifest
	selector := labels.SelectorFromSet(r.getAddonLabel(addon))
	cmd, err := r.getApplyCommand(ctx, kubeconfigFilename, manifestFilename, selector, cluster.Status.Versions.ControlPlane)
	if err != nil {
		return fmt.Errorf("failed to create command: %w", err)
	}

	cmdLog := log.With("cmd", strings.Join(cmd.Args, " "))

	cmdLog.Debug("Applying manifest...")
	out, err := cmd.CombinedOutput()
	cmdLog.Debugw("Finished executing command", "output", string(out))
	if err != nil {
		return fmt.Errorf("failed to execute '%s' for addon %s of cluster %s: %w\n%s", strings.Join(cmd.Args, " "), addon.Name, cluster.Name, err, string(out))
	}

	return nil
}

func (r *Reconciler) ensureFinalizerIsSet(ctx context.Context, addon *kubermaticv1.Addon) error {
	return kuberneteshelper.TryAddFinalizer(ctx, r, addon, cleanupFinalizerName)
}

func (r *Reconciler) ensureResourcesCreatedConditionIsSet(ctx context.Context, addon *kubermaticv1.Addon) error {
	if addon.Status.Conditions[kubermaticv1.AddonResourcesCreated].Status == corev1.ConditionTrue {
		return nil
	}

	oldAddon := addon.DeepCopy()
	setAddonCodition(addon, kubermaticv1.AddonResourcesCreated, corev1.ConditionTrue)
	return r.Client.Status().Patch(ctx, addon, ctrlruntimeclient.MergeFrom(oldAddon))
}

func (r *Reconciler) cleanupManifests(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) error {
	kubeconfigFilename, manifestFilename, done, err := r.setupManifestInteraction(ctx, log, addon, cluster)
	if err != nil {
		// FIXME: use a dedicated error type and proper error unwrapping when we have the technology to do it
		if strings.Contains(err.Error(), "no such file or directory") { // if the manifest is already deleted, that's ok
			log.Debugf("cleanupManifests failed for addon %s/%s: %v", addon.Namespace, addon.Name, err)
			return nil
		}
		return err
	}
	defer done()

	binary, err := kubectl.BinaryForClusterVersion(&cluster.Status.Versions.ControlPlane)
	if err != nil {
		return fmt.Errorf("failed to determine kubectl binary to use: %w", err)
	}

	cmd := exec.CommandContext(ctx, binary, "--kubeconfig", kubeconfigFilename, "delete", "-f", manifestFilename, "--ignore-not-found")
	cmdLog := log.With("cmd", strings.Join(cmd.Args, " "))

	cmdLog.Debug("Deleting resources...")
	out, err := cmd.CombinedOutput()
	cmdLog.Debugw("Finished executing command", "output", string(out))
	if err != nil {
		return fmt.Errorf("failed to execute '%s' for addon %s of cluster %s: %w\n%s", strings.Join(cmd.Args, " "), addon.Name, cluster.Name, err, string(out))
	}
	return nil
}

func (r *Reconciler) ensureRequiredResourceTypesExist(ctx context.Context, log *zap.SugaredLogger, addon *kubermaticv1.Addon, cluster *kubermaticv1.Cluster) (*reconcile.Result, error) {
	if len(addon.Spec.RequiredResourceTypes) == 0 {
		// Avoid constructing a client we don't need and just return early
		return nil, nil
	}
	userClusterClient, err := r.KubeconfigProvider.GetClient(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get client for usercluster: %w", err)
	}

	for _, requiredResource := range addon.Spec.RequiredResourceTypes {
		unstructuedList := &metav1unstructured.UnstructuredList{}
		unstructuedList.SetAPIVersion(requiredResource.Group + "/" + requiredResource.Version)
		unstructuedList.SetKind(requiredResource.Kind)

		// We do not care about the result, just if the resource is served, so make sure we only
		// get as little as possible.
		listOpts := &ctrlruntimeclient.ListOptions{Limit: 1}
		if err := userClusterClient.List(ctx, unstructuedList, listOpts); err != nil {
			if meta.IsNoMatchError(err) {
				// Try again later
				log.Infow("Required resource isn't served, trying again in 10 seconds", "resource", formatGVK(requiredResource))
				return &reconcile.Result{RequeueAfter: 10 * time.Second}, nil
			}
			return nil, fmt.Errorf("failed to check if type %q is served: %w", formatGVK(requiredResource), err)
		}
	}

	return nil, nil
}

func formatGVK(gvk kubermaticv1.GroupVersionKind) string {
	return fmt.Sprintf("%s/%s %s", gvk.Group, gvk.Version, gvk.Kind)
}

func setAddonCodition(a *kubermaticv1.Addon, condType kubermaticv1.AddonConditionType, status corev1.ConditionStatus) {
	now := metav1.Now()

	condition, exists := a.Status.Conditions[condType]
	if exists && condition.Status != status {
		condition.LastTransitionTime = now
	}

	condition.Status = status
	condition.LastHeartbeatTime = now

	if a.Status.Conditions == nil {
		a.Status.Conditions = map[kubermaticv1.AddonConditionType]kubermaticv1.AddonCondition{}
	}
	a.Status.Conditions[condType] = condition
}

func addonResourcesCreated(addon *kubermaticv1.Addon) bool {
	return addon.Status.Conditions[kubermaticv1.AddonResourcesCreated].Status == corev1.ConditionTrue
}

func hasEnsureResourcesLabel(addon *kubermaticv1.Addon) bool {
	return addon.Labels[addonEnsureLabelKey] == "true"
}
