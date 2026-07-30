package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"nats/core/engine"

	_ "nats/discovery/arp"
	_ "nats/discovery/blescan"
	_ "nats/discovery/icmp"
	_ "nats/discovery/mdns"
	_ "nats/discovery/ssdp"
	_ "nats/enrich/banner"
	_ "nats/enrich/dns"
	_ "nats/enrich/oui"
	_ "nats/enrich/tcpconnect"
	_ "nats/enrich/tcpsyn"
	_ "nats/enrich/udpscan"
	_ "nats/report/json"
	_ "nats/report/markdown"
	_ "nats/report/plain"
	_ "nats/report/table"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nats",
	Short: "Network scanning tool",
	Long: `nats discovers devices on your local network and enriches them with
identifying information (hostname, MAC vendor, open ports).

Run "nats scan --help" for the full set of flags, including which
techniques/enrichers need root/sudo.`,
}

func buildOptions(cmd *cobra.Command) engine.Options {
	opts := engine.DefaultOptions()
	if subnet, _ := cmd.Flags().GetString("subnet"); subnet != "" {
		opts.Subnet = subnet
	}
	if techniques, _ := cmd.Flags().GetString("techniques"); techniques != "" {
		opts.Techniques = splitTechniques(techniques)
	}
	// --enrich opts in to additional enrichers (tcpsyn, udpscan, banner) on
	// top of the default set (dns, oui, tcpconnect) — unlike --techniques,
	// it never replaces the defaults (FR-6: these three must never run
	// unless explicitly named, but the always-on default set doesn't stop
	// running just because deeper enrichment was also requested).
	if enrich, _ := cmd.Flags().GetString("enrich"); enrich != "" {
		combined := strings.Join(opts.EnrichOptions, ",") + "," + enrich
		opts.EnrichOptions = splitTechniques(combined)
	}
	if format, _ := cmd.Flags().GetString("format"); strings.TrimSpace(format) != "" {
		opts.OutputFormat = strings.ToLower(strings.TrimSpace(format))
	}
	return opts
}

type scanProgress struct {
	techniques     map[string]string
	addressed      int
	totalAddresses int
	devices        []engine.Device
	done           bool
}

func renderProgress(w io.Writer, p *scanProgress) {
	if p.done {
		return
	}
	names := make([]string, 0, len(p.techniques))
	for name := range p.techniques {
		names = append(names, name)
	}
	sort.Strings(names)
	techs := make([]string, 0, len(names))
	for _, name := range names {
		techs = append(techs, name+":"+p.techniques[name])
	}

	// totalAddresses is 0 when no selected technique can enumerate its
	// sweep targets (e.g. mdns/ssdp-only scans) — fall back to a bare count.
	var progressText string
	if p.totalAddresses > 0 {
		pending := p.totalAddresses - p.addressed
		if pending < 0 {
			pending = 0
		}
		progressText = fmt.Sprintf("Addresses probed: %d/%d (%d pending)", p.addressed, p.totalAddresses, pending)
	} else {
		progressText = fmt.Sprintf("Addresses probed: %d", p.addressed)
	}

	// \x1b[K erases to end of line, so a shorter frame can never leave
	// stale characters from a longer previous one.
	fmt.Fprintf(w, "\r\x1b[KTechniques: %s | %s", strings.Join(techs, ", "), progressText)
}

func renderDone(w io.Writer, p *scanProgress) {
	p.done = true
	deviceWord := "devices"
	if len(p.devices) == 1 {
		deviceWord = "device"
	}
	fmt.Fprintf(w, "\r\x1b[KScan complete. %d %s found, %d addresses probed.\n",
		len(p.devices), deviceWord, p.addressed)
}

var engineRun = engine.Run
var writeReportFile = os.WriteFile
var progressWriter io.Writer = os.Stderr
var diagnosticWriter io.Writer = os.Stderr
var reportWriter io.Writer = os.Stdout

// renderDiagnostic is the single function through which every Diagnostic —
// regardless of whether it originated in core/engine or cmd/cli — is
// printed (AD-8). Nothing else in this package prints a Diagnostic's
// Severity/Message/Reason directly.
func renderDiagnostic(w io.Writer, d engine.Diagnostic) {
	severity := d.Severity
	if severity == "" {
		severity = "notice"
	}
	fmt.Fprintf(w, "%s: %s\n", severity, d.Message)
	if d.Reason != "" {
		fmt.Fprintf(w, "  reason: %s\n", d.Reason)
	}
}

