package internal

import (
	"context"
	"os"
	"testing"
	"time"

	temporaliov1alpha1 "github.com/temporalio/temporal-worker-controller/api/v1alpha1"
	"github.com/temporalio/temporal-worker-controller/internal/k8s"
	"go.temporal.io/server/common/dynamicconfig"
	"go.temporal.io/server/temporal"
	"go.temporal.io/server/temporaltest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO (Shivam): Redefining these for now to make readability of this file easier
const (
	testPollerHistoryTTLDuplicate              = time.Second
	testDrainageVisibilityGracePeriodDuplicate = time.Second
	testDrainageRefreshIntervalDuplicate       = time.Second
)

// TestIntegrationWithBuilder shows how the test would look using the builder pattern
func TestIntegrationWithBuilder(t *testing.T) {
	// Set faster reconcile interval for testing
	os.Setenv("RECONCILE_INTERVAL", "1s")

	// Set up test environment
	cfg, k8sClient, _, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// Create test namespace
	testNamespace := createTestNamespace(t, k8sClient)
	defer cleanupTestNamespace(t, cfg, k8sClient, testNamespace)

	// Create test Temporal server and client
	dc := dynamicconfig.NewMemoryClient()
	// make versions eligible for deletion faster
	dc.OverrideValue("matching.PollerHistoryTTL", testPollerHistoryTTLDuplicate)
	// make versions drain faster
	dc.OverrideValue("matching.wv.VersionDrainageStatusVisibilityGracePeriod", testDrainageVisibilityGracePeriodDuplicate)
	dc.OverrideValue("matching.wv.VersionDrainageStatusRefreshInterval", testDrainageRefreshIntervalDuplicate)
	ts := temporaltest.NewServer(
		temporaltest.WithT(t),
		temporaltest.WithBaseServerOptions(temporal.WithDynamicConfigClient(dc)),
	)

	testRampPercentStep1 := float32(5)

	tests := map[string]testCaseStructure{
		"all-at-once-rollout-2-replicas": NewTestCase().
			WithInput(
				NewTemporalWorkerDeploymentBuilder("all-at-once-rollout-2-replicas").
					WithAllAtOnceStrategy().
					WithReplicas(2).
					WithVersion("v1").
					WithNamespace(testNamespace.Name).
					WithTemporalConnection("all-at-once-rollout-2-replicas").
					WithTemporalNamespace(ts.GetDefaultNamespace()).
					Build(),
			).
			WithExpectedStatus(
				NewStatusBuilder().
					WithCurrentVersion(testNamespace.Name, "all-at-once-rollout-2-replicas", "v1", true, false).
					Build(),
			).
			Build(),

		"progressive-rollout-expect-first-step": NewTestCase().
			WithInput(
				NewTemporalWorkerDeploymentBuilder("progressive-rollout-expect-first-step").
					WithProgressiveStrategy(ProgressiveStep(5, time.Hour)).
					WithVersion("v1").
					WithNamespace(testNamespace.Name).
					WithTemporalConnection("progressive-rollout-expect-first-step").
					WithTemporalNamespace(ts.GetDefaultNamespace()).
					WithCurrentVersionStatus(testNamespace.Name, "progressive-rollout-expect-first-step", "v0", true, true).
					Build(),
			).
			WithDeprecatedBuildReplicas("progressive-rollout-expect-first-step:v0", 1).
			WithDeprecatedBuildImages("progressive-rollout-expect-first-step:v0", "v0").
			WithExpectedStatus(
				NewStatusBuilder().
					WithTargetVersion(testNamespace.Name, "progressive-rollout-expect-first-step", "v1", testRampPercentStep1).
					WithRampingVersion(testNamespace.Name, "progressive-rollout-expect-first-step", "v1", testRampPercentStep1).
					Build(),
			).
			Build(),
	}

	for testName, tc := range tests {
		t.Run(testName, func(t *testing.T) {
			ctx := context.Background()
			TestTemporalWorkerDeploymentCreation(ctx, t, k8sClient, ts, tc)
		})
	}
}

// Even better: we could create specific test case factory functions for common patterns
func CreateAllAtOnceTestCase(name string, namespace string, replicas int32, temporalNamespace string) testCaseStructure {
	return NewTestCase().
		WithInput(
			NewTemporalWorkerDeploymentBuilder(name).
				WithAllAtOnceStrategy().
				WithReplicas(replicas).
				WithVersion("v1").
				WithNamespace(namespace).
				WithTemporalConnection(name).
				WithTemporalNamespace(temporalNamespace).
				Build(),
		).
		WithExpectedStatus(
			NewStatusBuilder().
				WithCurrentVersion(namespace, name, "v1", true, false).
				Build(),
		).
		Build()
}

func CreateProgressiveTestCase(name string, namespace string, temporalNamespace string, rampPercentage float32) testCaseStructure {
	return NewTestCase().
		WithInput(
			NewTemporalWorkerDeploymentBuilder(name).
				WithProgressiveStrategy(ProgressiveStep(rampPercentage, time.Hour)).
				WithVersion("v1").
				WithNamespace(namespace).
				WithTemporalConnection(name).
				WithTemporalNamespace(temporalNamespace).
				WithCurrentVersionStatus(namespace, name, "v0", true, true).
				Build(),
		).
		WithDeprecatedBuildReplicas(name+":v0", 1).
		WithDeprecatedBuildImages(name+":v0", "v0").
		WithExpectedStatus(
			NewStatusBuilder().
				WithTargetVersion(namespace, name, "v1", rampPercentage).
				WithRampingVersion(namespace, name, "v1", rampPercentage).
				Build(),
		).
		Build()
}

// TestTemporalWorkerDeploymentCreation tests the creation of a TemporalWorkerDeployment and waits for the expected status
func TestTemporalWorkerDeploymentCreation(
	ctx context.Context,
	t *testing.T,
	k8sClient client.Client,
	ts *temporaltest.TestServer,
	tc testCaseStructure,
) {
	twd := tc.twd
	expectedStatus := tc.expectedStatus

	t.Log("Creating a TemporalConnection")
	temporalConnection := &temporaliov1alpha1.TemporalConnection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      twd.Spec.WorkerOptions.TemporalConnection,
			Namespace: twd.Namespace,
		},
		Spec: temporaliov1alpha1.TemporalConnectionSpec{
			HostPort: ts.GetFrontendHostPort(),
		},
	}
	if err := k8sClient.Create(ctx, temporalConnection); err != nil {
		t.Fatalf("failed to create TemporalConnection: %v", err)
	}

	MakePreliminaryStatusTrue(ctx, t, k8sClient, ts, twd, temporalConnection, tc.deprecatedBuildReplicas, tc.deprecatedBuildImages)

	t.Log("Creating a TemporalWorkerDeployment")
	if err := k8sClient.Create(ctx, twd); err != nil {
		t.Fatalf("failed to create TemporalWorkerDeployment: %v", err)
	}

	t.Log("Waiting for the controller to reconcile")
	expectedDeploymentName := k8s.ComputeVersionedDeploymentName(twd.Name, k8s.ComputeBuildID(twd))
	waitForDeployment(t, k8sClient, expectedDeploymentName, twd.Namespace, 30*time.Second)
	workerStopFuncs := applyDeployment(t, ctx, k8sClient, expectedDeploymentName, twd.Namespace)
	defer func() {
		for _, f := range workerStopFuncs {
			if f != nil {
				f()
			}
		}
	}()

	verifyTemporalWorkerDeploymentStatusEventually(t, ctx, k8sClient, twd.Name, twd.Namespace, expectedStatus, 60*time.Second, 10*time.Second)
}

