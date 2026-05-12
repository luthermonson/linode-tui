package theme

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Name      string
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	// Accent is a third headline color used in places where Primary and
	// Secondary alone would clash (e.g. multi-section help headers on light
	// themes where Primary is dim-blue and Secondary is purple). It should
	// read distinctly against both Primary and Secondary on the chosen Bg.
	Accent lipgloss.Color
	Text   lipgloss.Color
	Muted  lipgloss.Color
	Bg     lipgloss.Color
	Border lipgloss.Color
	Error  lipgloss.Color
	Warn   lipgloss.Color
	Ok     lipgloss.Color
}

func Dark() Theme {
	return Theme{
		Name:      "dark",
		Primary:   "#00ADD8",
		Secondary: "#7B61FF",
		Accent:    "#F49E4C",
		Text:      "#E5E5E5",
		Muted:     "#6E6E6E",
		Bg:        "#0E0E10",
		Border:    "#2C2C2E",
		Error:     "#FF5C5C",
		Warn:      "#FFB454",
		Ok:        "#5CDB95",
	}
}

func Light() Theme {
	return Theme{
		Name:      "light",
		Primary:   "#006B82",
		Secondary: "#5235B8",
		Accent:    "#B8500B",
		Text:      "#1A1A1A",
		Muted:     "#7A7A7A",
		Bg:        "#FAFAFA",
		Border:    "#D4D4D4",
		Error:     "#B00020",
		Warn:      "#A86400",
		Ok:        "#2E7D32",
	}
}

func Dracula() Theme {
	return Theme{
		Name:      "dracula",
		Primary:   "#BD93F9",
		Secondary: "#FF79C6",
		Accent:    "#8BE9FD",
		Text:      "#F8F8F2",
		Muted:     "#6272A4",
		Bg:        "#282A36",
		Border:    "#44475A",
		Error:     "#FF5555",
		Warn:      "#FFB86C",
		Ok:        "#50FA7B",
	}
}

func SolarizedLight() Theme {
	return Theme{
		Name:      "solarized-light",
		Primary:   "#268BD2",
		Secondary: "#6C71C4",
		Accent:    "#CB4B16",
		Text:      "#073642",
		Muted:     "#93A1A1",
		Bg:        "#FDF6E3",
		Border:    "#EEE8D5",
		Error:     "#DC322F",
		Warn:      "#B58900",
		Ok:        "#859900",
	}
}

func ByName(name string) (Theme, bool) {
	switch name {
	case "dark":
		return Dark(), true
	case "light":
		return Light(), true
	case "dracula":
		return Dracula(), true
	case "solarized-light":
		return SolarizedLight(), true
	default:
		return Theme{}, false
	}
}

func Names() []string {
	return []string{"dark", "light", "dracula", "solarized-light"}
}
