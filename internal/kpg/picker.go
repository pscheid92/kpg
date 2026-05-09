package kpg

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func PickTarget(in io.Reader, out io.Writer, targets []Target) (Target, error) {
	if len(targets) == 0 {
		return Target{}, errors.New("no targets found")
	}
	SortTargets(targets)
	if err := writeln(out, "Select target:"); err != nil {
		return Target{}, err
	}
	if err := writeln(out); err != nil {
		return Target{}, err
	}
	widths := computeTargetPickerWidths(targets)
	if err := writef(out, "  #  %-*s  %-*s  %-*s  %-*s\n", widths.Target, "Target", widths.Provider, "Provider", widths.Database, "Database", widths.User, "User"); err != nil {
		return Target{}, err
	}
	for i, target := range targets {
		if err := writef(out, "  %-2d %-*s  %-*s  %-*s  %-*s\n", i+1, widths.Target, target.ID(), widths.Provider, valueOrDash(target.Provider), widths.Database, valueOrDash(target.Database), widths.User, valueOrDash(target.User)); err != nil {
			return Target{}, err
		}
	}
	if err := writeln(out); err != nil {
		return Target{}, err
	}
	if err := writef(out, "Target [1-%d]: ", len(targets)); err != nil {
		return Target{}, err
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return Target{}, err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(targets) {
		return Target{}, fmt.Errorf("invalid target selection")
	}
	return targets[choice-1], nil
}

type pickerWidths struct {
	Target   int
	Provider int
	Database int
	User     int
}

func computeTargetPickerWidths(targets []Target) pickerWidths {
	widths := pickerWidths{
		Target:   len("Target"),
		Provider: len("Provider"),
		Database: len("Database"),
		User:     len("User"),
	}
	for _, target := range targets {
		widths.Target = max(widths.Target, len(target.ID()))
		widths.Provider = max(widths.Provider, len(valueOrDash(target.Provider)))
		widths.Database = max(widths.Database, len(valueOrDash(target.Database)))
		widths.User = max(widths.User, len(valueOrDash(target.User)))
	}
	return widths
}
