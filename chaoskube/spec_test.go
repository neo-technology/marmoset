package chaoskube_test

import (
	"fmt"
	"github.com/neo-technology/marmoset/chaoskube"
	"github.com/neo-technology/marmoset/util"
	"github.com/sirupsen/logrus/hooks/test"
	"k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
	"time"
)

var (
	logger, logOutput = test.NewNullLogger()
)

var now = time.Date(1773, 12, 16, 18, 10, 23, 0, time.UTC)

func TestPodChaos(t *testing.T) {
	for _, testCase := range []struct {
		name                  string
		givenLabelFilter      string
		givenAnnotationFilter string
		givenNamespaceFilter  string
		givenAgeFilter        time.Duration

		given []runtime.Object

		expectEventuallyChosen []string
	}{
		{
			name:                   "Any pod filter eventually chooses all pods",
			given:                  []runtime.Object{pod("A"), pod("B")},
			expectEventuallyChosen: []string{"A", "B"},
		},
		{
			name:                   "Label filter leaves pods alone",
			givenLabelFilter:       "include=true",
			given:                  []runtime.Object{pod("A"), pod("B", label("include", "true"))},
			expectEventuallyChosen: []string{"B"},
		},
		{
			name:                   "Label filter leaves pods alone",
			givenLabelFilter:       "include!=true",
			given:                  []runtime.Object{pod("A"), pod("B", label("include", "true"))},
			expectEventuallyChosen: []string{"A"},
		},
		{
			name:                   "Annocation filter leaves pods alone",
			givenAnnotationFilter:  "include=true",
			given:                  []runtime.Object{pod("A"), pod("B", annotation("include", "true"))},
			expectEventuallyChosen: []string{"B"},
		},
		{
			name:                   "Namespace filter leaves pods alone",
			givenNamespaceFilter:   "somenamespace",
			given:                  []runtime.Object{pod("A"), pod("B", namespace("somenamespace"))},
			expectEventuallyChosen: []string{"B"},
		},
		{
			name:                   "Age filter leaves pods alone",
			givenAgeFilter:         100 * time.Hour,
			given:                  []runtime.Object{pod("A", age(100*time.Hour)), pod("B")},
			expectEventuallyChosen: []string{},
		},
		{
			name:                   "Age filter only lets through old-enough pods",
			givenAgeFilter:         100 * time.Hour,
			given:                  []runtime.Object{pod("A"), pod("B", age(1000*time.Hour))},
			expectEventuallyChosen: []string{"B"},
		},
		{
			name:                   "Only running pods are targeted",
			given:                  []runtime.Object{pod("A"), pod("B", phase(v1.PodPending))},
			expectEventuallyChosen: []string{"A"},
		},
		{
			name:                   "No matching pods is ok",
			givenNamespaceFilter:   "someunusednamespace",
			given:                  []runtime.Object{pod("A"), pod("B")},
			expectEventuallyChosen: []string{},
		},
	} {
		tc := testCase // get a local var so testCase doesn't change under our feet
		t.Run(tc.name, func(t *testing.T) {
			expectedNames := asMap(tc.expectEventuallyChosen)
			seenNames := make(map[string]bool)
			client := fake.NewSimpleClientset(tc.given...)
			recorder := &recordPodAction{}

			spec := chaoskube.NewPodChaosSpec(recorder, selector(tc.givenLabelFilter),
				selector(tc.givenAnnotationFilter), selector(tc.givenNamespaceFilter),
				tc.givenAgeFilter, logger)

			for i := 0; i < 1000; i++ {
				// When
				err := spec.Apply(client, now)

				if err != nil {
					t.Fatalf("Spec application failed: %s", err)
					return
				}

				if recorder.lastGivenPod != nil {
					seenNames[recorder.lastGivenPod.Name] = true
					if _, ok := expectedNames[recorder.lastGivenPod.Name]; !ok {
						t.Fatalf("Unexpected pod chosen: %s", recorder.lastGivenPod.Name)
						return
					}
				}
			}

			for _, expected := range tc.expectEventuallyChosen {
				if _, ok := seenNames[expected]; !ok {
					t.Fatalf("Expected pod to be selected: %s", expected)
				}
			}
		})
	}
}

