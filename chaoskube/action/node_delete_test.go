package action_test

import (
	"github.com/neo-technology/marmoset/chaoskube/action"
	"k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestDeleteNodeAction(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name: "test-node",
		},
	}
	noTouching := &v1.Node{
		ObjectMeta: k8smeta.ObjectMeta{
			Name: "shouldnt-be-touched-node",
		},
	}
	client := fake.NewSimpleClientset(node, noTouching)
	act := action.NewDeleteNodeAction()

	err := act.ApplyToNode(client, node)

	if err != nil {
		t.Fatalf("Expected smooth sailing, got: %s", err)
	}

	nodes, _ := client.CoreV1().Nodes().List(k8smeta.ListOptions{})
	if len(nodes.Items) > 1 {
		t.Fatalf("Expected the node to have been deleted, found: %v", nodes.Items)
	}
	if len(nodes.Items) < 1 {
		t.Fatalf("Expected the extra node to be left alone, but found no nodes")
	}
	if nodes.Items[0].Name != noTouching.Name {
		t.Fatalf("Expected the extra node to not have been touched, but the surviving node is: %v", nodes.Items[0])
	}
}
