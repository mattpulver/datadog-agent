// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build orchestrator

package k8s

import (
	"sort"
	"testing"
	"time"

	model "github.com/DataDog/agent-payload/v5/process"
	"github.com/DataDog/datadog-agent/pkg/collector/corechecks/cluster/orchestrator/processors"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestExtractReplicaSet(t *testing.T) {
	timestamp := metav1.NewTime(time.Date(2014, time.January, 15, 0, 0, 0, 0, time.UTC)) // 1389744000
	testInt32 := int32(2)
	tests := map[string]struct {
		input             appsv1.ReplicaSet
		labelsAsTags      map[string]string
		annotationsAsTags map[string]string
		expected          model.ReplicaSet
	}{
		"full rs": {
			input: appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					UID:               types.UID("e42e5adc-0749-11e8-a2b8-000c29dea4f6"),
					Name:              "replicaset",
					Namespace:         "namespace",
					CreationTimestamp: timestamp,
					Labels: map[string]string{
						"label": "foo",
					},
					Annotations: map[string]string{
						"annotation": "bar",
					},
					ResourceVersion: "1234",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: &testInt32,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-deploy",
						},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "cluster",
								Operator: "NotIn",
								Values:   []string{"staging", "prod"},
							},
						},
					},
				},
				Status: appsv1.ReplicaSetStatus{
					Replicas:             2,
					FullyLabeledReplicas: 2,
					ReadyReplicas:        1,
					AvailableReplicas:    1,
					Conditions: []appsv1.ReplicaSetCondition{
						{
							Type:               appsv1.ReplicaSetReplicaFailure,
							Status:             v1.ConditionFalse,
							LastTransitionTime: timestamp,
							Reason:             "test reason",
							Message:            "test message",
						},
					},
				},
			},
			labelsAsTags: map[string]string{
				"label": "application",
			},
			annotationsAsTags: map[string]string{
				"annotation": "annotation_key",
			},
			expected: model.ReplicaSet{
				Metadata: &model.Metadata{
					Name:              "replicaset",
					Namespace:         "namespace",
					Uid:               "e42e5adc-0749-11e8-a2b8-000c29dea4f6",
					CreationTimestamp: 1389744000,
					Labels:            []string{"label:foo"},
					Annotations:       []string{"annotation:bar"},
					ResourceVersion:   "1234",
				},
				Conditions: []*model.ReplicaSetCondition{
					{
						Type:               string(appsv1.ReplicaSetReplicaFailure),
						Status:             string(v1.ConditionFalse),
						LastTransitionTime: timestamp.Unix(),
						Reason:             "test reason",
						Message:            "test message",
					},
				},
				Tags: []string{
					"kube_condition_replicafailure:false",
					"application:foo",
					"annotation_key:bar",
				},
				Selectors: []*model.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: "In",
						Values:   []string{"test-deploy"},
					},
					{
						Key:      "cluster",
						Operator: "NotIn",
						Values:   []string{"staging", "prod"},
					},
				},
				ReplicasDesired:      2,
				Replicas:             2,
				FullyLabeledReplicas: 2,
				ReadyReplicas:        1,
				AvailableReplicas:    1,
			},
		},
		"empty rs": {input: appsv1.ReplicaSet{}, expected: model.ReplicaSet{Metadata: &model.Metadata{}, ReplicasDesired: 1}},
		"rs with resources": {
			input: appsv1.ReplicaSet{
				Spec: appsv1.ReplicaSetSpec{Template: getTemplateWithResourceRequirements()},
			},
			expected: model.ReplicaSet{
				Metadata:             &model.Metadata{},
				ReplicasDesired:      1,
				ResourceRequirements: getExpectedModelResourceRequirements(),
			}},
		"partial rs": {
			input: appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deploy",
					Namespace: "namespace",
				},
				Status: appsv1.ReplicaSetStatus{
					ReadyReplicas:     1,
					AvailableReplicas: 0,
				},
			}, expected: model.ReplicaSet{
				Metadata: &model.Metadata{
					Name:      "deploy",
					Namespace: "namespace",
				},
				ReplicasDesired:   1,
				ReadyReplicas:     1,
				AvailableReplicas: 0,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			pctx := &processors.K8sProcessorContext{
				LabelsAsTags:      tc.labelsAsTags,
				AnnotationsAsTags: tc.annotationsAsTags,
			}
			actual := ExtractReplicaSet(pctx, &tc.input)
			sort.Strings(actual.Tags)
			sort.Strings(tc.expected.Tags)
			assert.Equal(t, &tc.expected, actual)
		})
	}
}
