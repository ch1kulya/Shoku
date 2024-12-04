package main

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

// Disk represents a single disk's usage information.
type Disk struct {
	Mountpoint string
	Used       float64
	Total      float64
	Progress   progress.Model
}

// model holds the state of the application.
type model struct {
	cpuUsage    float64
	memUsed     float64
	memTotal    float64
	disks       []Disk
	sysInfo     string
	width       int
	height      int
	err         error
	cpuProgress progress.Model
	memProgress progress.Model
}

// Messages for updating CPU usage and ticking.
type cpuUsageMsg float64
type tickMsg time.Time

func main() {
	p := tea.NewProgram(initialModel())
	if err := p.Start(); err != nil {
		fmt.Println("Error starting program:", err)
		os.Exit(1)
	}
}

// initialModel initializes the application state.
func initialModel() model {
	// Initialize progress models with custom styles.
	cpuP := progress.New(progress.WithDefaultGradient(), progress.WithScaledGradient("#60bfff", "#bfe5ff"))
	memP := progress.New(progress.WithDefaultGradient(), progress.WithScaledGradient("#60bfff", "#bfe5ff"))

	// Initialize disk progress models for all mounted partitions.
	partitions, err := disk.Partitions(false)
	var disks []Disk
	if err == nil {
		for _, p := range partitions {
			usage, err := disk.Usage(p.Mountpoint)
			if err == nil {
				diskProgress := progress.New(progress.WithDefaultGradient(), progress.WithScaledGradient("#60bfff", "#bfe5ff"))
				disks = append(disks, Disk{
					Mountpoint: p.Mountpoint,
					Used:       float64(usage.Used) / (1024 * 1024 * 1024), // Convert bytes to GB
					Total:      float64(usage.Total) / (1024 * 1024 * 1024),
					Progress:   diskProgress,
				})
			}
		}
	}

	return model{
		cpuProgress: cpuP,
		memProgress: memP,
		disks:       disks,
	}
}

// Init initializes the program's initial commands.
func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		tea.EnterAltScreen,
		m.cpuProgress.Init(),
		m.memProgress.Init(),
		startCPUUsageMonitor(), // Start monitoring CPU usage
	}

	// Initialize each disk's progress model.
	for i := range m.disks {
		cmds = append(cmds, m.disks[i].Progress.Init())
	}

	return tea.Batch(cmds...)
}

// Command to periodically fetch CPU usage.
func startCPUUsageMonitor() tea.Cmd {
	return func() tea.Msg {
		percent, err := cpu.Percent(1*time.Second, false)
		if err == nil && len(percent) > 0 {
			return cpuUsageMsg(percent[0])
		}
		return cpuUsageMsg(0)
	}
}

