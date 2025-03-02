//go:build e2e

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

package etcdlauncher

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	"k8c.io/kubermatic/v2/pkg/test/e2e/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	datacenter = "kubermatic"
	location   = "hetzner-hel1"
	version    = utils.KubernetesVersion()
	credential = "e2e-hetzner"
)

const (
	scaleUpCount           = 5
	scaleDownCount         = 3
	minioBackupDestination = "minio"
	namespaceName          = "backup-test"
)

func TestBackup(t *testing.T) {
	ctx := context.Background()

	client, _, _, err := utils.GetClients()
	if err != nil {
		t.Fatalf("failed to get client for seed cluster: %v", err)
	}

	// login
	masterToken, err := utils.RetrieveMasterToken(ctx)
	if err != nil {
		t.Fatalf("failed to get master token: %v", err)
	}
	testClient := utils.NewTestClient(masterToken, t)

	// create dummy project
	t.Log("creating project...")
	project, err := testClient.CreateProject(rand.String(10))
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	defer cleanupProject(t, project.ID)

	// create dummy cluster (NB: If these tests fail, the etcd ring can be
	// _so_ dead that any cleanup attempt is futile; make sure to not create
	// any cloud resources, as they might be orphaned)

	t.Log("creating cluster...")
	apiCluster, err := testClient.CreateHetznerCluster(project.ID, datacenter, rand.String(10), credential, version, location, 0)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// wait for the cluster to become healthy
	if err := testClient.WaitForClusterHealthy(project.ID, datacenter, apiCluster.ID); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	// get the cluster object (the CRD, not the API's representation)
	cluster := &kubermaticv1.Cluster{}
	if err := client.Get(ctx, types.NamespacedName{Name: apiCluster.ID}, cluster); err != nil {
		t.Fatalf("failed to get cluster: %v", err)
	}

	t.Log("creating client for user cluster...")
	userClient, err := testClient.GetUserClusterClient(datacenter, project.ID, apiCluster.ID)
	if err != nil {
		t.Fatalf("error creating user cluster client: %v", err)
	}

	// Create a resource on the cluster so that we can see that the backup works
	testNamespace := &corev1.Namespace{}
	testNamespace.Name = namespaceName
	err = userClient.Create(ctx, testNamespace)
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}
	t.Log("created test namespace")

	// create etcd backup that will be restored later
	err, backup := createBackup(ctx, t, client, cluster)
	if err != nil {
		t.Fatalf("failed to create etcd backup: %v", err)
	}
	t.Log("created etcd backup")

	// delete the test resource
	err = userClient.Delete(ctx, testNamespace)
	if err != nil {
		t.Fatalf("failed to delete test namespace: %v", err)
	}
	t.Log("deleted test namespace")

	// enable etcd-launcher feature after creating a backup
	if err := enableLauncher(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to enable etcd-launcher: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	// restore from backup
	if err := restoreBackup(ctx, t, client, cluster, backup); err != nil {
		t.Fatalf("failed to restore etcd backup: %v", err)
	}
	t.Log("restored etcd backup")

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	// check if resource was restored
	restoredNamespace := &corev1.Namespace{}
	err = userClient.Get(ctx, types.NamespacedName{Name: namespaceName}, restoredNamespace)
	if err != nil {
		t.Fatalf("failed to get restored test namespace: %v", err)
	}
	t.Log("deleted namespace was restored by backup")

	t.Log("tests succeeded")
}

