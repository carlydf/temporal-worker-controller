// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

const (
	// TODO(carlydf): make this configurable by the user
	defaultScaleToZeroWaitTime = 1 * time.Minute
	defaultDeleteWaitTime      = 1 * time.Hour
)

type plan struct {
	// Where to take actions

	TemporalNamespace string
	DeploymentName    string

	// Which actions to take

	DeleteDeployments []*appsv1.Deployment
	CreateDeployment  *appsv1.Deployment
	ScaleDeployments  map[*v1.ObjectReference]uint32
	// Register a new build ID as the default or with ramp
	UpdateVersionConfig *versionConfig

	// Start a workflow
	startTestWorkflows []startWorkflowConfig
}

type versionConfig struct {
	// Token to use for conflict detection
	conflictToken []byte
	// version ID for which this config applies
	versionID string

	// One of rampPercentage OR setDefault must be set to a non-zero value.

	// Set this as the default build ID for all new executions
	setDefault bool
	// Acceptable values [0,100]
	rampPercentage float32
}

type startWorkflowConfig struct {
	workflowType string
	workflowID   string
	versionID    string
	taskQueue    string
}

func (r *TemporalWorkerReconciler) generatePlan(
	ctx context.Context,
	w *temporaliov1alpha1.TemporalWorker,
	connection temporaliov1alpha1.TemporalConnectionSpec,
) (*plan, error) {
	plan := plan{
		TemporalNamespace: w.Spec.WorkerOptions.TemporalNamespace,
		DeploymentName:    w.Spec.WorkerOptions.DeploymentName,
		ScaleDeployments:  make(map[*v1.ObjectReference]uint32),
	}

	// Scale the active deployment if it doesn't match desired replicas
	if w.Status.DefaultVersion != nil && w.Status.DefaultVersion.Deployment != nil {
		defaultDeployment := w.Status.DefaultVersion.Deployment
		d, err := r.getDeployment(ctx, defaultDeployment)
		if err != nil {
			return nil, err
		}
		if d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas {
			plan.ScaleDeployments[defaultDeployment] = uint32(*w.Spec.Replicas)
		}
	}

	// TODO(jlegrone): generate warnings/events on the TemporalWorker resource when buildIDs are reachable
	//                 but have no corresponding Deployment.

	// Scale or delete deployments based on drainage status
	for _, version := range w.Status.DeprecatedVersions {
		if version.Deployment == nil {
			// There's nothing we can do if the deployment was already deleted out of band.
			continue
		}

		d, err := r.getDeployment(ctx, version.Deployment)
		if err != nil {
			return nil, err
		}
		// TODO(jlegrone): Compute scale based on load? Or percentage of replicas?
		// TODO(carlydf): Consolidate scale up cases and verify that scale up is correct for inactive versions
		switch version.Status {
		case temporaliov1alpha1.VersionStatusInactive:
			// Scale up inactive deployments because they may be needed for canary tests
			if d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas {
				plan.ScaleDeployments[version.Deployment] = uint32(*w.Spec.Replicas)
			}
		case temporaliov1alpha1.VersionStatusRamping:
			// Scale up ramping deployments
			if d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas {
				plan.ScaleDeployments[version.Deployment] = uint32(*w.Spec.Replicas)
			}
		case temporaliov1alpha1.VersionStatusCurrent:
			// Scale up current deployments
			if d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas {
				plan.ScaleDeployments[version.Deployment] = uint32(*w.Spec.Replicas)
			}
		case temporaliov1alpha1.VersionStatusDrained:
			if time.Since(version.DrainedSince.Time) > defaultDeleteWaitTime {
				plan.DeleteDeployments = append(plan.DeleteDeployments, d)
			} else if time.Since(version.DrainedSince.Time) > defaultScaleToZeroWaitTime {
				// Scale down drained deployments. We do this instead
				// of deleting them so that they can be scaled back up if
				// their build ID is promoted to default again (i.e. during
				// a rollback).
				if d.Spec.Replicas != nil && *d.Spec.Replicas != 0 {
					plan.ScaleDeployments[version.Deployment] = 0
				}
			}
		case temporaliov1alpha1.VersionStatusNotRegistered:
			// Delete unregistered deployments
			plan.DeleteDeployments = append(plan.DeleteDeployments, d)
		}
	}

	desiredVersionID := computeVersionID(&w.Spec)

	if targetVersion := w.Status.TargetVersion; targetVersion != nil {
		if targetVersion.Deployment == nil {
			// Create new deployment from current pod template when it doesn't exist
			d, err := r.newDeployment(w, desiredVersionID, connection)
			if err != nil {
				return nil, err
			}
			existing, _ := r.getDeployment(ctx, newObjectRef(d))
			if existing == nil {
				plan.CreateDeployment = d
			}
		} else {
			d, err := r.getDeployment(ctx, targetVersion.Deployment)
			if err != nil {
				return nil, err
			}

			if targetVersion.VersionID != desiredVersionID {
				// Delete the latest (unregistered) deployment if the desired version ID has changed
				plan.DeleteDeployments = append(plan.DeleteDeployments, d)
			} else {
				// Scale the existing deployment and update versioning config

				// Scale deployment if necessary
				if d.Spec.Replicas == nil || (d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas) {
					plan.ScaleDeployments[newObjectRef(d)] = uint32(*w.Spec.Replicas)
				}

				// Start a test workflow if the target version is not yet the default version and no test workflow is already running
				if w.Status.DefaultVersion.VersionID != targetVersion.VersionID && w.Spec.RolloutStrategy.Gate != nil {
					taskQueuesWithWorkflows := map[string]struct{}{}
					for _, wf := range targetVersion.TestWorkflows {
						taskQueuesWithWorkflows[wf.TaskQueue] = struct{}{}
					}
					for _, tq := range targetVersion.TaskQueues {
						if _, ok := taskQueuesWithWorkflows[tq.Name]; !ok {
							plan.startTestWorkflows = append(plan.startTestWorkflows, startWorkflowConfig{
								workflowType: w.Spec.RolloutStrategy.Gate.WorkflowType,
								workflowID:   getTestWorkflowID(plan.DeploymentName, tq.Name, targetVersion.VersionID),
								versionID:    targetVersion.VersionID,
								taskQueue:    tq.Name,
							})
						}
					}
				}

				// Update version configuration
				plan.UpdateVersionConfig = getVersionConfigDiff(w.Spec.RolloutStrategy, &w.Status)
				if plan.UpdateVersionConfig != nil {
					plan.UpdateVersionConfig.conflictToken = w.Status.VersionConflictToken
				}
			}
		}
	}

	return &plan, nil
}

