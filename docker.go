package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/network"
	"go.uber.org/zap"
	"go4.org/netipx"
)

var (
	dockerLogger    = logger.Named("Docker")
	dockerNetworks  []string
	dockerClient    context.Context
	dockerNetIPSets = map[string]*netipx.IPSet{}
	dockerActiveIPs = &netipx.IPSet{}
	dockerNewIP     = make(chan netip.Addr, 64)
	ctMap           = make(map[string]struct{})
)

func dockerListen() (e error) {
	dockerClient, e = bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if e != nil {
		return e
	}
	go func() {
		for {
			for _, n := range dockerNetworks {
				dockerRefreshNetwork(n)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	return nil
}

func dockerRefreshNetwork(name string) {
	inspect, e := network.Inspect(dockerClient, name, nil)
	if e != nil {
		dockerLogger.Warn("NetworkInfo error "+name, zap.Error(e))
		return
	}

	var b netipx.IPSetBuilder
	var ipAddrs []string
	var newIPs []netip.Addr
	for ctID, ct := range inspect.Containers {
		for _, iface := range ct.Interfaces {
			for _, subnet := range iface.Subnets {
				if subnet.IPNet.IP.IsGlobalUnicast() && len(subnet.IPNet.IP) == net.IPv6len {
					fmt.Println(subnet.IPNet.IP.String())
					ip := netip.AddrFrom16([16]byte(subnet.IPNet.IP.To16()))
					b.Add(ip)
					ipAddrs = append(ipAddrs, ip.String())
					if _, ok := ctMap[ctID]; !ok {
						newIPs = append(newIPs, ip)
						ctMap[ctID] = struct{}{}
					}
				}
			}
		}
	}
	dockerLogger.Info("active IPs updated", zap.String("network", inspect.Name), zap.Strings("ip", ipAddrs))
	dockerNetIPSets[inspect.ID], _ = b.IPSet()
	for n, inset := range dockerNetIPSets {
		if n != inspect.ID {
			b.AddSet(inset)
		}
	}
	dockerActiveIPs, _ = b.IPSet()
	for _, ip := range newIPs {
		dockerNewIP <- ip
	}
}
