package action

import (
	"fmt"
	"k8s.io/api/core/v1"
	k8spolicy "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	EvictionKind          = "Eviction"
	LabelMarmosetCordoned = "marmoset/cordoned"
)

func NewDrainNodeAction() NodeAction {
	return &drainNode{}
}

type drainNode struct{}

func (s *drainNode) Init(client kubernetes.Interface) error {
	return crashRecoverNodeDrain(client)
}
func (a *drainNode) ApplyToNode(client kubernetes.Interface, victim *v1.Node) (err error) {
	victim = victim.DeepCopy()
	if err = crashRecoverNodeDrain(client); err != nil {
		return err
	}

	// No matter what, try to uncordon the node before we're done here
	defer func() {
		_, deferErr := uncordonNode(client, victim)
		// If there's no other error, set the return error to be whatever the outcome
		// of the uncordon was; otherwise don't overwrite any prior error.
		if err == nil {
			err = deferErr
		}
	}()

	// Label and Cordon node
	victim, err = cordonNode(client, victim)
	if err != nil {
		return err
	}

	// Create evictions for all non-daemon nodes
	if err = evictAllPodsOnNode(client, victim); err != nil {
		return err
	}

	return err
}
func (a *drainNode) Name() string {
	return "cordon node"
}

func cordonNode(client kubernetes.Interface, victim *v1.Node) (*v1.Node, error) {
	return updateNode(client, victim, func(node *v1.Node) {
		if node.Labels == nil {
			node.Labels = make(map[string]string, 0)
		}
		node.Labels[LabelMarmosetCordoned] = "true"
		node.Spec.Unschedulable = true
	})
}

func uncordonNode(client kubernetes.Interface, victim *v1.Node) (*v1.Node, error) {
	return updateNode(client, victim, func(node *v1.Node) {
		delete(node.Labels, LabelMarmosetCordoned)
		node.Spec.Unschedulable = false
	})
}

// update with retry-on-out-of-date
func updateNode(client kubernetes.Interface, node *v1.Node, changes func(*v1.Node)) (*v1.Node, error) {
	for tries := 0; ; tries++ {
		changes(node)
		newNode, err := client.CoreV1().Nodes().Update(node)
		if err == nil {
			return newNode, nil
		}

		if !errors.IsConflict(err) || tries > 4 {
			return nil, err
		}

		// Fetch the latest version and try again
		node, err = client.CoreV1().Nodes().Get(node.Name, k8smeta.GetOptions{})
		if err != nil {
			return nil, err
		}
	}
}

// Evict all pods on the given node, respecting PDBs etc.
// block until all pods evicted, error or 10-minute timeout
func evictAllPodsOnNode(client kubernetes.Interface, victim *v1.Node) error {
	pods, err := client.CoreV1().Pods(k8smeta.NamespaceAll).List(k8smeta.ListOptions{
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": victim.Name}).String()})
	if err != nil {
		return fmt.Errorf("unable to list pods: %s", err)
	}

	// Ignore daemon pods, because they are immediately rescheduled even on cordoned nodes;
	// this mimics kubectl drain behavior
	victims := make([]v1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if isDaemon(&pod) {
			continue
		}
		victims = append(victims, pod)
	}

	for _, pod := range victims {
		if err = evictPod(client, &pod); err != nil {
			return fmt.Errorf("unable to evict pod %s: %s", pod.Name, err)
		}
	}

	// Wait for evictions to take effect
	if err = waitForDelete(client, victims, 1*time.Minute, 10*time.Minute); err != nil {
		return err
	}
	return nil
}

func evictPod(client kubernetes.Interface, pod *v1.Pod) error {
	eviction := &k8spolicy.Eviction{
		TypeMeta: k8smeta.TypeMeta{
			APIVersion: "v1beta1",
			Kind:       EvictionKind,
		},
		ObjectMeta: k8smeta.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: nil,
	}
	// Remember to change change the URL manipulation func when Evction's version change
	return client.PolicyV1beta1().Evictions(eviction.Namespace).Evict(eviction)
}

func waitForDelete(client kubernetes.Interface, pods []v1.Pod, interval, timeout time.Duration) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		pendingPods := make([]v1.Pod, 0)
		for i, pod := range pods {
			p, err := client.CoreV1().Pods(pod.Namespace).Get(pod.Name, k8smeta.GetOptions{})
			if errors.IsNotFound(err) || (p != nil && p.ObjectMeta.UID != pod.ObjectMeta.UID) {
				continue
			} else if err != nil {
				return false, err
			} else {
				pendingPods = append(pendingPods, pods[i])
			}
		}
		pods = pendingPods
		if len(pendingPods) > 0 {
			return false, nil
		}
		return true, nil
	})
}

// To guard against us crashing in the middle of draining a node and not uncordoning it,
// this finds any node with our marker label and uncordons them.
func crashRecoverNodeDrain(client kubernetes.Interface) error {
	nodeList, err := client.CoreV1().Nodes().List(k8smeta.ListOptions{LabelSelector: fmt.Sprintf("%s=true", LabelMarmosetCordoned)})
	if err != nil {
		return err
	}
	for _, node := range nodeList.Items {
		if _, err = uncordonNode(client, &node); err != nil {
			return err
		}
	}
	return nil
}

func isDaemon(pod *v1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}
