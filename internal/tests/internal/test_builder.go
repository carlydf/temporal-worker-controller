package internal

import (
	"time"

	temporaliov1alpha1 "github.com/temporalio/temporal-worker-controller/api/v1alpha1"
	"github.com/temporalio/temporal-worker-controller/internal/k8s"
	"github.com/temporalio/temporal-worker-controller/internal/testhelpers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemporalWorkerDeploymentBuilder provides a fluent interface for building test TWD objects
type TemporalWorkerDeploymentBuilder struct {
	twd *temporaliov1alpha1.TemporalWorkerDeployment
}

// NewTemporalWorkerDeploymentBuilder creates a new builder with sensible defaults
func NewTemporalWorkerDeploymentBuilder(name string) *TemporalWorkerDeploymentBuilder {
	return &TemporalWorkerDeploymentBuilder{
		twd: testhelpers.MakeTWDWithName(name),
	}
}

// WithAllAtOnceStrategy sets the rollout strategy to all-at-once
func (b *TemporalWorkerDeploymentBuilder) WithAllAtOnceStrategy() *TemporalWorkerDeploymentBuilder {
	b.twd.Spec.RolloutStrategy.Strategy = temporaliov1alpha1.UpdateAllAtOnce
	return b
}

// WithProgressiveStrategy sets the rollout strategy to progressive with given steps
func (b *TemporalWorkerDeploymentBuilder) WithProgressiveStrategy(steps ...temporaliov1alpha1.RolloutStep) *TemporalWorkerDeploymentBuilder {
	b.twd.Spec.RolloutStrategy.Strategy = temporaliov1alpha1.UpdateProgressive
	b.twd.Spec.RolloutStrategy.Steps = steps
	return b
}

// WithReplicas sets the number of replicas
func (b *TemporalWorkerDeploymentBuilder) WithReplicas(replicas int32) *TemporalWorkerDeploymentBuilder {
	b.twd.Spec.Replicas = &replicas
	return b
}

// WithVersion sets the worker version (creates HelloWorld pod spec)
func (b *TemporalWorkerDeploymentBuilder) WithVersion(version string) *TemporalWorkerDeploymentBuilder {
	b.twd.Spec.Template = testhelpers.MakeHelloWorldPodSpec(version)
	return b
}

// WithNamespace sets the namespace
func (b *TemporalWorkerDeploymentBuilder) WithNamespace(namespace string) *TemporalWorkerDeploymentBuilder {
	if b.twd.ObjectMeta.Name == "" {
		b.twd.ObjectMeta.Name = "test-deployment"
	}
	b.twd.ObjectMeta.Namespace = namespace
	return b
}

// WithTemporalConnection sets the temporal connection name
func (b *TemporalWorkerDeploymentBuilder) WithTemporalConnection(connectionName string) *TemporalWorkerDeploymentBuilder {
	if b.twd.Spec.WorkerOptions.TemporalConnection == "" {
		b.twd.Spec.WorkerOptions.TemporalConnection = connectionName
	}
	return b
}

// WithTemporalNamespace sets the temporal namespace
func (b *TemporalWorkerDeploymentBuilder) WithTemporalNamespace(temporalNamespace string) *TemporalWorkerDeploymentBuilder {
	b.twd.Spec.WorkerOptions.TemporalNamespace = temporalNamespace
	return b
}

// WithLabels sets the labels
func (b *TemporalWorkerDeploymentBuilder) WithLabels(labels map[string]string) *TemporalWorkerDeploymentBuilder {
	if b.twd.ObjectMeta.Labels == nil {
		b.twd.ObjectMeta.Labels = make(map[string]string)
	}
	for k, v := range labels {
		b.twd.ObjectMeta.Labels[k] = v
	}
	return b
}

// WithCurrentVersionStatus sets the status to have a current version
func (b *TemporalWorkerDeploymentBuilder) WithCurrentVersionStatus(namespace, name, version string, hasDeployment, isRegistered bool) *TemporalWorkerDeploymentBuilder {
	b.twd.Status.CurrentVersion = testhelpers.MakeCurrentVersion(namespace, name, version, hasDeployment, isRegistered)
	return b
}

// Build returns the constructed TemporalWorkerDeployment
func (b *TemporalWorkerDeploymentBuilder) Build() *temporaliov1alpha1.TemporalWorkerDeployment {
	// Set defaults if not already set
	if b.twd.Spec.WorkerOptions.TemporalConnection == "" {
		b.twd.Spec.WorkerOptions.TemporalConnection = b.twd.ObjectMeta.Name
	}

	if b.twd.ObjectMeta.Labels == nil {
		b.twd.ObjectMeta.Labels = map[string]string{"app": "test-worker"}
	}

	return b.twd
}

// StatusBuilder provides a fluent interface for building expected status objects
type StatusBuilder struct {
	status *temporaliov1alpha1.TemporalWorkerDeploymentStatus
}

// NewStatusBuilder creates a new status builder
func NewStatusBuilder() *StatusBuilder {
	return &StatusBuilder{
		status: &temporaliov1alpha1.TemporalWorkerDeploymentStatus{},
	}
}

// WithCurrentVersion sets the current version in the status
func (sb *StatusBuilder) WithCurrentVersion(namespace, name, version string, hasDeployment, isRegistered bool) *StatusBuilder {
	sb.status.CurrentVersion = testhelpers.MakeCurrentVersion(namespace, name, version, hasDeployment, isRegistered)
	return sb
}

// WithTargetVersion sets the target version in the status
func (sb *StatusBuilder) WithTargetVersion(namespace, name, version string, rampPercentage float32) *StatusBuilder {
	sb.status.TargetVersion = &temporaliov1alpha1.TargetWorkerDeploymentVersion{
		BaseWorkerDeploymentVersion: temporaliov1alpha1.BaseWorkerDeploymentVersion{
			VersionID: testhelpers.MakeVersionId(namespace, name, version),
			Deployment: &corev1.ObjectReference{
				Namespace: namespace,
				Name: k8s.ComputeVersionedDeploymentName(
					name,
					testhelpers.MakeBuildId(name, version, nil),
				),
			},
		},
		RampPercentage: &rampPercentage,
	}
	return sb
}

// WithRampingVersion sets the ramping version in the status
func (sb *StatusBuilder) WithRampingVersion(namespace, name, version string, rampPercentage float32) *StatusBuilder {
	sb.status.RampingVersion = &temporaliov1alpha1.TargetWorkerDeploymentVersion{
		BaseWorkerDeploymentVersion: temporaliov1alpha1.BaseWorkerDeploymentVersion{
			VersionID: testhelpers.MakeVersionId(namespace, name, version),
			Deployment: &corev1.ObjectReference{
				Namespace: namespace,
				Name: k8s.ComputeVersionedDeploymentName(
					name,
					testhelpers.MakeBuildId(name, version, nil),
				),
			},
		},
		RampPercentage: &rampPercentage,
	}
	return sb
}

// Build returns the constructed status
func (sb *StatusBuilder) Build() *temporaliov1alpha1.TemporalWorkerDeploymentStatus {
	return sb.status
}

type testCaseStructure struct {
	// If starting from a particular state, specify that in input.Status
	twd *temporaliov1alpha1.TemporalWorkerDeployment
	// TemporalWorkerDeploymentStatus only tracks the names of the Deployments for deprecated
	// versions, so for test scenarios that start with existing deprecated version Deployments,
	// specify the number of replicas for each deprecated build here.
	deprecatedBuildReplicas map[string]int32
	deprecatedBuildImages   map[string]string
	expectedStatus          *temporaliov1alpha1.TemporalWorkerDeploymentStatus
}

// TestCaseBuilder provides a fluent interface for building test cases
type TestCaseBuilder struct {
	tc testCaseStructure
}

// NewTestCase creates a new test case builder
func NewTestCase() *TestCaseBuilder {
	return &TestCaseBuilder{
		tc: testCaseStructure{
			deprecatedBuildReplicas: make(map[string]int32),
			deprecatedBuildImages:   make(map[string]string),
		},
	}
}

// WithInput sets the input TWD
func (tcb *TestCaseBuilder) WithInput(twd *temporaliov1alpha1.TemporalWorkerDeployment) *TestCaseBuilder {
	tcb.tc.twd = twd
	return tcb
}

// WithDeprecatedBuildReplicas adds deprecated build replicas
func (tcb *TestCaseBuilder) WithDeprecatedBuildReplicas(buildId string, replicas int32) *TestCaseBuilder {
	tcb.tc.deprecatedBuildReplicas[buildId] = replicas
	return tcb
}

// WithDeprecatedBuildImages adds deprecated build images
func (tcb *TestCaseBuilder) WithDeprecatedBuildImages(buildId, image string) *TestCaseBuilder {
	tcb.tc.deprecatedBuildImages[buildId] = image
	return tcb
}

// WithExpectedStatus sets the expected status
func (tcb *TestCaseBuilder) WithExpectedStatus(status *temporaliov1alpha1.TemporalWorkerDeploymentStatus) *TestCaseBuilder {
	tcb.tc.expectedStatus = status
	return tcb
}

// Build returns the constructed test case
func (tcb *TestCaseBuilder) Build() testCaseStructure {
	return tcb.tc
}

// Helper function to create a progressive rollout step
func ProgressiveStep(rampPercentage float32, pauseDuration time.Duration) temporaliov1alpha1.RolloutStep {
	return temporaliov1alpha1.RolloutStep{
		RampPercentage: rampPercentage,
		PauseDuration:  metav1.Duration{Duration: pauseDuration},
	}
}
