package spireentry

import (
	"errors"
	"testing"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	spirev1alpha1 "github.com/spiffe/spire-controller-manager/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	clusterName   = "test"
	clusterDomain = "cluster.local"
	trustDomain   = "example.org"
)

func TestRenderPodEntry(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			UID: "uid",
		},
		Spec: corev1.NodeSpec{},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "namespace",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "test",
		},
	}
	endpointsList := &corev1.EndpointsList{
		Items: []corev1.Endpoints{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "endpoint",
					Namespace: "namespace",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-endpoint",
					Namespace: "namespace",
				},
			},
		},
	}

	for _, test := range []struct {
		name             string
		spiffeIDTemplate string
		dnsNameTemplates []string
		expectedErr      string
	}{
		{
			name:             "Valid config with Duplicate DNS Names",
			spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}",
			dnsNameTemplates: []string{
				"{{ .PodSpec.ServiceAccountName }}.{{ .PodMeta.Namespace }}.svc.{{ .ClusterDomain }}",
				"{{ .PodMeta.Name }}.{{ .PodMeta.Namespace }}.svc.{{ .ClusterDomain }}",
				"{{ .PodMeta.Name }}.{{ .TrustDomain }}.svc",
			},
		},
		{
			name:             "Invalid DNS Name ends with non-alphanumeric",
			spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}",
			dnsNameTemplates: []string{
				"{{ .PodMeta.Name }}-",
			},
			expectedErr: "failed to render DNS name: invalid DNS name \"" + pod.Name + "-\": label does not match regex: " + pod.Name + "-",
		},
		{
			name:             "Invalid DNS Name invalid character in the middle",
			spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}",
			dnsNameTemplates: []string{
				"{{ .PodMeta.Name }}@end",
			},
			expectedErr: "failed to render DNS name: invalid DNS name \"" + pod.Name + "@end\": label does not match regex: " + pod.Name + "@end",
		},
		{
			name:             "Invalid DNS Name too long",
			spiffeIDTemplate: "spiffe://{{ .TrustDomain }}/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}",
			dnsNameTemplates: []string{
				"{{ .PodMeta.Name }}---------------------------------------------------------end",
			},
			expectedErr: "failed to render DNS name: invalid DNS name \"" + pod.Name +
				"---------------------------------------------------------end\": label length exceeded: " + pod.Name +
				"---------------------------------------------------------end",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := &spirev1alpha1.ClusterSPIFFEIDSpec{
				SPIFFEIDTemplate: test.spiffeIDTemplate,
				DNSNameTemplates: test.dnsNameTemplates,
			}
			parsedSpec, err := spirev1alpha1.ParseClusterSPIFFEIDSpec(spec)
			require.NoError(t, err)
			td, err := spiffeid.TrustDomainFromString(trustDomain)
			require.NoError(t, err)

			entry, err := renderPodEntry(parsedSpec, node, pod, endpointsList, td, clusterName, clusterDomain)
			if test.expectedErr != "" {
				require.EqualError(t, errors.New(err.Error()), test.expectedErr)
				return
			}
			require.NoError(t, err)

			// SPIFFE ID rendered correctly
			spiffeID, err := spiffeid.FromPathf(td, "/ns/%s/sa/%s", pod.Namespace, pod.Spec.ServiceAccountName)
			require.NoError(t, err)
			require.Equal(t, entry.SPIFFEID.String(), spiffeID.String())

			// Parent ID rendered correctly
			parentID, err := spiffeid.FromPathf(td, "/spire/agent/k8s_psat/%s/%s", clusterName, node.UID)
			require.NoError(t, err)
			require.Equal(t, entry.ParentID.String(), parentID.String())

			// DNS names are unique
			dnsNamesSet := make(map[string]struct{})
			for _, dnsName := range entry.DNSNames {
				_, exists := dnsNamesSet[dnsName]
				require.False(t, exists)
				dnsNamesSet[dnsName] = struct{}{}
			}

			// DNS names templates rendered correctly
			require.Contains(t, entry.DNSNames, pod.Name+"."+pod.Namespace+".svc."+clusterDomain)
			require.Contains(t, entry.DNSNames, pod.Name+"."+trustDomain+".svc")

			// Endpoint DNS Names auto populated
			for _, endpoint := range endpointsList.Items {
				require.Contains(t, entry.DNSNames, endpoint.Name)
				require.Contains(t, entry.DNSNames, endpoint.Name+"."+endpoint.Namespace)
				require.Contains(t, entry.DNSNames, endpoint.Name+"."+endpoint.Namespace+".svc")
				require.Contains(t, entry.DNSNames, endpoint.Name+"."+endpoint.Namespace+".svc."+clusterDomain)
			}
		})
	}
}
