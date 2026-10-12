package swarm

import (
	"strings"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
)

func svc(name, stack string, running, desired uint64) docker.Service {
	labels := map[string]string{}
	if stack != "" {
		labels[docker.LabelStackNamespace] = stack
	}
	return docker.Service{Name: name, Labels: labels, Mode: docker.ModeReplicated, Running: running, Desired: desired}
}

func stackLabel(name string) map[string]string {
	return map[string]string{docker.LabelStackNamespace: name}
}

func TestBuildStacksGroupsByNamespace(t *testing.T) {
	stacks := BuildStacks(Inventory{
		Services: []docker.Service{
			svc("shop_web", "shop", 3, 3),
			svc("probe", "", 1, 1),
			svc("blog_db", "blog", 1, 1),
			svc("shop_api", "shop", 1, 2),
		},
		Networks: []docker.Network{
			{Name: "shop_default", Labels: stackLabel("shop")},
			{Name: "ingress"},
		},
		Secrets: []docker.Secret{{Name: "shop_pw", Labels: stackLabel("shop")}, {Name: "loose"}},
		Configs: []docker.Config{{Name: "gone_conf", Labels: stackLabel("gone")}},
	})

	var names []string
	for _, s := range stacks {
		names = append(names, s.Name)
	}
	// Sorted, with the loose services last whatever their name.
	if got := strings.Join(names, ","); got != "blog,gone,shop," {
		t.Fatalf("stacks = %q", got)
	}

	shop := stacks[2]
	if len(shop.Services) != 2 || shop.Services[0].Name != "shop_api" {
		t.Fatalf("shop services = %+v", shop.Services)
	}
	if len(shop.Networks) != 1 || len(shop.Secrets) != 1 {
		t.Fatalf("shop objects = %d networks, %d secrets", len(shop.Networks), len(shop.Secrets))
	}
	if shop.Running() != 4 || shop.Desired() != 5 || !shop.Degraded() {
		t.Fatalf("shop counts = %d/%d degraded=%v", shop.Running(), shop.Desired(), shop.Degraded())
	}

	// A stack whose services are gone is still found through what it left,
	// which is what an interrupted `stack rm` looks like.
	if gone := stacks[1]; len(gone.Services) != 0 || len(gone.Configs) != 1 {
		t.Fatalf("gone = %+v", gone)
	}
	if !stacks[3].Loose() || stacks[3].Services[0].Name != "probe" {
		t.Fatalf("loose = %+v", stacks[3])
	}
}

// A job that ran to completion runs nothing and wants nothing more: it is not
// a stack falling short.
func TestDegradedIgnoresJobs(t *testing.T) {
	job := svc("batch", "etl", 0, 3)
	job.Mode = docker.ModeReplicatedJob
	st := Stack{Services: []docker.Service{job, svc("agent", "etl", 1, 1)}}
	if st.Degraded() {
		t.Fatal("a finished job reads as degraded")
	}
	if st.Running() != 1 || st.Desired() != 1 {
		t.Fatalf("counts %d/%d include the job", st.Running(), st.Desired())
	}
}

func TestShortName(t *testing.T) {
	cases := map[[2]string]string{
		{"shop", "shop_web"}:  "web",
		{"shop", "shopweb"}:   "shopweb",
		{"shop", "shop_"}:     "shop_",
		{"", "probe"}:         "probe",
		{"shop", "other_web"}: "other_web",
	}
	for in, want := range cases {
		if got := ShortName(in[0], in[1]); got != want {
			t.Errorf("ShortName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestCreateArgs(t *testing.T) {
	args, err := CreateArgs(ServiceForm{
		Name: "web", Image: "nginx:alpine", Replicas: "3",
		Ports:       []string{"8080:80", "53/udp"},
		Mounts:      []string{"data:/var/lib/data", "/srv/conf:/etc/conf:ro"},
		Env:         []string{"A=1"},
		Networks:    []string{"front"},
		Constraints: []string{"node.role==worker"},
		Command:     []string{"nginx", "-g", "daemon off;"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "service create --detach --with-registry-auth --name web --mode replicated --replicas 3 " +
		"--publish published=8080,target=80,protocol=tcp --publish target=53,protocol=udp " +
		"--mount type=volume,source=data,target=/var/lib/data " +
		"--mount type=bind,source=/srv/conf,target=/etc/conf,readonly " +
		"--env A=1 --network front --constraint node.role==worker nginx:alpine nginx -g daemon off;"
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestCreateArgsRefusals(t *testing.T) {
	cases := map[string]ServiceForm{
		"no image":             {Name: "x"},
		"replicas on global":   {Image: "a", Mode: "global", Replicas: "2"},
		"replicas not numeric": {Image: "a", Replicas: "two"},
		"bad port":             {Image: "a", Ports: []string{"http:80"}},
		"bad protocol":         {Image: "a", Ports: []string{"80/icmp"}},
		"relative bind":        {Image: "a", Mounts: []string{"./conf:/etc/conf"}},
		"relative target":      {Image: "a", Mounts: []string{"data:var"}},
		"bad mount option":     {Image: "a", Mounts: []string{"data:/var:rx"}},
		"not a constraint":     {Image: "a", Constraints: []string{"sleep"}},
		"constraint operator":  {Image: "a", Constraints: []string{"node.role=worker"}},
	}
	for name, form := range cases {
		if _, err := CreateArgs(form); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDeployArgs(t *testing.T) {
	got := strings.Join(DeployArgs("shop", []string{"/s/a.yml", "/s/b.yml"}, true), " ")
	want := "stack deploy --detach=true --with-registry-auth --prune -c /s/a.yml -c /s/b.yml shop"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestMissingVariables(t *testing.T) {
	body := `services:
  db:
    environment:
      PASSWORD: ${DB_PASSWORD}
      USER: $DB_USER
      HOST: ${DB_HOST:-db}
      PORT: ${DB_PORT-5432}
      NAME: ${DB_NAME:?set DB_NAME}
      COST: $$5
      HOME: ${HOME}
      AGAIN: ${DB_PASSWORD}
`
	env := map[string]string{"HOME": "/home/me"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	got := strings.Join(MissingVariables(body, lookup), ",")
	if got != "DB_PASSWORD,DB_USER" {
		t.Fatalf("missing = %q", got)
	}
}

func TestConstraints(t *testing.T) {
	got := SplitConstraints("node.role == worker, node.labels.zone!=eu ,")
	if len(got) != 2 {
		t.Fatalf("split = %q", got)
	}
	for in, want := range map[string]string{
		"node.role == worker":    "node.role==worker",
		"node.labels.zone != eu": "node.labels.zone!=eu",
		"node.hostname==db1":     "node.hostname==db1",
	} {
		if c, err := ParseConstraint(in); err != nil || c != want {
			t.Errorf("ParseConstraint(%q) = %q, %v", in, c, err)
		}
	}
	for _, bad := range []string{"node.role", "==worker", "node.role===worker", "a=b"} {
		if _, err := ParseConstraint(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
