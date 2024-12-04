package main

import (
	"fmt"
	"time"

	"github.com/rivo/tview"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

func main() {
	app := tview.NewApplication()

	table := tview.NewTable().
		SetBorders(false)

	updateTable := func() {
		cpuUsage, _ := cpu.Percent(0, true)
		memStat, _ := mem.VirtualMemory()

		table.Clear()
		table.SetCell(0, 0, tview.NewTableCell("CPU Usage").
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
		for i, usage := range cpuUsage {
			table.SetCell(i+1, 0, tview.NewTableCell(fmt.Sprintf("Core %d: %.2f%%", i+1, usage)).
				SetAlign(tview.AlignLeft))
		}
		table.SetCell(len(cpuUsage)+1, 0, tview.NewTableCell(fmt.Sprintf("Memory: %.2f%% used", memStat.UsedPercent)).
			SetAlign(tview.AlignLeft))
	}

	updateTable()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			app.QueueUpdateDraw(updateTable)
		}
	}()

	if err := app.SetRoot(table, true).Run(); err != nil {
		panic(err)
	}
}
