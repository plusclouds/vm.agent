package system

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	gopsnet "github.com/shirou/gopsutil/v3/net"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// Module collects system information and resource metrics from the host VM.
type Module struct {
	iso *isoconfig.ISOMetadata
}

// New creates a new system Module. iso may be a zero-value ISOMetadata
// (e.g. when running without a config drive); VMID and TenantID will
// simply be empty in that case.
func New(iso *isoconfig.ISOMetadata) *Module {
	return &Module{iso: iso}
}

// GetInfo returns static/semi-static information about the host VM.
func (m *Module) GetInfo(ctx context.Context) (*SystemInfo, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting host info: %w", err)
	}

	si := &SystemInfo{
		Hostname:      info.Hostname,
		OS:            fmt.Sprintf("%s %s", info.Platform, info.PlatformVersion),
		KernelVersion: info.KernelVersion,
		Architecture:  info.KernelArch,
		Uptime:        int64(info.Uptime),
		BootTime:      int64(info.BootTime),
	}

	if m.iso != nil {
		si.VMID = m.iso.VMID()
		si.TenantID = m.iso.TenantID()
	}

	return si, nil
}

// GetMetrics returns a point-in-time snapshot of all resource metrics.
func (m *Module) GetMetrics(ctx context.Context) (*SystemMetrics, error) {
	cpuStats, err := m.GetCPU(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting CPU metrics: %w", err)
	}

	memStats, err := m.GetMemory(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting memory metrics: %w", err)
	}

	diskStats, err := m.GetDisk(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting disk metrics: %w", err)
	}

	netStats, err := m.GetNetwork(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting network metrics: %w", err)
	}

	return &SystemMetrics{
		CPU:         *cpuStats,
		Memory:      *memStats,
		Disk:        *diskStats,
		Network:     *netStats,
		CollectedAt: time.Now().Unix(),
	}, nil
}

// GetCPU returns current CPU utilisation and load average data.
// It samples CPU usage over a 500 ms window.
func (m *Module) GetCPU(ctx context.Context) (*CPUStats, error) {
	// Percent with interval=0 returns usage since last call (or since boot on
	// first call). We use a short non-zero interval for a meaningful reading.
	percents, err := cpu.PercentWithContext(ctx, 500*time.Millisecond, false)
	if err != nil {
		return nil, fmt.Errorf("cpu.Percent: %w", err)
	}

	var usagePct float64
	if len(percents) > 0 {
		usagePct = percents[0]
	}

	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("cpu.Info: %w", err)
	}

	modelName := ""
	if len(infos) > 0 {
		modelName = infos[0].ModelName
	}

	coreCount, err := cpu.CountsWithContext(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("cpu.Counts: %w", err)
	}

	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load.Avg: %w", err)
	}

	return &CPUStats{
		UsagePercent: usagePct,
		CoreCount:    coreCount,
		ModelName:    modelName,
		LoadAvg1:     avg.Load1,
		LoadAvg5:     avg.Load5,
		LoadAvg15:    avg.Load15,
	}, nil
}

// GetMemory returns current RAM and swap utilisation.
func (m *Module) GetMemory(ctx context.Context) (*MemoryStats, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("mem.VirtualMemory: %w", err)
	}

	sm, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("mem.SwapMemory: %w", err)
	}

	return &MemoryStats{
		TotalBytes:   vm.Total,
		UsedBytes:    vm.Used,
		FreeBytes:    vm.Free,
		UsagePercent: vm.UsedPercent,
		SwapTotal:    sm.Total,
		SwapUsed:     sm.Used,
	}, nil
}

// GetDisk returns usage data for all locally mounted partitions.
// Pseudo-filesystems (proc, sys, devtmpfs, etc.) are skipped.
func (m *Module) GetDisk(ctx context.Context) (*DiskStats, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("disk.Partitions: %w", err)
	}

	var stats []PartitionStats
	for _, p := range partitions {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			// Some pseudo-mounts may fail; skip them gracefully.
			continue
		}
		stats = append(stats, PartitionStats{
			Device:       p.Device,
			Mountpoint:   p.Mountpoint,
			Fstype:       p.Fstype,
			TotalBytes:   usage.Total,
			UsedBytes:    usage.Used,
			FreeBytes:    usage.Free,
			UsagePercent: usage.UsedPercent,
		})
	}

	return &DiskStats{Partitions: stats}, nil
}

// GetNetwork returns I/O counters and address information for all interfaces.
func (m *Module) GetNetwork(ctx context.Context) (*NetworkStats, error) {
	counters, err := gopsnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("net.IOCounters: %w", err)
	}

	ifaces, err := gopsnet.InterfacesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("net.Interfaces: %w", err)
	}

	// Build a map from interface name → gopsutil interface for address lookup.
	ifaceMap := make(map[string]gopsnet.InterfaceStat, len(ifaces))
	for _, iface := range ifaces {
		ifaceMap[iface.Name] = iface
	}

	var interfaces []InterfaceStats
	for _, c := range counters {
		iface := InterfaceStats{
			Name:        c.Name,
			BytesSent:   c.BytesSent,
			BytesRecv:   c.BytesRecv,
			PacketsSent: c.PacketsSent,
			PacketsRecv: c.PacketsRecv,
		}

		if netIface, ok := ifaceMap[c.Name]; ok {
			// Check if the interface is up by looking for the "up" flag.
			for _, flag := range netIface.Flags {
				if flag == "up" {
					iface.IsUp = true
					break
				}
			}
			// Collect all assigned IP addresses.
			for _, addr := range netIface.Addrs {
				iface.IPAddresses = append(iface.IPAddresses, addr.Addr)
			}
		}

		interfaces = append(interfaces, iface)
	}

	return &NetworkStats{Interfaces: interfaces}, nil
}
