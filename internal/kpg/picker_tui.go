package kpg

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func PickTargetInteractive(in io.Reader, out io.Writer, targets []Target) (Target, error) {
	if len(targets) == 0 {
		return Target{}, errors.New("no targets found")
	}
	SortTargets(targets)
	model := newTargetPickerModel(targets)
	program := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out))
	finalModel, err := program.Run()
	if err != nil {
		return PickTarget(in, out, targets)
	}
	result, ok := finalModel.(targetPickerModel)
	if !ok {
		return Target{}, errors.New("target picker failed")
	}
	if result.canceled {
		return Target{}, errors.New("target selection canceled")
	}
	if result.selected < 0 || result.selected >= len(result.targets) {
		return Target{}, errors.New("no target selected")
	}
	return result.targets[result.selected], nil
}

type targetPickerModel struct {
	targets  []Target
	matches  []int
	cursor   int
	selected int
	canceled bool
	query    string
	width    int
	height   int
	widths   pickerWidths
}

func newTargetPickerModel(targets []Target) targetPickerModel {
	sorted := append([]Target(nil), targets...)
	SortTargets(sorted)
	model := targetPickerModel{
		targets:  sorted,
		selected: -1,
		width:    96,
		height:   18,
		widths:   computeTargetPickerWidths(sorted),
	}
	model.applyFilter()
	return model
}

func (m targetPickerModel) Init() tea.Cmd {
	return nil
}

func (m targetPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "enter":
			if len(m.matches) == 0 {
				return m, nil
			}
			m.selected = m.matches[m.cursor]
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
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
			if len(msg.Runes) > 0 {
				m.query += string(msg.Runes)
				m.applyFilter()
			}
		}
	}
	return m, nil
}

func (m targetPickerModel) View() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("Select target")
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))

	b.WriteString(title)
	b.WriteString("\n\n")
	if m.query == "" {
		b.WriteString(muted.Render("Filter: type to search namespace, cluster, provider, database, or user"))
	} else {
		b.WriteString("Filter: ")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.query))
	}
	b.WriteString("\n\n")

	tableHeader := fmt.Sprintf("   %-*s  %-*s  %-*s  %-*s", m.widths.Target, "Target", m.widths.Provider, "Provider", m.widths.Database, "Database", m.widths.User, "User")
	b.WriteString(header.Render(tableHeader))
	b.WriteString("\n")

	if len(m.matches) == 0 {
		b.WriteString("\n")
		b.WriteString(muted.Render("No matching targets"))
		b.WriteString("\n\n")
		b.WriteString(muted.Render("Backspace edits the filter; Esc cancels."))
		return b.String()
	}

	start, end := m.visibleRange()
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Bold(true)
	for visibleIndex := start; visibleIndex < end; visibleIndex++ {
		target := m.targets[m.matches[visibleIndex]]
		prefix := "  "
		if visibleIndex == m.cursor {
			prefix = "> "
		}
		row := prefix + targetPickerRow(target, m.widths)
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
	b.WriteString(muted.Render("Type to filter; up/down or j/k move; Enter connects; Esc cancels."))
	return b.String()
}

func (m targetPickerModel) visibleRange() (int, int) {
	limit := m.height - 8
	if limit < 5 {
		limit = 5
	}
	if limit > 12 {
		limit = 12
	}
	if len(m.matches) <= limit {
		return 0, len(m.matches)
	}
	start := m.cursor - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(m.matches) {
		start = len(m.matches) - limit
	}
	return start, start + limit
}

func (m *targetPickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.matches = m.matches[:0]
	for i, target := range m.targets {
		if query == "" || targetMatchesQuery(target, query) {
			m.matches = append(m.matches, i)
		}
	}
	sort.Ints(m.matches)
	if len(m.matches) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.matches) {
		m.cursor = len(m.matches) - 1
	}
}

func (m *targetPickerModel) deleteLastRune() {
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

func targetPickerRow(target Target, widths pickerWidths) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s", widths.Target, target.ID(), widths.Provider, valueOrDash(target.Provider), widths.Database, valueOrDash(target.Database), widths.User, valueOrDash(target.User))
}
