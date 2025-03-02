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

package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-05-01/network"
	"github.com/Azure/go-autorest/autorest"
	"go.uber.org/zap"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	kuberneteshelper "k8c.io/kubermatic/v2/pkg/kubernetes"
	"k8c.io/kubermatic/v2/pkg/log"
	"k8c.io/kubermatic/v2/pkg/provider"
)

const (
	resourceNamePrefix = "kubernetes-"

	clusterTagKey = "cluster"

	// FinalizerSecurityGroup will instruct the deletion of the security group.
	FinalizerSecurityGroup = "kubermatic.k8c.io/cleanup-azure-security-group"
	// FinalizerRouteTable will instruct the deletion of the route table.
	FinalizerRouteTable = "kubermatic.k8c.io/cleanup-azure-route-table"
	// FinalizerSubnet will instruct the deletion of the subnet.
	FinalizerSubnet = "kubermatic.k8c.io/cleanup-azure-subnet"
	// FinalizerVNet will instruct the deletion of the virtual network.
	FinalizerVNet = "kubermatic.k8c.io/cleanup-azure-vnet"
	// FinalizerResourceGroup will instruct the deletion of the resource group.
	FinalizerResourceGroup = "kubermatic.k8c.io/cleanup-azure-resource-group"
	// FinalizerAvailabilitySet will instruct the deletion of the availability set.
	FinalizerAvailabilitySet = "kubermatic.k8c.io/cleanup-azure-availability-set"

	denyAllTCPSecGroupRuleName   = "deny_all_tcp"
	denyAllUDPSecGroupRuleName   = "deny_all_udp"
	allowAllICMPSecGroupRuleName = "icmp_by_allow_all"
)

type Azure struct {
	dc                *kubermaticv1.DatacenterSpecAzure
	log               *zap.SugaredLogger
	secretKeySelector provider.SecretKeySelectorValueFunc
}

// New returns a new Azure provider.
func New(dc *kubermaticv1.Datacenter, secretKeyGetter provider.SecretKeySelectorValueFunc) (*Azure, error) {
	if dc.Spec.Azure == nil {
		return nil, errors.New("datacenter is not an Azure datacenter")
	}
	return &Azure{
		dc:                dc.Spec.Azure,
		log:               log.Logger,
		secretKeySelector: secretKeyGetter,
	}, nil
}

var _ provider.ReconcilingCloudProvider = &Azure{}

// Azure API doesn't allow programmatically getting the number of available fault domains in a given region.
// We must therefore hardcode these based on https://docs.microsoft.com/en-us/azure/virtual-machines/windows/manage-availability
//
// The list of region codes was generated by `az account list-locations | jq .[].id --raw-output | cut -d/ -f5 | sed -e 's/^/"/' -e 's/$/" : ,/'`.
var faultDomainsPerRegion = map[string]int32{
	"eastasia":           2,
	"southeastasia":      2,
	"centralus":          3,
	"eastus":             3,
	"eastus2":            3,
	"westus":             3,
	"northcentralus":     3,
	"southcentralus":     3,
	"northeurope":        3,
	"westeurope":         3,
	"japanwest":          2,
	"japaneast":          2,
	"brazilsouth":        2,
	"australiaeast":      2,
	"australiasoutheast": 2,
	"southindia":         2,
	"centralindia":       2,
	"westindia":          2,
	"canadacentral":      3,
	"canadaeast":         2,
	"uksouth":            2,
	"ukwest":             2,
	"westcentralus":      2,
	"westus2":            2,
	"koreacentral":       2,
	"koreasouth":         2,
}

