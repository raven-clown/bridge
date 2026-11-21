// Package style holds the colors and text styles every view shares.
package style

import "github.com/charmbracelet/lipgloss"

var (
	Accent  = lipgloss.AdaptiveColor{Light: "#5b21b6", Dark: "#a78bfa"}
	Muted   = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	Danger  = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	Good    = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	Warning = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#facc15"}

	Title     = lipgloss.NewStyle().Bold(true)
	Dim       = lipgloss.NewStyle().Foreground(Muted)
	Error     = lipgloss.NewStyle().Foreground(Danger)
	Banner    = lipgloss.NewStyle().Foreground(Warning)
	Section   = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	Cursor    = lipgloss.NewStyle().Reverse(true)
	Selected  = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	StatusBar = lipgloss.NewStyle().Foreground(Muted)
	Panel     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(0, 1)
)
