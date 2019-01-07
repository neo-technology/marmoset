package action

import (
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func NewDeletePodAction(client kubernetes.Interface) PodAction {
	return &deletePod{client}
}

// Simply ask k8s to delete the victim pod
type deletePod struct {
	client kubernetes.Interface
}

func (s *deletePod) ApplyToPod(victim v1.Pod) error {
	return s.client.CoreV1().Pods(victim.Namespace).Delete(victim.Name, nil)
}
func (s *deletePod) Name() string { return "delete pod" }

var _ PodAction = &deletePod{}