func getOldestBuildIDCreateTime(rules *workflowservice.GetWorkerVersioningRulesResponse, buildID string) *timestamppb.Timestamp {
	var rule *taskqueue.TimestampedBuildIdAssignmentRule
	for _, r := range rules.GetAssignmentRules() {
		if r.GetRule().GetTargetBuildId() != buildID {
			break
		}
		rule = r
	}
	return rule.GetCreateTime()
}

func getVersionConfigDiff(strategy temporaliov1alpha1.RolloutStrategy, status *temporaliov1alpha1.TemporalWorkerStatus) *versionConfig {
	vcfg := getVersionConfig(strategy, status)
	if vcfg == nil {
		return nil
	}
	vcfg.versionID = status.TargetVersion.VersionID

	// Set default version if there isn't one yet
	if status.DefaultVersion == nil {
		vcfg.setDefault = true
		vcfg.rampPercentage = 0
		return vcfg
	}

	// Don't make updates if desired default is already the default
	if vcfg.setDefault &&
		vcfg.versionID == status.DefaultVersion.VersionID {
		return nil
	}

	// Don't make updates if desired ramping version is already the target, and ramp percentage is correct
	if !vcfg.setDefault &&
		vcfg.versionID == status.TargetVersion.VersionID &&
		vcfg.rampPercentage == status.TargetVersionRampPercentage {
		return nil
	}

	return vcfg
}

func getVersionConfig(strategy temporaliov1alpha1.RolloutStrategy, status *temporaliov1alpha1.TemporalWorkerStatus) *versionConfig {
	// Do nothing if target version's deployment is not healthy yet
	if status == nil || status.TargetVersion.HealthySince == nil {
		return nil
	}

	// Do nothing if the test workflows have not completed successfully
	if strategy.Gate != nil {
		if len(status.TargetVersion.TaskQueues) == 0 {
			return nil
		}
		if len(status.TargetVersion.TestWorkflows) < len(status.TargetVersion.TaskQueues) {
			return nil
		}
		for _, wf := range status.TargetVersion.TestWorkflows {
			if wf.Status != temporaliov1alpha1.WorkflowExecutionStatusCompleted {
				return nil
			}
		}
	}

	switch strategy.Strategy {
	case temporaliov1alpha1.UpdateManual:
		return nil
	case temporaliov1alpha1.UpdateAllAtOnce:
		// Set new default version immediately
		return &versionConfig{
			setDefault: true,
		}
	case temporaliov1alpha1.UpdateProgressive:
		// Determine the correct percentage ramp
		var (
			healthyDuration    time.Duration
			currentRamp        float32
			totalPauseDuration = healthyDuration
		)
		if status.TargetVersionRampingSinceTime != nil {
			healthyDuration = time.Since(status.TargetVersionRampingSinceTime.Time)
		}
		for _, s := range strategy.Steps {
			if s.RampPercentage != 0 {
				currentRamp = s.RampPercentage
			}
			totalPauseDuration += s.PauseDuration.Duration
			if healthyDuration < totalPauseDuration {
				break
			}
		}
		// We've progressed through all steps; it should now be safe to update the default version
		if healthyDuration > 0 && healthyDuration > totalPauseDuration {
			return &versionConfig{
				setDefault: true,
			}
		}
		// We haven't finished waiting for all steps; use the latest ramp value
		return &versionConfig{
			rampPercentage: currentRamp,
		}
	}

	return nil
}

