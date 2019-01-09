package action

import (
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func NewDryRunPodAction() PodAction {
	return &podDryRun{}
}

// no-op
type podDryRun struct {
}

func (s *podDryRun) Init(k8sclient kubernetes.Interface) error {
	return nil
}
func (s *podDryRun) ApplyToPod(victim v1.Pod) error {
	return nil
}
func (s *podDryRun) Name() string { return "dry run" }

var _ PodAction = &podDryRun{}