func runScan(w io.Writer, events <-chan engine.Event, writer engine.Writer, outputFile string) {
	progress := &scanProgress{
		techniques: make(map[string]string),
	}

	for evt := range events {
		switch evt.Kind {
		case engine.EventKindTechniqueStarted:
			progress.techniques[evt.Technique] = "running"
			if evt.TotalAddresses > progress.totalAddresses {
				progress.totalAddresses = evt.TotalAddresses
			}
			renderProgress(w, progress)
		case engine.EventKindAddressProbed:
			progress.addressed++
			renderProgress(w, progress)
		case engine.EventKindTechniqueSkipped:
			progress.techniques[evt.Technique] = "skipped"
			renderProgress(w, progress)
		case engine.EventKindDeviceFound:
			progress.devices = append(progress.devices, evt.Device)
		case engine.EventKindDeviceUpdated:
			for i, d := range progress.devices {
				if d.IP == evt.Device.IP {
					progress.devices[i] = evt.Device
					break
				}
			}
		case engine.EventKindDone:
			renderDone(w, progress)
			for _, d := range evt.Diagnostics {
				renderDiagnostic(diagnosticWriter, d)
			}
			out, err := writer.Write(evt.Report)
			if err != nil {
				renderDiagnostic(diagnosticWriter, engine.Diagnostic{
					Severity: "error",
					Message:  "failed to render scan summary",
					Reason:   err.Error(),
				})
				continue
			}
			if _, err := reportWriter.Write(out); err != nil {
				renderDiagnostic(diagnosticWriter, engine.Diagnostic{
					Severity: "error",
					Message:  "failed to write scan summary",
					Reason:   err.Error(),
				})
			}
			// File persistence wraps the same Writer output already written to
			// stdout above (AD-7) — it never blocks or replaces the stdout
			// write, it's strictly additive (FR-10).
			if outputFile != "" {
				if err := writeReportFile(outputFile, out, 0o644); err != nil {
					renderDiagnostic(diagnosticWriter, engine.Diagnostic{
						Severity: "error",
						Message:  "failed to write scan summary to file",
						Reason:   err.Error(),
					})
				}
			}
		}
	}
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the local network for devices",
	Long: `Scan the local network for devices.

By default, nats auto-detects the local subnet and runs a single discovery
technique (ARP) plus three always-on enrichers (reverse DNS, MAC OUI vendor
lookup, TCP connect port scan). Every other technique/enricher is opt-in via
--techniques/--enrich.

Privilege requirements: ARP discovery (the default) opens a raw packet
capture handle, which typically needs root/sudo (Linux/macOS/Termux) or
Administrator (Windows). The --enrich opt-in "tcpsyn" and "udpscan"
enrichers need the same elevated privilege for the same reason. ICMP
discovery tries an unprivileged socket first and only falls back to a
privileged raw socket if that fails, so whether it needs root/sudo depends
on the host's configuration (e.g. Linux's net.ipv4.ping_group_range).
mDNS/SSDP discovery and the dns/oui/tcpconnect/banner enrichers never need
elevated privilege. When a selected technique/enricher can't get the
privilege it needs, nats skips just that one, emits a "warning" diagnostic
naming it and why, and still completes the scan with everything else that
was requested.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := buildOptions(cmd)

		// Resolved before the scan starts (not deferred to the Done event) so
		// an invalid --format is reported immediately, rather than after the
		// user waits through a full, potentially privileged scan.
		writer, ok := engine.GetWriter(opts.OutputFormat)
		if !ok {
			renderDiagnostic(diagnosticWriter, engine.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("unrecognized output format %q", opts.OutputFormat),
				Reason:   "expected one of: " + strings.Join(engine.WriterNames(), ", "),
			})
			return nil
		}

		ctx := context.Background()
		events, err := engineRun(ctx, opts)
		if err != nil {
			return fmt.Errorf("engine run failed: %w", err)
		}

		outputFile, _ := cmd.Flags().GetString("output-file")
		runScan(progressWriter, events, writer, strings.TrimSpace(outputFile))
		return nil
	},
}

func splitTechniques(s string) []string {
	if s == "" {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, part := range strings.Split(s, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

// init wires the cobra command tree at package load time (rather than
// inside Execute()) so tests can assert on rootCmd's registered commands
// and flag sets without invoking Execute(), which parses the real process
// os.Args.
func init() {
	scanCmd.Flags().String("subnet", "", "Subnet to scan, CIDR notation (e.g. 192.168.1.0/24). Unset: auto-detect the local subnet.")
	scanCmd.Flags().String("techniques", "", "Comma-separated discovery techniques to run instead of the default (arp, icmp, mdns, ssdp). "+
		"arp typically requires root/sudo (raw packet capture); icmp depends on host config (falls back to a privileged socket only if the unprivileged one fails); mdns and ssdp do not. Default: arp.")
	scanCmd.Flags().String("enrich", "", "Comma-separated opt-in enrichers to run in addition to the always-on defaults (tcpsyn, udpscan, banner). "+
		"tcpsyn and udpscan typically require root/sudo (raw packet capture); banner does not. Default (always on): dns, oui, tcpconnect.")
	scanCmd.Flags().String("format", "table", "Output format for the scan summary: table, json, markdown, or plain.")
	scanCmd.Flags().String("output-file", "", "Additionally write the scan summary to this file, verbatim, alongside the normal stdout output. Unset: stdout only.")
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(bleCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