func (a *Azure) CleanUpCloudProvider(ctx context.Context, cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	var err error

	credentials, err := GetCredentialsForCluster(cluster.Spec.Cloud, a.secretKeySelector)
	if err != nil {
		return nil, err
	}

	clientSet, err := GetClientSet(cluster.Spec.Cloud, credentials)
	if err != nil {
		return nil, err
	}

	logger := a.log.With("cluster", cluster.Name)
	if kuberneteshelper.HasFinalizer(cluster, FinalizerSecurityGroup) {
		logger.Infow("deleting security group", "group", cluster.Spec.Cloud.Azure.SecurityGroup)
		if err := deleteSecurityGroup(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete security group %q: %w", cluster.Spec.Cloud.Azure.SecurityGroup, err)
			}
		}
		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerSecurityGroup)
		})
		if err != nil {
			return nil, err
		}
	}

	if kuberneteshelper.HasFinalizer(cluster, FinalizerRouteTable) {
		logger.Infow("deleting route table", "routeTableName", cluster.Spec.Cloud.Azure.RouteTableName)
		if err := deleteRouteTable(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete route table %q: %w", cluster.Spec.Cloud.Azure.RouteTableName, err)
			}
		}
		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerRouteTable)
		})
		if err != nil {
			return nil, err
		}
	}

	if kuberneteshelper.HasFinalizer(cluster, FinalizerSubnet) {
		logger.Infow("deleting subnet", "subnet", cluster.Spec.Cloud.Azure.SubnetName)
		if err := deleteSubnet(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete sub-network %q: %w", cluster.Spec.Cloud.Azure.SubnetName, err)
			}
		}
		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerSubnet)
		})
		if err != nil {
			return nil, err
		}
	}

	if kuberneteshelper.HasFinalizer(cluster, FinalizerVNet) {
		logger.Infow("deleting vnet", "vnet", cluster.Spec.Cloud.Azure.VNetName)
		if err := deleteVNet(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete virtual network %q: %w", cluster.Spec.Cloud.Azure.VNetName, err)
			}
		}

		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerVNet)
		})
		if err != nil {
			return nil, err
		}
	}

	if kuberneteshelper.HasFinalizer(cluster, FinalizerAvailabilitySet) {
		logger.Infow("deleting availability set", "availabilitySet", cluster.Spec.Cloud.Azure.AvailabilitySet)
		if err := deleteAvailabilitySet(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete availability set %q: %w", cluster.Spec.Cloud.Azure.AvailabilitySet, err)
			}
		}

		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerAvailabilitySet)
		})
		if err != nil {
			return nil, err
		}
	}

	if kuberneteshelper.HasFinalizer(cluster, FinalizerResourceGroup) {
		logger.Infow("deleting resource group", "resourceGroup", cluster.Spec.Cloud.Azure.ResourceGroup)
		if err := deleteResourceGroup(ctx, clientSet, cluster.Spec.Cloud); err != nil {
			var detErr *autorest.DetailedError
			if !errors.As(err, &detErr) || detErr.StatusCode != http.StatusNotFound {
				return cluster, fmt.Errorf("failed to delete resource group %q: %w", cluster.Spec.Cloud.Azure.ResourceGroup, err)
			}
		}

		cluster, err = update(ctx, cluster.Name, func(updatedCluster *kubermaticv1.Cluster) {
			kuberneteshelper.RemoveFinalizer(updatedCluster, FinalizerResourceGroup)
		})
		if err != nil {
			return nil, err
		}
	}

	return cluster, nil
}

func (a *Azure) InitializeCloudProvider(ctx context.Context, cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	return a.reconcileCluster(ctx, cluster, update, false, true)
}

func (a *Azure) ReconcileCluster(ctx context.Context, cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	return a.reconcileCluster(ctx, cluster, update, true, true)
}

func (a *Azure) reconcileCluster(ctx context.Context, cluster *kubermaticv1.Cluster, update provider.ClusterUpdater, force bool, setTags bool) (*kubermaticv1.Cluster, error) {
	var err error
	logger := a.log.With("cluster", cluster.Name)
	location := a.dc.Location

	credentials, err := GetCredentialsForCluster(cluster.Spec.Cloud, a.secretKeySelector)
	if err != nil {
		return nil, err
	}

	clientSet, err := GetClientSet(cluster.Spec.Cloud, credentials)
	if err != nil {
		return nil, err
	}

	if force || cluster.Spec.Cloud.Azure.ResourceGroup == "" {
		logger.Infow("reconciling resource group", "resourceGroup", cluster.Spec.Cloud.Azure.ResourceGroup)
		cluster, err = reconcileResourceGroup(ctx, clientSet, location, cluster, update)
		if err != nil {
			return nil, err
		}
	}

	if force || cluster.Spec.Cloud.Azure.VNetName == "" {
		logger.Infow("reconciling vnet", "vnet", vnetName(cluster))
		cluster, err = reconcileVNet(ctx, clientSet, location, cluster, update)
		if err != nil {
			return nil, err
		}
	}

	if force || cluster.Spec.Cloud.Azure.SubnetName == "" {
		logger.Infow("reconciling subnet", "subnet", subnetName(cluster))
		cluster, err = reconcileSubnet(ctx, clientSet, location, cluster, update)
		if err != nil {
			return nil, err
		}
	}

	if force || cluster.Spec.Cloud.Azure.RouteTableName == "" {
		logger.Infow("reconciling route table", "routeTableName", routeTableName(cluster))
		cluster, err = reconcileRouteTable(ctx, clientSet, location, cluster, update)
		if err != nil {
			return nil, err
		}
	}

	if force || cluster.Spec.Cloud.Azure.SecurityGroup == "" {
		logger.Infow("reconciling security group", "securityGroup", securityGroupName(cluster))
		cluster, err = reconcileSecurityGroup(ctx, clientSet, location, cluster, update)
		if err != nil {
			return nil, err
		}
	}

	if force || cluster.Spec.Cloud.Azure.AvailabilitySet == "" {
		if cluster.Spec.Cloud.Azure.AssignAvailabilitySet == nil ||
			*cluster.Spec.Cloud.Azure.AssignAvailabilitySet {
			logger.Infow("reconciling AvailabilitySet", "availabilitySet", availabilitySetName(cluster))
			cluster, err = reconcileAvailabilitySet(ctx, clientSet, location, cluster, update)
			if err != nil {
				return nil, err
			}
		}
	}

	return cluster, nil
}

