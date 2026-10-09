package kpg

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// errPickerUnavailable reports that the full-screen picker could not run, for
// example because the terminal does not support raw mode.
var errPickerUnavailable = errors.New("interactive picker unavailable")

// PickTargetInteractive opens the full-screen target picker and falls back to
// the numbered prompt when the terminal cannot run it.
func PickTargetInteractive(in io.Reader, out io.Writer, targets []Target) (Target, error) {
	if len(targets) == 0 {
		return Target{}, errors.New("no targets found")
	}
	SortTargets(targets)
	target, err := runPicker(in, out, newTargetPickerModel(targets), "target")
	if errors.Is(err, errPickerUnavailable) {
		return PickTarget(in, out, targets)
	}
	return target, err
}

// PickFromListInteractive opens the full-screen picker for options and falls
// back to the numbered prompt when the terminal cannot run it.
func PickFromListInteractive(in io.Reader, out io.Writer, label string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no %s to choose from", label)
	}
	if len(options) == 1 {
		return options[0], nil
	}
	choice, err := runPicker(in, out, newListPickerModel(label, options), label)
	if errors.Is(err, errPickerUnavailable) {
		return PickFromList(in, out, label, options)
	}
	return choice, err
}

func runPicker[T any](in io.Reader, out io.Writer, model pickerModel[T], what string) (T, error) {
	var zero T
	program := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out))
	finalModel, err := program.Run()
	if err != nil {
		return zero, fmt.Errorf("%w: %w", errPickerUnavailable, err)
	}
	result, ok := finalModel.(pickerModel[T])
	if !ok {
		return zero, fmt.Errorf("%s picker failed", what)
	}
	if result.canceled {
		return zero, fmt.Errorf("%s selection canceled", what)
	}
	if result.selected < 0 || result.selected >= len(result.items) {
		return zero, fmt.Errorf("no %s selected", what)
	}
	return result.items[result.selected], nil
}

// pickerModel is the Bubble Tea model behind every interactive choice: a
// filter line, a cursor over the matching rows, Enter to select, Esc to
// cancel. Targets and plain strings only differ in how a row is rendered and
// matched.
type pickerModel[T any] struct {
	title    string
	hint     string
	header   string
	noMatch  string
	help     string
	items    []T
	row      func(T) string
	match    func(T, string) bool
	matches  []int
	cursor   int
	selected int
	canceled bool
	query    string
	height   int
}

type (
	targetPickerModel = pickerModel[Target]
	listPickerModel   = pickerModel[string]
)

const pickerDefaultHeight = 18

func newPickerModel[T any](model pickerModel[T]) pickerModel[T] {
	model.selected = -1
	model.height = pickerDefaultHeight
	model.applyFilter()
	return model
}

func newTargetPickerModel(targets []Target) targetPickerModel {
	sorted := slices.Clone(targets)
	SortTargets(sorted)
	widths := computeTargetPickerWidths(sorted)
	return newPickerModel(pickerModel[Target]{
		title:   "Select target",
		hint:    "type to search namespace, cluster, provider, database, or user",
		header:  "   " + targetPickerHeader(widths),
		noMatch: "No matching targets",
		help:    "Type to filter; up/down or ctrl+j/ctrl+k move; Enter connects; Esc cancels.",
		items:   sorted,
		row:     func(target Target) string { return targetPickerRow(target, widths) },
		match:   targetMatchesQuery,
	})
}

func newListPickerModel(label string, options []string) listPickerModel {
	return newPickerModel(pickerModel[string]{
		title:   "Select " + label,
		hint:    "type to search",
		noMatch: "No matching " + label,
		help:    "Type to filter; up/down or ctrl+j/ctrl+k move; Enter selects; Esc cancels.",
		items:   slices.Clone(options),
		row:     func(option string) string { return option },
		match: func(option string, query string) bool {
			return strings.Contains(strings.ToLower(option), query)
		},
	})
}

func (m pickerModel[T]) Init() tea.Cmd {
	return nil
}

func (m pickerModel[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.PasteMsg:
		m.query += msg.Content
		m.applyFilter()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "escape":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			if len(m.matches) == 0 {
				return m, nil
			}
			m.selected = m.matches[m.cursor]
			return m, tea.Quit
		case "up", "ctrl+p", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+n", "ctrl+j":
			if m.cursor < len(m.matches)-1 {
				m.cursor++
			}
		case "home":
			m.cursor = 0
		case "end":
			if len(m.matches) > 0 {
				m.cursor = len(m.matches) - 1
			}
		case "backspace", "ctrl+h":
			m.deleteLastRune()
		default:
			if msg.Text != "" {
				m.query += msg.Text
				m.applyFilter()
			}
		}
	}
	return m, nil
}

func (m pickerModel[T]) View() tea.View {
	return tea.NewView(m.render())
}

// render draws the picker inline: title, filter line, optional table header,
// the visible window of matching rows, and a help line.
func (m pickerModel[T]) render() string {
	var b strings.Builder
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Bold(true)

	b.WriteString(accent.Bold(true).Render(m.title))
	b.WriteString("\n\n")
	if m.query == "" {
		b.WriteString(muted.Render("Filter: " + m.hint))
	} else {
		b.WriteString("Filter: ")
		b.WriteString(accent.Render(m.query))
	}
	b.WriteString("\n\n")
	if m.header != "" {
		b.WriteString(header.Render(m.header))
		b.WriteString("\n")
	}

	if len(m.matches) == 0 {
		b.WriteString("\n")
		b.WriteString(muted.Render(m.noMatch))
		b.WriteString("\n\n")
		b.WriteString(muted.Render("Backspace edits the filter; Esc cancels."))
		return b.String()
	}

	start, end := m.visibleRange()
	for visibleIndex := start; visibleIndex < end; visibleIndex++ {
		prefix := "  "
		if visibleIndex == m.cursor {
			prefix = "> "
		}
		row := prefix + m.row(m.items[m.matches[visibleIndex]])
		if visibleIndex == m.cursor {
			row = selected.Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	if end < len(m.matches) {
		b.WriteString(muted.Render(fmt.Sprintf("  ... %d more", len(m.matches)-end)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(muted.Render(m.help))
	return b.String()
}

func (m pickerModel[T]) visibleRange() (int, int) {
	limit := min(max(m.height-8, 5), 12)
	if len(m.matches) <= limit {
		return 0, len(m.matches)
	}
	start := max(m.cursor-limit/2, 0)
	if start+limit > len(m.matches) {
		start = len(m.matches) - limit
	}
	return start, start + limit
}

func (m *pickerModel[T]) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.matches = m.matches[:0]
	for i, item := range m.items {
		if query == "" || m.match(item, query) {
			m.matches = append(m.matches, i)
		}
	}
	if len(m.matches) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.matches) {
		m.cursor = len(m.matches) - 1
	}
}

func (m *pickerModel[T]) deleteLastRune() {
	if m.query == "" {
		return
	}
	runes := []rune(m.query)
	m.query = string(runes[:len(runes)-1])
	m.applyFilter()
}

func targetMatchesQuery(target Target, query string) bool {
	fields := []string{
		target.ID(),
		target.QualifiedID(),
		target.Provider,
		target.Namespace,
		target.Cluster,
		target.Database,
		target.User,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}
