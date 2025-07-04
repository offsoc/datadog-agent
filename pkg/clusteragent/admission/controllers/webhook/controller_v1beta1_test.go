// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.
// Skip mac os test as cluster agent doesn't run on mac os
//go:build kubeapiserver && !darwin

package webhook

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	admiv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/DataDog/datadog-agent/comp/core"
	configComp "github.com/DataDog/datadog-agent/comp/core/config"
	workloadmeta "github.com/DataDog/datadog-agent/comp/core/workloadmeta/def"
	workloadmetafxmock "github.com/DataDog/datadog-agent/comp/core/workloadmeta/fx-mock"
	"github.com/DataDog/datadog-agent/pkg/clusteragent/admission/common"
	"github.com/DataDog/datadog-agent/pkg/clusteragent/admission/mutate/cwsinstrumentation"
	configmock "github.com/DataDog/datadog-agent/pkg/config/mock"
	"github.com/DataDog/datadog-agent/pkg/config/model"
	"github.com/DataDog/datadog-agent/pkg/util/fxutil"
	"github.com/DataDog/datadog-agent/pkg/util/kubernetes/certificate"
)

func getV1beta1Cfg(t *testing.T) Config { return NewConfig(false, false, false, configComp.NewMock(t)) }

func TestSecretNotFoundV1beta1(t *testing.T) {
	f := newFixtureV1beta1(t)
	v1beta1Cfg := getV1beta1Cfg(t)

	stopCh := make(chan struct{})
	defer close(stopCh)
	c := f.run(stopCh)

	_, err := c.mutatingWebhooksLister.Get(v1beta1Cfg.getWebhookName())
	if !errors.IsNotFound(err) {
		t.Fatal("Webhook shouldn't be created")
	}

	// The queue might not be closed yet because it's done asynchronously
	assert.Eventually(t, func() bool {
		return c.queue.Len() == 0
	}, waitFor, tick, "Work queue isn't empty")
}

func TestCreateWebhookV1beta1(t *testing.T) {
	f := newFixtureV1beta1(t)
	v1beta1Cfg := getV1beta1Cfg(t)

	data, err := certificate.GenerateSecretData(time.Now(), time.Now().Add(365*24*time.Hour), []string{"my.svc.dns"})
	if err != nil {
		t.Fatalf("Failed to create the Secret: %v", err)
	}

	secret := buildSecret(data, v1beta1Cfg)
	f.populateSecretsCache(secret)

	stopCh := make(chan struct{})
	defer close(stopCh)
	c := f.run(stopCh)

	var validatingWebhookConfiguration *admiv1beta1.ValidatingWebhookConfiguration
	require.Eventually(t, func() bool {
		validatingWebhookConfiguration, err = c.validatingWebhooksLister.Get(v1beta1Cfg.getWebhookName())
		return err == nil
	}, waitFor, tick)

	var mutatingWebhookConfiguration *admiv1beta1.MutatingWebhookConfiguration
	require.Eventually(t, func() bool {
		mutatingWebhookConfiguration, err = c.mutatingWebhooksLister.Get(v1beta1Cfg.getWebhookName())
		return err == nil
	}, waitFor, tick)

	validateV1beta1(t, validatingWebhookConfiguration, mutatingWebhookConfiguration, secret)

	assert.Eventually(t, func() bool {
		return c.queue.Len() == 0
	}, waitFor, tick, "Work queue isn't empty")
}

