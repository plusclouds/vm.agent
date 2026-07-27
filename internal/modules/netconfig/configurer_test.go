package netconfig

import (
	"reflect"
	"testing"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// privateNIC and publicNIC mirror the two real NICs in
// pkg/isoconfig/testdata/pc-meta-data.json, so these tests double as a
// regression guard against metadata-shape drift.
func privateNIC() isoconfig.VirtualNetworkCard {
	return isoconfig.VirtualNetworkCard{
		DeviceNumber: 1,
		MACAddr:      "02:00:00:00:00:01",
		NetworkName:  "example-private-net",
		Network: isoconfig.Network{
			Name:           "example-private-net",
			IPAddr:         "10.128.0.0/16",
			Gateway:        nil,
			Subnet:         "16",
			Netmask:        "255.255.0.0",
			NetworkAddress: "10.128.0.0",
			DHCPServer:     "",
			DNSNameservers: []string{"198.51.100.53/32"},
			MTU:            1500,
		},
		IPList: isoconfig.DataList[isoconfig.IPEntry]{
			Data: []isoconfig.IPEntry{{ID: 1010, IPAddr: "10.128.0.1/32", IsReserved: false}},
		},
	}
}

func publicNIC() isoconfig.VirtualNetworkCard {
	return isoconfig.VirtualNetworkCard{
		DeviceNumber: 0,
		MACAddr:      "02:00:00:00:00:02",
		NetworkName:  "Public Internet",
		Network: isoconfig.Network{
			Name:           "Public Internet",
			IPAddr:         "203.0.113.0/24",
			Gateway:        nil,
			Subnet:         "24",
			Netmask:        "255.255.255.0",
			NetworkAddress: "203.0.113.0",
			DHCPServer:     "203.0.113.1/32",
			DNSNameservers: []string{"198.51.100.53/32", "198.51.100.54/32"},
			MTU:            1500,
		},
		IPList: isoconfig.DataList[isoconfig.IPEntry]{
			Data: []isoconfig.IPEntry{{ID: 1011, IPAddr: "203.0.113.16/32", IsReserved: false}},
		},
	}
}

func TestBuildAddresses(t *testing.T) {
	if got := buildAddresses(privateNIC()); !reflect.DeepEqual(got, []string{"10.128.0.1/32"}) {
		t.Errorf("buildAddresses(private) = %v", got)
	}
	if got := buildAddresses(publicNIC()); !reflect.DeepEqual(got, []string{"203.0.113.16/32"}) {
		t.Errorf("buildAddresses(public) = %v", got)
	}
}

func TestBuildAddresses_MultipleIPsIncludingReserved(t *testing.T) {
	nic := privateNIC()
	nic.IPList.Data = append(nic.IPList.Data, isoconfig.IPEntry{
		ID: 1012, IPAddr: "10.128.0.2/32", IsReserved: true,
	})
	got := buildAddresses(nic)
	want := []string{"10.128.0.1/32", "10.128.0.2/32"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildAddresses() = %v, want %v (reserved entries must still be assigned)", got, want)
	}
}

func TestStripDNSCIDR(t *testing.T) {
	got := stripDNSCIDR([]string{"198.51.100.53/32", "198.51.100.54/32"})
	want := []string{"198.51.100.53", "198.51.100.54"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stripDNSCIDR() = %v, want %v", got, want)
	}
}

func TestStripDNSCIDR_Empty(t *testing.T) {
	if got := stripDNSCIDR(nil); got == nil || len(got) != 0 {
		t.Errorf("stripDNSCIDR(nil) = %v, want empty slice", got)
	}
}
