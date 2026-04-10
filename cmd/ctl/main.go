// Command plsctl is the PlusClouds agent CLI client.
// It communicates with a running plusclouds-agent instance over HTTP.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/plusclouds/ubuntu-agent/internal/config"
	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
	"github.com/plusclouds/ubuntu-agent/internal/modules/system"
	"github.com/plusclouds/ubuntu-agent/pkg/cmdutil"
)

// Global flags.
var (
	baseURL    string
	apiKey     string
	outputFmt  string
)

// apiResponse mirrors the agent's standard Response envelope.
type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	root := buildRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "plsctl",
		Short: "PlusClouds agent CLI",
		Long:  "plsctl interacts with the PlusClouds Ubuntu VM agent over its REST API.",
	}

	root.PersistentFlags().StringVar(&baseURL, "url", "http://localhost:8080",
		"Base URL of the agent API (env: PLSCTL_URL)")
	root.PersistentFlags().StringVar(&apiKey, "api-key", "",
		"API key for authentication (env: PLSCTL_API_KEY)")
	root.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table",
		"Output format: table, json")

	// Allow environment variable overrides for flags.
	if u := os.Getenv("PLSCTL_URL"); u != "" && baseURL == "http://localhost:8080" {
		baseURL = u
	}
	if k := os.Getenv("PLSCTL_API_KEY"); k != "" && apiKey == "" {
		apiKey = k
	}

	root.AddCommand(
		buildSystemCmd(),
		buildServiceCmd(),
		buildMetadataCmd(),
		buildAgentCmd(),
	)

	return root
}

// ---------------------------------------------------------------------------
// system commands
// ---------------------------------------------------------------------------

func buildSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Query system resource information",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "info",
			Short: "Show VM identity and OS information",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.SystemInfo]("/api/v1/system/info",
					func(v *system.SystemInfo) {
						cmdutil.PrintTable(
							[]string{"Field", "Value"},
							[][]string{
								{"Hostname", v.Hostname},
								{"OS", v.OS},
								{"Kernel", v.KernelVersion},
								{"Architecture", v.Architecture},
								{"Uptime", formatSeconds(v.Uptime)},
								{"Boot Time", time.Unix(v.BootTime, 0).Format(time.RFC3339)},
								{"VM ID", v.VMID},
								{"Tenant ID", v.TenantID},
							},
						)
					})
			},
		},
		&cobra.Command{
			Use:   "metrics",
			Short: "Show all resource metrics (CPU, memory, disk, network)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.SystemMetrics]("/api/v1/system/metrics",
					func(v *system.SystemMetrics) {
						fmt.Printf("Collected at: %s\n\n", time.Unix(v.CollectedAt, 0).Format(time.RFC3339))
						printCPU(&v.CPU)
						printMemory(&v.Memory)
					})
			},
		},
		&cobra.Command{
			Use:   "cpu",
			Short: "Show CPU statistics",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.CPUStats]("/api/v1/system/cpu",
					func(v *system.CPUStats) { printCPU(v) })
			},
		},
		&cobra.Command{
			Use:   "memory",
			Short: "Show memory statistics",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.MemoryStats]("/api/v1/system/memory",
					func(v *system.MemoryStats) { printMemory(v) })
			},
		},
		&cobra.Command{
			Use:   "disk",
			Short: "Show disk usage per partition",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.DiskStats]("/api/v1/system/disk",
					func(v *system.DiskStats) {
						rows := make([][]string, 0, len(v.Partitions))
						for _, p := range v.Partitions {
							rows = append(rows, []string{
								p.Device,
								p.Mountpoint,
								p.Fstype,
								formatBytes(p.TotalBytes),
								formatBytes(p.UsedBytes),
								formatBytes(p.FreeBytes),
								fmt.Sprintf("%.1f%%", p.UsagePercent),
							})
						}
						cmdutil.PrintTable(
							[]string{"Device", "Mountpoint", "FS", "Total", "Used", "Free", "Use%"},
							rows,
						)
					})
			},
		},
		&cobra.Command{
			Use:   "network",
			Short: "Show network interface statistics",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[system.NetworkStats]("/api/v1/system/network",
					func(v *system.NetworkStats) {
						rows := make([][]string, 0, len(v.Interfaces))
						for _, iface := range v.Interfaces {
							up := "no"
							if iface.IsUp {
								up = "yes"
							}
							rows = append(rows, []string{
								iface.Name,
								strings.Join(iface.IPAddresses, ", "),
								formatBytes(iface.BytesRecv),
								formatBytes(iface.BytesSent),
								up,
							})
						}
						cmdutil.PrintTable(
							[]string{"Interface", "IP Addresses", "Bytes Recv", "Bytes Sent", "Up"},
							rows,
						)
					})
			},
		},
	)
	return cmd
}

