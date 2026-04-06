// Package system provides types and logic for collecting system information
// and resource metrics from the host VM using gopsutil.
package system

// SystemInfo holds static/semi-static information about the host VM.
type SystemInfo struct {
	// Hostname is the OS-level hostname of the VM.
	Hostname string `json:"hostname"`
	// OS is the operating system name (e.g. "Ubuntu 24.04 LTS").
	OS string `json:"os"`
	// KernelVersion is the running kernel version string.
	KernelVersion string `json:"kernel_version"`
	// Architecture is the CPU architecture (e.g. "x86_64").
	Architecture string `json:"architecture"`
	// Uptime is the number of seconds since the last boot.
	Uptime int64 `json:"uptime"`
	// BootTime is the Unix timestamp (seconds) of the last system boot.
	BootTime int64 `json:"boot_time"`
	// VMID is the PlusClouds VM identifier read from the ISO metadata.
	VMID string `json:"vm_id,omitempty"`
	// TenantID is the PlusClouds tenant identifier read from the ISO metadata.
	TenantID string `json:"tenant_id,omitempty"`
}

// CPUStats contains current CPU utilisation and load average data.
type CPUStats struct {
	// UsagePercent is the overall CPU usage across all cores (0–100).
	UsagePercent float64 `json:"usage_percent"`
	// CoreCount is the number of logical CPU cores available.
	CoreCount int `json:"core_count"`
	// ModelName is the CPU model string reported by the kernel.
	ModelName string `json:"model_name"`
	// LoadAvg1 is the 1-minute system load average.
	LoadAvg1 float64 `json:"load_avg_1"`
	// LoadAvg5 is the 5-minute system load average.
	LoadAvg5 float64 `json:"load_avg_5"`
	// LoadAvg15 is the 15-minute system load average.
	LoadAvg15 float64 `json:"load_avg_15"`
}

// MemoryStats contains current RAM and swap utilisation figures.
type MemoryStats struct {
	// TotalBytes is the total physical RAM in bytes.
	TotalBytes uint64 `json:"total_bytes"`
	// UsedBytes is the amount of RAM currently in use (bytes).
	UsedBytes uint64 `json:"used_bytes"`
	// FreeBytes is the amount of RAM not in use (bytes).
	FreeBytes uint64 `json:"free_bytes"`
	// UsagePercent is RAM utilisation as a percentage (0–100).
	UsagePercent float64 `json:"usage_percent"`
	// SwapTotal is the total swap space in bytes.
	SwapTotal uint64 `json:"swap_total"`
	// SwapUsed is the amount of swap currently in use (bytes).
	SwapUsed uint64 `json:"swap_used"`
}

// DiskStats aggregates per-partition disk usage information.
type DiskStats struct {
	// Partitions holds usage data for each mounted filesystem.
	Partitions []PartitionStats `json:"partitions"`
}

// PartitionStats holds disk usage for a single mounted partition.
type PartitionStats struct {
	// Device is the block device path (e.g. "/dev/sda1").
	Device string `json:"device"`
	// Mountpoint is where the partition is mounted (e.g. "/").
	Mountpoint string `json:"mountpoint"`
	// Fstype is the filesystem type (e.g. "ext4").
	Fstype string `json:"fstype"`
	// TotalBytes is the partition capacity in bytes.
	TotalBytes uint64 `json:"total_bytes"`
	// UsedBytes is the amount of space currently used (bytes).
	UsedBytes uint64 `json:"used_bytes"`
	// FreeBytes is the amount of free space (bytes).
	FreeBytes uint64 `json:"free_bytes"`
	// UsagePercent is disk utilisation as a percentage (0–100).
	UsagePercent float64 `json:"usage_percent"`
}

// NetworkStats aggregates per-interface network counters.
type NetworkStats struct {
	// Interfaces holds counters for each network interface.
	Interfaces []InterfaceStats `json:"interfaces"`
}

// InterfaceStats holds network I/O counters for a single NIC.
type InterfaceStats struct {
	// Name is the interface name (e.g. "eth0").
	Name string `json:"name"`
	// IPAddresses holds all IP addresses (v4 and v6) assigned to the interface.
	IPAddresses []string `json:"ip_addresses,omitempty"`
	// BytesSent is the total bytes transmitted since boot.
	BytesSent uint64 `json:"bytes_sent"`
	// BytesRecv is the total bytes received since boot.
	BytesRecv uint64 `json:"bytes_recv"`
	// PacketsSent is the total packets transmitted since boot.
	PacketsSent uint64 `json:"packets_sent"`
	// PacketsRecv is the total packets received since boot.
	PacketsRecv uint64 `json:"packets_recv"`
	// IsUp indicates whether the interface is currently up.
	IsUp bool `json:"is_up"`
}

// SystemMetrics is a point-in-time snapshot of all resource metrics.
type SystemMetrics struct {
	// CPU contains CPU utilisation and load averages.
	CPU CPUStats `json:"cpu"`
	// Memory contains RAM and swap utilisation.
	Memory MemoryStats `json:"memory"`
	// Disk contains per-partition disk usage.
	Disk DiskStats `json:"disk"`
	// Network contains per-interface I/O counters.
	Network NetworkStats `json:"network"`
	// CollectedAt is the Unix timestamp (seconds) when the metrics were gathered.
	CollectedAt int64 `json:"collected_at"`
}
