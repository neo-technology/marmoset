package chaoskube

import (
	"fmt"
	"github.com/neo-technology/marmoset/chaoskube/action"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clientset "k8s.io/client-go/kubernetes"
	"math/rand"
	"time"
)

type ChaosSpec interface {
	Apply(k8sclient clientset.Interface, now time.Time) error
}

// == Node chaos ==

type NodeChaosSpec struct {
	Action action.NodeAction
	// an instance of logrus.StdLogger to write log messages to
	Logger log.FieldLogger
}

func (s *NodeChaosSpec) Apply(client clientset.Interface, now time.Time) error {
	candidates, err := s.candidates(client, now)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		s.Logger.Debugf(msgVictimNotFound)
		return nil
	}

	index := rand.Intn(len(candidates))
	victim := candidates[index]

	s.Logger.WithFields(log.Fields{
		"namespace": victim.Namespace,
		"name":      victim.Name,
	}).Info(s.Action.Name())

	return s.Action.ApplyToNode(client, &victim)
}

func (s *NodeChaosSpec) candidates(client clientset.Interface, now time.Time) ([]v1.Node, error) {
	nodeList, err := client.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	nodes := nodeList.Items
	return nodes, nil
}

func NewNodeChaosSpec(action action.NodeAction, logger log.FieldLogger) ChaosSpec {
	return &NodeChaosSpec{
		Action: action,
		Logger: logger,
	}
}

// == Pod chaos ==

type PodChaosSpec struct {
	Action action.PodAction
	// a label selector which restricts the pods to choose from
	Labels labels.Selector
	// an annotation selector which restricts the pods to choose from
	Annotations labels.Selector
	// a namespace selector which restricts the pods to choose from
	Namespaces labels.Selector
	// minimum age of pods to consider
	MinimumAge time.Duration
	// an instance of logrus.StdLogger to write log messages to
	Logger log.FieldLogger
}

func (s *PodChaosSpec) Apply(client clientset.Interface, now time.Time) error {
	candidates, err := s.candidates(client, now)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		s.Logger.Debugf(msgVictimNotFound)
		return nil
	}

	index := rand.Intn(len(candidates))
	victim := candidates[index]

	s.Logger.WithFields(log.Fields{
		"namespace": victim.Namespace,
		"name":      victim.Name,
	}).Info(s.Action.Name())

	return s.Action.ApplyToPod(victim)
}

func (s *PodChaosSpec) candidates(client clientset.Interface, now time.Time) ([]v1.Pod, error) {
	listOptions := metav1.ListOptions{LabelSelector: s.Labels.String()}

	podList, err := client.CoreV1().Pods(v1.NamespaceAll).List(listOptions)
	if err != nil {
		return nil, err
	}

	pods, err := filterByNamespaces(podList.Items, s.Namespaces)
	if err != nil {
		return nil, err
	}

	pods = filterByAnnotations(pods, s.Annotations)
	pods = filterByPhase(pods, v1.PodRunning)
	pods = filterByMinimumAge(pods, s.MinimumAge, now)

	return pods, nil
}

func NewPodChaosSpec(action action.PodAction, labels, annotations, namespaces labels.Selector, minimumAge time.Duration,
	logger log.FieldLogger) ChaosSpec {
	return &PodChaosSpec{
		Action:      action,
		Labels:      labels,
		Annotations: annotations,
		Namespaces:  namespaces,
		MinimumAge:  minimumAge,
		Logger:      logger,
	}
}

// filterByNamespaces filters a list of pods by a given namespace selector.
func filterByNamespaces(pods []v1.Pod, namespaces labels.Selector) ([]v1.Pod, error) {
	// empty filter returns original list
	if namespaces.Empty() {
		return pods, nil
	}

	// split requirements into including and excluding groups
	reqs, _ := namespaces.Requirements()
	reqIncl := []labels.Requirement{}
	reqExcl := []labels.Requirement{}

	for _, req := range reqs {
		switch req.Operator() {
		case selection.Exists:
			reqIncl = append(reqIncl, req)
		case selection.DoesNotExist:
			reqExcl = append(reqExcl, req)
		default:
			return nil, fmt.Errorf("unsupported operator: %s", req.Operator())
		}
	}

	filteredList := []v1.Pod{}

	for _, pod := range pods {
		// if there aren't any including requirements, we're in by default
		included := len(reqIncl) == 0

		// convert the pod's namespace to an equivalent label selector
		selector := labels.Set{pod.Namespace: ""}

		// include pod if one including requirement matches
		for _, req := range reqIncl {
			if req.Matches(selector) {
				included = true
				break
			}
		}

		// exclude pod if it is filtered out by at least one excluding requirement
		for _, req := range reqExcl {
			if !req.Matches(selector) {
				included = false
				break
			}
		}

		if included {
			filteredList = append(filteredList, pod)
		}
	}

	return filteredList, nil
}

// filterByAnnotations filters a list of pods by a given annotation selector.
func filterByAnnotations(pods []v1.Pod, annotations labels.Selector) []v1.Pod {
	// empty filter returns original list
	if annotations.Empty() {
		return pods
	}

	filteredList := []v1.Pod{}

	for _, pod := range pods {
		// convert the pod's annotations to an equivalent label selector
		selector := labels.Set(pod.Annotations)

		// include pod if its annotations match the selector
		if annotations.Matches(selector) {
			filteredList = append(filteredList, pod)
		}
	}

	return filteredList
}

// filterByPhase filters a list of pods by a given PodPhase, e.g. Running.
func filterByPhase(pods []v1.Pod, phase v1.PodPhase) []v1.Pod {
	filteredList := []v1.Pod{}

	for _, pod := range pods {
		if pod.Status.Phase == phase {
			filteredList = append(filteredList, pod)
		}
	}

	return filteredList
}

// filterByMinimumAge filters pods by creation time. Only pods
// older than minimumAge are returned
func filterByMinimumAge(pods []v1.Pod, minimumAge time.Duration, now time.Time) []v1.Pod {
	if minimumAge <= time.Duration(0) {
		return pods
	}

	creationTime := now.Add(-minimumAge)

	filteredList := []v1.Pod{}

	for _, pod := range pods {
		if pod.ObjectMeta.CreationTimestamp.Time.Before(creationTime) {
			filteredList = append(filteredList, pod)
		}
	}

	return filteredList
}
