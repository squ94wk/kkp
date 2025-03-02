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

package validation

import (
	"context"
	"errors"
	"fmt"
	"net"

	semverlib "github.com/Masterminds/semver/v3"
	"github.com/coreos/locksmith/pkg/timeutil"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	"k8c.io/kubermatic/v2/pkg/features"
	kuberneteshelper "k8c.io/kubermatic/v2/pkg/kubernetes"
	"k8c.io/kubermatic/v2/pkg/provider"
	"k8c.io/kubermatic/v2/pkg/resources"
	"k8c.io/kubermatic/v2/pkg/version"
	"k8c.io/kubermatic/v2/pkg/version/cni"

	"k8s.io/apimachinery/pkg/api/equality"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	kubenetutil "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var (
	// ErrCloudChangeNotAllowed describes that it is not allowed to change the cloud provider.
	ErrCloudChangeNotAllowed  = errors.New("not allowed to change the cloud provider")
	azureLoadBalancerSKUTypes = sets.NewString("", string(kubermaticv1.AzureStandardLBSKU), string(kubermaticv1.AzureBasicLBSKU))

	// UnsafeCNIUpgradeLabel allows unsafe CNI version upgrade (difference in versions more than one minor version).
	UnsafeCNIUpgradeLabel = "unsafe-cni-upgrade"
	// UnsafeCNIMigrationLabel allows unsafe CNI type migration.
	UnsafeCNIMigrationLabel = "unsafe-cni-migration"
)

// ValidateClusterSpec validates the given cluster spec. If this is not called from within another validation
// routine, parentFieldPath can be nil.
func ValidateClusterSpec(spec *kubermaticv1.ClusterSpec, dc *kubermaticv1.Datacenter, enabledFeatures features.FeatureGate, versions []*version.Version, parentFieldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if spec.HumanReadableName == "" {
		allErrs = append(allErrs, field.Required(parentFieldPath.Child("humanReadableName"), "no name specified"))
	}

	if spec.Version.Semver() == nil || spec.Version.String() == "" {
		allErrs = append(allErrs, field.Required(parentFieldPath.Child("version"), "version is required but was not specified"))
	}

	var (
		validVersions []string
		versionValid  bool
	)

	for _, availableVersion := range versions {
		validVersions = append(validVersions, availableVersion.Version.String())
		if spec.Version.Semver().Equal(availableVersion.Version) {
			versionValid = true
			break
		}
	}

	if !versionValid {
		allErrs = append(allErrs, field.NotSupported(parentFieldPath.Child("version"), spec.Version.String(), validVersions))
	}

	if !kubermaticv1.AllExposeStrategies.Has(spec.ExposeStrategy) {
		allErrs = append(allErrs, field.NotSupported(parentFieldPath.Child("exposeStrategy"), spec.ExposeStrategy, kubermaticv1.AllExposeStrategies.Items()))
	}

	if spec.ExposeStrategy == kubermaticv1.ExposeStrategyTunneling && !enabledFeatures.Enabled(features.TunnelingExposeStrategy) {
		allErrs = append(allErrs, field.Forbidden(parentFieldPath.Child("exposeStrategy"), "cannot create cluster with Tunneling expose strategy because the TunnelingExposeStrategy feature gate is not enabled"))
	}

	if spec.CNIPlugin != nil {
		if !cni.GetSupportedCNIPlugins().Has(spec.CNIPlugin.Type.String()) {
			allErrs = append(allErrs, field.NotSupported(parentFieldPath.Child("cniPlugin", "type"), spec.CNIPlugin.Type.String(), cni.GetSupportedCNIPlugins().List()))
		} else if versions, err := cni.GetAllowedCNIPluginVersions(spec.CNIPlugin.Type); err != nil || !versions.Has(spec.CNIPlugin.Version) {
			allErrs = append(allErrs, field.NotSupported(parentFieldPath.Child("cniPlugin", "version"), spec.CNIPlugin.Version, versions.List()))
		}

		// Dual-stack is not supported on Canal < v3.22
		if spec.ClusterNetwork.IPFamily == kubermaticv1.IPFamilyDualStack && spec.CNIPlugin.Type == kubermaticv1.CNIPluginTypeCanal {
			gte322Constraint, _ := semverlib.NewConstraint(">= 3.22")
			cniVer, _ := semverlib.NewVersion(spec.CNIPlugin.Version)
			if cniVer != nil && !gte322Constraint.Check(cniVer) {
				allErrs = append(allErrs, field.Forbidden(parentFieldPath.Child("cniPlugin"), "dual-stack not allowed on Canal CNI version lower than 3.22"))
			}
		}
	}

	allErrs = append(allErrs, ValidateLeaderElectionSettings(&spec.ComponentsOverride.ControllerManager.LeaderElectionSettings, parentFieldPath.Child("componentsOverride", "controllerManager", "leaderElection"))...)
	allErrs = append(allErrs, ValidateLeaderElectionSettings(&spec.ComponentsOverride.Scheduler.LeaderElectionSettings, parentFieldPath.Child("componentsOverride", "scheduler", "leaderElection"))...)

	// general cloud spec logic
	if errs := ValidateCloudSpec(spec.Cloud, dc, parentFieldPath.Child("cloud")); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	if errs := validateMachineNetworksFromClusterSpec(spec, parentFieldPath); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	if errs := ValidateClusterNetworkConfig(&spec.ClusterNetwork, spec.CNIPlugin, parentFieldPath.Child("networkConfig")); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	portRangeFld := field.NewPath("componentsOverride", "apiserver", "nodePortRange")
	if err := ValidateNodePortRange(spec.ComponentsOverride.Apiserver.NodePortRange, portRangeFld); err != nil {
		allErrs = append(allErrs, err)
	}

	if errs := validateEncryptionConfiguration(spec, parentFieldPath.Child("encryptionConfiguration")); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	return allErrs
}