func TestUpdateOutdatedWebhookV1beta1(t *testing.T) {
	f := newFixtureV1beta1(t)
	v1Cfg := getV1Cfg(t)
	v1beta1Cfg := getV1beta1Cfg(t)

	data, err := certificate.GenerateSecretData(time.Now(), time.Now().Add(365*24*time.Hour), []string{"my.svc.dns"})
	if err != nil {
		t.Fatalf("Failed to create the Secret: %v", err)
	}

	secret := buildSecret(data, v1beta1Cfg)
	f.populateSecretsCache(secret)

	oldValidatingWebhookConfiguration := &admiv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1Cfg.getWebhookName(),
		},
		Webhooks: []admiv1beta1.ValidatingWebhook{
			{
				Name: "webhook-foo",
			},
			{
				Name: "webhook-bar",
			},
		},
	}
	f.populateValidatingWebhooksCache(oldValidatingWebhookConfiguration)

	oldMutatingWebhookConfiguration := &admiv1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1Cfg.getWebhookName(),
		},
		Webhooks: []admiv1beta1.MutatingWebhook{
			{
				Name: "webhook-foo",
			},
			{
				Name: "webhook-bar",
			},
		},
	}
	f.populateMutatingWebhooksCache(oldMutatingWebhookConfiguration)

	stopCh := make(chan struct{})
	defer close(stopCh)
	c := f.run(stopCh)

	var newValidatingWebhookConfiguration *admiv1beta1.ValidatingWebhookConfiguration
	require.Eventually(t, func() bool {
		newValidatingWebhookConfiguration, err = c.validatingWebhooksLister.Get(v1Cfg.getWebhookName())
		return err == nil && !reflect.DeepEqual(oldValidatingWebhookConfiguration, newValidatingWebhookConfiguration)
	}, waitFor, tick)

	var newMutatingWebhookConfiguration *admiv1beta1.MutatingWebhookConfiguration
	require.Eventually(t, func() bool {
		newMutatingWebhookConfiguration, err = c.mutatingWebhooksLister.Get(v1Cfg.getWebhookName())
		return err == nil && !reflect.DeepEqual(oldMutatingWebhookConfiguration, newMutatingWebhookConfiguration)
	}, waitFor, tick)

	validateV1beta1(t, newValidatingWebhookConfiguration, newMutatingWebhookConfiguration, secret)

	assert.Eventually(t, func() bool {
		return c.queue.Len() == 0
	}, waitFor, tick, "Work queue isn't empty")
}

func TestAdmissionControllerFailureModeV1beta1(t *testing.T) {
	mockConfig := configmock.New(t)
	f := newFixtureV1beta1(t)
	c, _ := f.createController()

	for _, value := range []string{"Ignore", "ignore", "BadVal", ""} {
		mockConfig.SetWithoutSource("admission_controller.failure_policy", value)
		c.config = NewConfig(true, false, false, mockConfig)

		validatingWebhookSkeleton := c.getValidatingWebhookSkeleton("foo", "/bar", []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nil, nil, nil, 0)
		assert.Equal(t, admiv1beta1.Ignore, *validatingWebhookSkeleton.FailurePolicy)
		mutatingWebhookSkeleton := c.getMutatingWebhookSkeleton("foo", "/bar", []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nil, nil, nil, 0)
		assert.Equal(t, admiv1beta1.Ignore, *mutatingWebhookSkeleton.FailurePolicy)
	}

	for _, value := range []string{"Fail", "fail"} {
		mockConfig.SetWithoutSource("admission_controller.failure_policy", value)
		c.config = NewConfig(true, false, false, mockConfig)

		validatingWebhookSkeleton := c.getValidatingWebhookSkeleton("foo", "/bar", []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nil, nil, nil, 0)
		assert.Equal(t, admiv1beta1.Fail, *validatingWebhookSkeleton.FailurePolicy)
		mutatingWebhookSkeleton := c.getMutatingWebhookSkeleton("foo", "/bar", []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nil, nil, nil, 0)
		assert.Equal(t, admiv1beta1.Fail, *mutatingWebhookSkeleton.FailurePolicy)
	}
}

func TestAdmissionControllerReinvocationPolicyV1beta1(t *testing.T) {
	mockConfig := configmock.New(t)
	f := newFixtureV1beta1(t)
	c, _ := f.createController()

	for _, value := range []string{"IfNeeded", "ifneeded", "Never", "never", "wrong", ""} {
		mockConfig.SetWithoutSource("admission_controller.reinvocationpolicy", value)
		c.config = NewConfig(true, false, false, mockConfig)

		mutatingWebhookSkeleton := c.getMutatingWebhookSkeleton("foo", "/bar", []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nil, nil, nil, 0)
		assert.Equal(t, admiv1beta1.IfNeededReinvocationPolicy, *mutatingWebhookSkeleton.ReinvocationPolicy)
	}
}