func TestScaling(t *testing.T) {
	ctx := context.Background()

	client, _, _, err := utils.GetClients()
	if err != nil {
		t.Fatalf("failed to get client for seed cluster: %v", err)
	}

	// login
	masterToken, err := utils.RetrieveMasterToken(ctx)
	if err != nil {
		t.Fatalf("failed to get master token: %v", err)
	}
	testClient := utils.NewTestClient(masterToken, t)

	// create dummy project
	t.Log("creating project...")
	project, err := testClient.CreateProject(rand.String(10))
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	defer cleanupProject(t, project.ID)

	// create dummy cluster (NB: If these tests fail, the etcd ring can be
	// _so_ dead that any cleanup attempt is futile; make sure to not create
	// any cloud resources, as they might be orphaned)

	t.Log("creating cluster...")
	apiCluster, err := testClient.CreateHetznerCluster(project.ID, datacenter, rand.String(10), credential, version, location, 0)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// wait for the cluster to become healthy
	if err := testClient.WaitForClusterHealthy(project.ID, datacenter, apiCluster.ID); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	// get the cluster object (the CRD, not the API's representation)
	cluster := &kubermaticv1.Cluster{}
	if err := client.Get(ctx, types.NamespacedName{Name: apiCluster.ID}, cluster); err != nil {
		t.Fatalf("failed to get cluster: %v", err)
	}

	// we run all these tests in the same cluster to speed up the e2e test
	if err := enableLauncher(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to enable etcd-launcher: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	if err := scaleUp(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to scale up: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	if err := scaleDown(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to scale down: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	if err := disableLauncher(ctx, t, client, cluster); err != nil {
		t.Fatalf("succeeded in disabling immutable feature etcd-launcher: %v", err)
	}

	t.Log("tests succeeded")
}

func TestRecovery(t *testing.T) {
	ctx := context.Background()

	client, _, _, err := utils.GetClients()
	if err != nil {
		t.Fatalf("failed to get client for seed cluster: %v", err)
	}

	// login
	masterToken, err := utils.RetrieveMasterToken(ctx)
	if err != nil {
		t.Fatalf("failed to get master token: %v", err)
	}
	testClient := utils.NewTestClient(masterToken, t)

	// create dummy project
	t.Log("creating project...")
	project, err := testClient.CreateProject(rand.String(10))
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	defer cleanupProject(t, project.ID)

	// create dummy cluster (NB: If these tests fail, the etcd ring can be
	// _so_ dead that any cleanup attempt is futile; make sure to not create
	// any cloud resources, as they might be orphaned)

	t.Log("creating cluster...")
	apiCluster, err := testClient.CreateHetznerCluster(project.ID, datacenter, rand.String(10), credential, version, location, 0)
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	// wait for the cluster to become healthy
	if err := testClient.WaitForClusterHealthy(project.ID, datacenter, apiCluster.ID); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	// get the cluster object (the CRD, not the API's representation)
	cluster := &kubermaticv1.Cluster{}
	if err := client.Get(ctx, types.NamespacedName{Name: apiCluster.ID}, cluster); err != nil {
		t.Fatalf("failed to get cluster: %v", err)
	}

	if err := enableLauncher(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to enable etcd-launcher: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	if err := breakAndRecoverPV(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to test volume recovery: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}

	if err := breakAndRecoverPVC(ctx, t, client, cluster); err != nil {
		t.Fatalf("failed to recover from PVC deletion: %v", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}
}

func createBackup(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) (error, *kubermaticv1.EtcdBackupConfig) {
	t.Log("creating backup of etcd data...")
	backup := &kubermaticv1.EtcdBackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-e2e-backup",
			Namespace: cluster.Status.NamespaceName,
		},
		Spec: kubermaticv1.EtcdBackupConfigSpec{
			Cluster: corev1.ObjectReference{
				Kind:            cluster.Kind,
				Name:            cluster.Name,
				Namespace:       cluster.Namespace,
				UID:             cluster.UID,
				APIVersion:      cluster.APIVersion,
				ResourceVersion: cluster.ResourceVersion,
			},
			Destination: minioBackupDestination,
		},
	}

	if err := client.Create(ctx, backup); err != nil {
		return fmt.Errorf("failed to create EtcdBackupConfig: %w", err), nil
	}

	if err := waitForEtcdBackup(ctx, t, client, backup); err != nil {
		return fmt.Errorf("failed to wait for etcd backup finishing: %w (%v)", err, backup.Status), nil
	}

	return nil, backup
}

func restoreBackup(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster, backup *kubermaticv1.EtcdBackupConfig) error {
	t.Log("restoring etcd cluster from backup...")
	restore := &kubermaticv1.EtcdRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-e2e-restore",
			Namespace: backup.Namespace,
		},
		Spec: kubermaticv1.EtcdRestoreSpec{
			Cluster: corev1.ObjectReference{
				Kind:            cluster.Kind,
				Name:            cluster.Name,
				Namespace:       cluster.Namespace,
				UID:             cluster.UID,
				APIVersion:      cluster.APIVersion,
				ResourceVersion: cluster.ResourceVersion,
			},
			BackupName:  backup.Status.CurrentBackups[0].BackupName,
			Destination: minioBackupDestination,
		},
	}

	if err := client.Create(ctx, restore); err != nil {
		return fmt.Errorf("failed to create EtcdRestore: %w", err)
	}

	if err := waitForEtcdRestore(ctx, t, client, restore); err != nil {
		return fmt.Errorf("failed to wait for etcd restore: %w", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("failed to wait for cluster to become healthy again: %w", err)
	}

	return nil
}

func enableLauncher(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	t.Log("enabling etcd-launcher...")
	if err := enableLauncherForCluster(ctx, client, cluster); err != nil {
		return fmt.Errorf("failed to enable etcd-launcher: %w", err)
	}

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("etcd cluster is not healthy: %w", err)
	}

	if err := waitForStrictTLSMode(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("etcd cluster is not running in strict TLS peer mode: %w", err)
	}

	active, err := isEtcdLauncherActive(ctx, client, cluster)
	if err != nil {
		return fmt.Errorf("failed to check StatefulSet command: %w", err)
	}

	if !active {
		return errors.New("feature flag had no effect on the StatefulSet")
	}

	return nil
}

func disableLauncher(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	t.Log("trying to disable etcd-launcher (not expected to succeed) ...")
	if err := disableEtcdlauncherForCluster(ctx, client, cluster); err == nil {
		return fmt.Errorf("no error disabling etcd-launcher, expected validation to fail")
	}

	return nil
}

func scaleUp(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	t.Logf("scaling etcd cluster up to %d nodes...", scaleUpCount)
	if err := resizeEtcd(ctx, client, cluster, scaleUpCount); err != nil {
		return fmt.Errorf("failed while trying to scale up the etcd cluster: %w", err)
	}

	if err := waitForRollout(ctx, t, client, cluster, scaleUpCount); err != nil {
		return fmt.Errorf("rollout got stuck: %w", err)
	}
	t.Log("etcd cluster scaled up successfully.")

	return nil
}

func scaleDown(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	t.Logf("scaling etcd cluster down to %d nodes...", scaleDownCount)
	if err := resizeEtcd(ctx, client, cluster, scaleDownCount); err != nil {
		return fmt.Errorf("failed while trying to scale down the etcd cluster: %w", err)
	}

	if err := waitForRollout(ctx, t, client, cluster, scaleDownCount); err != nil {
		return fmt.Errorf("rollout got stuck: %w", err)
	}
	t.Log("etcd cluster scaled down successfully.")

	return nil
}

func breakAndRecoverPV(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	// delete one of the etcd node PVs
	t.Log("testing etcd node PV automatic recovery...")
	if err := forceDeleteEtcdPV(ctx, client, cluster); err != nil {
		return fmt.Errorf("failed to delete etcd node PV: %w", err)
	}

	// wait for a bit before checking health as the PV recovery process
	// is a controller-manager loop that doesn't necessarily kick in immediately
	time.Sleep(30 * time.Second)

	// auto recovery should kick in. We need to wait for it
	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("etcd cluster is not healthy: %w", err)
	}
	t.Log("etcd node PV recovered successfully.")

	return nil
}