func TestNodeChaos(t *testing.T) {
	// Note: Keep an eye to keep this close to TestPodChaos; you can probably factor out something
	// common eventually.
	for _, testCase := range []struct {
		name                   string
		given                  []runtime.Object
		expectEventuallyChosen []string
	}{
		{
			name:                   "Eventually chooses all nodes",
			given:                  []runtime.Object{node("A"), node("B")},
			expectEventuallyChosen: []string{"A", "B"},
		},
		{
			name:                   "No matching nodes is ok",
			given:                  []runtime.Object{},
			expectEventuallyChosen: []string{},
		},
	} {
		tc := testCase // get a local var so testCase doesn't change under our feet
		t.Run(tc.name, func(t *testing.T) {
			expectedNames := asMap(tc.expectEventuallyChosen)
			seenNames := make(map[string]bool)
			client := fake.NewSimpleClientset(tc.given...)
			recorder := &recordNodeAction{}

			spec := chaoskube.NewNodeChaosSpec(recorder, logger)

			for i := 0; i < 1000; i++ {
				// When
				err := spec.Apply(client, now)

				if err != nil {
					t.Fatalf("Spec application failed: %s", err)
					return
				}

				if recorder.lastGivenNode != nil {
					seenNames[recorder.lastGivenNode.Name] = true
					if _, ok := expectedNames[recorder.lastGivenNode.Name]; !ok {
						t.Fatalf("Unexpected node chosen: %s", recorder.lastGivenNode.Name)
						return
					}
				}
			}

			for _, expected := range tc.expectEventuallyChosen {
				if _, ok := seenNames[expected]; !ok {
					t.Fatalf("Expected node to be selected: %s", expected)
				}
			}
		})
	}
}

func TestNodeSpecInitDelegatesToActionInit(t *testing.T) {
	client := fake.NewSimpleClientset()
	action := &recordNodeAction{}
	spec := chaoskube.NodeChaosSpec{Action:action}

	err := spec.Init(client)

	if err != nil {
		t.Errorf("Expected sunshine, got: %s", err)
	}
	if action.initCalledWithClient != client {
		t.Errorf("Expected action init called with client, got %v", action.initCalledWithClient)
	}
}

func TestPodSpecInitDelegatesToActionInit(t *testing.T) {
	client := fake.NewSimpleClientset()
	action := &recordPodAction{}
	spec := chaoskube.PodChaosSpec{Action:action}

	err := spec.Init(client)

	if err != nil {
		t.Errorf("Expected sunshine, got: %s", err)
	}
	if action.initCalledWithClient != client {
		t.Errorf("Expected action init called with client, got %v", action.initCalledWithClient)
	}
}

type recordPodAction struct {
	lastGivenPod *v1.Pod
	initCalledWithClient kubernetes.Interface
}

func (a *recordPodAction) Init(k8sclient kubernetes.Interface) error {
	a.initCalledWithClient = k8sclient
	return nil
}
func (a *recordPodAction) ApplyToPod(victim v1.Pod) error {
	a.lastGivenPod = &victim
	return nil
}
func (a *recordPodAction) Name() string {
	return "record-pod"
}

type recordNodeAction struct {
	lastGivenNode *v1.Node
	initCalledWithClient kubernetes.Interface
}

func (a *recordNodeAction) Init(k8sclient kubernetes.Interface) error {
	a.initCalledWithClient = k8sclient
	return nil
}
func (a *recordNodeAction) ApplyToNode(client kubernetes.Interface, victim *v1.Node) error {
	a.lastGivenNode = victim
	return nil
}
func (a *recordNodeAction) Name() string {
	return "record-node"
}

func node(name string, modifiers ...func(*v1.Node)) runtime.Object {
	n := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name: name,
		},
	}
	n.CreationTimestamp = k8smeta.Time{now}
	for _, mod := range modifiers {
		mod(n)
	}
	return n
}

func pod(name string, modifiers ...func(*v1.Pod)) runtime.Object {
	p := util.NewPod("default", name, v1.PodRunning)
	p.CreationTimestamp = k8smeta.Time{now}
	for _, mod := range modifiers {
		mod(&p)
	}
	return &p
}

func label(key, val string) func(pod *v1.Pod) {
	return func(pod *v1.Pod) {
		pod.Labels[key] = val
	}
}

func annotation(key, val string) func(pod *v1.Pod) {
	return func(pod *v1.Pod) {
		pod.Annotations[key] = val
	}
}

func namespace(namespace string) func(pod *v1.Pod) {
	return func(pod *v1.Pod) {
		pod.Namespace = namespace
	}
}

func age(duration time.Duration) func(pod *v1.Pod) {
	return func(pod *v1.Pod) {
		pod.CreationTimestamp.Time = now.Add(-duration)
	}
}

func phase(phase v1.PodPhase) func(pod *v1.Pod) {
	return func(pod *v1.Pod) {
		pod.Status.Phase = phase
	}
}

// Utilities

func selector(spec string) labels.Selector {
	if spec == "" {
		return labels.Everything()
	}

	selector, err := labels.Parse(spec)
	if err != nil {
		panic(fmt.Sprintf("Test specifies an invalid selector: '%s'. Error: %s", selector, err))
	}

	return selector
}

func asMap(list []string) (out map[string]bool) {
	out = make(map[string]bool, len(list))
	for _, name := range list {
		out[name] = true
	}
	return
}
