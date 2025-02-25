// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type WorkerOptions struct {
	// The name of a TemporalConnection in the same namespace as the TemporalWorker.
	TemporalConnection string `json:"connection"`
	// The Temporal namespace for the worker to connect to.
	TemporalNamespace string `json:"temporalNamespace"`
	// Assign a deployment series name to this worker. Different versions of the same worker
	// service/application are linked together by sharing a series name.
	//
	// If not set, then the deployment name will default to the worker's name and Kubernetes
	// namespace.
	//
	// +optional
	DeploymentName string `json:"name"`
}

// TemporalWorkerSpec defines the desired state of TemporalWorker
type TemporalWorkerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	// This field makes TemporalWorkerSpec implement the scale subresource, which is compatible with auto-scalers.
	// TODO(jlegrone): Configure min replicas per thousand workflow/activity tasks?
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Label selector for pods. Existing ReplicaSets whose pods are
	// selected by this will be the ones affected by this deployment.
	// It must match the pod template's labels.
	// +optional
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,2,opt,name=selector"`

	// Template describes the pods that will be created.
	// The only allowed template.spec.restartPolicy value is "Always".
	Template v1.PodTemplateSpec `json:"template"`

	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`

	// The maximum time in seconds for a deployment to make progress before it
	// is considered to be failed. The deployment controller will continue to
	// process failed deployments and a condition with a ProgressDeadlineExceeded
	// reason will be surfaced in the deployment status. Note that progress will
	// not be estimated during the time a deployment is paused. Defaults to 600s.
	ProgressDeadlineSeconds *int32 `json:"progressDeadlineSeconds,omitempty" protobuf:"varint,9,opt,name=progressDeadlineSeconds"`

	// How to cut over new workflow executions to the target worker version.
	RolloutStrategy RolloutStrategy `json:"cutover"`

	// TODO(jlegrone): add godoc
	WorkerOptions WorkerOptions `json:"workerOptions"`
}

// VersionStatus indicates the status of a version.
// +enum
type VersionStatus string

const (
	// VersionStatusNotRegistered indicates that the version is not registered
	// with Temporal for any worker deployment.
	VersionStatusNotRegistered VersionStatus = "NotRegistered"

	// VersionStatusInactive indicates that the version is registered in a Temporal
	// worker deployment, but has not been set to current or ramping.
	// A version is registered in a worker deployment after a poller with appropriate
	// DeploymentOptions starts polling.
	VersionStatusInactive VersionStatus = "Inactive"

	// VersionStatusRamping indicates that the version is the ramping version of its
	// worker deployment. It is accepting some percentage of new workflow executions.
	VersionStatusRamping = "Ramping"

	// VersionStatusCurrent indicates that the version is the current version of its
	// worker deployment. It is accepting all new workflow executions except for the
	// percent that are sent to the ramping version, if one exists.
	VersionStatusCurrent = "Current"

	// VersionStatusDraining indicates that the version has stopped accepting new workflows
	// (is no longer ramping or current) and DOES have open workflows pinned to it.
	VersionStatusDraining VersionStatus = "Draining"

	// VersionStatusDrained indicates that the version has stopped accepting new workflows
	// (is no longer ramping or current) and does NOT have open workflows pinned to it.
	VersionStatusDrained VersionStatus = "Drained"
)

// TemporalWorkerStatus defines the observed state of TemporalWorker
type TemporalWorkerStatus struct {
	// Remember, status should be able to be reconstituted from the state of the world,
	// so it’s generally not a good idea to read from the status of the root object.
	// Instead, you should reconstruct it every run.

	// TargetVersion is the desired next version. If the version is nil,
	// then the controller should create it. If not nil, the controller should
	// wait for it to become healthy and then move it to the DefaultVersion.
	TargetVersion *WorkerDeploymentVersion `json:"targetVersion"`

	// TargetVersionRampPercentage is the ramp percentage of the target version.
	TargetVersionRampPercentage float32 `json:"targetVersionRampPercentage"`

	// TargetVersionRampingSinceTime is time when the target version first started ramping.
	TargetVersionRampingSinceTime *metav1.Time `json:"targetVersionRampingSinceTime"`

	// DefaultVersion is the version that is currently registered with
	// Temporal as the current version of its worker deployment. This must never be nil.
	//
	// RampPercentage should always be nil for this version.
	DefaultVersion *WorkerDeploymentVersion `json:"defaultVersion"`

	// DeprecatedVersions are deployment versions that are no longer the default. Any
	// deployment versions that are unreachable should be deleted by the controller.
	//
	// RampPercentage should only be set for DeprecatedVersions when rollout
	// strategy is set to manual.
	DeprecatedVersions []*WorkerDeploymentVersion `json:"deprecatedVersions,omitempty"`

	// TODO(jlegrone): Add description
	VersionConflictToken []byte `json:"versionConflictToken"`
}

// WorkflowExecutionStatus describes the current state of a workflow.
// +enum
type WorkflowExecutionStatus string

const (
	// WorkflowExecutionStatusRunning indicates that the workflow is currently running.
	WorkflowExecutionStatusRunning WorkflowExecutionStatus = "Running"
	// WorkflowExecutionStatusCompleted indicates that the workflow has completed successfully.
	WorkflowExecutionStatusCompleted WorkflowExecutionStatus = "Completed"
	// WorkflowExecutionStatusFailed indicates that the workflow has failed.
	WorkflowExecutionStatusFailed WorkflowExecutionStatus = "Failed"
	// WorkflowExecutionStatusCanceled indicates that the workflow has been canceled.
	WorkflowExecutionStatusCanceled WorkflowExecutionStatus = "Canceled"
	// WorkflowExecutionStatusTerminated indicates that the workflow has been terminated.
	WorkflowExecutionStatusTerminated WorkflowExecutionStatus = "Terminated"
	// WorkflowExecutionStatusTimedOut indicates that the workflow has timed out.
	WorkflowExecutionStatusTimedOut WorkflowExecutionStatus = "TimedOut"
)

type WorkflowExecution struct {
	WorkflowID string                  `json:"workflowID"`
	RunID      string                  `json:"runID"`
	Status     WorkflowExecutionStatus `json:"status"`
	TaskQueue  string                  `json:"taskQueue"`
}

type TaskQueue struct {
	// Name is the name of the task queue.
	Name string `json:"name"`
}

type WorkerDeploymentVersion struct {
	// Healthy indicates whether the deployment version is healthy.
	// +optional
	HealthySince *metav1.Time `json:"healthySince"`

	// The string representation of the deployment version.
	// Currently, this is always `deployment_name.build_id`.
	VersionID string `json:"versionID"`

	// Status indicates whether workers in this version may
	// be eligible to receive tasks from the Temporal server.
	Status VersionStatus `json:"status"`

	// RampPercentage is the percentage of new workflow executions that are
	// configured to start on this version.
	//
	// Acceptable range is [0,100].
	RampPercentage *float32 `json:"rampPercentage,omitempty"`

	// DrainedSince is the time at which the version
	// became drained.
	DrainedSince *metav1.Time `json:"drainedSince"`

	// A pointer to the version's managed k8s deployment.
	// +optional
	Deployment *v1.ObjectReference `json:"deployment"`

	// TaskQueues is a list of task queues that are associated with this version.
	TaskQueues []TaskQueue `json:"taskQueues,omitempty"`

	// A TestWorkflow is used to validate the deployment version before making it the default.
	// +optional
	TestWorkflows []WorkflowExecution `json:"testWorkflows,omitempty"`

	// ManagedBy is the identity of the client that is managing the rollout of this version.
	// +optional
	ManagedBy string `json:"managedBy,omitempty"`
}

// DefaultVersionUpdateStrategy describes how to cut over new workflow executions
// to the target worker deployment version.
// +kubebuilder:validation:Enum=Manual;AllAtOnce;Progressive
type DefaultVersionUpdateStrategy string

const (
	UpdateManual DefaultVersionUpdateStrategy = "Manual"

	UpdateAllAtOnce DefaultVersionUpdateStrategy = "AllAtOnce"

	UpdateProgressive DefaultVersionUpdateStrategy = "Progressive"
)

type GateWorkflowConfig struct {
	WorkflowType string `json:"workflowType"`
}

// RolloutStrategy defines strategy to apply during next rollout
type RolloutStrategy struct {
	// Specifies how to treat concurrent executions of a Job.
	// Valid values are:
	// - "Manual": do not automatically update the default worker deployment version;
	// - "AllAtOnce": start 100% of new workflow executions on the new worker deployment version as soon as it's healthy;
	// - "Progressive": ramp up the percentage of new workflow executions targeting the new worker deployment version over time.
	Strategy DefaultVersionUpdateStrategy `json:"strategy"`

	// Gate specifies a workflow type that must run once to completion on the new worker deployment version before
	// any traffic is directed to the new version.
	Gate *GateWorkflowConfig `json:"gate,omitempty"`

	// Steps to execute progressive rollouts. Only required when strategy is "Progressive".
	// +optional
	Steps []RolloutStep `json:"steps,omitempty" protobuf:"bytes,3,rep,name=steps"`
}

type AllAtOnceRolloutStrategy struct{}

type RolloutStep struct {
	// RampPercentage indicates what percentage of new workflow executions should be
	// routed to the new worker deployment version while this step is active.
	//
	// Acceptable range is [0,100].
	RampPercentage float32 `json:"rampPercentage"`

	// PauseDuration indicates how long to pause before progressing to the next step.
	PauseDuration metav1.Duration `json:"pauseDuration"`
}

type ManualRolloutStrategy struct{}

type QueueStatistics struct {
	// The approximate number of tasks backlogged in this task queue. May count expired tasks but eventually converges
	// to the right value.
	ApproximateBacklogCount int64 `json:"approximateBacklogCount,omitempty"`
	// Approximate age of the oldest task in the backlog based on the creation timestamp of the task at the head of the queue.
	ApproximateBacklogAge metav1.Duration `json:"approximateBacklogAge,omitempty"`
	// Approximate tasks per second added to the task queue based on activity within a fixed window. This includes both backlogged and
	// sync-matched tasks.
	TasksAddRate float32 `json:"tasksAddRate,omitempty"`
	// Approximate tasks per second dispatched to workers based on activity within a fixed window. This includes both backlogged and
	// sync-matched tasks.
	TasksDispatchRate float32 `json:"tasksDispatchRate,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Default",type="string",JSONPath=".status.defaultVersion.buildID",description="Default BuildID for new workflows"
//+kubebuilder:printcolumn:name="Target",type="string",JSONPath=".status.targetVersion.buildID",description="BuildID of the current worker template"
//+kubebuilder:printcolumn:name="Target-Ramp",type="integer",JSONPath=".status.targetVersion.rampPercentage",description="Percentage of new workflows starting on Target BuildID"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TemporalWorker is the Schema for the temporalworkers API
//
// TODO(jlegrone): Implement default/validate interface https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
type TemporalWorker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemporalWorkerSpec   `json:"spec,omitempty"`
	Status TemporalWorkerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TemporalWorkerList contains a list of TemporalWorker
type TemporalWorkerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemporalWorker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemporalWorker{}, &TemporalWorkerList{})
}
