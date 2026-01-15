package utils

import v1 "k8s.io/api/core/v1"

func CalculateRequsetsByContainers(containers []v1.Container) (resources v1.ResourceList) {
	resources = make(v1.ResourceList, 0)
	for j := range containers {
		container := &containers[j]
		resources = SumResources(resources, container.Resources.Requests)
	}
	return resources
}

func SumResources(resources ...v1.ResourceList) v1.ResourceList {
	result := make(v1.ResourceList)
	for _, res := range resources {
		for name, quantity := range res {
			if v, ok := result[name]; !ok {
				result[name] = quantity.DeepCopy()
			} else {
				v.Add(quantity)
				result[name] = v
			}
		}
	}
	return result
}

// SubtractResourceList 从 total 中减去 used
func SubtractResourceList(total, used v1.ResourceList) v1.ResourceList {
	result := total.DeepCopy()
	for name, usedQuantity := range used {
		if totalQuantity, exists := result[name]; exists {
			totalQuantity.Sub(usedQuantity)
			result[name] = totalQuantity
		}
	}
	return result
}

// CanFitResources 检查可用资源是否能满足请求的资源
func CanFitResources(available, requested v1.ResourceList) bool {
	for name, reqQuantity := range requested {
		availQuantity, exists := available[name]
		if !exists {
			return false
		}
		if availQuantity.Cmp(reqQuantity) < 0 {
			return false
		}
	}
	return true
}