func breakAndRecoverPVC(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	// delete one of the etcd node PVCs
	t.Log("testing etcd-launcher recovery from deleted PVC ...")
	if err := deleteEtcdPVC(ctx, client, cluster); err != nil {
		return fmt.Errorf("failed to delete etcd node PVC: %w", err)
	}

	time.Sleep(30 * time.Second)

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("etcd cluster is not healthy: %w", err)
	}

	t.Log("etcd node recovered from PVC deletion successfully.")

	return nil
}

// enable etcd launcher for the cluster.
func enableLauncherForCluster(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	return setClusterLauncherFeature(ctx, client, cluster, true)
}

func disableEtcdlauncherForCluster(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	return setClusterLauncherFeature(ctx, client, cluster, false)
}

func setClusterLauncherFeature(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster, flag bool) error {
	return patchCluster(ctx, client, cluster, func(c *kubermaticv1.Cluster) error {
		if cluster.Spec.Features == nil {
			cluster.Spec.Features = map[string]bool{}
		}

		cluster.Spec.Features[kubermaticv1.ClusterFeatureEtcdLauncher] = flag
		return nil
	})
}

// isClusterEtcdHealthy checks whether the etcd status on the Cluster object
// is Healthy and the StatefulSet is fully rolled out.
func isClusterEtcdHealthy(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) (bool, error) {
	// refresh cluster status
	if err := client.Get(ctx, types.NamespacedName{Name: cluster.Name}, cluster); err != nil {
		return false, fmt.Errorf("failed to get cluster: %w", err)
	}

	sts := &appsv1.StatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: "etcd", Namespace: clusterNamespace(cluster)}, sts); err != nil {
		return false, fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	clusterSize := int32(3)
	if size := cluster.Spec.ComponentsOverride.Etcd.ClusterSize; size != nil {
		clusterSize = *size
	}

	// we are healthy if the cluster controller is happy and the sts has ready replicas
	// matching the cluster's expected etcd cluster size
	return cluster.Status.ExtendedHealth.Etcd == kubermaticv1.HealthStatusUp &&
		clusterSize == sts.Status.ReadyReplicas, nil
}

