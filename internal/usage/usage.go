package usage

import (
	"encoding/json"
	"strconv"
	"strings"
)

type Gauge struct {
	Used  uint64
	Total uint64
}

type Node struct {
	Name     string
	CPU      Gauge
	Memory   Gauge
	Disk     Gauge
	Measured bool
}

type Pod struct {
	Namespace string
	Name      string
	Service   string
	Node      string
	Phase     string
	Reason    string
	Ready     bool
	CPU       uint64
	Memory    uint64
	Disk      uint64
}

type Sample struct {
	Nodes []Node
	Pods  []Pod
}

func Parse(raw string) (Sample, error) {
	nodesRaw, podsRaw, summaries := split(raw)
	var listed nodeList
	if err := json.Unmarshal([]byte(nodesRaw), &listed); err != nil {
		return Sample{}, err
	}
	var pods podList
	if strings.TrimSpace(podsRaw) != "" {
		if err := json.Unmarshal([]byte(podsRaw), &pods); err != nil {
			return Sample{}, err
		}
	}
	index := map[string]int{}
	var sample Sample
	for _, item := range listed.Items {
		node := Node{Name: item.Metadata.Name}
		node.CPU.Total = parseCPU(item.Status.Capacity["cpu"])
		node.Memory.Total = parseBytes(item.Status.Capacity["memory"])
		node.Disk.Total = parseBytes(item.Status.Capacity["ephemeral-storage"])
		index[node.Name] = len(sample.Nodes)
		sample.Nodes = append(sample.Nodes, node)
	}
	for name, body := range summaries {
		at, ok := index[name]
		if !ok {
			index[name] = len(sample.Nodes)
			sample.Nodes = append(sample.Nodes, Node{Name: name})
			at = index[name]
		}
		var sum summary
		if err := json.NewDecoder(strings.NewReader(body)).Decode(&sum); err != nil {
			continue
		}
		node := &sample.Nodes[at]
		if sum.Node.CPU.UsageNanoCores > 0 || sum.Node.Memory.WorkingSetBytes > 0 || sum.Node.FS.UsedBytes > 0 {
			node.Measured = true
		}
		node.CPU.Used = milli(sum.Node.CPU.UsageNanoCores)
		node.Memory.Used = sum.Node.Memory.WorkingSetBytes
		if sum.Node.FS.CapacityBytes > 0 {
			node.Disk.Total = sum.Node.FS.CapacityBytes
		}
		node.Disk.Used = sum.Node.FS.UsedBytes
	}
	usageByPod := map[string]podUse{}
	for _, body := range summaries {
		var sum summary
		if err := json.NewDecoder(strings.NewReader(body)).Decode(&sum); err != nil {
			continue
		}
		for _, pod := range sum.Pods {
			disk := pod.Ephemeral.UsedBytes
			for _, volume := range pod.Volume {
				disk += volume.UsedBytes
			}
			usageByPod[pod.PodRef.Namespace+"/"+pod.PodRef.Name] = podUse{
				CPU: milli(pod.CPU.UsageNanoCores), Memory: pod.Memory.WorkingSetBytes, Disk: disk,
			}
		}
	}
	for _, item := range pods.Items {
		pod := Pod{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Service:   item.Metadata.Labels["myinfra.io/service"],
			Node:      item.Spec.NodeName,
			Phase:     item.Status.Phase,
		}
		ready := item.Status.Phase == "Running" && len(item.Status.ContainerStatuses) > 0
		for _, container := range item.Status.ContainerStatuses {
			if !container.Ready {
				ready = false
			}
			if pod.Reason == "" {
				pod.Reason = container.State.Waiting.Reason
			}
			if pod.Reason == "" {
				pod.Reason = container.State.Terminated.Reason
			}
			if pod.Reason == "" {
				pod.Reason = container.LastState.Terminated.Reason
			}
		}
		pod.Ready = ready
		if use, ok := usageByPod[pod.Namespace+"/"+pod.Name]; ok {
			pod.CPU = use.CPU
			pod.Memory = use.Memory
			pod.Disk = use.Disk
		}
		sample.Pods = append(sample.Pods, pod)
	}
	return sample, nil
}

