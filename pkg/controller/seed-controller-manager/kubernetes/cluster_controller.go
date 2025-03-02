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

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"go.uber.org/zap"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	kubermaticv1helper "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1/helper"
	k8cuserclusterclient "k8c.io/kubermatic/v2/pkg/cluster/client"
	"k8c.io/kubermatic/v2/pkg/clusterdeletion"
	controllerutil "k8c.io/kubermatic/v2/pkg/controller/util"
	kuberneteshelper "k8c.io/kubermatic/v2/pkg/kubernetes"
	"k8c.io/kubermatic/v2/pkg/provider"
	kubernetesprovider "k8c.io/kubermatic/v2/pkg/provider/kubernetes"
	"k8c.io/kubermatic/v2/pkg/resources/certificates"
	"k8c.io/kubermatic/v2/pkg/version/kubermatic"

	appsv1 "k8s.io/api/apps/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	autoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/tools/record"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ControllerName = "kkp-kubernetes-controller"
)

// userClusterConnectionProvider offers functions to retrieve clients for the given user clusters.
type userClusterConnectionProvider interface {
	GetClient(context.Context, *kubermaticv1.Cluster, ...k8cuserclusterclient.ConfigOption) (ctrlruntimeclient.Client, error)
}

type Features struct {
	VPA                          bool
	EtcdDataCorruptionChecks     bool
	KubernetesOIDCAuthentication bool
	EtcdLauncher                 bool
	Konnectivity                 bool
}

// Reconciler is a controller which is responsible for managing clusters.
type Reconciler struct {
	ctrlruntimeclient.Client
	log                     *zap.SugaredLogger
	userClusterConnProvider userClusterConnectionProvider
	workerName              string

	externalURL  string
	seedGetter   provider.SeedGetter
	configGetter provider.KubermaticConfigurationGetter

	recorder record.EventRecorder

	overwriteRegistry                string
	nodeAccessNetwork                string
	etcdDiskSize                     resource.Quantity
	userClusterMLAEnabled            bool
	dockerPullConfigJSON             []byte
	kubermaticImage                  string
	etcdLauncherImage                string
	dnatControllerImage              string
	machineControllerImageTag        string
	machineControllerImageRepository string
	concurrentClusterUpdates         int
	backupSchedule                   time.Duration

	oidcIssuerURL      string
	oidcIssuerClientID string

	features Features
	versions kubermatic.Versions

	tunnelingAgentIP string
	caBundle         *certificates.CABundle
}

