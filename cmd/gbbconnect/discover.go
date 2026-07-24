package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/discovery"
	"github.com/spf13/cobra"
)

const defaultDiscoveryConcurrency = 64

type discoveryDependencies struct {
	discoverUDP func(
		ctx context.Context,
		ifaceIP string,
		timeout time.Duration,
	) ([]discovery.Dongle, error)
	scanSubnet func(
		ctx context.Context,
		cidr string,
		port int,
		concurrency int,
	) ([]discovery.Dongle, error)
}

func defaultDiscoveryDependencies() discoveryDependencies {
	return discoveryDependencies{
		discoverUDP: discovery.DiscoverUDP,
		scanSubnet:  discovery.ScanSubnet,
	}
}

func newDiscoverCommand(dependencies discoveryDependencies) *cobra.Command {
	var interfaceIP string
	var broadcast bool
	var subnet string
	var port int
	var timeout time.Duration
	var jsonOutput bool

	command := &cobra.Command{
		Use:   "discover",
		Short: "Discover supported inverter dongles",
		Long: "Discover supported inverter dongles using Solarman UDP broadcast " +
			"and optional TCP subnet scanning. Subnet scanning reports reachable " +
			"hosts but may not obtain their logger serials.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !broadcast && subnet == "" {
				return errors.New("enable --broadcast or provide --subnet")
			}

			var found []discovery.Dongle
			if broadcast {
				if dependencies.discoverUDP == nil {
					return errors.New("UDP discovery is unavailable")
				}
				dongles, err := dependencies.discoverUDP(
					cmd.Context(),
					interfaceIP,
					timeout,
				)
				if err != nil {
					return fmt.Errorf("UDP discovery: %w", err)
				}
				found = mergeDongles(found, dongles)
			}
			if subnet != "" {
				if dependencies.scanSubnet == nil {
					return errors.New("subnet discovery is unavailable")
				}
				dongles, err := dependencies.scanSubnet(
					cmd.Context(),
					subnet,
					port,
					defaultDiscoveryConcurrency,
				)
				if err != nil {
					return fmt.Errorf("subnet discovery: %w", err)
				}
				found = mergeDongles(found, dongles)
			}
			sortDongles(found)

			if jsonOutput {
				return writeDiscoveryJSON(cmd, found)
			}
			return writeDiscoveryTable(cmd, found)
		},
	}
	command.Flags().StringVar(
		&interfaceIP,
		"interface",
		"",
		"local IPv4 address for UDP broadcast (default: auto)",
	)
	command.Flags().BoolVar(
		&broadcast,
		"broadcast",
		true,
		"use UDP broadcast discovery",
	)
	command.Flags().StringVar(
		&subnet,
		"subnet",
		"",
		"additionally scan an IPv4 CIDR (for example 192.168.1.0/24)",
	)
	command.Flags().IntVar(
		&port,
		"port",
		8899,
		"dongle TCP port used by subnet scan",
	)
	command.Flags().DurationVar(
		&timeout,
		"timeout",
		5*time.Second,
		"UDP discovery timeout",
	)
	command.Flags().BoolVar(
		&jsonOutput,
		"json",
		false,
		"emit machine-readable JSON",
	)
	return command
}

func mergeDongles(
	current []discovery.Dongle,
	additional []discovery.Dongle,
) []discovery.Dongle {
	for _, candidate := range additional {
		match := -1
		for index, existing := range current {
			sameSerial := candidate.Serial != 0 &&
				existing.Serial == candidate.Serial
			sameIP := candidate.IP != "" && existing.IP == candidate.IP
			if sameSerial || sameIP {
				match = index
				break
			}
		}
		if match < 0 {
			current = append(current, candidate)
			continue
		}
		if current[match].IP == "" {
			current[match].IP = candidate.IP
		}
		if current[match].MAC == "" {
			current[match].MAC = candidate.MAC
		}
		if current[match].Serial == 0 {
			current[match].Serial = candidate.Serial
		}
		if current[match].Raw == "" {
			current[match].Raw = candidate.Raw
		}
	}
	return current
}

func sortDongles(dongles []discovery.Dongle) {
	slices.SortFunc(dongles, func(left, right discovery.Dongle) int {
		leftIP, leftErr := netip.ParseAddr(left.IP)
		rightIP, rightErr := netip.ParseAddr(right.IP)
		switch {
		case leftErr == nil && rightErr == nil:
			if compared := leftIP.Compare(rightIP); compared != 0 {
				return compared
			}
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		}
		if left.Serial < right.Serial {
			return -1
		}
		if left.Serial > right.Serial {
			return 1
		}
		if left.Raw < right.Raw {
			return -1
		}
		if left.Raw > right.Raw {
			return 1
		}
		return 0
	})
}

func writeDiscoveryJSON(cmd *cobra.Command, dongles []discovery.Dongle) error {
	if dongles == nil {
		dongles = []discovery.Dongle{}
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Dongles []discovery.Dongle `json:"dongles"`
	}{Dongles: dongles})
}

func writeDiscoveryTable(cmd *cobra.Command, dongles []discovery.Dongle) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "Discovered Solarman dongles:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "  IP\tMAC\tSerial\tRaw"); err != nil {
		return err
	}
	for _, dongle := range dongles {
		serial := ""
		if dongle.Serial != 0 {
			serial = fmt.Sprintf("%d", dongle.Serial)
		}
		if _, err := fmt.Fprintf(
			writer,
			"  %s\t%s\t%s\t%s\n",
			dongle.IP,
			dongle.MAC,
			serial,
			dongle.Raw,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "\n%d dongle(s) found.\n", len(dongles)); err != nil {
		return err
	}
	return writer.Flush()
}
