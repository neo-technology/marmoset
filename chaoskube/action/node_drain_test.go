package action_test

import (
	"github.com/neo-technology/marmoset/chaoskube/action"
	"github.com/neo-technology/marmoset/util"
	"k8s.io/api/core/v1"
	k8spolicy "k8s.io/api/policy/v1beta1"
	k8smeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	k8stypedpolicy "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	k8sfakepolicy "k8s.io/client-go/kubernetes/typed/policy/v1beta1/fake"
	k8stesting "k8s.io/client-go/testing"
	"testing"
)

func TestDrainNode(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name: "test-node",
		},
	}
	targetPod := newPodOnNode("p1", node.Name)
	client := fixPolicyFake(fake.NewSimpleClientset(node, targetPod))
	client.Fake.PrependReactor("create", "pods", evictionReaction(client.Fake.ReactionChain[0].React))
	act := action.NewDrainNodeAction()

	// When I apply the drain action..
	err := act.ApplyToNode(client, node)
	if err != nil {
		t.Fatalf("ApplyToNode failed with: %s", err)
	}

	// Then the first action taken is that node is labeled and unschedulable
	actions := client.Fake.Actions()
	actionNo := 1
	patchNode := actions[actionNo].(k8stesting.UpdateAction).GetObject().(*v1.Node)
	if patchNode.Labels[action.LabelMarmosetCordoned] != "true" {
		t.Errorf("Expected node to be labeled %s=true, actual: %v", action.LabelMarmosetCordoned, patchNode.Labels)
	}
	if !patchNode.Spec.Unschedulable {
		t.Errorf("Expected node to be unscheduleable.")
	}
	actionNo++

	// After that, the action lists available pods
	listAction, ok := actions[actionNo].(k8stesting.ListAction)
	if !ok {
		t.Errorf("Expected the action to list available pods, found %v", actions[actionNo])
	}
	podFilter := listAction.GetListRestrictions().Fields.String()
	if podFilter != "spec.nodeName=test-node" {
		t.Errorf("Wrong field filter for listing pods: %s", podFilter)
	}
	actionNo++

	// After that, an eviction request is created
	createEviction := actions[actionNo].(k8stesting.CreateAction)
	eviction := createEviction.GetObject().(*k8spolicy.Eviction)
	if eviction.Name != targetPod.Name {
		t.Errorf("Expected target of eviction to be %s, found %v", targetPod.Name, eviction.Name)
	}
	actionNo++

	// After that, it polls to check that the pod is gone
	pollAction := actions[actionNo].(k8stesting.GetAction)
	if pollAction.GetName() != targetPod.Name {
		t.Errorf("Expected target of poll to be %s, found %v", targetPod.Name, pollAction)
	}
	actionNo++

	// And finally it uncordons the node
	uncordonNode := actions[actionNo].(k8stesting.UpdateAction).GetObject().(*v1.Node)
	if uncordonNode.Labels[action.LabelMarmosetCordoned] != "" {
		t.Errorf("Expected marmoset label to be cleared, found %v", patchNode.Labels)
	}
	if uncordonNode.Spec.Unschedulable {
		t.Errorf("Expected node to be scheduleable.")
	}
	actionNo++
}

func TestDrainNodeUncordonsAnyPartiallyDrainedNode(t *testing.T) {
	victim := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name: "test-node",
		},
	}
	leftCordoned := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name:   "cordoned",
			Labels: map[string]string{action.LabelMarmosetCordoned: "true"},
		},
		Spec: v1.NodeSpec{
			Unschedulable: true,
		},
	}
	client := fixPolicyFake(fake.NewSimpleClientset(victim, leftCordoned))
	act := action.NewDrainNodeAction()

	// When I apply the drain action..
	err := act.ApplyToNode(client, victim)
	if err != nil {
		t.Fatalf("ApplyToNode failed with: %s", err)
	}

	// Then the already cordoned node is found and uncordoned
	actions := client.Fake.Actions()
	patchNode := actions[1].(k8stesting.UpdateAction).GetObject().(*v1.Node)
	if patchNode.Name != leftCordoned.Name {
		t.Errorf("Expected %s to be uncordoned, got %v", leftCordoned.Name, patchNode)
	}
	if patchNode.Labels[action.LabelMarmosetCordoned] != "" {
		t.Errorf("Expected node have label cleared, got %v", patchNode.Labels)
	}
	if patchNode.Spec.Unschedulable {
		t.Errorf("Expected node to be made schedulable.")
	}
}

func evictionReaction(defaultReaction k8stesting.ReactionFunc) k8stesting.ReactionFunc {
	return func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		if !action.Matches("create", "pods") || action.GetSubresource() != "eviction" {
			return false, nil, nil
		}

		// Act by handling like a delete action
		eviction := action.(k8stesting.CreateAction).GetObject().(*k8spolicy.Eviction)
		return defaultReaction(k8stesting.NewDeleteAction(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, eviction.Namespace, eviction.Name))
	}
}

func newPodOnNode(podName, nodeName string) *v1.Pod {
	pod := util.NewPod("default", podName, v1.PodRunning)
	pod.Spec.NodeName = nodeName
	return &pod
}

// Decorate fake.Clientset to workaround issues in the policy fakes fixed by
// https://github.com/kubernetes/client-go/commit/e2d85a507946471958cfba11f58b217cb9a1b1f1
// This can be dropped once we upgrade to a client version that has that patch; currently there is no
// client release with this patch.
func fixPolicyFake(client *fake.Clientset) *clientset {
	return &clientset{client}
}

type clientset struct {
	*fake.Clientset
}

func (c *clientset) PolicyV1beta1() k8stypedpolicy.PolicyV1beta1Interface {
	return &fakePolicyV1beta1{k8sfakepolicy.FakePolicyV1beta1{Fake: &c.Fake}}
}

type fakePolicyV1beta1 struct {
	k8sfakepolicy.FakePolicyV1beta1
}

func (c *fakePolicyV1beta1) Evictions(namespace string) v1beta1.EvictionInterface {
	return &FakeEvictions{&c.FakePolicyV1beta1, namespace}
}

// FakeEvictions implements EvictionInterface
type FakeEvictions struct {
	Fake *k8sfakepolicy.FakePolicyV1beta1
	ns   string
}

func (c *FakeEvictions) Evict(eviction *k8spolicy.Eviction) error {
	action := k8stesting.CreateActionImpl{}
	action.Verb = "create"
	action.Namespace = c.ns
	action.Resource = schema.GroupVersionResource{Group: "", Version: "", Resource: "pods"}
	action.Subresource = "eviction"
	action.Object = eviction
	_, err := c.Fake.Invokes(action, eviction)
	return err
}
