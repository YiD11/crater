package crclient

import (
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
)

// IsNodeSchedulable 检查节点是否可调度
func IsNodeSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// FilterNodesBySelectors 根据选择器过滤节点
func FilterNodesBySelectors(nodes []corev1.Node, selectors []corev1.NodeSelectorRequirement) []*corev1.Node {
	nodePtrs := make([]*corev1.Node, len(nodes))
	for i := range nodes {
		nodePtrs[i] = &nodes[i]
	}
	if len(selectors) == 0 {
		return nodePtrs
	}
	filtered := lo.FilterMap(nodePtrs, func(node *corev1.Node, _ int) (*corev1.Node, bool) {
		return node, MatchesSelectors(node, selectors)
	})
	return filtered
}

// MatchesSelectors 检查节点是否匹配选择器
func MatchesSelectors(node *corev1.Node, selectors []corev1.NodeSelectorRequirement) bool {
	for _, selector := range selectors {
		labelValue, exists := node.Labels[selector.Key]
		switch selector.Operator {
		case corev1.NodeSelectorOpIn:
			if !exists || !lo.Contains(selector.Values, labelValue) {
				return false
			}
		case corev1.NodeSelectorOpNotIn:
			if exists && lo.Contains(selector.Values, labelValue) {
				return false
			}
		case corev1.NodeSelectorOpExists:
			if !exists {
				return false
			}
		case corev1.NodeSelectorOpDoesNotExist:
			if exists {
				return false
			}
		}
	}
	return true
}
