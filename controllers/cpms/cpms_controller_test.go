/*
Copyright 2024.
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

package cpms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/osd-metrics-exporter/pkg/metrics"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func makeTestCPMS(name, namespace string, cpmsSpec machinev1.ControlPlaneMachineSetSpec) *machinev1.ControlPlaneMachineSet {
	cpms := &machinev1.ControlPlaneMachineSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       cpmsSpec,
	}
	return cpms
}

func makeTestMachineSpecAWS() *runtime.RawExtension {
	bytes, err := json.Marshal(machinev1beta1.AWSMachineProviderConfig{InstanceType: "m5.2xlarge"})
	if err != nil {
		return nil
	}
	return &runtime.RawExtension{Raw: bytes}
}

func makeTestMachineSpecGCP() *runtime.RawExtension {
	bytes, err := json.Marshal(machinev1beta1.GCPMachineProviderSpec{MachineType: "custom-4-16384"})
	if err != nil {
		return nil
	}
	return &runtime.RawExtension{Raw: bytes}
}

func makeTestMachineSpecAzure() *runtime.RawExtension {
	bytes, err := json.Marshal(machinev1beta1.AzureMachineProviderSpec{VMSize: "test"})
	if err != nil {
		return nil
	}
	return &runtime.RawExtension{Raw: bytes}
}

func makeTestCPMSTemplate(provider string) machinev1.ControlPlaneMachineSetTemplate {
	var providerSpec machinev1beta1.ProviderSpec
	var machineTemplate machinev1.OpenShiftMachineV1Beta1MachineTemplate
	if provider == "AWS" {
		providerSpec = machinev1beta1.ProviderSpec{Value: makeTestMachineSpecAWS()}
	} else if provider == "GCP" {
		providerSpec = machinev1beta1.ProviderSpec{Value: makeTestMachineSpecGCP()}
	} else if provider == "Azure" {
		providerSpec = machinev1beta1.ProviderSpec{Value: makeTestMachineSpecAzure()}
	}
	machineTemplate = machinev1.OpenShiftMachineV1Beta1MachineTemplate{
		Spec:           machinev1beta1.MachineSpec{ProviderSpec: providerSpec},
		FailureDomains: &machinev1.FailureDomains{Platform: configv1.PlatformType(provider)},
	}

	return machinev1.ControlPlaneMachineSetTemplate{MachineType: machinev1.OpenShiftMachineV1Beta1MachineType, OpenShiftMachineV1Beta1Machine: &machineTemplate}
}

func TestReconcileCPMS_Reconcile(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		cpmsSpec                 machinev1.ControlPlaneMachineSetSpec
		expectedClusterIDResults string
		expectedCPMSResults      string
		expectError              bool
	}{
		{
			name: "with active ControlPlaneMachineSet(aws)",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Active",
				Template: makeTestCPMSTemplate("AWS"),
			},
			expectedCPMSResults: `
# HELP cpms_enabled Indicates if the controlplanemachineset is enabled
# TYPE cpms_enabled gauge
cpms_enabled{_id="cluster-id",label_node_kubernetes_io_instance_type="m5.2xlarge",name="osd_exporter"} 1
`,
			expectError: false,
		},
		{
			name: "with inactive ControlPlaneMachineSet(aws)",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Inactive",
				Template: makeTestCPMSTemplate("AWS"),
			},
			expectedCPMSResults: `
# HELP cpms_enabled Indicates if the controlplanemachineset is enabled
# TYPE cpms_enabled gauge
cpms_enabled{_id="cluster-id",label_node_kubernetes_io_instance_type="m5.2xlarge",name="osd_exporter"} 0
`,
			expectError: false,
		},
		{
			name: "with active ControlPlaneMachineSet(gcp)",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Active",
				Template: makeTestCPMSTemplate("GCP"),
			},
			expectedCPMSResults: `
# HELP cpms_enabled Indicates if the controlplanemachineset is enabled
# TYPE cpms_enabled gauge
cpms_enabled{_id="cluster-id",label_node_kubernetes_io_instance_type="custom-4-16384",name="osd_exporter"} 1
`,
			expectError: false,
		},
		{
			name: "with inactive ControlPlaneMachineSet(gcp)",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Inactive",
				Template: makeTestCPMSTemplate("GCP"),
			},
			expectedCPMSResults: `
# HELP cpms_enabled Indicates if the controlplanemachineset is enabled
# TYPE cpms_enabled gauge
cpms_enabled{_id="cluster-id",label_node_kubernetes_io_instance_type="custom-4-16384",name="osd_exporter"} 0
`,
			expectError: false,
		},
		{
			name: "with unsupported cloud provider",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Inactive",
				Template: makeTestCPMSTemplate("Azure"),
			},
			expectError: true,
		},
		{
			name: "with invalid MachineType",
			cpmsSpec: machinev1.ControlPlaneMachineSetSpec{
				State:    "Inactive",
				Template: machinev1.ControlPlaneMachineSetTemplate{MachineType: "i_am_no_machinetype"},
			},
			expectError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metricsAggregator := metrics.NewMetricsAggregator(time.Second, "cluster-id")
			done := metricsAggregator.Run()
			defer close(done)
			err := machinev1.Install(scheme.Scheme)
			require.NoError(t, err)

			testName := "cluster"
			testNamespace := "openshift-machine-api"

			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(makeTestCPMS(testName, testNamespace, tc.cpmsSpec)).Build()
			reconciler := CPMSReconciler{
				Client:            fakeClient,
				MetricsAggregator: metricsAggregator,
				ClusterId:         "cluster-id",
			}
			result, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: testNamespace,
					Name:      testName,
				},
			})
			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// sleep to allow the aggregator to aggregate metrics in the background
			time.Sleep(time.Second * 3)
			require.NoError(t, err)
			require.NotNil(t, result)
			var testCPMS machinev1.ControlPlaneMachineSet
			err = fakeClient.Get(context.Background(), types.NamespacedName{Name: testName, Namespace: testNamespace}, &testCPMS)
			require.NoError(t, err)

			metric := metricsAggregator.GetCPMSMetric()
			err = testutil.CollectAndCompare(metric, strings.NewReader(tc.expectedCPMSResults))
			require.NoError(t, err)
		})
	}
}