func (r *TemporalWorkerReconciler) getDeployment(ctx context.Context, ref *v1.ObjectReference) (*appsv1.Deployment, error) {
	var d appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *TemporalWorkerReconciler) newDeployment(
	w *temporaliov1alpha1.TemporalWorker,
	buildID string,
	connection temporaliov1alpha1.TemporalConnectionSpec,
) (*appsv1.Deployment, error) {
	d := newDeploymentWithoutOwnerRef(&w.TypeMeta, &w.ObjectMeta, &w.Spec, buildID, connection)
	if err := ctrl.SetControllerReference(w, d, r.Scheme); err != nil {
		return nil, err
	}
	return d, nil
}

func newDeploymentWithoutOwnerRef(
	typeMeta *metav1.TypeMeta,
	objectMeta *metav1.ObjectMeta,
	spec *temporaliov1alpha1.TemporalWorkerSpec,
	buildID string,
	connection temporaliov1alpha1.TemporalConnectionSpec,
) *appsv1.Deployment {
	selectorLabels := map[string]string{}
	// Merge labels from TemporalWorker with build ID
	if spec.Selector != nil {
		for k, v := range spec.Selector.MatchLabels {
			selectorLabels[k] = v
		}
	}
	selectorLabels[buildIDLabel] = buildID

	// Set pod labels
	if spec.Template.Labels == nil {
		spec.Template.Labels = selectorLabels
	} else {
		for k, v := range selectorLabels {
			spec.Template.Labels[k] = v
		}
	}

	for i, container := range spec.Template.Spec.Containers {
		container.Env = append(container.Env,
			v1.EnvVar{
				Name:  "TEMPORAL_HOST_PORT",
				Value: connection.HostPort,
			},
			v1.EnvVar{
				Name:  "TEMPORAL_NAMESPACE",
				Value: spec.WorkerOptions.TemporalNamespace,
			},
			v1.EnvVar{
				Name:  "TEMPORAL_DEPLOYMENT_NAME",
				Value: spec.WorkerOptions.DeploymentName,
			},
			v1.EnvVar{
				Name:  "WORKER_BUILD_ID",
				Value: buildID,
			},
		)
		spec.Template.Spec.Containers[i] = container
	}

	// Add TLS config if mTLS is enabled
	if connection.MutualTLSSecret != "" {
		for i, container := range spec.Template.Spec.Containers {
			container.Env = append(container.Env,
				v1.EnvVar{
					Name:  "TEMPORAL_TLS_KEY_PATH",
					Value: "/etc/temporal/tls/tls.key",
				},
				v1.EnvVar{
					Name:  "TEMPORAL_TLS_CERT_PATH",
					Value: "/etc/temporal/tls/tls.crt",
				},
			)
			container.VolumeMounts = append(container.VolumeMounts, v1.VolumeMount{
				Name:      "temporal-tls",
				MountPath: "/etc/temporal/tls",
			})
			spec.Template.Spec.Containers[i] = container
		}
		spec.Template.Spec.Volumes = append(spec.Template.Spec.Volumes, v1.Volume{
			Name: "temporal-tls",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: connection.MutualTLSSecret,
				},
			},
		})
	}

	blockOwnerDeletion := true

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       fmt.Sprintf("%s-%s", objectMeta.Name, buildID),
			Namespace:                  objectMeta.Namespace,
			DeletionGracePeriodSeconds: nil,
			Labels:                     selectorLabels,
			Annotations:                spec.Template.Annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         typeMeta.APIVersion,
				Kind:               typeMeta.Kind,
				Name:               objectMeta.Name,
				UID:                objectMeta.UID,
				BlockOwnerDeletion: &blockOwnerDeletion,
				Controller:         nil,
			}},
			// TODO(jlegrone): Add finalizer managed by the controller in order to prevent
			//                 deleting deployments that are still reachable.
			Finalizers: nil,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template:        spec.Template,
			MinReadySeconds: spec.MinReadySeconds,
		},
	}
}