func (a *Azure) DefaultCloudSpec(ctx context.Context, cloud *kubermaticv1.CloudSpec) error {
	if cloud.Azure == nil {
		return errors.New("no Azure cloud spec found")
	}

	if cloud.Azure.LoadBalancerSKU == "" {
		cloud.Azure.LoadBalancerSKU = kubermaticv1.AzureBasicLBSKU
	}

	return nil
}

func (a *Azure) ValidateCloudSpec(ctx context.Context, cloud kubermaticv1.CloudSpec) error {
	credentials, err := GetCredentialsForCluster(cloud, a.secretKeySelector)
	if err != nil {
		return err
	}

	if cloud.Azure.ResourceGroup != "" {
		rgClient, err := getGroupsClient(cloud, credentials)
		if err != nil {
			return err
		}

		if _, err = rgClient.Get(ctx, cloud.Azure.ResourceGroup); err != nil {
			return err
		}
	}

	var resourceGroup = cloud.Azure.ResourceGroup
	if cloud.Azure.VNetResourceGroup != "" {
		resourceGroup = cloud.Azure.VNetResourceGroup
	}

	if cloud.Azure.VNetName != "" {
		vnetClient, err := getNetworksClient(cloud, credentials)
		if err != nil {
			return err
		}

		if _, err = vnetClient.Get(ctx, resourceGroup, cloud.Azure.VNetName, ""); err != nil {
			return err
		}
	}

	if cloud.Azure.SubnetName != "" {
		subnetClient, err := getSubnetsClient(cloud, credentials)
		if err != nil {
			return err
		}

		if _, err = subnetClient.Get(ctx, resourceGroup, cloud.Azure.VNetName, cloud.Azure.SubnetName, ""); err != nil {
			return err
		}
	}

	if cloud.Azure.RouteTableName != "" {
		routeTablesClient, err := getRouteTablesClient(cloud, credentials)
		if err != nil {
			return err
		}

		if _, err = routeTablesClient.Get(ctx, cloud.Azure.ResourceGroup, cloud.Azure.RouteTableName, ""); err != nil {
			return err
		}
	}

	if cloud.Azure.SecurityGroup != "" {
		sgClient, err := getSecurityGroupsClient(cloud, credentials)
		if err != nil {
			return err
		}

		if _, err = sgClient.Get(ctx, cloud.Azure.ResourceGroup, cloud.Azure.SecurityGroup, ""); err != nil {
			return err
		}
	}

	return nil
}

