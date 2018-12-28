package chaoskube

import (
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"os"
)

type NodeAction interface {
	// Imbue chaos in the given victim
	ApplyToNode(client kubernetes.Interface, victim *v1.Node) error
	// Name of this action, ideally a verb - like "terminate pod"
	Name() string
}

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

type PodAction interface {
	// Imbue chaos in the given victim
	ApplyToPod(victim v1.Pod) error
	// Name of this action, ideally a verb - like "terminate pod"
	Name() string
}

func NewDryRunPodAction() PodAction {
	return &podDryRun{}
}

func NewDeletePodAction(client kubernetes.Interface) PodAction {
	return &deletePod{client}
}

func NewExecAction(client restclient.Interface, config *restclient.Config, containerName string, command []string) PodAction {
	return &execOnPod{client, config, containerName, command}
}

// no-op
type podDryRun struct {
}

func (s *podDryRun) ApplyToPod(victim v1.Pod) error {
	return nil
}
func (s *podDryRun) Name() string { return "dry run" }

var _ PodAction = &podDryRun{}

// Simply ask k8s to delete the victim pod
type deletePod struct {
	client kubernetes.Interface
}

func (s *deletePod) ApplyToPod(victim v1.Pod) error {
	return s.client.CoreV1().Pods(victim.Namespace).Delete(victim.Name, nil)
}
func (s *deletePod) Name() string { return "delete pod" }

var _ PodAction = &deletePod{}

// Execute the given command on victim pods
type execOnPod struct {
	client restclient.Interface
	config *restclient.Config

	containerName string
	command       []string
}

// Based on https://github.com/kubernetes/kubernetes/blob/master/pkg/kubectl/cmd/exec.go
func (s *execOnPod) ApplyToPod(pod v1.Pod) error {
	var container string
	if s.containerName == "" {
		for _, c := range pod.Spec.Containers {
			container = c.Name
			break
		}
	} else {
		container = s.containerName
	}

	req := s.client.Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("container", container)
	req.VersionedParams(&v1.PodExecOptions{
		Container: container,
		Command:   s.command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		return err
	}
	// TODO: Collect stderr/stdout in RAM and log
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             nil,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		Tty:               false,
		TerminalSizeQueue: nil,
	})

	return err
}
func (s *execOnPod) Name() string { return fmt.Sprintf("exec '%v'", s.command) }

var _ PodAction = &execOnPod{}