func isStrictTLSEnabled(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) (bool, error) {
	etcdHealthy, err := isClusterEtcdHealthy(ctx, client, cluster)
	if err != nil {
		return false, fmt.Errorf("etcd health check failed: %w", err)
	}

	sts := &appsv1.StatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: "etcd", Namespace: clusterNamespace(cluster)}, sts); err != nil {
		return false, fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	strictModeEnvSet := false

	for _, env := range sts.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "PEER_TLS_MODE" && env.Value == "strict" {
			strictModeEnvSet = true
		}
	}

	return etcdHealthy && strictModeEnvSet, nil
}

// isEtcdLauncherActive deduces from the StatefulSet's current spec whether or
// or not the etcd-launcher is enabled (and reconciled).
func isEtcdLauncherActive(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) (bool, error) {
	etcdHealthy, err := isClusterEtcdHealthy(ctx, client, cluster)
	if err != nil {
		return false, fmt.Errorf("etcd health check failed: %w", err)
	}

	sts := &appsv1.StatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: "etcd", Namespace: clusterNamespace(cluster)}, sts); err != nil {
		return false, fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	return etcdHealthy && sts.Spec.Template.Spec.Containers[0].Command[0] == "/opt/bin/etcd-launcher", nil
}

func isEtcdBackupCompleted(status *kubermaticv1.EtcdBackupConfigStatus) bool {
	if length := len(status.CurrentBackups); length != 1 {
		return false
	}

	if status.CurrentBackups[0].BackupPhase == kubermaticv1.BackupStatusPhaseCompleted {
		return true
	}

	return false
}

func isEtcdRestoreCompleted(status *kubermaticv1.EtcdRestoreStatus) bool {
	return status.Phase == kubermaticv1.EtcdRestorePhaseCompleted
}

// resizeEtcd changes the etcd cluster size.
func resizeEtcd(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster, size int) error {
	if size > kubermaticv1.MaxEtcdClusterSize || size < kubermaticv1.MinEtcdClusterSize {
		return fmt.Errorf("Invalid etcd cluster size: %d", size)
	}

	return patchCluster(ctx, client, cluster, func(c *kubermaticv1.Cluster) error {
		n := int32(size)
		cluster.Spec.ComponentsOverride.Etcd.ClusterSize = &n
		return nil
	})
}