func (a *Azure) AddICMPRulesIfRequired(ctx context.Context, cluster *kubermaticv1.Cluster) error {
	credentials, err := GetCredentialsForCluster(cluster.Spec.Cloud, a.secretKeySelector)
	if err != nil {
		return err
	}

	azure := cluster.Spec.Cloud.Azure
	if azure.SecurityGroup == "" {
		return nil
	}
	sgClient, err := getSecurityGroupsClient(cluster.Spec.Cloud, credentials)
	if err != nil {
		return fmt.Errorf("failed to get security group client: %w", err)
	}
	sg, err := sgClient.Get(ctx, azure.ResourceGroup, azure.SecurityGroup, "")
	if err != nil {
		return fmt.Errorf("failed to get security group %q: %w", azure.SecurityGroup, err)
	}

	// we do not want to add IMCP rules to a NSG we do not own;
	// which is the case when a pre-provisioned NSG is configured.
	if !hasOwnershipTag(sg.Tags, cluster) {
		return nil
	}

	var hasDenyAllTCPRule, hasDenyAllUDPRule, hasICMPAllowAllRule bool
	if sg.SecurityRules != nil {
		for _, rule := range *sg.SecurityRules {
			if rule.Name == nil {
				continue
			}
			// We trust that no one will alter the content of the rules
			switch *rule.Name {
			case denyAllTCPSecGroupRuleName:
				hasDenyAllTCPRule = true
			case denyAllUDPSecGroupRuleName:
				hasDenyAllUDPRule = true
			case allowAllICMPSecGroupRuleName:
				hasICMPAllowAllRule = true
			}
		}
	}

	var newSecurityRules []network.SecurityRule
	if !hasDenyAllTCPRule {
		a.log.With("cluster", cluster.Name).Info("Creating TCP deny all rule")
		newSecurityRules = append(newSecurityRules, tcpDenyAllRule())
	}
	if !hasDenyAllUDPRule {
		a.log.With("cluster", cluster.Name).Info("Creating UDP deny all rule")
		newSecurityRules = append(newSecurityRules, udpDenyAllRule())
	}
	if !hasICMPAllowAllRule {
		a.log.With("cluster", cluster.Name).Info("Creating ICMP allow all rule")
		newSecurityRules = append(newSecurityRules, icmpAllowAllRule())
	}

	if len(newSecurityRules) > 0 {
		newSecurityGroupRules := append(*sg.SecurityRules, newSecurityRules...)
		sg.SecurityRules = &newSecurityGroupRules
		_, err := sgClient.CreateOrUpdate(ctx, azure.ResourceGroup, azure.SecurityGroup, sg)
		if err != nil {
			return fmt.Errorf("failed to add new rules to security group %q: %w", *sg.Name, err)
		}
	}
	return nil
}

// ValidateCloudSpecUpdate verifies whether an update of cloud spec is valid and permitted.
func (a *Azure) ValidateCloudSpecUpdate(_ context.Context, oldSpec kubermaticv1.CloudSpec, newSpec kubermaticv1.CloudSpec) error {
	if oldSpec.Azure == nil || newSpec.Azure == nil {
		return errors.New("'azure' spec is empty")
	}

	// we validate that a couple of resources are not changed.
	// the exception being the provider itself updating it in case the field
	// was left empty to dynamically generate resources.

	if oldSpec.Azure.ResourceGroup != "" && oldSpec.Azure.ResourceGroup != newSpec.Azure.ResourceGroup {
		return fmt.Errorf("updating Azure resource group is not supported (was %s, updated to %s)", oldSpec.Azure.ResourceGroup, newSpec.Azure.ResourceGroup)
	}

	if oldSpec.Azure.VNetResourceGroup != "" && oldSpec.Azure.VNetResourceGroup != newSpec.Azure.VNetResourceGroup {
		return fmt.Errorf("updating Azure vnet resource group is not supported (was %s, updated to %s)", oldSpec.Azure.VNetResourceGroup, newSpec.Azure.VNetResourceGroup)
	}

	if oldSpec.Azure.VNetName != "" && oldSpec.Azure.VNetName != newSpec.Azure.VNetName {
		return fmt.Errorf("updating Azure vnet name is not supported (was %s, updated to %s)", oldSpec.Azure.VNetName, newSpec.Azure.VNetName)
	}

	if oldSpec.Azure.SubnetName != "" && oldSpec.Azure.SubnetName != newSpec.Azure.SubnetName {
		return fmt.Errorf("updating Azure subnet name is not supported (was %s, updated to %s)", oldSpec.Azure.SubnetName, newSpec.Azure.SubnetName)
	}

	if oldSpec.Azure.RouteTableName != "" && oldSpec.Azure.RouteTableName != newSpec.Azure.RouteTableName {
		return fmt.Errorf("updating Azure route table name is not supported (was %s, updated to %s)", oldSpec.Azure.RouteTableName, newSpec.Azure.RouteTableName)
	}

	if oldSpec.Azure.SecurityGroup != "" && oldSpec.Azure.SecurityGroup != newSpec.Azure.SecurityGroup {
		return fmt.Errorf("updating Azure security group is not supported (was %s, updated to %s)", oldSpec.Azure.SecurityGroup, newSpec.Azure.SecurityGroup)
	}

	if oldSpec.Azure.AvailabilitySet != "" && oldSpec.Azure.AvailabilitySet != newSpec.Azure.AvailabilitySet {
		return fmt.Errorf("updating Azure availability set is not supported (was %s, updated to %s)", oldSpec.Azure.AvailabilitySet, newSpec.Azure.AvailabilitySet)
	}

	return nil
}

type Credentials struct {
	TenantID       string
	SubscriptionID string
	ClientID       string
	ClientSecret   string
}