func ValidateNewClusterSpec(ctx context.Context, spec *kubermaticv1.ClusterSpec, dc *kubermaticv1.Datacenter, cloudProvider provider.CloudProvider, versionManager *version.Manager, enabledFeatures features.FeatureGate, parentFieldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	versions, err := versionManager.GetVersionsForProvider(kubermaticv1.ProviderType(spec.Cloud.ProviderName))
	if err != nil {
		allErrs = append(allErrs, field.InternalError(parentFieldPath.Child("version"), fmt.Errorf("failed to get available versions: %w", err)))
	}

	if errs := ValidateClusterSpec(spec, dc, enabledFeatures, versions, parentFieldPath); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	if cloudProvider != nil {
		if err := cloudProvider.ValidateCloudSpec(ctx, spec.Cloud); err != nil {
			// Just using spec.Cloud for the error leads to a Go-representation of the struct being printed in
			// the error message, which looks awful an is not helpful. However any other encoding (e.g. JSON)
			// could lead to us leaking credentials that were given in the CloudSpec, so to be safe, we never
			// reveal the CloudSpec in an error.
			allErrs = append(allErrs, field.Invalid(parentFieldPath.Child("cloud"), "<redacted>", err.Error()))
		}
	}

	return allErrs
}

// ValidateClusterUpdate validates the new cluster and if no forbidden changes were attempted.
func ValidateClusterUpdate(ctx context.Context, newCluster, oldCluster *kubermaticv1.Cluster, dc *kubermaticv1.Datacenter, cloudProvider provider.CloudProvider, versionManager *version.Manager, features features.FeatureGate) field.ErrorList {
	specPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	versions, err := versionManager.GetVersionsForProvider(kubermaticv1.ProviderType(oldCluster.Spec.Cloud.ProviderName))
	if err != nil {
		allErrs = append(allErrs, field.InternalError(specPath.Child("version"), fmt.Errorf("failed to get available versions: %w", err)))
	}

	// perform general basic checks on the new cluster spec
	if errs := ValidateClusterSpec(&newCluster.Spec, dc, features, versions, specPath); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	if cloudProvider != nil {
		if err := cloudProvider.ValidateCloudSpecUpdate(ctx, oldCluster.Spec.Cloud, newCluster.Spec.Cloud); err != nil {
			allErrs = append(allErrs, field.Forbidden(specPath.Child("cloud"), err.Error()))
		}
	}

	// ensure neither cloud nor datacenter were changed
	if err := ValidateCloudChange(newCluster.Spec.Cloud, oldCluster.Spec.Cloud); err != nil {
		allErrs = append(allErrs, field.Forbidden(specPath.Child("cloud"), err.Error()))
	}

	if newCluster.Address.AdminToken != "" {
		if err := kuberneteshelper.ValidateKubernetesToken(newCluster.Address.AdminToken); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("address", "adminToken"), newCluster.Address.AdminToken, err.Error()))
		}
	}

	// Validate ExternalCloudProvider feature flag immutability.
	// Once the feature flag is enabled, it must not be disabled.
	if vOld, v := oldCluster.Spec.Features[kubermaticv1.ClusterFeatureExternalCloudProvider],
		newCluster.Spec.Features[kubermaticv1.ClusterFeatureExternalCloudProvider]; vOld && !v {
		allErrs = append(allErrs, field.Invalid(specPath.Child("features").Key(kubermaticv1.ClusterFeatureExternalCloudProvider), v, fmt.Sprintf("feature gate %q cannot be disabled once it's enabled", kubermaticv1.ClusterFeatureExternalCloudProvider)))
	}

	// Validate EtcdLauncher feature flag immutability.
	// Once the feature flag is enabled, it must not be disabled.
	if vOld, v := oldCluster.Spec.Features[kubermaticv1.ClusterFeatureEtcdLauncher],
		newCluster.Spec.Features[kubermaticv1.ClusterFeatureEtcdLauncher]; vOld && !v {
		allErrs = append(allErrs, field.Invalid(specPath.Child("features").Key(kubermaticv1.ClusterFeatureEtcdLauncher), v, fmt.Sprintf("feature gate %q cannot be disabled once it's enabled", kubermaticv1.ClusterFeatureEtcdLauncher)))
	}

	if oldCluster.Spec.ExposeStrategy != "" {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			newCluster.Spec.ExposeStrategy,
			oldCluster.Spec.ExposeStrategy,
			specPath.Child("exposeStrategy"),
		)...)
	}

	if oldCluster.Spec.ComponentsOverride.Apiserver.NodePortRange != "" {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			newCluster.Spec.ComponentsOverride.Apiserver.NodePortRange,
			oldCluster.Spec.ComponentsOverride.Apiserver.NodePortRange,
			specPath.Child("componentsOverride", "apiserver", "nodePortRange"),
		)...)
	}

	if oldCluster.Spec.EnableUserSSHKeyAgent != nil {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			newCluster.Spec.EnableUserSSHKeyAgent,
			oldCluster.Spec.EnableUserSSHKeyAgent,
			specPath.Child("enableUserSSHKeyAgent"),
		)...)
	} else if newCluster.Spec.EnableUserSSHKeyAgent != nil && !*newCluster.Spec.EnableUserSSHKeyAgent {
		path := field.NewPath("cluster", "spec", "enableUserSSHKeyAgent")
		allErrs = append(allErrs, field.Invalid(path, *newCluster.Spec.EnableUserSSHKeyAgent, "UserSSHKey agent is enabled by default for user clusters created prior KKP 2.16 version"))
	}

	// EnableOperatingSystemManager is immutable field as of now but in future this field will be mutable
	if oldCluster.Spec.EnableOperatingSystemManager != newCluster.Spec.EnableOperatingSystemManager {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			newCluster.Spec.EnableOperatingSystemManager,
			oldCluster.Spec.EnableOperatingSystemManager,
			specPath.Child("enableOperatingSystemManager"),
		)...)
	}

	allErrs = append(allErrs, validateClusterNetworkingConfigUpdateImmutability(&newCluster.Spec.ClusterNetwork, &oldCluster.Spec.ClusterNetwork, specPath.Child("clusterNetwork"))...)

	// even though ErrorList later in ToAggregate() will filter out nil errors, it does so by
	// stringifying them. A field.Error that is nil will panic when doing so, so one cannot simply
	// append a nil *field.Error to allErrs.
	if err := validateCNIUpdate(newCluster.Spec.CNIPlugin, oldCluster.Spec.CNIPlugin, newCluster.Labels); err != nil {
		allErrs = append(allErrs, err)
	}

	if errs := validateEncryptionUpdate(newCluster, oldCluster); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	if !equality.Semantic.DeepEqual(newCluster.TypeMeta, oldCluster.TypeMeta) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("typeMeta"), "type meta cannot be changed"))
	}

	return allErrs
}