func waitForEtcdBackup(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, backup *kubermaticv1.EtcdBackupConfig) error {
	before := time.Now()
	if err := wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
		if err := client.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace}, backup); err != nil {
			return false, err
		}

		return isEtcdBackupCompleted(&backup.Status), nil
	}); err != nil {
		return err
	}

	t.Logf("etcd backup finished after %v.", time.Since(before))
	return nil
}

func waitForEtcdRestore(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, restore *kubermaticv1.EtcdRestore) error {
	before := time.Now()
	if err := wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
		if err := client.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: restore.Namespace}, restore); err != nil {
			return false, err
		}

		return isEtcdRestoreCompleted(&restore.Status), nil
	}); err != nil {
		return fmt.Errorf("failed waiting for restore to complete: %w (%v)", err, restore.Status)
	}

	t.Logf("etcd restore finished after %v.", time.Since(before))
	return nil
}

func waitForClusterHealthy(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	before := time.Now()

	// let's briefly sleep to give controllers a chance to kick in
	time.Sleep(10 * time.Second)

	if err := wait.PollImmediate(3*time.Second, 10*time.Minute, func() (bool, error) {
		// refresh cluster object for updated health status
		if err := client.Get(ctx, types.NamespacedName{Name: cluster.Name}, cluster); err != nil {
			return false, fmt.Errorf("failed to get cluster: %w", err)
		}

		healthy, err := isClusterEtcdHealthy(ctx, client, cluster)
		if err != nil {
			t.Logf("failed to check cluster etcd health status: %v", err)
			return false, nil
		}
		return healthy, nil
	}); err != nil {
		return fmt.Errorf("failed to check etcd health status: %w", err)
	}

	t.Logf("etcd cluster became healthy after %v.", time.Since(before))

	return nil
}

func waitForStrictTLSMode(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	before := time.Now()
	if err := wait.PollImmediate(3*time.Second, 10*time.Minute, func() (bool, error) {
		// refresh cluster object for updated health status
		if err := client.Get(ctx, types.NamespacedName{Name: cluster.Name}, cluster); err != nil {
			return false, fmt.Errorf("failed to get cluster: %w", err)
		}

		healthy, err := isStrictTLSEnabled(ctx, client, cluster)
		if err != nil {
			t.Logf("failed to check cluster etcd health status: %v", err)
			return false, nil
		}
		return healthy, nil
	}); err != nil {
		return fmt.Errorf("failed to check etcd health status: %w", err)
	}

	t.Logf("etcd cluster is running in strict TLS mode after %v.", time.Since(before))

	return nil
}

func waitForRollout(ctx context.Context, t *testing.T, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster, targetSize int) error {
	t.Log("waiting for rollout...")

	if err := waitForClusterHealthy(ctx, t, client, cluster); err != nil {
		return fmt.Errorf("etcd cluster is not healthy: %w", err)
	}

	// count the pods
	readyPods, err := getStsReadyPodsCount(ctx, client, cluster)
	if err != nil {
		return fmt.Errorf("failed to check ready pods count: %w", err)
	}
	if int(readyPods) != targetSize {
		return fmt.Errorf("failed to scale etcd cluster: want [%d] nodes, got [%d]", targetSize, readyPods)
	}

	return nil
}

