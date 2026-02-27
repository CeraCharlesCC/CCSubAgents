package action

import (
	"fmt"
)

type Node struct {
	ID        string
	DependsOn []string
}

func TopoSort(nodes []Node) ([]string, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	byID := make(map[string]Node, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	out := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("action id cannot be empty")
		}
		if _, exists := byID[n.ID]; exists {
			return nil, fmt.Errorf("duplicate action id %q", n.ID)
		}
		byID[n.ID] = n
		inDegree[n.ID] = 0
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if _, exists := byID[dep]; !exists {
				return nil, fmt.Errorf("action %q depends on unknown action %q", n.ID, dep)
			}
			inDegree[n.ID]++
			out[dep] = append(out[dep], n.ID)
		}
	}

	queue := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, next := range out[cur] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("action graph contains a cycle")
	}
	return order, nil
}
