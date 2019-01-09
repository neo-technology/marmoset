package action

import (
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type NodeAction interface {
	// Called once at startup, do any initial setup here
	Init(k8sclient kubernetes.Interface) error
	// Imbue chaos in the given victim
	ApplyToNode(client kubernetes.Interface, victim *v1.Node) error
	// Name of this action, ideally a verb - like "terminate pod"
	Name() string
}

type PodAction interface {
	// Called once at startup, do any initial setup here
	Init(k8sclient kubernetes.Interface) error
	// Imbue chaos in the given victim
	ApplyToPod(victim v1.Pod) error
	// Name of this action, ideally a verb - like "terminate pod"
	Name() string
}
