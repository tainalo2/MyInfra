package usage

import "testing"

func TestParseNodeAndPod(t *testing.T) {
	raw := `---NODES---
{"items":[{"metadata":{"name":"test-1"},"status":{"capacity":{"cpu":"2","memory":"2048Mi","ephemeral-storage":"20Gi"}}}]}
---PODS---
{"items":[{"metadata":{"name":"web","namespace":"mi1","labels":{"myinfra.io/service":"web"}},"spec":{"nodeName":"test-1"},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}]}
---NODE test-1---
{"node":{"cpu":{"usageNanoCores":400000000},"memory":{"workingSetBytes":800000000},"fs":{"capacityBytes":20000000000,"usedBytes":4000000000}},"pods":[{"podRef":{"name":"web","namespace":"mi1"},"cpu":{"usageNanoCores":15000000},"memory":{"workingSetBytes":20000000},"ephemeral-storage":{"usedBytes":1000},"volume":[{"usedBytes":5000}]}]}
`
	sample, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sample.Nodes) != 1 || !sample.Nodes[0].Measured || sample.Nodes[0].CPU.Used != 400 || sample.Nodes[0].CPU.Total != 2000 {
		t.Fatalf("node %+v", sample.Nodes)
	}
	if sample.Nodes[0].Memory.Total != 2048*1024*1024 || sample.Nodes[0].Disk.Used != 4000000000 || sample.Nodes[0].Disk.Total != 20000000000 {
		t.Fatalf("capacity %+v", sample.Nodes[0])
	}
	if len(sample.Pods) != 1 || !sample.Pods[0].Ready || sample.Pods[0].CPU != 15 || sample.Pods[0].Disk != 6000 || sample.Pods[0].Node != "test-1" || sample.Pods[0].Service != "web" {
		t.Fatalf("pod %+v", sample.Pods)
	}
}

func TestParseMarksACrashedPod(t *testing.T) {
	raw := `---NODES---
{"items":[]}
---PODS---
{"items":[{"metadata":{"name":"web","namespace":"mi1"},"status":{"phase":"Running","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"Error"}}}]}}]}
`
	sample, err := Parse(raw)
	if err != nil || len(sample.Pods) != 1 || sample.Pods[0].Reason != "CrashLoopBackOff" || !PodFailed(sample.Pods[0]) {
		t.Fatalf("%+v %v", sample.Pods, err)
	}
}