func TestGenerateTemplatesV1beta1(t *testing.T) {
	mockConfig := configmock.New(t)
	defaultReinvocationPolicy := admiv1beta1.IfNeededReinvocationPolicy
	failurePolicy := admiv1beta1.Ignore
	matchPolicy := admiv1beta1.Exact
	sideEffects := admiv1beta1.SideEffectClassNone
	port := int32(443)
	timeout := mockConfig.GetInt32("admission_controller.timeout_seconds")
	webhook := func(name, path string, objSelector, nsSelector *metav1.LabelSelector, matchConditions []admiv1beta1.MatchCondition, operations []admiv1beta1.OperationType, resources []string, to int32) admiv1beta1.MutatingWebhook {
		return admiv1beta1.MutatingWebhook{
			Name: name,
			ClientConfig: admiv1beta1.WebhookClientConfig{
				Service: &admiv1beta1.ServiceReference{
					Namespace: "nsfoo",
					Name:      "datadog-admission-controller",
					Port:      &port,
					Path:      &path,
				},
			},
			Rules: []admiv1beta1.RuleWithOperations{
				{
					Operations: operations,
					Rule: admiv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   resources,
					},
				},
			},
			ReinvocationPolicy:      &defaultReinvocationPolicy,
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			SideEffects:             &sideEffects,
			TimeoutSeconds:          &to,
			AdmissionReviewVersions: []string{"v1beta1"},
			ObjectSelector:          objSelector,
			NamespaceSelector:       nsSelector,
			MatchConditions:         matchConditions,
		}
	}
	tests := []struct {
		name        string
		setupConfig func(model.Config)
		configFunc  func(model.Config) Config
		want        func() []admiv1beta1.MutatingWebhook
	}{
		{
			name: "config injection, mutate all",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.agent.config", "/injectconfig", &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "admission.datadoghq.com/enabled",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"false"},
						},
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "config injection, mutate labelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.agent.config", "/injectconfig", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "tags injection, mutate all",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.standard.tags", "/injecttags", &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "admission.datadoghq.com/enabled",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"false"},
						},
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "tags injection, mutate labelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.standard.tags", "/injecttags", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "lib injection, mutate all",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.lib.injection", "/injectlib", &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "admission.datadoghq.com/enabled",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"false"},
						},
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "lib injection, mutate labelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook("datadog.webhook.lib.injection", "/injectlib", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "config and tags injection, mutate labelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhookConfig := webhook("datadog.webhook.agent.config", "/injectconfig", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				webhookTags := webhook("datadog.webhook.standard.tags", "/injecttags", &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhookConfig, webhookTags}
			},
		},
		{
			name: "config and tags injection, mutate all",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhookConfig := webhook("datadog.webhook.agent.config", "/injectconfig", &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "admission.datadoghq.com/enabled",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"false"},
						},
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				webhookTags := webhook("datadog.webhook.standard.tags", "/injecttags", &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "admission.datadoghq.com/enabled",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"false"},
						},
					},
				}, nil, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhookConfig, webhookTags}
			},
		},
		{
			name: "namespace selector enabled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", true)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhookConfig := webhook("datadog.webhook.agent.config", "/injectconfig", nil, &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				webhookTags := webhook("datadog.webhook.standard.tags", "/injecttags", nil, &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"admission.datadoghq.com/enabled": "true",
					},
				}, []admiv1beta1.MatchCondition{}, []admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"}, timeout)
				return []admiv1beta1.MutatingWebhook{webhookConfig, webhookTags}
			},
		},
		{
			name: "AKS-specific label selector without namespace selector enabled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.add_aks_selectors", true)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook(
					"datadog.webhook.agent.config",
					"/injectconfig",
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "admission.datadoghq.com/enabled",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"false"},
							},
						},
					},
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "control-plane",
								Operator: metav1.LabelSelectorOpDoesNotExist,
							},
							{
								Key:      "control-plane",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"true"},
							},
							{
								Key:      "kubernetes.azure.com/managedby",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"aks"},
							},
						},
					},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "AKS-specific label selector with namespace selector enabled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.add_aks_selectors", true)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", true)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				webhook := webhook(
					"datadog.webhook.agent.config",
					"/injectconfig",
					nil,
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "admission.datadoghq.com/enabled",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"false"},
							},
							{
								Key:      "control-plane",
								Operator: metav1.LabelSelectorOpDoesNotExist,
							},
							{
								Key:      "control-plane",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"true"},
							},
							{
								Key:      "kubernetes.azure.com/managedby",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"aks"},
							},
						},
					},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create}, []string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{webhook}
			},
		},
		{
			name: "cws instrumentation",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("cluster_agent.service_account_name", "datadog-cluster-agent")
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.timeout", 2)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.cws.pod.instrumentation",
					"/inject-pod-cws",
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      cwsinstrumentation.PodLabelEnabled,
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"false"},
							},
						},
					},
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				execWebhook := webhook(
					"datadog.webhook.cws.exec.instrumentation",
					"/inject-command-cws",
					nil,
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Connect},
					[]string{"pods/exec"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				return []admiv1beta1.MutatingWebhook{podWebhook, execWebhook}
			},
		},
		{
			name: "cws instrumentation, mutate unlabelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("cluster_agent.service_account_name", "datadog-cluster-agent")
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.timeout", 2)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, false, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.cws.pod.instrumentation",
					"/inject-pod-cws",
					&metav1.LabelSelector{
						MatchLabels: map[string]string{
							cwsinstrumentation.PodLabelEnabled: "true",
						},
					},
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				execWebhook := webhook(
					"datadog.webhook.cws.exec.instrumentation",
					"/inject-command-cws",
					nil,
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Connect},
					[]string{"pods/exec"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				return []admiv1beta1.MutatingWebhook{podWebhook, execWebhook}
			},
		},
		{
			name: "cws instrumentation, namespace selector",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", true)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", true)
				mockConfig.SetWithoutSource("cluster_agent.service_account_name", "datadog-cluster-agent")
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.timeout", 2)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.cws.pod.instrumentation",
					"/inject-pod-cws",
					nil,
					&metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      cwsinstrumentation.PodLabelEnabled,
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"false"},
							},
						},
					},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				execWebhook := webhook(
					"datadog.webhook.cws.exec.instrumentation",
					"/inject-command-cws",
					nil,
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Connect},
					[]string{"pods/exec"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				return []admiv1beta1.MutatingWebhook{podWebhook, execWebhook}
			},
		},
		{
			name: "cws instrumentation, namespace selector, mutate unlabelled",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", true)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("cluster_agent.service_account_name", "datadog-cluster-agent")
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.timeout", 2)
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(false, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.cws.pod.instrumentation",
					"/inject-pod-cws",
					nil,
					&metav1.LabelSelector{
						MatchLabels: map[string]string{
							cwsinstrumentation.PodLabelEnabled: "true",
						},
					},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				execWebhook := webhook(
					"datadog.webhook.cws.exec.instrumentation",
					"/inject-command-cws",
					nil,
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Connect},
					[]string{"pods/exec"},
					mockConfig.GetInt32("admission_controller.cws_instrumentation.timeout"),
				)
				return []admiv1beta1.MutatingWebhook{podWebhook, execWebhook}
			},
		},
		{
			name: "agent sidecar injection, misconfigured profiles, supported provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "fargate")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "misconfigured")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				return []admiv1beta1.MutatingWebhook{}
			},
		},
		{
			name: "agent sidecar injection, no selectors specified, supported provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "fargate")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.agent.sidecar",
					"/agentsidecar",
					&metav1.LabelSelector{
						MatchLabels: map[string]string{
							"agent.datadoghq.com/sidecar": "fargate",
						},
					},
					nil,
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{podWebhook}
			},
		},
		{
			name: "agent sidecar injection, no selectors specified, unsupported provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "unsupported-prov")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				return []admiv1beta1.MutatingWebhook{}
			},
		},
		{
			name: "agent sidecar injection, no selectors specified, no provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				return []admiv1beta1.MutatingWebhook{}
			},
		},
		{
			name: "agent sidecar injection, only single namespace selector, no provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[{\"NamespaceSelector\": {\"MatchLabels\": {\"labelKey\": \"labelVal\"}}}]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.agent.sidecar",
					"/agentsidecar",
					&metav1.LabelSelector{},
					&metav1.LabelSelector{
						MatchLabels: map[string]string{"labelKey": "labelVal"},
					},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{podWebhook}
			},
		},
		{
			name: "agent sidecar injection, valid selector specified, unsupported provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "unsupported-prov")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[{\"ObjectSelector\": {\"MatchLabels\": {\"labelKey\": \"labelVal\"}}}]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				return []admiv1beta1.MutatingWebhook{}
			},
		},
		{
			name: "agent sidecar injection, only single object selector, no provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[{\"ObjectSelector\": {\"MatchLabels\": {\"labelKey\": \"labelVal\"}}}]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.agent.sidecar",
					"/agentsidecar",
					&metav1.LabelSelector{MatchLabels: map[string]string{"labelKey": "labelVal"}},
					&metav1.LabelSelector{},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{podWebhook}
			},
		},
		{
			name: "agent sidecar injection, one object selector and one namespace selector, no provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[{\"ObjectSelector\": {\"MatchLabels\": {\"labelKey1\": \"labelVal1\"}}, \"NamespaceSelector\": {\"MatchLabels\": {\"labelKey2\": \"labelVal2\"}}}]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				podWebhook := webhook(
					"datadog.webhook.agent.sidecar",
					"/agentsidecar",
					&metav1.LabelSelector{MatchLabels: map[string]string{"labelKey1": "labelVal1"}},
					&metav1.LabelSelector{MatchLabels: map[string]string{"labelKey2": "labelVal2"}},
					[]admiv1beta1.MatchCondition{},
					[]admiv1beta1.OperationType{admiv1beta1.Create},
					[]string{"pods"},
					timeout,
				)
				return []admiv1beta1.MutatingWebhook{podWebhook}
			},
		},
		{
			name: "agent sidecar injection, multiple selectors (should refuse to create webhook), provider specified",
			setupConfig: func(mockConfig model.Config) {
				mockConfig.SetWithoutSource("admission_controller.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.namespace_selector_fallback", false)
				mockConfig.SetWithoutSource("admission_controller.inject_config.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.inject_tags.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.auto_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.enabled", false)
				mockConfig.SetWithoutSource("admission_controller.cws_instrumentation.mutate_unlabelled", false)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.enabled", true)
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.provider", "fargate")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.selectors", "[{\"NamespaceSelector\": {\"MatchLabels\":{\"labelKey1\": \"labelVal1\"}}} , {\"ObjectSelector\": {\"MatchLabels\": {\"labelKey2\": \"labelVal2\"}}}]")
				mockConfig.SetWithoutSource("admission_controller.agent_sidecar.profiles", "[]")
			},
			configFunc: func(mockConfig model.Config) Config { return NewConfig(true, true, false, mockConfig) },
			want: func() []admiv1beta1.MutatingWebhook {
				return []admiv1beta1.MutatingWebhook{}
			},
		},
	}

	wmeta := fxutil.Test[workloadmeta.Component](t,
		core.MockBundle(),
		fx.Replace(configComp.MockParams{Overrides: map[string]interface{}{"kube_resources_namespace": "nsfoo"}}),
		workloadmetafxmock.MockModule(workloadmeta.NewParams()),
	)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := configmock.New(t)
			mockConfig.SetWithoutSource("kube_resources_namespace", "nsfoo")
			tt.setupConfig(mockConfig)

			c := &ControllerV1beta1{}
			c.config = tt.configFunc(mockConfig)
			c.webhooks = c.generateWebhooks(wmeta, nil, mockConfig, nil)
			c.generateTemplates()

			assert.EqualValues(t, tt.want(), c.mutatingWebhookTemplates)
		})
	}
}