func ValidateClusterNetworkConfig(n *kubermaticv1.ClusterNetworkingConfig, cni *kubermaticv1.CNIPluginSettings, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// Maximum 2 (one IPv4 + one IPv6) CIDR blocks are allowed
	if len(n.Pods.CIDRBlocks) > 2 {
		allErrs = append(allErrs, field.TooMany(fldPath.Child("pods", "cidrBlocks"), len(n.Pods.CIDRBlocks), 2))
	}
	if len(n.Services.CIDRBlocks) > 2 {
		allErrs = append(allErrs, field.TooMany(fldPath.Child("services", "cidrBlocks"), len(n.Services.CIDRBlocks), 2))
	}
	if len(n.Pods.CIDRBlocks) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("pods", "cidrBlocks"), "pod CIDR must be provided"))
	}
	if len(n.Services.CIDRBlocks) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("services", "cidrBlocks"), "service CIDR must be provided"))
	}
	if len(n.Pods.CIDRBlocks) < len(n.Services.CIDRBlocks) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("pods", "cidrBlocks"), n.Pods.CIDRBlocks,
			fmt.Sprintf("%d pod CIDRs must be provided", len(n.Services.CIDRBlocks))),
		)
	}
	if len(n.Services.CIDRBlocks) < len(n.Pods.CIDRBlocks) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("services", "cidrBlocks"), n.Services.CIDRBlocks,
			fmt.Sprintf("%d services CIDRs must be provided", len(n.Pods.CIDRBlocks))),
		)
	}

	// Verify that provided CIDRs are well-formed
	if err := validateClusterCIDRBlocks(n.Pods.CIDRBlocks, fldPath.Child("pods", "cidrBlocks")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := validateClusterCIDRBlocks(n.Services.CIDRBlocks, fldPath.Child("services", "cidrBlocks")); err != nil {
		allErrs = append(allErrs, err)
	}

	// Verify that IP family is consistent with provided pod CIDRs
	if (n.IPFamily == kubermaticv1.IPFamilyIPv4) && len(n.Pods.CIDRBlocks) != 1 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("ipFamily"), n.IPFamily,
			fmt.Sprintf("IP family %q does not match with provided pods CIDRs %q", n.IPFamily, n.Pods.CIDRBlocks)),
		)
	}
	if n.IPFamily == kubermaticv1.IPFamilyDualStack && len(n.Pods.CIDRBlocks) != 2 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("ipFamily"), n.IPFamily,
			fmt.Sprintf("IP family %q does not match with provided pods CIDRs %q", n.IPFamily, n.Pods.CIDRBlocks)),
		)
	}

	// Verify that node CIDR mask sizes are longer than the mask size of pod CIDRs
	if err := validateNodeCIDRMaskSize(n.NodeCIDRMaskSizeIPv4, n.Pods.GetIPv4CIDR(), fldPath.Child("nodeCidrMaskSizeIPv4")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := validateNodeCIDRMaskSize(n.NodeCIDRMaskSizeIPv6, n.Pods.GetIPv6CIDR(), fldPath.Child("nodeCidrMaskSizeIPv6")); err != nil {
		allErrs = append(allErrs, err)
	}

	// TODO Remove all hardcodes before allowing arbitrary domain names.
	if n.DNSDomain != "cluster.local" {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("dnsDomain"), n.DNSDomain, "dnsDomain must be 'cluster.local'"))
	}

	if n.ProxyMode != resources.IPVSProxyMode && n.ProxyMode != resources.IPTablesProxyMode && n.ProxyMode != resources.EBPFProxyMode {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("proxyMode"), n.ProxyMode,
			[]string{resources.IPVSProxyMode, resources.IPTablesProxyMode, resources.EBPFProxyMode}))
	}

	if n.ProxyMode == resources.EBPFProxyMode && (cni == nil || cni.Type != kubermaticv1.CNIPluginTypeCilium) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("proxyMode"), n.ProxyMode,
			fmt.Sprintf("%s proxy mode is valid only for %s CNI", resources.EBPFProxyMode, kubermaticv1.CNIPluginTypeCilium)))
	}

	if n.ProxyMode == resources.EBPFProxyMode && (n.KonnectivityEnabled == nil || !*n.KonnectivityEnabled) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("proxyMode"), n.ProxyMode,
			fmt.Sprintf("%s proxy mode can be used only when Konnectivity is enabled", resources.EBPFProxyMode)))
	}

	return allErrs
}

