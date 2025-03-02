//go:build ee

/*
Copyright 2022 The Kubermatic Kubernetes Platform contributors.

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

	"go.uber.org/zap"

	clusterv1alpha1 "github.com/kubermatic/machine-controller/pkg/apis/cluster/v1alpha1"
	eemachinevalidation "k8c.io/kubermatic/v2/pkg/ee/validation/machine"
	"k8c.io/kubermatic/v2/pkg/resources/certificates"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func validateQuota(ctx context.Context, log *zap.SugaredLogger, seedClient, userClient ctrlruntimeclient.Client,
	machine *clusterv1alpha1.Machine, caBundle *certificates.CABundle) error {
	return eemachinevalidation.ValidateQuota(ctx, log, seedClient, userClient, machine, caBundle)
}