// tickCmd returns a command that sends a tickMsg after one second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles incoming messages and updates the model accordingly.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case cpuUsageMsg:
		m.cpuUsage = float64(msg)
		cmds = append(cmds, m.cpuProgress.SetPercent(m.cpuUsage/100))
		// Continue monitoring CPU usage
		cmds = append(cmds, startCPUUsageMonitor())

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	case tickMsg:
		// Update system information
		var err error
		m.memUsed, m.memTotal, err = getMemUsage()
		if err != nil {
			return m, tea.Quit
		}
		m.sysInfo, err = getSysInfo()
		if err != nil {
			return m, tea.Quit
		}

		// Update disk usages
		partitions, err := disk.Partitions(false)
		if err != nil {
			m.err = err
			return m, tea.Quit
		}

		// Update existing disks or add new ones
		for _, p := range partitions {
			usage, err := disk.Usage(p.Mountpoint)
			if err != nil {
				continue
			}
			usedGB := float64(usage.Used) / (1024 * 1024 * 1024)
			totalGB := float64(usage.Total) / (1024 * 1024 * 1024)

			found := false
			for i, d := range m.disks {
				if d.Mountpoint == p.Mountpoint {
					m.disks[i].Used = usedGB
					m.disks[i].Total = totalGB
					percent := usedGB / totalGB
					cmds = append(cmds, m.disks[i].Progress.SetPercent(percent))
					found = true
					break
				}
			}
			if !found {
				// New disk found, add to the list
				diskProgress := progress.New(progress.WithDefaultGradient(), progress.WithScaledGradient("#60bfff", "#bfe5ff"))
				newDisk := Disk{
					Mountpoint: p.Mountpoint,
					Used:       usedGB,
					Total:      totalGB,
					Progress:   diskProgress,
				}
				m.disks = append(m.disks, newDisk)
				cmds = append(cmds, newDisk.Progress.Init())
				cmds = append(cmds, newDisk.Progress.SetPercent(usedGB/totalGB))
			}
		}

		// Update Memory progress
		cmds = append(cmds, m.memProgress.SetPercent(m.memUsed/m.memTotal))

		// Schedule the next tick
		cmds = append(cmds, tickCmd())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	// Update CPU Progress
	updatedProgress, cmd := m.cpuProgress.Update(msg)
	if cpuP, ok := updatedProgress.(progress.Model); ok {
		m.cpuProgress = cpuP
		cmds = append(cmds, cmd)
	} else {
		m.err = fmt.Errorf("failed to cast cpuProgress to progress.Model")
		return m, tea.Quit
	}

	// Update Memory Progress
	updatedProgress, cmd = m.memProgress.Update(msg)
	if memP, ok := updatedProgress.(progress.Model); ok {
		m.memProgress = memP
		cmds = append(cmds, cmd)
	} else {
		m.err = fmt.Errorf("failed to cast memProgress to progress.Model")
		return m, tea.Quit
	}

	// Update each Disk's Progress
	for i := range m.disks {
		updatedProgress, cmd = m.disks[i].Progress.Update(msg)
		if dp, ok := updatedProgress.(progress.Model); ok {
			m.disks[i].Progress = dp
			cmds = append(cmds, cmd)
		} else {
			m.err = fmt.Errorf("failed to cast diskProgress to progress.Model for mountpoint %s", m.disks[i].Mountpoint)
			return m, tea.Quit
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the UI.
func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("An error occurred: %v\nPress q to quit.", m.err)
	}

	// Calculate content width, accounting for padding and borders
	contentWidth := m.width - boxStyle.GetHorizontalFrameSize()

	// Title
	title := titleStyle.Width(contentWidth).Render("System Monitor")

	// System Info Box
	infoBox := boxStyle.Width(contentWidth).Render(infoStyle.Render(m.sysInfo))

	// Calculate half width for side-by-side boxes
	halfWidth := (contentWidth - lipgloss.Width("│")*2) / 2

	// CPU Usage Box
	cpuBox := boxStyle.Width(halfWidth).Render(
		fmt.Sprintf("CPU Usage: %.2f%%\n%s", m.cpuUsage, m.cpuProgress.View()),
	)

	// Memory Usage Box
	memBox := boxStyle.Width(halfWidth).Render(
		fmt.Sprintf("Memory: %.2f GB / %.2f GB\n%s", m.memUsed, m.memTotal, m.memProgress.View()),
	)

	// CPU and Memory side by side
	cpuMemRow := lipgloss.JoinHorizontal(lipgloss.Top, cpuBox, memBox)

	// Disks Usage Boxes
	var leftDiskBoxes []string
	var rightDiskBoxes []string
	for i, disk := range m.disks {
		diskBox := boxStyle.Width(halfWidth).Render(
			fmt.Sprintf("Disk (%s): %.2f GB / %.2f GB\n%s", disk.Mountpoint, disk.Used, disk.Total, disk.Progress.View()),
		)
		if i%2 == 0 {
			leftDiskBoxes = append(leftDiskBoxes, diskBox)
		} else {
			rightDiskBoxes = append(rightDiskBoxes, diskBox)
		}
	}

	// Create left and right disk columns
	leftDiskColumn := lipgloss.JoinVertical(lipgloss.Top, leftDiskBoxes...)
	rightDiskColumn := lipgloss.JoinVertical(lipgloss.Top, rightDiskBoxes...)

	// Disks side by side
	disksView := lipgloss.JoinHorizontal(lipgloss.Top, leftDiskColumn, rightDiskColumn)

	// Combine all sections
	mainContent := lipgloss.JoinVertical(lipgloss.Top, title, infoBox, cpuMemRow, disksView)

	// Render the content
	return mainContent
}

// getMemUsage retrieves the used and total memory in GB.
func getMemUsage() (usedGB float64, totalGB float64, err error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, err
	}
	usedGB = float64(memStat.Used) / (1024 * 1024 * 1024)
	totalGB = float64(memStat.Total) / (1024 * 1024 * 1024)
	return usedGB, totalGB, nil
}

// getSysInfo retrieves system information such as hostname, OS details, and uptime.
func getSysInfo() (string, error) {
	info, err := host.Info()
	if err != nil {
		return "", err
	}
	uptime := time.Duration(info.Uptime) * time.Second
	uptimeStr := fmt.Sprintf("%d days %d hrs %d min %d s",
		int(uptime.Hours()/24),
		int(uptime.Hours())%24,
		int(uptime.Minutes())%60,
		int(uptime.Seconds())%60)
	return fmt.Sprintf("Hostname: %s\nOS: %s \nUptime: %s",
		info.Hostname,
		info.Platform,
		uptimeStr), nil
}

var (
	// Define styles using lipgloss
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#007ACC")).
			Padding(0, 1).
			Margin(0, 1).
			Align(lipgloss.Center)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Align(lipgloss.Left)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 1).
			BorderForeground(lipgloss.Color("#007ACC"))
)