func validateEncryptionConfiguration(spec *kubermaticv1.ClusterSpec, fieldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if spec.EncryptionConfiguration != nil && spec.EncryptionConfiguration.Enabled {
		if enabled, ok := spec.Features[kubermaticv1.ClusterFeatureEncryptionAtRest]; !ok || !enabled {
			allErrs = append(allErrs, field.Forbidden(fieldPath.Child("enabled"),
				fmt.Sprintf("cannot enable encryption configuration if feature gate '%s' is not set", kubermaticv1.ClusterFeatureEncryptionAtRest)))
		}

		// TODO: Update with implementations of other encryption providers (KMS)

		if spec.EncryptionConfiguration.Secretbox == nil {
			allErrs = append(allErrs, field.Required(fieldPath.Child("secretbox"),
				"exactly one encryption provider (secretbox, kms) needs to be configured"))
		} else {
			for i, key := range spec.EncryptionConfiguration.Secretbox.Keys {
				childPath := fieldPath.Child("secretbox", "keys").Index(i)
				if key.Name == "" {
					allErrs = append(allErrs, field.Required(childPath.Child("name"),
						"secretbox key name is required"))
				}

				if key.Value == "" && key.SecretRef == nil {
					allErrs = append(allErrs, field.Required(childPath,
						"either 'value' or 'secretRef' must be set"))
				}

				if key.Value != "" && key.SecretRef != nil {
					allErrs = append(allErrs, field.Invalid(childPath, key,
						"'value' and 'secretRef' cannot be set at the same time"))
				}
			}
		}

		// END TODO
	}

	return allErrs
}

func validateEncryptionUpdate(oldCluster *kubermaticv1.Cluster, newCluster *kubermaticv1.Cluster) field.ErrorList {
	allErrs := field.ErrorList{}

	if enabled, ok := oldCluster.Spec.Features[kubermaticv1.ClusterFeatureEncryptionAtRest]; ok && enabled {
		if oldCluster.Status.Encryption != nil {
			if oldCluster.Status.Encryption.Phase != "" && oldCluster.Status.Encryption.Phase != kubermaticv1.ClusterEncryptionPhaseActive {
				if !equality.Semantic.DeepEqual(oldCluster.Spec.EncryptionConfiguration, newCluster.Spec.EncryptionConfiguration) {
					allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "encryptionConfiguration"),
						fmt.Sprintf("no changes to encryption configuration are allowed while encryption phase is '%s'", oldCluster.Status.Encryption.Phase),
					))
				}
			}

			encryptionConfigExists :=
				oldCluster.Spec.EncryptionConfiguration != nil &&
					newCluster.Spec.EncryptionConfiguration != nil

			if encryptionConfigExists {
				encryptionConfigEnabled :=
					oldCluster.Spec.EncryptionConfiguration.Enabled &&
						newCluster.Spec.EncryptionConfiguration.Enabled

				if encryptionConfigEnabled && !equality.Semantic.DeepEqual(oldCluster.Spec.EncryptionConfiguration.Resources, newCluster.Spec.EncryptionConfiguration.Resources) {
					allErrs = append(
						allErrs,
						field.Forbidden(
							field.NewPath("spec", "encryptionConfiguration", "resources"),
							"list of encrypted resources cannot be changed. Please disable encryption and re-configure",
						),
					)
				}
			}
		}
	}

	// prevent removing the feature flag while the cluster is still in some encryption-active configuration or state
	if enabled, ok := newCluster.Spec.Features[kubermaticv1.ClusterFeatureEncryptionAtRest]; (!ok || !enabled) && (newCluster.IsEncryptionEnabled() || newCluster.IsEncryptionActive()) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("features"),
			fmt.Sprintf("cannot disable %q feature flag while encryption is still configured or active", kubermaticv1.ClusterFeatureEncryptionAtRest),
		))
	}

	return allErrs
}

