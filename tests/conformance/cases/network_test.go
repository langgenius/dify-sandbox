package cases

import "testing"

func TestNetworkEvidenceRequiresPositiveControlForEveryTarget(t *testing.T) {
	targets := []NetworkTarget{{Protocol: "tcp", Address: "192.0.2.10", Port: 8080}, {Protocol: "udp", Address: "2001:db8::10", Port: 8081}}
	before := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 3, targets[1].key(): 8}}
	missing := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 4, targets[1].key(): 8}}
	if compareNetwork(before, missing, targets, true) == nil {
		t.Fatal("unobserved UDP/v6 control accepted")
	}
	positive := NetworkCounters{BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 4, targets[1].key(): 9}}
	if err := compareNetwork(before, positive, targets, true); err != nil {
		t.Fatal(err)
	}
	if compareNetwork(before, positive, targets, false) == nil {
		t.Fatal("denied canary traffic accepted")
	}
	if err := compareNetwork(positive, positive, targets, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []NetworkCounters{{BootID: "restart", Counters: positive.Counters}, {BootID: "boot-one", Counters: map[string]uint64{targets[0].key(): 2, targets[1].key(): 8}}, {BootID: "boot-one", Counters: map[string]uint64{}}} {
		if compareNetwork(before, bad, targets, false) == nil {
			t.Fatal("unreliable counter evidence accepted")
		}
	}
}
func TestNetworkFixtureRequiresAllProtocolsAndFamilies(t *testing.T) {
	fixture := NetworkFixture{CollectorURL: "http://collector.fixture.test/counts"}
	if fixture.validate() == nil {
		t.Fatal("empty fixture accepted")
	}
	for _, protocol := range []string{"tcp", "udp", "dns", "http", "https"} {
		for _, address := range []string{"192.0.2.10", "2001:db8::10"} {
			fixture.Targets = append(fixture.Targets, NetworkTarget{Protocol: protocol, Address: address, Host: address, Port: 8080})
		}
	}
	for _, protocol := range []string{"http", "https"} {
		for _, address := range []string{"192.0.2.11", "2001:db8::11"} {
			fixture.AlternateTargets = append(fixture.AlternateTargets, NetworkTarget{Protocol: protocol, Address: address, Host: address, Port: 8080})
		}
	}
	if err := fixture.validate(); err != nil {
		t.Fatal(err)
	}
	fixture.Targets = fixture.Targets[:len(fixture.Targets)-1]
	if fixture.validate() == nil {
		t.Fatal("missing HTTPS/v6 positive control accepted")
	}
}
