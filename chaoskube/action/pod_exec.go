package action

import (
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"os"
)

func NewExecAction(client restclient.Interface, config *restclient.Config, containerName string, command []string) PodAction {
	return &execOnPod{client, config, containerName, command}
}

// Execute the given command on victim pods
type execOnPod struct {
	client restclient.Interface
	config *restclient.Config

	containerName string
	command       []string
}

func (s *execOnPod) Init(k8sclient kubernetes.Interface) error {
	return nil
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