func validateClusterCIDRBlocks(cidrBlocks []string, fldPath *field.Path) *field.Error {
	for i, cidr := range cidrBlocks {
		addr, _, err := net.ParseCIDR(cidr)
		if err != nil {
			return field.Invalid(fldPath.Index(i), cidr, fmt.Sprintf("couldn't parse CIDR %q: %v", cidr, err))
		}
		// At this point, KKP only supports IPv4 as the primary CIDR and IPv6 as the secondary CIDR.
		// The first provided CIDR has to be IPv4
		if i == 0 && addr.To4() == nil {
			return field.Invalid(fldPath.Child("pods", "cidrBlocks").Index(i), cidr,
				fmt.Sprintf("invalid address family for primary CIDR %q: has to be IPv4", cidr))
		}
		// The second provided CIDR has to be IPv6
		if i == 1 && addr.To4() != nil {
			return field.Invalid(fldPath.Child("pods", "cidrBlocks").Index(i), cidr,
				fmt.Sprintf("invalid address family for secondary CIDR %q: has to be IPv6", cidr))
		}
	}
	return nil
}

func validateNodeCIDRMaskSize(nodeCIDRMaskSize *int32, podCIDR string, fldPath *field.Path) *field.Error {
	if podCIDR == "" || nodeCIDRMaskSize == nil {
		return nil
	}
	_, podCIDRNet, err := net.ParseCIDR(podCIDR)
	if err != nil {
		return field.Invalid(fldPath, podCIDR, fmt.Sprintf("couldn't parse CIDR %q: %v", podCIDR, err))
	}
	podCIDRMaskSize, _ := podCIDRNet.Mask.Size()

	if int32(podCIDRMaskSize) >= *nodeCIDRMaskSize {
		return field.Invalid(fldPath, nodeCIDRMaskSize,
			fmt.Sprintf("node CIDR mask size (%d) must be longer than the mask size of the pod CIDR (%q)", *nodeCIDRMaskSize, podCIDR))
	}
	return nil
}

func validateMachineNetworksFromClusterSpec(spec *kubermaticv1.ClusterSpec, parentFieldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	networks := spec.MachineNetworks
	basePath := parentFieldPath.Child("machineNetworks")

	if len(networks) == 0 {
		return allErrs
	}

	if len(networks) > 0 && spec.Cloud.VSphere == nil {
		allErrs = append(allErrs, field.Invalid(basePath, networks, "machine networks are only supported with the vSphere provider"))
	}

	for i, network := range networks {
		_, _, err := net.ParseCIDR(network.CIDR)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(basePath.Index(i), network.CIDR, fmt.Sprintf("could not parse CIDR: %v", err)))
		}

		if net.ParseIP(network.Gateway) == nil {
			allErrs = append(allErrs, field.Invalid(basePath.Index(i), network.Gateway, fmt.Sprintf("could not parse gateway: %v", err)))
		}

		if len(network.DNSServers) > 0 {
			for j, dnsServer := range network.DNSServers {
				if net.ParseIP(dnsServer) == nil {
					allErrs = append(allErrs, field.Invalid(basePath.Index(i).Child("dnsServers").Index(j), dnsServer, fmt.Sprintf("could not parse DNS server: %v", err)))
				}
			}
		}
	}

	return allErrs
}

// ValidateCloudChange validates if the cloud provider has been changed.
func ValidateCloudChange(newSpec, oldSpec kubermaticv1.CloudSpec) error {
	if newSpec.DatacenterName != oldSpec.DatacenterName {
		return errors.New("changing the datacenter is not allowed")
	}

	oldCloudProvider, err := provider.ClusterCloudProviderName(oldSpec)
	if err != nil {
		return fmt.Errorf("could not determine old cloud provider: %w", err)
	}

	newCloudProvider, err := provider.ClusterCloudProviderName(newSpec)
	if err != nil {
		return fmt.Errorf("could not determine new cloud provider: %w", err)
	}

	if oldCloudProvider != newCloudProvider {
		return ErrCloudChangeNotAllowed
	}

	return nil
}