// Uses input.Status + deprecatedBuildReplicas to create (and maybe kill) pollers for deprecated versions in temporal
// also gets routing config of the deployment into the starting state before running the test.
// Does not set Status.VersionConflictToken, since that is only set internally by the server.
func MakePreliminaryStatusTrue(
	ctx context.Context,
	t *testing.T,
	k8sClient client.Client,
	ts *temporaltest.TestServer,
	twd *temporaliov1alpha1.TemporalWorkerDeployment,
	connection *temporaliov1alpha1.TemporalConnection,
	replicas map[string]int32,
	images map[string]string,
) {
	t.Logf("Creating starting test env based on input.Status")
	for _, dv := range twd.Status.DeprecatedVersions {
		t.Logf("Handling deprecated version %v", dv.VersionID)
		switch dv.Status {
		case temporaliov1alpha1.VersionStatusInactive:
			// start a poller -- is this included in deprecated versions list?
		case temporaliov1alpha1.VersionStatusRamping, temporaliov1alpha1.VersionStatusCurrent:
			// these won't be in deprecated versions
		case temporaliov1alpha1.VersionStatusDraining:
			// TODO(carlydf): start a poller, set ramp, start a wf on that version, then unset
		case temporaliov1alpha1.VersionStatusDrained:
			// TODO(carlydf): start a poller, set ramp, unset, wait for drainage status visibility grace period
		case temporaliov1alpha1.VersionStatusNotRegistered:
			// no-op, although I think this won't occur in deprecated versions either
		}
	}
	// TODO(carlydf): handle Status.LastModifierIdentity
	if cv := twd.Status.CurrentVersion; cv != nil {
		t.Logf("Handling current version %v", cv.VersionID)
		if cv.Status != temporaliov1alpha1.VersionStatusCurrent {
			t.Errorf("Current Version's status must be Current")
		}
		if cv.Deployment != nil {
			t.Logf("Creating Deployment %s for Current Version", cv.Deployment.Name)
			CreateWorkerDeployment(ctx, t, k8sClient, twd, cv.VersionID, connection.Spec, replicas, images)
			expectedDeploymentName := k8s.ComputeVersionedDeploymentName(twd.Name, k8s.ComputeBuildID(twd))
			waitForDeployment(t, k8sClient, expectedDeploymentName, twd.Namespace, 30*time.Second)
			workerStopFuncs := applyDeployment(t, ctx, k8sClient, expectedDeploymentName, twd.Namespace)
			defer func() {
				for _, f := range workerStopFuncs {
					if f != nil {
						f()
					}
				}
			}()
		}
	}

	if rv := twd.Status.RampingVersion; rv != nil {
		t.Logf("Handling ramping version %v", rv.VersionID)
		if rv.Status != temporaliov1alpha1.VersionStatusRamping {
			t.Errorf("Ramping Version's status must be Ramping")
		}
		if rv.Deployment != nil {
			t.Logf("Creating Deployment %s for Ramping Version", rv.Deployment.Name)
		}
		// TODO(carlydf): do this
	}
}