func forceDeleteEtcdPV(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	ns := clusterNamespace(cluster)

	selector, err := labels.Parse("app=etcd")
	if err != nil {
		return fmt.Errorf("failed to parse label selector: %w", err)
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	opt := &ctrlruntimeclient.ListOptions{
		LabelSelector: selector,
		Namespace:     ns,
	}
	if err := client.List(ctx, pvcList, opt); err != nil || len(pvcList.Items) == 0 {
		return fmt.Errorf("failed to list PVCs or empty list in cluster namespace: %w", err)
	}

	// pick a random PVC, get its PV and delete it
	pvc := pvcList.Items[rand.Intn(len(pvcList.Items))]
	pvName := pvc.Spec.VolumeName
	typedName := types.NamespacedName{Name: pvName, Namespace: ns}

	pv := &corev1.PersistentVolume{}
	if err := client.Get(ctx, typedName, pv); err != nil {
		return fmt.Errorf("failed to get etcd node PV %s: %w", pvName, err)
	}
	oldPv := pv.DeepCopy()

	// first, we delete it
	if err := client.Delete(ctx, pv); err != nil {
		return fmt.Errorf("failed to delete etcd node PV %s: %w", pvName, err)
	}

	// now it will get stuck, we need to patch it to remove the pv finalizer
	pv.Finalizers = nil
	if err := client.Patch(ctx, pv, ctrlruntimeclient.MergeFrom(oldPv)); err != nil {
		return fmt.Errorf("failed to delete the PV %s finalizer: %w", pvName, err)
	}

	// make sure it's gone
	return wait.PollImmediate(2*time.Second, 3*time.Minute, func() (bool, error) {
		if err := client.Get(ctx, typedName, pv); apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
}

func deleteEtcdPVC(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) error {
	ns := clusterNamespace(cluster)

	selector, err := labels.Parse("app=etcd")
	if err != nil {
		return fmt.Errorf("failed to parse label selector: %w", err)
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	opt := &ctrlruntimeclient.ListOptions{
		LabelSelector: selector,
		Namespace:     ns,
	}
	if err := client.List(ctx, pvcList, opt); err != nil || len(pvcList.Items) == 0 {
		return fmt.Errorf("failed to list PVCs or empty list in cluster namespace: %w", err)
	}

	// pick a random PVC and get the corresponding pod
	index := rand.Intn(len(pvcList.Items))
	pvc := pvcList.Items[index]
	oldPvc := pvc.DeepCopy()

	podList := &corev1.PodList{}
	if err := client.List(ctx, podList, opt); err != nil || len(podList.Items) != len(pvcList.Items) {
		return fmt.Errorf("failed to list etcd pods or bad number of pods: %w", err)
	}

	pod := podList.Items[index]

	// first, we delete it
	if err := client.Delete(ctx, &pvc); err != nil {
		return fmt.Errorf("failed to delete etcd node PVC %s: %w", pvc.Name, err)
	}

	// now, we delete the pod so the PVC can be finalised
	if err := client.Delete(ctx, &pod); err != nil {
		return fmt.Errorf("failed to delete etcd pod %s: %w", pod.Name, err)
	}

	// make sure the PVC is recreated by checking the CreationTimestamp against a DeepCopy
	// created of the PVC resource.
	return wait.PollImmediate(2*time.Second, 3*time.Minute, func() (bool, error) {
		if err := client.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, &pvc); err == nil {
			if oldPvc.ObjectMeta.CreationTimestamp.Before(&pvc.ObjectMeta.CreationTimestamp) {
				return true, nil
			}
		}
		return false, nil
	})
}

func getStsReadyPodsCount(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster) (int32, error) {
	sts := &appsv1.StatefulSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: "etcd", Namespace: clusterNamespace(cluster)}, sts); err != nil {
		return 0, fmt.Errorf("failed to get StatefulSet: %w", err)
	}
	return sts.Status.ReadyReplicas, nil
}

func clusterNamespace(cluster *kubermaticv1.Cluster) string {
	return fmt.Sprintf("cluster-%s", cluster.Name)
}

type patchFunc func(cluster *kubermaticv1.Cluster) error

func patchCluster(ctx context.Context, client ctrlruntimeclient.Client, cluster *kubermaticv1.Cluster, patch patchFunc) error {
	if err := client.Get(ctx, types.NamespacedName{Name: cluster.Name}, cluster); err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	oldCluster := cluster.DeepCopy()
	if err := patch(cluster); err != nil {
		return err
	}

	if err := client.Patch(ctx, cluster, ctrlruntimeclient.MergeFrom(oldCluster)); err != nil {
		return fmt.Errorf("failed to patch cluster: %w", err)
	}

	// give KKP some time to reconcile
	time.Sleep(10 * time.Second)

	return nil
}