// ValidateCloudSpec validates if the cloud spec is valid
// If this is not called from within another validation
// routine, parentFieldPath can be nil.
func ValidateCloudSpec(spec kubermaticv1.CloudSpec, dc *kubermaticv1.Datacenter, parentFieldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if spec.DatacenterName == "" {
		allErrs = append(allErrs, field.Required(parentFieldPath.Child("dc"), "no node datacenter specified"))
	}

	providerName, err := provider.ClusterCloudProviderName(spec)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(parentFieldPath, "<redacted>", err.Error()))
	}

	// if this field is set, it must match the given provider;
	// if the field is not set, the mutation webhook will fill it in
	if spec.ProviderName != "" {
		if spec.ProviderName != providerName {
			msg := fmt.Sprintf("expected providerName to be %q", providerName)
			allErrs = append(allErrs, field.Invalid(parentFieldPath.Child("providerName"), spec.ProviderName, msg))
		}
	}

	if dc != nil {
		clusterCloudProvider, err := provider.ClusterCloudProviderName(spec)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(parentFieldPath, nil, fmt.Sprintf("could not determine cluster cloud provider: %v", err)))
		}

		dcCloudProvider, err := provider.DatacenterCloudProviderName(&dc.Spec)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(parentFieldPath, nil, fmt.Sprintf("could not determine datacenter cloud provider: %v", err)))
		}

		// this should never happen, unless the caller did the wrong thing
		// (i.e. user input should never lead to this place)
		if clusterCloudProvider != dcCloudProvider {
			allErrs = append(allErrs, field.Invalid(parentFieldPath, nil, fmt.Sprintf("expected datacenter provider to be %q, but got %q", clusterCloudProvider, dcCloudProvider)))
		}
	}

	var providerErr error

	switch {
	case spec.AWS != nil:
		providerErr = validateAWSCloudSpec(spec.AWS)
	case spec.Alibaba != nil:
		providerErr = validateAlibabaCloudSpec(spec.Alibaba)
	case spec.Anexia != nil:
		providerErr = validateAnexiaCloudSpec(spec.Anexia)
	case spec.Azure != nil:
		providerErr = validateAzureCloudSpec(spec.Azure)
	case spec.BringYourOwn != nil:
		providerErr = nil
	case spec.Digitalocean != nil:
		providerErr = validateDigitaloceanCloudSpec(spec.Digitalocean)
	case spec.Fake != nil:
		providerErr = validateFakeCloudSpec(spec.Fake)
	case spec.GCP != nil:
		providerErr = validateGCPCloudSpec(spec.GCP)
	case spec.Hetzner != nil:
		providerErr = validateHetznerCloudSpec(spec.Hetzner)
	case spec.Kubevirt != nil:
		providerErr = validateKubevirtCloudSpec(spec.Kubevirt)
	case spec.Openstack != nil:
		providerErr = validateOpenStackCloudSpec(spec.Openstack, dc)
	case spec.Packet != nil:
		providerErr = validatePacketCloudSpec(spec.Packet)
	case spec.VSphere != nil:
		providerErr = validateVSphereCloudSpec(spec.VSphere)
	case spec.Nutanix != nil:
		providerErr = validateNutanixCloudSpec(spec.Nutanix)
	case spec.VMwareCloudDirector != nil:
		providerErr = validateVMwareCloudDirectorCloudSpec(spec.VMwareCloudDirector)
	default:
		providerErr = errors.New("no cloud provider specified")
	}

	if providerErr != nil {
		allErrs = append(allErrs, field.Invalid(parentFieldPath, "<redacted>", providerErr.Error()))
	}

	return allErrs
}

func validateOpenStackCloudSpec(spec *kubermaticv1.OpenstackCloudSpec, dc *kubermaticv1.Datacenter) error {
	// validate applicationCredentials
	if spec.ApplicationCredentialID != "" && spec.ApplicationCredentialSecret == "" {
		return errors.New("no applicationCredentialSecret specified")
	}
	if spec.ApplicationCredentialID != "" && spec.ApplicationCredentialSecret != "" {
		return nil
	}

	if spec.Domain == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.OpenstackDomain); err != nil {
			return err
		}
	}
	if spec.Username == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.OpenstackUsername); err != nil {
			return err
		}
	}
	if spec.Password == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.OpenstackPassword); err != nil {
			return err
		}
	}
	if spec.NodePortsAllowedIPRange != "" {
		if _, _, err := net.ParseCIDR(spec.NodePortsAllowedIPRange); err != nil {
			return err
		}
	}
	if err := spec.NodePortsAllowedIPRanges.Validate(); err != nil {
		return err
	}

	var errs []error
	if spec.Project == "" && spec.CredentialsReference != nil && spec.CredentialsReference.Name != "" && spec.CredentialsReference.Namespace == "" {
		errs = append(errs, fmt.Errorf("%q and %q cannot be empty at the same time", resources.OpenstackProject, resources.OpenstackTenant))
	}
	if spec.ProjectID == "" && spec.CredentialsReference != nil && spec.CredentialsReference.Name != "" && spec.CredentialsReference.Namespace == "" {
		errs = append(errs, fmt.Errorf("%q and %q cannot be empty at the same time", resources.OpenstackProjectID, resources.OpenstackTenantID))
	}
	if len(errs) > 0 {
		return errors.New("no tenant name or ID specified")
	}

	if dc != nil && spec.FloatingIPPool == "" && dc.Spec.Openstack != nil && dc.Spec.Openstack.EnforceFloatingIP {
		return errors.New("no floating ip pool specified")
	}

	return nil
}

func validateAWSCloudSpec(spec *kubermaticv1.AWSCloudSpec) error {
	if spec.AccessKeyID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AWSAccessKeyID); err != nil {
			return err
		}
	}
	if spec.SecretAccessKey == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AWSSecretAccessKey); err != nil {
			return err
		}
	}
	if spec.NodePortsAllowedIPRange != "" {
		if _, _, err := net.ParseCIDR(spec.NodePortsAllowedIPRange); err != nil {
			return err
		}
	}
	if err := spec.NodePortsAllowedIPRanges.Validate(); err != nil {
		return err
	}

	return nil
}

func validateGCPCloudSpec(spec *kubermaticv1.GCPCloudSpec) error {
	if spec.ServiceAccount == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.GCPServiceAccount); err != nil {
			return err
		}
	}
	if spec.NodePortsAllowedIPRange != "" {
		if _, _, err := net.ParseCIDR(spec.NodePortsAllowedIPRange); err != nil {
			return err
		}
	}
	if err := spec.NodePortsAllowedIPRanges.Validate(); err != nil {
		return err
	}
	return nil
}

func validateHetznerCloudSpec(spec *kubermaticv1.HetznerCloudSpec) error {
	if spec.Token == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.HetznerToken); err != nil {
			return err
		}
	}

	return nil
}

func validatePacketCloudSpec(spec *kubermaticv1.PacketCloudSpec) error {
	if spec.APIKey == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.PacketAPIKey); err != nil {
			return err
		}
	}
	if spec.ProjectID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.PacketProjectID); err != nil {
			return err
		}
	}
	return nil
}

