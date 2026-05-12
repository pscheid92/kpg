package kpg

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func PickFromList(in io.Reader, out io.Writer, label string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no %s to choose from", label)
	}
	if len(options) == 1 {
		return options[0], nil
	}
	if err := writef(out, "Select %s:\n\n", label); err != nil {
		return "", err
	}
	for i, opt := range options {
		if err := writef(out, "  %-2d %s\n", i+1, opt); err != nil {
			return "", err
		}
	}
	if err := writef(out, "\n%s [1-%d]: ", label, len(options)); err != nil {
		return "", err
	}
	line, err := readUnbufferedLine(in)
	if err != nil {
		return "", err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(options) {
		return "", fmt.Errorf("invalid %s selection", label)
	}
	return options[choice-1], nil
}

func readUnbufferedLine(in io.Reader) (string, error) {
	var b strings.Builder
	one := make([]byte, 1)
	for {
		n, err := in.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return b.String(), nil
			}
			return "", err
		}
	}
}

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
