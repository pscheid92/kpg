package kpg

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PickFromList asks for one of options with a numbered prompt. It is the
// fallback when the interactive picker cannot run.
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

// readUnbufferedLine reads one line byte by byte so that input typed ahead
// for the next prompt, or for the client command, is not swallowed.
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

// PickTarget asks for one of targets with a numbered table prompt.
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
	if err := writef(out, "  #  %s\n", targetPickerHeader(widths)); err != nil {
		return Target{}, err
	}
	for i, target := range targets {
		if err := writef(out, "  %-2d %s\n", i+1, targetPickerRow(target, widths)); err != nil {
			return Target{}, err
		}
	}
	if err := writeln(out); err != nil {
		return Target{}, err
	}
	if err := writef(out, "Target [1-%d]: ", len(targets)); err != nil {
		return Target{}, err
	}

	line, err := readUnbufferedLine(in)
	if err != nil {
		return Target{}, err
	}
	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(targets) {
		return Target{}, errors.New("invalid target selection")
	}
	return targets[choice-1], nil
}

func computeTargetPickerWidths(targets []Target) tableWidths {
	widths := newTableWidths("Target", "Provider", "Database", "User")
	for _, target := range targets {
		widths.fit(target.ID(), valueOrDash(target.Provider), valueOrDash(target.Database), valueOrDash(target.User))
	}
	return widths
}

func targetPickerHeader(widths tableWidths) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s", widths.Target, "Target", widths.Provider, "Provider", widths.Database, "Database", widths.User, "User")
}

func targetPickerRow(target Target, widths tableWidths) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s", widths.Target, target.ID(), widths.Provider, valueOrDash(target.Provider), widths.Database, valueOrDash(target.Database), widths.User, valueOrDash(target.User))
}