// ---------------------------------------------------------------------------
// service commands
// ---------------------------------------------------------------------------

func buildServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage systemd services",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all loaded systemd services",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[[]services.ServiceInfo]("/api/v1/services",
					func(v *[]services.ServiceInfo) {
						rows := make([][]string, 0, len(*v))
						for _, s := range *v {
							enabled := "no"
							if s.Enabled {
								enabled = "yes"
							}
							rows = append(rows, []string{
								s.Name,
								string(s.State),
								s.SubState,
								enabled,
								strconv.FormatUint(uint64(s.PID), 10),
							})
						}
						cmdutil.PrintTable(
							[]string{"Name", "State", "SubState", "Enabled", "PID"},
							rows,
						)
					})
			},
		},
		&cobra.Command{
			Use:   "get <name>",
			Short: "Show details for a single service",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchAndPrint[services.ServiceInfo]("/api/v1/services/"+args[0],
					func(v *services.ServiceInfo) {
						enabled := "no"
						if v.Enabled {
							enabled = "yes"
						}
						cmdutil.PrintTable(
							[]string{"Field", "Value"},
							[][]string{
								{"Name", v.Name},
								{"Description", v.Description},
								{"State", string(v.State)},
								{"Sub-State", v.SubState},
								{"Enabled", enabled},
								{"PID", strconv.FormatUint(uint64(v.PID), 10)},
							},
						)
					})
			},
		},
		buildServiceActionCmd("start", "Start a service"),
		buildServiceActionCmd("stop", "Stop a service"),
		buildServiceActionCmd("restart", "Restart a service"),
		buildServiceActionCmd("reload", "Reload a service configuration"),
		buildServiceActionCmd("enable", "Enable a service to start on boot"),
		buildServiceActionCmd("disable", "Disable a service from starting on boot"),
	)
	return cmd
}

// buildServiceActionCmd creates a cobra.Command for a POST service action.
func buildServiceActionCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fetchAndPrint[services.ActionResult](
				"/api/v1/services/"+args[0]+"/"+action,
				func(v *services.ActionResult) {
					if v.Success {
						cmdutil.PrintSuccess(v.Message)
					} else {
						cmdutil.PrintError(v.Message)
					}
				},
				withMethod(http.MethodPost),
			)
		},
	}
}

// ---------------------------------------------------------------------------
// metadata commands
// ---------------------------------------------------------------------------

func buildMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Show VM ISO metadata",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show all available metadata",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchRawAndPrint("/api/v1/metadata")
			},
		},
		&cobra.Command{
			Use:   "instance",
			Short: "Show VM instance metadata",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchRawAndPrint("/api/v1/metadata/instance")
			},
		},
		&cobra.Command{
			Use:   "network",
			Short: "Show VM network metadata",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchRawAndPrint("/api/v1/metadata/network")
			},
		},
	)
	return cmd
}

// ---------------------------------------------------------------------------
// agent commands
// ---------------------------------------------------------------------------

func buildAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent status and version information",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Check if the agent is alive",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fetchRawAndPrint("/healthz")
			},
		},
		&cobra.Command{
			Use:   "version",
			Short: "Print the agent version",
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Println("plsctl version:", config.AgentVersion)
			},
		},
	)
	return cmd
}

// ---------------------------------------------------------------------------
// HTTP client helpers
// ---------------------------------------------------------------------------

type requestOptions struct {
	method string
}

type requestOption func(*requestOptions)

func withMethod(method string) requestOption {
	return func(o *requestOptions) { o.method = method }
}

// doRequest performs an HTTP request to the agent API and returns the parsed
// apiResponse. It applies the global baseURL, apiKey, and output format.
func doRequest(path string, opts ...requestOption) (*apiResponse, error) {
	o := &requestOptions{method: http.MethodGet}
	for _, opt := range opts {
		opt(o)
	}

	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequest(o.method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to agent at %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		// Return raw body on non-JSON responses (e.g. plaintext errors).
		return nil, fmt.Errorf("parsing response (status %d): %w\n%s", resp.StatusCode, err, body)
	}

	if !ar.Success && ar.Error != nil {
		return nil, fmt.Errorf("[%s] %s", ar.Error.Code, ar.Error.Message)
	}

	return &ar, nil
}

// fetchAndPrint is a generic helper that fetches a typed API response and
// calls the provided table printer (unless the user requested JSON output,
// in which case the raw JSON is printed).
func fetchAndPrint[T any](path string, tablePrinter func(*T), opts ...requestOption) error {
	ar, err := doRequest(path, opts...)
	if err != nil {
		cmdutil.PrintError(err.Error())
		return err
	}

	if outputFmt == "json" {
		var v T
		if err := json.Unmarshal(ar.Data, &v); err != nil {
			return fmt.Errorf("parsing data: %w", err)
		}
		cmdutil.PrintJSON(v)
		return nil
	}

	var v T
	if err := json.Unmarshal(ar.Data, &v); err != nil {
		return fmt.Errorf("parsing data: %w", err)
	}
	tablePrinter(&v)
	return nil
}

// fetchRawAndPrint fetches the raw JSON payload and prints it regardless of
// output format.
func fetchRawAndPrint(path string, opts ...requestOption) error {
	ar, err := doRequest(path, opts...)
	if err != nil {
		cmdutil.PrintError(err.Error())
		return err
	}
	// Pretty-print the data field.
	var v interface{}
	if err := json.Unmarshal(ar.Data, &v); err != nil {
		fmt.Println(string(ar.Data))
		return nil
	}
	cmdutil.PrintJSON(v)
	return nil
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatSeconds(s int64) string {
	d := time.Duration(s) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func printCPU(v *system.CPUStats) {
	cmdutil.PrintTable(
		[]string{"Field", "Value"},
		[][]string{
			{"CPU Model", v.ModelName},
			{"Core Count", strconv.Itoa(v.CoreCount)},
			{"Usage", fmt.Sprintf("%.1f%%", v.UsagePercent)},
			{"Load Avg 1m", fmt.Sprintf("%.2f", v.LoadAvg1)},
			{"Load Avg 5m", fmt.Sprintf("%.2f", v.LoadAvg5)},
			{"Load Avg 15m", fmt.Sprintf("%.2f", v.LoadAvg15)},
		},
	)
}

func printMemory(v *system.MemoryStats) {
	cmdutil.PrintTable(
		[]string{"Field", "Value"},
		[][]string{
			{"Total RAM", formatBytes(v.TotalBytes)},
			{"Used RAM", formatBytes(v.UsedBytes)},
			{"Free RAM", formatBytes(v.FreeBytes)},
			{"RAM Usage", fmt.Sprintf("%.1f%%", v.UsagePercent)},
			{"Swap Total", formatBytes(v.SwapTotal)},
			{"Swap Used", formatBytes(v.SwapUsed)},
		},
	)
}