func CreateWorkerDeployment(
	ctx context.Context,
	t *testing.T,
	k8sClient client.Client,
	twd *temporaliov1alpha1.TemporalWorkerDeployment,
	versionID string,
	connection temporaliov1alpha1.TemporalConnectionSpec,
	replicas map[string]int32,
	images map[string]string,
) {
	t.Log("Creating a Deployment")
	_, buildId, err := k8s.SplitVersionID(versionID)
	if err != nil {
		t.Error(err)
	}

	prevImageName := twd.Spec.Template.Spec.Containers[0].Image
	prevReplicas := twd.Spec.Replicas
	// temporarily replace it
	twd.Spec.Template.Spec.Containers[0].Image = images[buildId]
	newReplicas := replicas[buildId]
	twd.Spec.Replicas = &newReplicas
	defer func() {
		twd.Spec.Template.Spec.Containers[0].Image = prevImageName
		twd.Spec.Replicas = prevReplicas
	}()

	dep := k8s.NewDeploymentWithOwnerRef(
		&twd.TypeMeta,
		&twd.ObjectMeta,
		&twd.Spec,
		k8s.ComputeWorkerDeploymentName(twd),
		buildId,
		connection,
	)

	if err := k8sClient.Create(ctx, dep); err != nil {
		t.Fatalf("failed to create Deployment: %v", err)
	}
}