// NewController creates a cluster controller.
func Add(
	mgr manager.Manager,
	log *zap.SugaredLogger,
	numWorkers int,
	workerName string,
	externalURL string,
	seedGetter provider.SeedGetter,
	configGetter provider.KubermaticConfigurationGetter,
	userClusterConnProvider userClusterConnectionProvider,
	overwriteRegistry string,
	nodeAccessNetwork string,
	etcdDiskSize resource.Quantity,
	userClusterMLAEnabled bool,
	dockerPullConfigJSON []byte,
	concurrentClusterUpdates int,
	backupSchedule time.Duration,

	oidcIssuerURL string,
	oidcIssuerClientID string,
	kubermaticImage string,
	etcdLauncherImage string,
	dnatControllerImage string,
	machineControllerImageTag string,
	machineControllerImageRepository string,

	tunnelingAgentIP string,
	caBundle *certificates.CABundle,

	features Features,
	versions kubermatic.Versions,
) error {
	reconciler := &Reconciler{
		log:                     log.Named(ControllerName),
		Client:                  mgr.GetClient(),
		userClusterConnProvider: userClusterConnProvider,
		workerName:              workerName,

		recorder: mgr.GetEventRecorderFor(ControllerName),

		overwriteRegistry:                overwriteRegistry,
		nodeAccessNetwork:                nodeAccessNetwork,
		etcdDiskSize:                     etcdDiskSize,
		userClusterMLAEnabled:            userClusterMLAEnabled,
		dockerPullConfigJSON:             dockerPullConfigJSON,
		kubermaticImage:                  kubermaticImage,
		etcdLauncherImage:                etcdLauncherImage,
		dnatControllerImage:              dnatControllerImage,
		machineControllerImageTag:        machineControllerImageTag,
		machineControllerImageRepository: machineControllerImageRepository,
		concurrentClusterUpdates:         concurrentClusterUpdates,
		backupSchedule:                   backupSchedule,

		externalURL:  externalURL,
		seedGetter:   seedGetter,
		configGetter: configGetter,

		oidcIssuerURL:      oidcIssuerURL,
		oidcIssuerClientID: oidcIssuerClientID,

		tunnelingAgentIP: tunnelingAgentIP,
		caBundle:         caBundle,

		features: features,
		versions: versions,
	}

	c, err := controller.New(ControllerName, mgr, controller.Options{Reconciler: reconciler, MaxConcurrentReconciles: numWorkers})
	if err != nil {
		return err
	}

	typesToWatch := []ctrlruntimeclient.Object{
		&corev1.Service{},
		&corev1.ServiceAccount{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.Namespace{},
		&appsv1.StatefulSet{},
		&appsv1.Deployment{},
		&batchv1beta1.CronJob{},
		&policyv1beta1.PodDisruptionBudget{},
		&autoscalingv1.VerticalPodAutoscaler{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&networkingv1.NetworkPolicy{},
	}

	for _, t := range typesToWatch {
		if err := c.Watch(&source.Kind{Type: t}, controllerutil.EnqueueClusterForNamespacedObject(mgr.GetClient())); err != nil {
			return fmt.Errorf("failed to create watcher for %T: %w", t, err)
		}
	}

	return c.Watch(&source.Kind{Type: &kubermaticv1.Cluster{}}, &handler.EnqueueRequestForObject{})
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.log.With("request", request)
	log.Debug("Processing")

	cluster := &kubermaticv1.Cluster{}
	if err := r.Get(ctx, request.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Could not find cluster")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	log = log.With("cluster", cluster.Name)

	// ensure new Cluster objects have basic status information set;
	// this should be done regardless of ClusterAvailableForReconciling()
	// and hence outside the ClusterReconcileWrapper
	if err := r.reconcileClusterStatus(ctx, cluster); err != nil {
		log.Errorw("Reconciling failed", zap.Error(err))
		r.recorder.Event(cluster, corev1.EventTypeWarning, "ReconcilingError", err.Error())

		return reconcile.Result{}, err
	}

	// the update controller needs to determine the target version based on the spec
	// before we can reconcile anything
	if cluster.Status.Versions.ControlPlane == "" {
		log.Debug("Cluster not yet ready for reconciling")

		return reconcile.Result{}, nil
	}

	// Add a wrapping here so we can emit an event on error
	result, err := kubermaticv1helper.ClusterReconcileWrapper(
		ctx,
		r.Client,
		r.workerName,
		cluster,
		r.versions,
		kubermaticv1.ClusterConditionClusterControllerReconcilingSuccess,
		func() (*reconcile.Result, error) {
			// only reconcile this cluster if there are not yet too many updates running
			if available, err := controllerutil.ClusterAvailableForReconciling(ctx, r, cluster, r.concurrentClusterUpdates); !available || err != nil {
				log.Infow("Concurrency limit reached, checking again in 10 seconds", "concurrency-limit", r.concurrentClusterUpdates)
				return &reconcile.Result{
					RequeueAfter: 10 * time.Second,
				}, err
			}

			return r.reconcile(ctx, log, cluster)
		},
	)
	if err != nil {
		log.Errorw("Reconciling failed", zap.Error(err))
		r.recorder.Event(cluster, corev1.EventTypeWarning, "ReconcilingError", err.Error())
	}

	if result == nil {
		result = &reconcile.Result{}
	}

	return *result, err
}

func (r *Reconciler) reconcileClusterStatus(ctx context.Context, cluster *kubermaticv1.Cluster) error {
	return kubermaticv1helper.UpdateClusterStatus(ctx, r, cluster, func(c *kubermaticv1.Cluster) {
		if c.Status.NamespaceName == "" {
			c.Status.NamespaceName = kubernetesprovider.NamespaceName(cluster.Name)
		}
	})
}

func (r *Reconciler) reconcile(ctx context.Context, log *zap.SugaredLogger, cluster *kubermaticv1.Cluster) (*reconcile.Result, error) {
	// synchronize cluster.status.health for Kubernetes clusters
	if err := r.syncHealth(ctx, cluster); err != nil {
		return nil, fmt.Errorf("failed to sync health: %w", err)
	}

	if cluster.DeletionTimestamp != nil {
		log.Debug("Cleaning up cluster")

		// Defer getting the client to make sure we only request it if we actually need it
		userClusterClientGetter := func() (ctrlruntimeclient.Client, error) {
			client, err := r.userClusterConnProvider.GetClient(ctx, cluster)
			if err != nil {
				return nil, fmt.Errorf("failed to get user cluster client: %w", err)
			}
			return client, nil
		}

		// Always requeue a cluster after we executed the cleanup.
		return &reconcile.Result{RequeueAfter: 10 * time.Second}, clusterdeletion.New(r.Client, userClusterClientGetter).CleanupCluster(ctx, log, cluster)
	}

	res, err := r.reconcileCluster(ctx, cluster)
	if err != nil {
		updateErr := r.updateClusterError(ctx, cluster, kubermaticv1.ReconcileClusterError, err.Error())
		if updateErr != nil {
			return nil, fmt.Errorf("failed to set the cluster error: %w", updateErr)
		}
		return nil, fmt.Errorf("failed to reconcile cluster: %w", err)
	}

	if err := r.clearClusterError(ctx, cluster); err != nil {
		return nil, fmt.Errorf("failed to clear error on cluster: %w", err)
	}

	return res, nil
}

func (r *Reconciler) updateCluster(ctx context.Context, cluster *kubermaticv1.Cluster, modify func(*kubermaticv1.Cluster), opts ...ctrlruntimeclient.MergeFromOption) error {
	oldCluster := cluster.DeepCopy()
	modify(cluster)
	if reflect.DeepEqual(oldCluster, cluster) {
		return nil
	}

	if !reflect.DeepEqual(oldCluster.Status, cluster.Status) {
		return errors.New("updateCluster must not change cluster status")
	}

	if err := r.Patch(ctx, cluster, ctrlruntimeclient.MergeFromWithOptions(oldCluster, opts...)); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) AddFinalizers(ctx context.Context, cluster *kubermaticv1.Cluster, finalizers ...string) (*reconcile.Result, error) {
	return &reconcile.Result{}, kuberneteshelper.TryAddFinalizer(ctx, r, cluster, finalizers...)
}

func (r *Reconciler) updateClusterError(ctx context.Context, cluster *kubermaticv1.Cluster, reason kubermaticv1.ClusterStatusError, message string) error {
	err := kubermaticv1helper.UpdateClusterStatus(ctx, r, cluster, func(c *kubermaticv1.Cluster) {
		c.Status.ErrorMessage = &message
		c.Status.ErrorReason = &reason
	})
	if err != nil {
		return fmt.Errorf("failed to set error status on cluster to: errorReason=%q errorMessage=%q. Could not update cluster: %w", reason, message, err)
	}

	return nil
}

func (r *Reconciler) clearClusterError(ctx context.Context, cluster *kubermaticv1.Cluster) error {
	return kubermaticv1helper.UpdateClusterStatus(ctx, r, cluster, func(c *kubermaticv1.Cluster) {
		c.Status.ErrorMessage = nil
		c.Status.ErrorReason = nil
	})
}

func (r *Reconciler) getOwnerRefForCluster(cluster *kubermaticv1.Cluster) metav1.OwnerReference {
	gv := kubermaticv1.SchemeGroupVersion
	return *metav1.NewControllerRef(cluster, gv.WithKind("Cluster"))
}