func TestGetValidatingWebhookSkeletonV1beta1(t *testing.T) {
	mockConfig := configmock.New(t)
	failurePolicy := admiv1beta1.Ignore
	matchPolicy := admiv1beta1.Exact
	sideEffects := admiv1beta1.SideEffectClassNone
	port := int32(443)
	path := "/bar"
	defaultTimeout := mockConfig.GetInt32("admission_controller.timeout_seconds")
	customTimeout := int32(2)
	namespaceSelector, _ := common.DefaultLabelSelectors(true)
	_, objectSelector := common.DefaultLabelSelectors(false)
	webhook := func(to *int32, objSelector, nsSelector *metav1.LabelSelector, matchConditions []admiv1beta1.MatchCondition) admiv1beta1.ValidatingWebhook {
		return admiv1beta1.ValidatingWebhook{
			Name: "datadog.webhook.foo",
			ClientConfig: admiv1beta1.WebhookClientConfig{
				Service: &admiv1beta1.ServiceReference{
					Namespace: "nsfoo",
					Name:      "datadog-admission-controller",
					Port:      &port,
					Path:      &path,
				},
			},
			Rules: []admiv1beta1.RuleWithOperations{
				{
					Operations: []admiv1beta1.OperationType{
						admiv1beta1.Create,
					},
					Rule: admiv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			SideEffects:             &sideEffects,
			TimeoutSeconds:          to,
			AdmissionReviewVersions: []string{"v1beta1"},
			ObjectSelector:          objSelector,
			NamespaceSelector:       nsSelector,
			MatchConditions:         matchConditions,
		}
	}
	type args struct {
		nameSuffix string
		path       string
	}
	tests := []struct {
		name              string
		args              args
		timeout           *int32
		namespaceSelector bool
		want              admiv1beta1.ValidatingWebhook
	}{
		{
			name: "nominal case",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			namespaceSelector: false,
			want:              webhook(&defaultTimeout, objectSelector, nil, nil),
		},
		{
			name: "namespace selector",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			namespaceSelector: true,
			want:              webhook(&defaultTimeout, nil, namespaceSelector, nil),
		},
		{
			name: "custom timeout",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			timeout:           &customTimeout,
			namespaceSelector: false,
			want:              webhook(&customTimeout, objectSelector, nil, nil),
		},
	}

	mockConfig.SetWithoutSource("kube_resources_namespace", "nsfoo")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout != nil {
				mockConfig.SetWithoutSource("admission_controller.timeout_seconds", *tt.timeout)
				defer mockConfig.SetDefault("admission_controller.timeout_seconds", defaultTimeout)
			}

			c := &ControllerV1beta1{}
			c.config = NewConfig(false, tt.namespaceSelector, false, mockConfig)

			nsSelector, objSelector := common.DefaultLabelSelectors(tt.namespaceSelector)

			assert.EqualValues(t, tt.want, c.getValidatingWebhookSkeleton(tt.args.nameSuffix, tt.args.path, []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nsSelector, objSelector, nil, 0))
		})
	}
}

