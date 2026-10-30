package api

import (
	"strings"
	"testing"
)

func TestVolumesOnNode(t *testing.T) {
	raw := []byte(`{"items":[
		{"metadata":{"name":"pvc-db"},"spec":{"storageClassName":"local-path","capacity":{"storage":"1Gi"},"accessModes":["ReadWriteOnce"],"local":{"path":"/var/lib/rancher/k3s/storage/pvc-db_ns_db"},"claimRef":{"namespace":"ns","name":"db"},"nodeAffinity":{"required":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"kubernetes.io/hostname","operator":"In","values":["outcluster"]}]}]}}}},
		{"metadata":{"name":"pvc-keep"},"spec":{"storageClassName":"local-path","capacity":{"storage":"2Gi"},"accessModes":["ReadWriteOnce"],"local":{"path":"/var/lib/rancher/k3s/storage/pvc-keep_ns_web"},"claimRef":{"namespace":"ns","name":"web"},"nodeAffinity":{"required":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"kubernetes.io/hostname","operator":"In","values":["incluster"]}]}]}}}}
	]}`)
	got, err := volumesOnNode(raw, "outcluster")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Claim != "db" || got[0].Path != "/var/lib/rancher/k3s/storage/pvc-db_ns_db" {
		t.Fatalf("%+v", got)
	}
}

func TestWorkloadsOfTheVolume(t *testing.T) {
	vols := []localVolume{{Namespace: "ns", Claim: "db"}}
	raw := []byte(`{"items":[
		{"kind":"Deployment","metadata":{"name":"db","namespace":"ns","annotations":{"myinfra.io/leave-replicas":"1"}},"spec":{"replicas":0,"template":{"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":"db"}}]}}}},
		{"kind":"Deployment","metadata":{"name":"web","namespace":"ns"},"spec":{"replicas":2,"template":{"spec":{"volumes":[{"emptyDir":{}}]}}}}
	]}`)
	got, err := workloadsOf(raw, vols)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "deployment" || got[0].Name != "db" || got[0].Replicas != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestStatelessPodsMoveAndDisksStay(t *testing.T) {
	raw := []byte(`{"items":[
		{"metadata":{"name":"move-probe-a","namespace":"default","labels":{"app":"move-probe"},"ownerReferences":[{"kind":"ReplicaSet"}]},"spec":{"nodeName":"outcluster","volumes":[{"emptyDir":{}}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"db-a","namespace":"ns","labels":{"myinfra.io/service":"db"},"ownerReferences":[{"kind":"ReplicaSet"}]},"spec":{"nodeName":"outcluster","volumes":[{"persistentVolumeClaim":{"claimName":"db"}}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"svclb","namespace":"kube-system","ownerReferences":[{"kind":"DaemonSet"}]},"spec":{"nodeName":"outcluster"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}
	]}`)
	pods, err := podsOf(raw)
	if err != nil {
		t.Fatal(err)
	}
	moving := statelessOnNode(pods, "outcluster")
	if len(moving) != 1 || moving[0].Name != "move-probe-a" {
		t.Fatalf("%+v", moving)
	}
	if !busyOnNode(pods, "outcluster") {
		t.Fatal("le pod du disque est encore sur la machine")
	}
	back := []byte(`{"items":[
		{"metadata":{"name":"move-probe-b","namespace":"default","labels":{"app":"move-probe"},"ownerReferences":[{"kind":"ReplicaSet"}]},"spec":{"nodeName":"incluster"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"db-b","namespace":"ns","labels":{"myinfra.io/service":"db"},"ownerReferences":[{"kind":"ReplicaSet"}]},"spec":{"nodeName":"incluster","volumes":[{"persistentVolumeClaim":{"claimName":"db"}}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}
	]}`)
	pods, err = podsOf(back)
	if err != nil {
		t.Fatal(err)
	}
	if busyOnNode(pods, "outcluster") {
		t.Fatal("la machine partie a encore des applications")
	}
	if !readyOffNode(pods, "outcluster", []string{"default/move-probe", "ns/db"}) {
		t.Fatal("les services ne sont pas prêts ailleurs")
	}
	if readyOffNode(pods, "outcluster", []string{"ns/absent"}) {
		t.Fatal("un service absent ne doit pas autoriser le départ")
	}
}

func TestVolumeManifestFollowsTheNode(t *testing.T) {
	text, err := volumeManifest(localVolume{
		Name: "pvc-db", Namespace: "ns", Claim: "db",
		Path: "/var/lib/rancher/k3s/storage/pvc-db_ns_db", Storage: "1Gi",
		AccessMode: "ReadWriteOnce", Class: "local-path",
	}, "incluster")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `values: ["incluster"]`) || !strings.Contains(text, "volumeName: pvc-db") || strings.Contains(text, "outcluster") {
		t.Fatal(text)
	}
	if _, err := volumeManifest(localVolume{Name: "pvc-db", Namespace: "ns", Claim: "db", Path: "/etc/passwd", Storage: "1Gi", AccessMode: "ReadWriteOnce", Class: "local-path"}, "incluster"); err == nil {
		t.Fatal("un chemin hors du stockage local est refusé")
	}
}

func TestDrainDoesNotForce(t *testing.T) {
	command := drainNodeCommand("outcluster")
	if strings.Contains(command, "--force") || strings.Contains(command, "/dev/null") || !strings.Contains(command, "&&") {
		t.Fatal(command)
	}
}
