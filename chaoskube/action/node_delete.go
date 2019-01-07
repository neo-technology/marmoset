package action

import (
"k8s.io/api/core/v1"
"k8s.io/client-go/kubernetes"
)

func NewDeleteNodeAction() NodeAction {
	return &deleteNode{}
}

type deleteNode struct{}

func (a *deleteNode) ApplyToNode(client kubernetes.Interface, victim *v1.Node) error {
	return client.CoreV1().Nodes().Delete(victim.Name, nil)
}
func (a *deleteNode) Name() string {
	return "delete node"
}