type podUse struct {
	CPU    uint64
	Memory uint64
	Disk   uint64
}

func PodFailed(pod Pod) bool {
	switch pod.Reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "RunContainerError", "StartError", "OOMKilled", "Error":
		return true
	}
	return pod.Phase == "Failed"
}

func milli(nano uint64) uint64 {
	if nano == 0 {
		return 0
	}
	out := nano / 1_000_000
	if out == 0 {
		return 1
	}
	return out
}

func split(raw string) (string, string, map[string]string) {
	nodes := section(raw, "---NODES---", "---PODS---")
	rest := after(raw, "---PODS---")
	pods := rest
	if idx := strings.Index(rest, "---NODE "); idx >= 0 {
		pods = rest[:idx]
		rest = rest[idx:]
	} else {
		rest = ""
	}
	summaries := map[string]string{}
	for _, part := range strings.Split(rest, "---NODE ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, body, ok := strings.Cut(part, "---")
		if !ok || !strings.HasPrefix(strings.TrimSpace(body), "{") {
			continue
		}
		summaries[strings.TrimSpace(name)] = strings.TrimSpace(body)
	}
	return strings.TrimSpace(nodes), strings.TrimSpace(pods), summaries
}

func section(raw, start, end string) string {
	from := strings.Index(raw, start)
	to := strings.Index(raw, end)
	if from < 0 || to < from {
		return ""
	}
	return raw[from+len(start) : to]
}

func after(raw, mark string) string {
	at := strings.Index(raw, mark)
	if at < 0 {
		return ""
	}
	return raw[at+len(mark):]
}

func parseCPU(raw string) uint64 {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "m") {
		n, _ := strconv.ParseUint(strings.TrimSuffix(raw, "m"), 10, 64)
		return n
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return uint64(f * 1000)
}

func parseBytes(raw string) uint64 {
	raw = strings.TrimSpace(raw)
	mult := uint64(1)
	switch {
	case strings.HasSuffix(raw, "Ki"):
		raw, mult = strings.TrimSuffix(raw, "Ki"), 1024
	case strings.HasSuffix(raw, "Mi"):
		raw, mult = strings.TrimSuffix(raw, "Mi"), 1024*1024
	case strings.HasSuffix(raw, "Gi"):
		raw, mult = strings.TrimSuffix(raw, "Gi"), 1024*1024*1024
	case strings.HasSuffix(raw, "Ti"):
		raw, mult = strings.TrimSuffix(raw, "Ti"), 1024*1024*1024*1024
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}

type nodeList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Capacity map[string]string `json:"capacity"`
		} `json:"status"`
	} `json:"items"`
}

type podList struct {
	Items []struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready bool `json:"ready"`
				State struct {
					Waiting struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
					Terminated struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
				LastState struct {
					Terminated struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

type summary struct {
	Node struct {
		CPU struct {
			UsageNanoCores uint64 `json:"usageNanoCores"`
		} `json:"cpu"`
		Memory struct {
			WorkingSetBytes uint64 `json:"workingSetBytes"`
		} `json:"memory"`
		FS struct {
			CapacityBytes uint64 `json:"capacityBytes"`
			UsedBytes     uint64 `json:"usedBytes"`
		} `json:"fs"`
	} `json:"node"`
	Pods []struct {
		PodRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"podRef"`
		CPU struct {
			UsageNanoCores uint64 `json:"usageNanoCores"`
		} `json:"cpu"`
		Memory struct {
			WorkingSetBytes uint64 `json:"workingSetBytes"`
		} `json:"memory"`
		Ephemeral struct {
			UsedBytes uint64 `json:"usedBytes"`
		} `json:"ephemeral-storage"`
		Volume []struct {
			UsedBytes uint64 `json:"usedBytes"`
		} `json:"volume"`
	} `json:"pods"`
}
