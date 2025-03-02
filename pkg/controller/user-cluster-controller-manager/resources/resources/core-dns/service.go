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

package coredns

import (
	"k8c.io/kubermatic/v2/pkg/resources"
	"k8c.io/kubermatic/v2/pkg/resources/reconciling"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceCreator creates the service for the CoreDNS.
func ServiceCreator(dnsClusterIP string) reconciling.NamedServiceCreatorGetter {
	return func() (string, reconciling.ServiceCreator) {
		labels := map[string]string{
			"kubernetes.io/cluster-service": "true",
			"app.kubernetes.io/name":        "KubeDNS",
		}
		return resources.CoreDNSServiceName, func(s *corev1.Service) (*corev1.Service, error) {
			s.Name = resources.CoreDNSServiceName
			s.Labels = resources.BaseAppLabels(resources.CoreDNSDeploymentName, labels)
			s.Spec.Selector = resources.BaseAppLabels(resources.CoreDNSDeploymentName, nil)
			s.Spec.ClusterIP = dnsClusterIP
			s.Spec.Ports = []corev1.ServicePort{
				{
					Name:       "dns-tcp",
					Protocol:   corev1.ProtocolTCP,
					Port:       53,
					TargetPort: intstr.FromInt(53),
				},
				{
					Name:       "dns",
					Protocol:   corev1.ProtocolUDP,
					Port:       53,
					TargetPort: intstr.FromInt(53),
				},
			}
			return s, nil
		}
	}
}