func validateVSphereCloudSpec(spec *kubermaticv1.VSphereCloudSpec) error {
	if spec.Username == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VsphereUsername); err != nil {
			return err
		}
	}
	if spec.Password == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VspherePassword); err != nil {
			return err
		}
	}

	return nil
}

func validateVMwareCloudDirectorCloudSpec(spec *kubermaticv1.VMwareCloudDirectorCloudSpec) error {
	if spec.Username == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VMwareCloudDirectorUsername); err != nil {
			return err
		}
	}
	if spec.Password == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VMwareCloudDirectorPassword); err != nil {
			return err
		}
	}
	if spec.Organization == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VMwareCloudDirectorOrganization); err != nil {
			return err
		}
	}
	if spec.VDC == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.VMwareCloudDirectorVDC); err != nil {
			return err
		}
	}

	return nil
}

func validateAzureCloudSpec(spec *kubermaticv1.AzureCloudSpec) error {
	if spec.TenantID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AzureTenantID); err != nil {
			return err
		}
	}
	if spec.SubscriptionID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AzureSubscriptionID); err != nil {
			return err
		}
	}
	if spec.ClientID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AzureClientID); err != nil {
			return err
		}
	}
	if spec.ClientSecret == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AzureClientSecret); err != nil {
			return err
		}
	}
	if !azureLoadBalancerSKUTypes.Has(string(spec.LoadBalancerSKU)) {
		return fmt.Errorf("azure LB SKU cannot be %q, allowed values are %v", spec.LoadBalancerSKU, azureLoadBalancerSKUTypes.List())
	}
	if spec.NodePortsAllowedIPRange != "" {
		if _, _, err := net.ParseCIDR(spec.NodePortsAllowedIPRange); err != nil {
			return err
		}
	}
	if err := spec.NodePortsAllowedIPRanges.Validate(); err != nil {
		return err
	}

	return nil
}

func validateDigitaloceanCloudSpec(spec *kubermaticv1.DigitaloceanCloudSpec) error {
	if spec.Token == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no token or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.DigitaloceanToken); err != nil {
			return err
		}
	}

	return nil
}

func validateFakeCloudSpec(spec *kubermaticv1.FakeCloudSpec) error {
	if spec.Token == "" {
		return errors.New("no token specified")
	}

	return nil
}

func validateKubevirtCloudSpec(spec *kubermaticv1.KubevirtCloudSpec) error {
	if spec.Kubeconfig == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.KubevirtKubeConfig); err != nil {
			return err
		}
	}

	return nil
}

func validateAlibabaCloudSpec(spec *kubermaticv1.AlibabaCloudSpec) error {
	if spec.AccessKeyID == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AlibabaAccessKeyID); err != nil {
			return err
		}
	}
	if spec.AccessKeySecret == "" {
		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AlibabaAccessKeySecret); err != nil {
			return err
		}
	}
	return nil
}

func validateAnexiaCloudSpec(spec *kubermaticv1.AnexiaCloudSpec) error {
	if spec.Token == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no token or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.AnexiaToken); err != nil {
			return err
		}
	}

	return nil
}

func validateNutanixCloudSpec(spec *kubermaticv1.NutanixCloudSpec) error {
	if spec.Username == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no username or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.NutanixUsername); err != nil {
			return err
		}
	}

	if spec.Password == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no password or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.NutanixPassword); err != nil {
			return err
		}
	}

	if spec.ClusterName == "" {
		return errors.New("no cluster name specified")
	}

	if spec.CSI == nil {
		return nil
	}

	// validate csi
	if spec.CSI.Username == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no CSI username or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.NutanixCSIUsername); err != nil {
			return err
		}
	}

	if spec.CSI.Password == "" {
		if spec.CredentialsReference == nil {
			return errors.New("no CSI password or credentials reference specified")
		}

		if err := kuberneteshelper.ValidateSecretKeySelector(spec.CredentialsReference, resources.NutanixCSIPassword); err != nil {
			return err
		}
	}

	if spec.CSI.Endpoint == "" {
		return errors.New("CSI Endpoint mut not be empty")
	}

	// should never happen due to defaulting
	if spec.CSI.Port == nil {
		return errors.New("CSI Port mut not be empty")
	}

	return nil
}

func ValidateUpdateWindow(updateWindow *kubermaticv1.UpdateWindow) error {
	if updateWindow != nil && updateWindow.Start != "" && updateWindow.Length != "" {
		_, err := timeutil.ParsePeriodic(updateWindow.Start, updateWindow.Length)
		if err != nil {
			return fmt.Errorf("error parsing update window: %w", err)
		}
	}
	return nil
}

func ValidateContainerRuntime(spec *kubermaticv1.ClusterSpec) error {
	if !sets.NewString("docker", "containerd").Has(spec.ContainerRuntime) {
		return fmt.Errorf("container runtime not supported: %s", spec.ContainerRuntime)
	}

	// Docker is supported until 1.24.0, excluding 1.24.0
	gteKube124Condition, _ := semverlib.NewConstraint(">= 1.24")
	if spec.ContainerRuntime == "docker" && gteKube124Condition.Check(spec.Version.Semver()) {
		return fmt.Errorf("docker not supported from version 1.24: %s", spec.ContainerRuntime)
	}

	return nil
}