func TestGetMutatingWebhookSkeletonV1beta1(t *testing.T) {
	mockConfig := configmock.New(t)
	failurePolicy := admiv1beta1.Ignore
	matchPolicy := admiv1beta1.Exact
	sideEffects := admiv1beta1.SideEffectClassNone
	defaultReinvocationPolicy := admiv1beta1.IfNeededReinvocationPolicy
	port := int32(443)
	path := "/bar"
	defaultTimeout := mockConfig.GetInt32("admission_controller.timeout_seconds")
	customTimeout := int32(2)
	namespaceSelector, _ := common.DefaultLabelSelectors(true)
	_, objectSelector := common.DefaultLabelSelectors(false)
	webhook := func(to *int32, objSelector, nsSelector *metav1.LabelSelector, matchConditions []admiv1beta1.MatchCondition) admiv1beta1.MutatingWebhook {
		return admiv1beta1.MutatingWebhook{
			Name: "datadog.webhook.foo",
			ClientConfig: admiv1beta1.WebhookClientConfig{
				Service: &admiv1beta1.ServiceReference{
					Namespace: "nsfoo",
					Name:      "datadog-admission-controller",
					Port:      &port,
					Path:      &path,
				},
			},
			Rules: []admiv1beta1.RuleWithOperations{
				{
					Operations: []admiv1beta1.OperationType{
						admiv1beta1.Create,
					},
					Rule: admiv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
			ReinvocationPolicy:      &defaultReinvocationPolicy,
			FailurePolicy:           &failurePolicy,
			MatchPolicy:             &matchPolicy,
			SideEffects:             &sideEffects,
			TimeoutSeconds:          to,
			AdmissionReviewVersions: []string{"v1beta1"},
			ObjectSelector:          objSelector,
			NamespaceSelector:       nsSelector,
			MatchConditions:         matchConditions,
		}
	}
	type args struct {
		nameSuffix string
		path       string
	}
	tests := []struct {
		name              string
		args              args
		timeout           *int32
		namespaceSelector bool
		want              admiv1beta1.MutatingWebhook
	}{
		{
			name: "nominal case",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			namespaceSelector: false,
			want:              webhook(&defaultTimeout, objectSelector, nil, nil),
		},
		{
			name: "namespace selector",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			namespaceSelector: true,
			want:              webhook(&defaultTimeout, nil, namespaceSelector, nil),
		},
		{
			name: "custom timeout",
			args: args{
				nameSuffix: "foo",
				path:       "/bar",
			},
			timeout:           &customTimeout,
			namespaceSelector: false,
			want:              webhook(&customTimeout, objectSelector, nil, nil),
		},
	}

	mockConfig.SetWithoutSource("kube_resources_namespace", "nsfoo")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout != nil {
				mockConfig.SetWithoutSource("admission_controller.timeout_seconds", *tt.timeout)
				defer mockConfig.SetDefault("admission_controller.timeout_seconds", defaultTimeout)
			}

			c := &ControllerV1beta1{}
			c.config = NewConfig(false, tt.namespaceSelector, false, mockConfig)

			nsSelector, objSelector := common.DefaultLabelSelectors(tt.namespaceSelector)

			assert.EqualValues(t, tt.want, c.getMutatingWebhookSkeleton(tt.args.nameSuffix, tt.args.path, []admiv1beta1.OperationType{admiv1beta1.Create}, map[string][]string{"": {"pods"}}, nsSelector, objSelector, nil, 0))
		})
	}
}

