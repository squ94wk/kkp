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

package packet

import (
	"context"
	"errors"

	"github.com/packethost/packngo"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	"k8c.io/kubermatic/v2/pkg/provider"
	"k8c.io/kubermatic/v2/pkg/resources"
)

const (
	defaultBillingCycle = "hourly"
)

type packet struct {
	secretKeySelector provider.SecretKeySelectorValueFunc
}

// NewCloudProvider creates a new packet provider.
func NewCloudProvider(secretKeyGetter provider.SecretKeySelectorValueFunc) provider.CloudProvider {
	return &packet{
		secretKeySelector: secretKeyGetter,
	}
}

var _ provider.CloudProvider = &packet{}

// DefaultCloudSpec adds defaults to the CloudSpec.
func (p *packet) DefaultCloudSpec(_ context.Context, _ *kubermaticv1.CloudSpec) error {
	return nil
}

// ValidateCloudSpec validates the given CloudSpec.
func (p *packet) ValidateCloudSpec(_ context.Context, spec kubermaticv1.CloudSpec) error {
	_, _, err := GetCredentialsForCluster(spec, p.secretKeySelector)
	return err
}

// InitializeCloudProvider initializes a cluster, in particular
// updates BillingCycle to the defaultBillingCycle, if it is not set.
func (p *packet) InitializeCloudProvider(ctx context.Context, cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	var err error
	if cluster.Spec.Cloud.Packet.BillingCycle == "" {
		cluster, err = update(ctx, cluster.Name, func(cluster *kubermaticv1.Cluster) {
			cluster.Spec.Cloud.Packet.BillingCycle = defaultBillingCycle
		})
		if err != nil {
			return nil, err
		}
	}

	return cluster, nil
}

// TODO: Hey, you! Yes, you! Why don't you implement reconciling for Packet? Would be really cool :)
// func (p *packet) ReconcileCluster(cluster *kubermaticv1.Cluster, update provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
// 	return cluster, nil
// }

// CleanUpCloudProvider.
func (p *packet) CleanUpCloudProvider(_ context.Context, cluster *kubermaticv1.Cluster, _ provider.ClusterUpdater) (*kubermaticv1.Cluster, error) {
	return cluster, nil
}

// ValidateCloudSpecUpdate verifies whether an update of cloud spec is valid and permitted.
func (p *packet) ValidateCloudSpecUpdate(_ context.Context, _ kubermaticv1.CloudSpec, _ kubermaticv1.CloudSpec) error {
	return nil
}

func GetCredentialsForCluster(cloudSpec kubermaticv1.CloudSpec, secretKeySelector provider.SecretKeySelectorValueFunc) (apiKey, projectID string, err error) {
	apiKey = cloudSpec.Packet.APIKey
	projectID = cloudSpec.Packet.ProjectID

	if apiKey == "" {
		if cloudSpec.Packet.CredentialsReference == nil {
			return "", "", errors.New("no credentials provided")
		}
		apiKey, err = secretKeySelector(cloudSpec.Packet.CredentialsReference, resources.PacketAPIKey)
		if err != nil {
			return "", "", err
		}
	}

	if projectID == "" {
		if cloudSpec.Packet.CredentialsReference == nil {
			return "", "", errors.New("no credentials provided")
		}
		projectID, err = secretKeySelector(cloudSpec.Packet.CredentialsReference, resources.PacketProjectID)
		if err != nil {
			return "", "", err
		}
	}

	return apiKey, projectID, nil
}

func ValidateCredentials(apiKey, projectID string) error {
	client := packngo.NewClientWithAuth("kubermatic", apiKey, nil)
	_, _, err := client.Projects.Get(projectID, nil)
	return err
}