func ValidateLeaderElectionSettings(l *kubermaticv1.LeaderElectionSettings, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if l.LeaseDurationSeconds != nil && *l.LeaseDurationSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("leaseDurationSeconds"), l.LeaseDurationSeconds, "lease duration seconds cannot be negative"))
	}
	if l.RenewDeadlineSeconds != nil && *l.RenewDeadlineSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("renewDeadlineSeconds"), l.RenewDeadlineSeconds, "renew deadline seconds cannot be negative"))
	}
	if l.RetryPeriodSeconds != nil && *l.RetryPeriodSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("retryPeriodSeconds"), l.RetryPeriodSeconds, "retry period seconds cannot be negative"))
	}
	if lds, rds := l.LeaseDurationSeconds, l.RenewDeadlineSeconds; (lds == nil) != (rds == nil) {
		allErrs = append(allErrs, field.Forbidden(fldPath, "leader election lease duration and renew deadline should be either both specified or unspecified"))
	}
	if lds, rds := l.LeaseDurationSeconds, l.RenewDeadlineSeconds; lds != nil && rds != nil && *lds < *rds {
		allErrs = append(allErrs, field.Forbidden(fldPath, "control plane leader election renew deadline cannot be smaller than lease duration"))
	}

	return allErrs
}

func ValidateNodePortRange(nodePortRange string, fldPath *field.Path) *field.Error {
	if nodePortRange == "" {
		return field.Required(fldPath, "node port range is required")
	}

	portRange, err := kubenetutil.ParsePortRange(nodePortRange)
	if err != nil {
		return field.Invalid(fldPath, nodePortRange, err.Error())
	}

	if portRange.Base == 0 || portRange.Size == 0 {
		return field.Invalid(fldPath, nodePortRange, "invalid nodeport range")
	}

	return nil
}

func validateClusterNetworkingConfigUpdateImmutability(c, oldC *kubermaticv1.ClusterNetworkingConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if oldC.IPFamily != "" {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.IPFamily,
			oldC.IPFamily,
			fldPath.Child("ipFamily"),
		)...)
	}

	if len(oldC.Pods.CIDRBlocks) != 0 {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.Pods.CIDRBlocks,
			oldC.Pods.CIDRBlocks,
			fldPath.Child("pods", "cidrBlocks"),
		)...)
	}

	if len(oldC.Services.CIDRBlocks) != 0 {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.Services.CIDRBlocks,
			oldC.Services.CIDRBlocks,
			fldPath.Child("services", "cidrBlocks"),
		)...)
	}

	if oldC.ProxyMode != "" {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.ProxyMode,
			oldC.ProxyMode,
			fldPath.Child("proxyMode"),
		)...)
	}

	if oldC.DNSDomain != "" {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.DNSDomain,
			oldC.DNSDomain,
			fldPath.Child("dnsDomain"),
		)...)
	}

	if oldC.NodeLocalDNSCacheEnabled != nil {
		allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
			c.NodeLocalDNSCacheEnabled,
			oldC.NodeLocalDNSCacheEnabled,
			fldPath.Child("nodeLocalDNSCacheEnabled"),
		)...)
	}

	return allErrs
}

func validateCNIUpdate(newCni *kubermaticv1.CNIPluginSettings, oldCni *kubermaticv1.CNIPluginSettings, labels map[string]string) *field.Error {
	basePath := field.NewPath("spec", "cniPlugin")

	// if there was no CNI setting, we allow the mutation to happen
	// allowed for backward compatibility with older KKP with existing clusters with no CNI settings
	if newCni == nil && oldCni == nil {
		return nil
	}

	if oldCni != nil && newCni == nil {
		return field.Required(basePath, "CNI plugin settings cannot be removed")
	}

	if oldCni == nil && newCni != nil {
		return nil // allowed for automated setting of CNI type and version
	}

	if newCni.Type != oldCni.Type {
		if _, ok := labels[UnsafeCNIMigrationLabel]; ok {
			return nil // allowed for CNI type migration path
		}

		return field.Forbidden(basePath.Child("type"), fmt.Sprintf("cannot change CNI plugin type, unless %s label is present", UnsafeCNIMigrationLabel))
	}

	if newCni.Version != oldCni.Version {
		if !cni.IsSupportedCNIPluginTypeAndVersion(oldCni) {
			return nil // allowed for automated migration from deprecated CNI
		}

		newV, err := semverlib.NewVersion(newCni.Version)
		if err != nil {
			return field.Invalid(basePath.Child("version"), newCni.Version, fmt.Sprintf("couldn't parse CNI version `%s`: %v", newCni.Version, err))
		}

		oldV, err := semverlib.NewVersion(oldCni.Version)
		if err != nil {
			return field.Invalid(basePath.Child("version"), oldCni.Version, fmt.Sprintf("couldn't parse CNI version `%s`: %v", oldCni.Version, err))
		}

		if newV.Major() != oldV.Major() || (newV.Minor() != oldV.Minor()+1 && oldV.Minor() != newV.Minor()+1) {
			if _, ok := labels[UnsafeCNIUpgradeLabel]; !ok {
				return field.Forbidden(basePath.Child("version"), fmt.Sprintf("cannot upgrade CNI from %s to %s, only one minor version difference is allowed unless %s label is present", oldCni.Version, newCni.Version, UnsafeCNIUpgradeLabel))
			}
		}
	}

	return nil
}