type fixtureV1beta1 struct {
	fixture
}

func newFixtureV1beta1(t *testing.T) *fixtureV1beta1 {
	f := &fixtureV1beta1{}
	f.t = t
	f.client = fake.NewSimpleClientset()
	return f
}

func (f *fixtureV1beta1) createController() (*ControllerV1beta1, informers.SharedInformerFactory) {
	factory := informers.NewSharedInformerFactory(f.client, time.Duration(0))
	wmeta := fxutil.Test[workloadmeta.Component](f.t, core.MockBundle(), workloadmetafxmock.MockModule(workloadmeta.NewParams()))
	datadogConfig := fxutil.Test[configComp.Component](f.t, core.MockBundle())
	return NewControllerV1beta1(
		f.client,
		factory.Core().V1().Secrets(),
		factory.Admissionregistration().V1beta1().ValidatingWebhookConfigurations(),
		factory.Admissionregistration().V1beta1().MutatingWebhookConfigurations(),
		func() bool { return true },
		make(<-chan struct{}),
		getV1beta1Cfg(f.t),
		wmeta,
		nil,
		datadogConfig,
		nil,
	), factory
}

func (f *fixtureV1beta1) run(stopCh chan struct{}) *ControllerV1beta1 {
	c, factory := f.createController()

	factory.Start(stopCh)
	go c.Run(stopCh)

	return c
}

