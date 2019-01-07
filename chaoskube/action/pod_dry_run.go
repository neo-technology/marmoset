package action

import (
	"k8s.io/api/core/v1"
)

func NewDryRunPodAction() PodAction {
	return &podDryRun{}
}

// no-op
type podDryRun struct {
}

func (s *podDryRun) ApplyToPod(victim v1.Pod) error {
	return nil
}
func (s *podDryRun) Name() string { return "dry run" }

var _ PodAction = &podDryRun{}
