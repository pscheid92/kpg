package kpg

import (
	"fmt"
	"io"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func PickFromListInteractive(in io.Reader, out io.Writer, label string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no %s to choose from", label)
	}
	if len(options) == 1 {
		return options[0], nil
	}
	model := newListPickerModel(label, options)
	program := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out))
	finalModel, err := program.Run()
	if err != nil {
		return PickFromList(in, out, label, options)
	}
	result, ok := finalModel.(listPickerModel)
	if !ok {
		return "", fmt.Errorf("%s picker failed", label)
	}
	if result.canceled {
		return "", fmt.Errorf("%s selection canceled", label)
	}
	if result.selected < 0 || result.selected >= len(result.options) {
		return "", fmt.Errorf("no %s selected", label)
	}
	return result.options[result.selected], nil
}

type listPickerModel struct {
	label    string
	options  []string
	matches  []int
	cursor   int
	selected int
	canceled bool
	query    string
	width    int
	height   int
}

func newListPickerModel(label string, options []string) listPickerModel {
	model := listPickerModel{
		label:    label,
		options:  append([]string(nil), options...),
		selected: -1,
		width:    72,
		height:   18,
	}
	model.applyFilter()
	return model
}

func (m listPickerModel) Init() tea.Cmd {
	return nil
}

func (m listPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m listPickerModel) View() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("Select " + m.label)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	b.WriteString(title)
	b.WriteString("\n\n")
	if m.query == "" {
		b.WriteString(muted.Render("Filter: type to search"))
	} else {
		b.WriteString("Filter: ")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.query))
	}
	b.WriteString("\n\n")

	if len(m.matches) == 0 {
		b.WriteString(muted.Render(fmt.Sprintf("No matching %s", m.label)))
		b.WriteString("\n\n")
		b.WriteString(muted.Render("Backspace edits the filter; Esc cancels."))
		return b.String()
	}

	start, end := m.visibleRange()
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Bold(true)
	for visibleIndex := start; visibleIndex < end; visibleIndex++ {
		option := m.options[m.matches[visibleIndex]]
		prefix := "  "
		if visibleIndex == m.cursor {
			prefix = "> "
		}
		row := prefix + option
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
	b.WriteString(muted.Render("Type to filter; up/down or j/k move; Enter selects; Esc cancels."))
	return b.String()
}

func (m listPickerModel) visibleRange() (int, int) {
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

func (m *listPickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.matches = m.matches[:0]
	for i, option := range m.options {
		if query == "" || strings.Contains(strings.ToLower(option), query) {
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

func (m *listPickerModel) deleteLastRune() {
	if m.query == "" {
		return
	}
	runes := []rune(m.query)
	m.query = string(runes[:len(runes)-1])
	m.applyFilter()
}