func (f *fixtureV1beta1) populateValidatingWebhooksCache(webhooks ...*admiv1beta1.ValidatingWebhookConfiguration) {
	for _, w := range webhooks {
		_, _ = f.client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(context.TODO(), w, metav1.CreateOptions{})
	}
}

func (f *fixtureV1beta1) populateMutatingWebhooksCache(webhooks ...*admiv1beta1.MutatingWebhookConfiguration) {
	for _, w := range webhooks {
		_, _ = f.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(context.TODO(), w, metav1.CreateOptions{})
	}
}

func validateV1beta1(t *testing.T, validatingWebhooks *admiv1beta1.ValidatingWebhookConfiguration, mutatingWebhooks *admiv1beta1.MutatingWebhookConfiguration, s *corev1.Secret) {
	// Validate the number of webhooks.
	require.Len(t, validatingWebhooks.Webhooks, 0)
	require.Len(t, mutatingWebhooks.Webhooks, 3)

	// Validate the CA bundle for webhooks.
	for i := 0; i < len(validatingWebhooks.Webhooks); i++ {
		require.Equal(t, validatingWebhooks.Webhooks[i].ClientConfig.CABundle, certificate.GetCABundle(s.Data))
	}
	for i := 0; i < len(mutatingWebhooks.Webhooks); i++ {
		require.Equal(t, mutatingWebhooks.Webhooks[i].ClientConfig.CABundle, certificate.GetCABundle(s.Data))
	}
}
